package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

// These are isolated repository tests (D), not real Runtime/E2E evidence.
func TestCancelIdleTaskClosesMailboxAndAllowsNextTask(t *testing.T) {
	for _, waiting := range []bool{false, true} {
		for _, claimed := range []bool{false, true} {
			t.Run(fmt.Sprintf("waiting=%t/claimed=%t", waiting, claimed), func(t *testing.T) {
				ctx := context.Background()
				repository, _ := openTestRepository(t, nil)
				fixture := seedRepository(t, repository)
				descriptor := messageDescriptor(openruntime.SteerQueued)
				worker, guard := registerMessageWorker(t, repository, fixture, descriptor)
				var created *CreateTaskResult
				if waiting {
					var run *domain.RunAttempt
					created, run = beginMessageRun(t, repository, fixture, worker, descriptor, "idle")
					if err := repository.FinishRun(ctx, guard, run.ID, 2, run.Version,
						openruntime.TurnResult{Status: openruntime.TurnResultWaitingInput, Result: "need input", SideEffectsKnown: true}, nil, 0, nil,
						journalEvent("event-idle-wait", "task.waiting_input", fixture.ownerPrincipal, fixture.organizationID),
						journalEvent("event-idle-finished", "run_attempt.finished", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
						t.Fatal(err)
					}
				} else {
					created = createTask(t, repository, fixture, "idle")
				}
				if claimed {
					item, err := repository.TryClaimMailbox(ctx, guard, 1, guard.CheckedAt.Add(time.Hour), journalEvent("event-idle-claim", "mailbox.claimed", fixture.ownerPrincipal, fixture.organizationID))
					if err != nil || item == nil || item.TaskID != created.Task.ID {
						t.Fatalf("claim=%+v err=%v", item, err)
					}
				}
				before, err := repository.GetTask(ctx, created.Task.ID)
				if err != nil {
					t.Fatal(err)
				}
				message, err := createRoutedMessage(t, repository, fixture, before.ID, before.Version, "idle-followup")
				if err != nil {
					t.Fatal(err)
				}
				before, err = repository.GetTask(ctx, before.ID)
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err := repository.RequestTaskCancel(ctx, before.ID, before.Version-1, fixture.ownerPrincipal, nil, nil, nil); !errors.Is(err, domain.ErrStaleVersion) {
					t.Fatalf("stale cancel error=%v", err)
				}
				canceled, control, err := repository.RequestTaskCancel(ctx, before.ID, before.Version, fixture.ownerPrincipal,
					&domain.MailboxItem{ID: "idle-cancel-control"}, journalEvent("event-idle-cancel", "task.cancel_requested", fixture.ownerPrincipal, fixture.organizationID), nil)
				if err != nil || canceled.Status != domain.TaskStatusCanceled || control != nil || canceled.Version != before.Version+1 {
					t.Fatalf("idle cancel task=%+v control=%+v err=%v", canceled, control, err)
				}
				if canceled.CancelRequestedAt == nil || canceled.CancelRequestedBy == nil || *canceled.CancelRequestedBy != fixture.ownerPrincipal {
					t.Fatalf("lost cancellation intent: %+v", canceled)
				}
				for _, id := range []string{created.MailboxItem.ID, message.MailboxItem.ID} {
					item, err := repository.GetMailboxItem(ctx, id)
					if err != nil || item.State != domain.MailboxStateSuperseded || item.WorkerInstanceID != "" || item.FencingToken != 0 || item.LeaseUntil != nil {
						t.Fatalf("cancel left executable mailbox=%+v err=%v", item, err)
					}
					assertCancelEventCount(t, repository, id, "mailbox.superseded", 1)
				}
				assertCancelEventCount(t, repository, before.ID, "task.cancel_requested", 1)
				assertCancelEventCount(t, repository, before.ID, "task.canceled", 1)
				backends, err := repository.ListWorkerBackends(ctx, worker.ID)
				if err != nil || len(backends) != 1 {
					t.Fatalf("backends=%+v err=%v", backends, err)
				}
				if claimed {
					resolved, err := json.Marshal(domain.ResolvedExecutionSpec{Version: 1, Spec: domain.ExecutionSpec{Network: backends[0].Network}})
					if err != nil {
						t.Fatal(err)
					}
					lateRun := &domain.RunAttempt{ID: "run-late-canceled", TaskID: before.ID, AgentID: fixture.agentID, Version: 1,
						Status: domain.RunAttemptStarting, WorkerInstanceID: worker.ID, FencingToken: worker.FencingToken,
						LeaseUntil: guard.CheckedAt.Add(time.Hour), ExecutionSpecVersion: 1, RequestedExecutionJSON: `{}`, ResolvedExecutionJSON: string(resolved),
						AdapterID: descriptor.AdapterID, BackendID: "local", Model: descriptor.Models[0], ReasoningMode: domain.ReasoningBackendDefault}
					if _, _, err := repository.BeginClaimedRunAttempt(ctx, guard, created.MailboxItem.ID, before.Version, lateRun, "", nil,
						journalEvent("event-late-task", "task.running", fixture.ownerPrincipal, fixture.organizationID),
						journalEvent("event-late-run", "run_attempt.started", fixture.ownerPrincipal, fixture.organizationID),
						journalEvent("event-late-mailbox", "mailbox.accepted", fixture.ownerPrincipal, fixture.organizationID)); err == nil || err.Error() != domain.ErrConflict("work item is not claimed by Worker").Error() {
						t.Fatalf("canceled claim must reject begin at mailbox ownership check: %v", err)
					}
					if _, err := repository.GetRunAttempt(ctx, lateRun.ID); !errors.Is(err, domain.ErrNotFound) {
						t.Fatalf("late RunAttempt persisted: %v", err)
					}
				}
				if waiting {
					prior, err := repository.GetRunAttempt(ctx, "run-idle")
					if err != nil || prior.Status != domain.RunAttemptSucceeded || prior.ResultJSON == "" {
						t.Fatalf("cancellation overwrote prior turn: %+v err=%v", prior, err)
					}
				}
				replayed, item, err := repository.RequestTaskCancel(ctx, before.ID, before.Version, fixture.ownerPrincipal, nil, nil, nil)
				if err != nil || item != nil || replayed.Version != canceled.Version || replayed.Status != domain.TaskStatusCanceled {
					t.Fatalf("replayed cancel task=%+v item=%+v err=%v", replayed, item, err)
				}
				assertCancelEventCount(t, repository, before.ID, "task.canceled", 1)
				next := createTask(t, repository, fixture, "after-idle-cancel")
				nextItem, err := repository.TryClaimMailbox(ctx, guard, 1, guard.CheckedAt.Add(time.Hour), journalEvent("event-next-claim", "mailbox.claimed", fixture.ownerPrincipal, fixture.organizationID))
				if err != nil || nextItem == nil || nextItem.TaskID != next.Task.ID {
					t.Fatalf("next claim=%+v err=%v", nextItem, err)
				}
				beginClaimedNetworkRun(t, repository, fixture, worker, guard, descriptor, next, nextItem, "after-idle-cancel", backends[0].Network)
			})
		}
	}
}

func assertCancelEventCount(t *testing.T, repository *Repository, id, event string, want int) {
	t.Helper()
	var count int
	if err := repository.db.QueryRow(`SELECT COUNT(*) FROM event_journal WHERE aggregate_id=? AND event_type=?`, id, event).Scan(&count); err != nil || count != want {
		t.Fatalf("event %s/%s count=%d want=%d err=%v", id, event, count, want, err)
	}
}

func TestCancelIdleRollbackPreservesTaskMailboxAndJournal(t *testing.T) {
	var fail atomic.Bool
	injected := errors.New("idle cancel commit fault")
	repository, _ := openTestRepository(t, func(point FaultPoint) error {
		if fail.Load() && point == FaultBeforeCommit {
			return injected
		}
		return nil
	})
	ctx := context.Background()
	fixture := seedRepository(t, repository)
	created := createTask(t, repository, fixture, "idle-rollback")
	events, err := repository.ListJournal(ctx, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	_, _, err = repository.RequestTaskCancel(ctx, created.Task.ID, created.Task.Version, fixture.ownerPrincipal, nil,
		journalEvent("event-idle-rollback-cancel", "task.cancel_requested", fixture.ownerPrincipal, fixture.organizationID), nil)
	if !errors.Is(err, injected) {
		t.Fatalf("expected commit fault, got %v", err)
	}
	fail.Store(false)
	task, err := repository.GetTask(ctx, created.Task.ID)
	if err != nil || task.Status != domain.TaskStatusQueued || task.Version != created.Task.Version || task.CancelRequestedBy != nil {
		t.Fatalf("rollback task=%+v err=%v", task, err)
	}
	item, err := repository.GetMailboxItem(ctx, created.MailboxItem.ID)
	if err != nil || item.State != domain.MailboxStatePending {
		t.Fatalf("rollback mailbox=%+v err=%v", item, err)
	}
	after, err := repository.ListJournal(ctx, 0, 1000)
	if err != nil || len(after) != len(events) {
		t.Fatalf("rollback journal count=%d want=%d err=%v", len(after), len(events), err)
	}
}
