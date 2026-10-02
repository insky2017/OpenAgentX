package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

func sessionBindingFixture(t *testing.T, fault func(FaultPoint) error) (*Repository, domain.WorkerWriteGuard, *domain.RunAttempt) {
	t.Helper()
	r, _ := openTestRepository(t, fault)
	f := seedRepository(t, r)
	d := messageDescriptor(openruntime.SteerNative)
	d.AdapterID = "codex-app-server"
	d.RuntimeIdentity.AdapterID = d.AdapterID
	w, guard := registerMessageWorker(t, r, f, d)
	created := createTask(t, r, f, "early-session")
	resolved, _ := json.Marshal(domain.ResolvedExecutionSpec{Version: 1, Spec: domain.ExecutionSpec{Session: domain.SessionSpec{Mode: domain.SessionModeNew, ContextID: created.Task.ID}}})
	run := &domain.RunAttempt{ID: "run-early-session", TaskID: created.Task.ID, AgentID: f.agentID, Version: 1,
		Status: domain.RunAttemptRunning, WorkerInstanceID: w.ID, FencingToken: w.FencingToken, LeaseUntil: repositoryTestTime.Add(time.Hour),
		ExecutionSpecVersion: 1, RequestedExecutionJSON: `{}`, ResolvedExecutionJSON: string(resolved), AdapterID: d.AdapterID, BackendID: "local", Model: d.Models[0], ReasoningMode: domain.ReasoningBackendDefault}
	if _, err := r.BeginRunAttempt(context.Background(), created.Task.Version, run,
		journalEvent("event-early-task", "task.running", f.ownerPrincipal, f.organizationID),
		journalEvent("event-early-run", "run_attempt.started", f.ownerPrincipal, f.organizationID)); err != nil {
		t.Fatal(err)
	}
	return r, guard, run
}

func sessionEvent(id, provider string, guard domain.WorkerWriteGuard) *domain.JournalEvent {
	raw, _ := json.Marshal(map[string]any{"runtime_event_type": "session.bound", "payload": map[string]string{"provider_session_id": provider, "source": "new"}})
	return &domain.JournalEvent{ID: id, EventType: "runtime.session.bound", ActorPrincipalID: guard.PrincipalID, Payload: raw}
}

func TestRuntimeSessionBindingEarlyIdempotentPrivateAndImmutable(t *testing.T) {
	r, guard, run := sessionBindingFixture(t, nil)
	ctx := context.Background()
	for _, id := range []string{"bind-first", "bind-retry"} {
		if err := r.AppendRunEvents(ctx, guard, run.ID, run.Version, []*domain.JournalEvent{sessionEvent(id, "private-native-id", guard)}); err != nil {
			t.Fatal(err)
		}
	}
	binding, err := r.GetSessionBinding(ctx, run.TaskID, run.AgentID, run.BackendID)
	if err != nil || binding.ProviderSessionID != "private-native-id" || binding.Version != 1 {
		t.Fatal("early binding not persisted exactly once")
	}
	var count int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM event_journal WHERE event_type='runtime.session.bound'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("binding journal count=%d err=%v", count, err)
	}
	journal, err := r.ListTaskJournal(ctx, run.TaskID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range journal {
		if strings.Contains(string(event.Payload), "private-native-id") || strings.Contains(string(event.Payload), "provider_session_id") {
			t.Fatal("private provider identity entered the public Journal")
		}
	}
	if err := r.AppendRunEvents(ctx, guard, run.ID, run.Version, []*domain.JournalEvent{sessionEvent("bind-conflict", "different-native-id", guard)}); err == nil {
		t.Fatal("different provider session replaced early binding")
	}
	binding.ProviderSessionID = "different-native-id"
	err = r.FinishRun(ctx, guard, run.ID, 2, run.Version, openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "done", SideEffectsKnown: true}, binding, binding.Version,
		journalEvent("finish-bind", "session_binding.saved", guard.PrincipalID, ""),
		journalEvent("finish-task", "task.settled", guard.PrincipalID, ""),
		journalEvent("finish-run", "run_attempt.finished", guard.PrincipalID, ""))
	if err == nil {
		t.Fatal("FinishRun replaced a Codex provider session")
	}
	storedRun, err := r.GetRunAttempt(ctx, run.ID)
	if err != nil || storedRun.Version != 1 || !storedRun.Status.Active() {
		t.Fatal("rejected finish partially settled Run")
	}
}

func TestRuntimeSessionBindingAndJournalRollbackTogether(t *testing.T) {
	var inject atomic.Bool
	r, guard, run := sessionBindingFixture(t, func(point FaultPoint) error {
		if inject.Load() && point == FaultBeforeCommit {
			return errors.New("early binding rollback fixture")
		}
		return nil
	})
	ctx := context.Background()
	inject.Store(true)
	err := r.AppendRunEvents(ctx, guard, run.ID, run.Version, []*domain.JournalEvent{sessionEvent("bind-rollback", "private-native-id", guard)})
	if err == nil || !strings.Contains(err.Error(), "rollback fixture") {
		t.Fatalf("expected injected rollback: %v", err)
	}
	inject.Store(false)
	if _, err := r.GetSessionBinding(ctx, run.TaskID, run.AgentID, run.BackendID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("binding survived journal transaction rollback")
	}
	var count int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM event_journal WHERE event_id='bind-rollback'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("Journal survived transaction rollback")
	}
	// A Journal insertion failure must also roll back the preceding binding INSERT.
	err = r.AppendRunEvents(ctx, guard, run.ID, run.Version, []*domain.JournalEvent{sessionEvent("event-early-run", "private-native-id", guard)})
	if err == nil {
		t.Fatal("duplicate Journal primary key did not reject transaction")
	}
	if _, err := r.GetSessionBinding(ctx, run.TaskID, run.AgentID, run.BackendID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("binding survived failed Journal insertion")
	}
	if err := r.AppendRunEvents(ctx, guard, run.ID, run.Version, []*domain.JournalEvent{sessionEvent("bind-after-rollback", "private-native-id", guard)}); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeSessionBindingGuardVersionAndLeaseBeforeMutation(t *testing.T) {
	for _, scenario := range []string{"token", "fencing", "version", "lease", "unknown-field", "wrong-event-type"} {
		t.Run(scenario, func(t *testing.T) {
			r, guard, run := sessionBindingFixture(t, nil)
			version := run.Version
			event := sessionEvent("bind-rejected", "private-native-id", guard)
			switch scenario {
			case "token":
				guard.SessionTokenDigest = "wrong"
			case "fencing":
				guard.FencingToken++
			case "version":
				version++
			case "lease":
				if _, err := r.db.Exec(`UPDATE run_attempts SET lease_until=? WHERE run_id=?`, formatTime(guard.CheckedAt), run.ID); err != nil {
					t.Fatal(err)
				}
			case "unknown-field":
				event.Payload = json.RawMessage(`{"runtime_event_type":"session.bound","payload":{"provider_session_id":"private-native-id","task_id":"forged"}}`)
			case "wrong-event-type":
				event.EventType = "runtime.event"
			}
			if err := r.AppendRunEvents(context.Background(), guard, run.ID, version, []*domain.JournalEvent{event}); err == nil {
				t.Fatal("invalid early binding was accepted")
			}
			if _, err := r.GetSessionBinding(context.Background(), run.TaskID, run.AgentID, run.BackendID); !errors.Is(err, domain.ErrNotFound) {
				t.Fatal("invalid request mutated binding")
			}
		})
	}
}

func TestRuntimeSessionBindingNotDroppedAtPublicOutputLimit(t *testing.T) {
	r, guard, run := sessionBindingFixture(t, nil)
	events := make([]*domain.JournalEvent, openruntime.MaxPublicOutputEvents)
	for i := range events {
		events[i] = journalEvent("output-"+time.Duration(i).String(), "runtime.turn.output", guard.PrincipalID, "")
	}
	if err := r.AppendRunEvents(context.Background(), guard, run.ID, run.Version, events); err != nil {
		t.Fatal(err)
	}
	if err := r.AppendRunEvents(context.Background(), guard, run.ID, run.Version, []*domain.JournalEvent{sessionEvent("bind-at-limit", "private-native-id", guard)}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetSessionBinding(context.Background(), run.TaskID, run.AgentID, run.BackendID); err != nil {
		t.Fatal(err)
	}
}
