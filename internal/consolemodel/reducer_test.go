package consolemodel

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	"openagentx/internal/domain"
)

func workerEvent(sequence, generation int64, workerID string, status domain.WorkerStatus, heartbeat, lease time.Time) openapi.JournalEventReadModel {
	return openapi.JournalEventReadModel{
		Sequence: sequence, ID: "event-worker", AggregateType: "worker_instance",
		AggregateID: workerID, EventType: "worker.heartbeat",
		Worker: &openapi.WorkerReadModel{WorkerInstanceID: workerID, AgentID: "quote",
			Generation: generation, Status: status, LastHeartbeatAt: heartbeat, LeaseUntil: lease},
	}
}

func TestReducerRejectsGenerationRollbackAndAdvancesIgnoredCursor(t *testing.T) {
	now := time.Date(2026, 9, 14, 16, 0, 0, 0, time.UTC)
	reducer, err := New(consoleapi.AttachResponse{AgentID: "quote", WorkerInstanceID: "worker-48",
		Generation: 48, WorkerStatus: domain.WorkerStatusOnline, LastHeartbeatAt: now,
		LeaseUntil: now.Add(time.Minute), SnapshotSequence: 1204})
	if err != nil {
		t.Fatal(err)
	}
	result, err := reducer.Apply(workerEvent(1205, 42, "worker-42", domain.WorkerStatusOffline, now, now))
	if err != nil {
		t.Fatal(err)
	}
	state := reducer.Snapshot()
	if !result.CursorAdvanced || result.Timeline != nil || reducer.Cursor() != 1205 ||
		state.Generation != 48 || state.WorkerInstanceID != "worker-48" || state.WorkerStatus != domain.WorkerStatusOnline {
		t.Fatalf("old generation changed current state: result=%+v cursor=%d state=%+v", result, reducer.Cursor(), state)
	}
}

func TestReducerHandlesDuplicateBackwardAndSameGenerationConflict(t *testing.T) {
	now := time.Now().UTC()
	reducer, err := New(consoleapi.AttachResponse{AgentID: "quote", WorkerInstanceID: "worker-current",
		Generation: 8, WorkerStatus: domain.WorkerStatusOnline, LeaseUntil: now.Add(time.Minute), SnapshotSequence: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := reducer.Apply(workerEvent(10, 8, "worker-current", domain.WorkerStatusOnline, now, now.Add(time.Minute))); err != nil || result.CursorAdvanced {
		t.Fatalf("duplicate result=%+v err=%v", result, err)
	}
	if _, err := reducer.Apply(workerEvent(9, 8, "worker-current", domain.WorkerStatusOnline, now, now.Add(time.Minute))); err == nil {
		t.Fatal("backward event was accepted")
	}
	result, err := reducer.Apply(workerEvent(11, 8, "worker-conflict", domain.WorkerStatusDraining, now, now.Add(time.Minute)))
	if err != nil || !result.CursorAdvanced || result.Timeline != nil {
		t.Fatalf("same-generation conflict result=%+v err=%v", result, err)
	}
	if state := reducer.Snapshot(); state.WorkerInstanceID != "worker-current" || state.WorkerStatus != domain.WorkerStatusOnline {
		t.Fatalf("same-generation conflict changed state: %+v", state)
	}
}

func TestReducerCoalescesHeartbeatBurstAndReportsMeaningfulTransitions(t *testing.T) {
	now := time.Now().UTC()
	reducer, err := New(consoleapi.AttachResponse{AgentID: "quote", WorkerInstanceID: "worker-2",
		Generation: 2, WorkerStatus: domain.WorkerStatusOnline, LeaseUntil: now.Add(time.Minute), SnapshotSequence: 20})
	if err != nil {
		t.Fatal(err)
	}
	for sequence := int64(21); sequence <= 40; sequence++ {
		result, applyErr := reducer.Apply(workerEvent(sequence, 2, "worker-2", domain.WorkerStatusOnline,
			now.Add(time.Duration(sequence)*time.Second), now.Add(2*time.Minute)))
		if applyErr != nil || result.Timeline != nil {
			t.Fatalf("heartbeat %d produced Timeline=%+v err=%v", sequence, result.Timeline, applyErr)
		}
	}
	transition, err := reducer.Apply(workerEvent(41, 2, "worker-2", domain.WorkerStatusDraining, now.Add(time.Minute), now.Add(3*time.Minute)))
	if err != nil || transition.Timeline == nil {
		t.Fatalf("draining transition=%+v err=%v", transition, err)
	}
	replacement, err := reducer.Apply(workerEvent(42, 3, "worker-3", domain.WorkerStatusOnline, now.Add(2*time.Minute), now.Add(4*time.Minute)))
	if err != nil || replacement.Timeline == nil {
		t.Fatalf("replacement=%+v err=%v", replacement, err)
	}
	state := reducer.Snapshot()
	if state.Generation != 3 || state.WorkerInstanceID != "worker-3" || state.WorkerStatus != domain.WorkerStatusOnline {
		t.Fatalf("replacement state=%+v", state)
	}
}

func TestReducerSwitchesAndClearsActiveRunFromSafeProjection(t *testing.T) {
	now := time.Now().UTC()
	reducer, err := New(consoleapi.AttachResponse{AgentID: "quote", WorkerInstanceID: "worker-1",
		Generation: 1, WorkerStatus: domain.WorkerStatusOnline, SnapshotSequence: 50})
	if err != nil {
		t.Fatal(err)
	}
	for sequence, run := range []openapi.RunAttemptReadModel{
		{ID: "run-1", TaskID: "task-1", AgentID: "quote", Status: domain.RunAttemptRunning, StartedAt: now, UpdatedAt: now},
		{ID: "run-2", TaskID: "task-2", AgentID: "quote", Status: domain.RunAttemptWaitingApproval, StartedAt: now, UpdatedAt: now.Add(time.Second)},
	} {
		result, applyErr := reducer.Apply(openapi.JournalEventReadModel{Sequence: int64(51 + sequence), ID: run.ID,
			AggregateType: "run_attempt", AggregateID: run.ID, EventType: "run_attempt.updated", Run: &run})
		if applyErr != nil || result.Timeline == nil {
			t.Fatalf("active run event result=%+v err=%v", result, applyErr)
		}
	}
	if active := reducer.Snapshot().ActiveRun; active == nil || active.RunID != "run-2" || active.Status != domain.RunAttemptWaitingApproval {
		t.Fatalf("active run did not switch: %+v", active)
	}
	finished := openapi.RunAttemptReadModel{ID: "run-2", TaskID: "task-2", AgentID: "quote", Status: domain.RunAttemptSucceeded, UpdatedAt: now.Add(2 * time.Second)}
	if _, err := reducer.Apply(openapi.JournalEventReadModel{Sequence: 53, ID: "run-2-finished",
		AggregateType: "run_attempt", AggregateID: "run-2", EventType: "run_attempt.finished", Run: &finished}); err != nil {
		t.Fatal(err)
	}
	if active := reducer.Snapshot().ActiveRun; active != nil {
		t.Fatalf("terminal run remained active: %+v", active)
	}
}

func TestReducerTimelineCanOnlyContainSafeOutputProjection(t *testing.T) {
	reducer, err := New(consoleapi.AttachResponse{AgentID: "quote", WorkerStatus: domain.WorkerStatusOffline, SnapshotSequence: 3})
	if err != nil {
		t.Fatal(err)
	}
	result, err := reducer.Apply(openapi.JournalEventReadModel{Sequence: 4, ID: "runtime-safe",
		AggregateType: "runtime", AggregateID: "run-1", EventType: "runtime.output",
		Output: &openapi.SafeOutputReadModel{Text: "token=[REDACTED]", Diagnostic: "stderr=[REDACTED]"}})
	if err != nil || result.Timeline == nil {
		t.Fatalf("safe output result=%+v err=%v", result, err)
	}
	encoded, err := json.Marshal(result.Timeline)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"raw-secret", "raw_stderr", "hidden_reasoning", "environment"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("Timeline leaked %q: %s", forbidden, encoded)
		}
	}
}
