package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

// Deterministic repository evidence only: no physical Runtime is invoked.
func TestQueuedCompleteReplyKeepsUnknownEffectsAndConsumesOnlyNewMessage(t *testing.T) {
	ctx := context.Background()
	fail := false
	injected := errors.New("queued reply commit fault")
	repository, _ := openTestRepository(t, func(at FaultPoint) error {
		if fail && at == FaultBeforeCommit {
			return injected
		}
		return nil
	})
	fixture := seedRepository(t, repository)
	descriptor := messageDescriptor(openruntime.SteerQueued)
	worker, guard := registerMessageWorker(t, repository, fixture, descriptor)
	created := createTask(t, repository, fixture, "queued-complete")
	initial, err := repository.TryClaimMailbox(ctx, guard, 1, guard.CheckedAt.Add(time.Hour), journalEvent("queued-initial-claim", "mailbox.claimed", fixture.ownerPrincipal, fixture.organizationID))
	if err != nil || initial == nil || initial.ID != created.MailboxItem.ID {
		t.Fatalf("initial claim=%+v err=%v", initial, err)
	}
	backends, err := repository.ListWorkerBackends(ctx, worker.ID)
	if err != nil || len(backends) != 1 {
		t.Fatalf("backends count=%d err=%v", len(backends), err)
	}
	first := beginClaimedNetworkRun(t, repository, fixture, worker, guard, descriptor, created, initial, "queued-first", backends[0].Network)
	message, err := createRoutedMessage(t, repository, fixture, created.Task.ID, 2, "queued-new-input")
	if err != nil || message.MailboxItem.Lane != domain.MailboxLaneWork {
		t.Fatalf("queued message=%+v err=%v", message, err)
	}
	before, err := repository.GetTask(ctx, created.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	firstResult := openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, FinalReply: true, Result: "first operation returned; effects unverified", SideEffectsKnown: false}
	finish := func() error {
		return repository.FinishRun(ctx, guard, first.ID, 2, first.Version, firstResult, nil, 0, nil,
			journalEvent("queued-first-task-finish", "task.settled", fixture.ownerPrincipal, fixture.organizationID),
			journalEvent("queued-first-run-finish", "run_attempt.finished", fixture.ownerPrincipal, fixture.organizationID))
	}
	fail = true
	if err := finish(); !errors.Is(err, injected) {
		t.Fatalf("finish rollback error=%v", err)
	}
	fail = false
	rolledBack, err := repository.GetTask(ctx, before.ID)
	if err != nil || !reflect.DeepEqual(rolledBack, before) {
		t.Fatalf("partial Task finish=%+v err=%v", rolledBack, err)
	}
	priorRun, err := repository.GetRunAttempt(ctx, first.ID)
	if err != nil || !priorRun.Status.Active() || priorRun.FinishedAt != nil || priorRun.ResultJSON != "" {
		t.Fatalf("partial Run finish=%+v err=%v", priorRun, err)
	}
	pending, err := repository.GetMailboxItem(ctx, message.MailboxItem.ID)
	if err != nil || pending.State != domain.MailboxStatePending {
		t.Fatalf("partial queued mailbox=%+v err=%v", pending, err)
	}
	assertCancelEventCount(t, repository, first.ID, "run_attempt.finished", 0)
	assertCancelEventCount(t, repository, before.ID, "task.settled", 0)
	if err := finish(); err != nil {
		t.Fatal(err)
	}
	waiting, err := repository.GetTask(ctx, before.ID)
	if err != nil || waiting.Status != domain.TaskStatusWaitingInput || waiting.CompletionBasis != "" || nullableValue(waiting.Error) != "business_effect_unverified" || nullableValue(waiting.Result) != firstResult.Result {
		t.Fatalf("queued complete reply task=%+v err=%v", waiting, err)
	}
	firstFinished, err := repository.GetRunAttempt(ctx, first.ID)
	if err != nil || firstFinished.Status != domain.RunAttemptSucceeded {
		t.Fatalf("first Run=%+v err=%v", firstFinished, err)
	}
	var recorded openruntime.TurnResult
	if err := json.Unmarshal([]byte(firstFinished.ResultJSON), &recorded); err != nil || !reflect.DeepEqual(recorded, firstResult) {
		t.Fatalf("original unknown effects were rewritten: %+v err=%v", recorded, err)
	}
	next, err := repository.TryClaimMailbox(ctx, guard, 1, guard.CheckedAt.Add(time.Hour), journalEvent("queued-next-claim", "mailbox.claimed", fixture.ownerPrincipal, fixture.organizationID))
	if err != nil || next == nil || next.ID != message.MailboxItem.ID || next.MessageID != message.Message.ID || next.Attempts != 1 {
		t.Fatalf("next claim replayed old work: %+v err=%v", next, err)
	}
	payload, err := repository.ResolveMailboxPayload(ctx, guard, next.ID)
	if err != nil || payload.Message == nil || payload.Message.ID != message.Message.ID || payload.Message.Content != message.Message.Content || payload.Message.Content == created.Task.Content {
		t.Fatalf("next payload is not the new Message: %+v err=%v", payload, err)
	}
	continued := *created
	continued.Task = *waiting
	second := beginClaimedNetworkRun(t, repository, fixture, worker, guard, descriptor, &continued, next, "queued-second", backends[0].Network)
	initialAfter, err := repository.GetMailboxItem(ctx, initial.ID)
	if err != nil || initialAfter.State != domain.MailboxStateAccepted || initialAfter.Attempts != 1 {
		t.Fatalf("original work replayed: %+v err=%v", initialAfter, err)
	}
	oldAfter, err := repository.GetRunAttempt(ctx, first.ID)
	if err != nil || !reflect.DeepEqual(oldAfter, firstFinished) {
		t.Fatal("second turn rewrote first Run evidence")
	}
	if err := repository.FinishRun(ctx, guard, second.ID, waiting.Version+1, second.Version,
		openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, FinalReply: true, Result: "reply only to the new Message", SideEffectsKnown: false}, nil, 0, nil,
		journalEvent("queued-second-task-finish", "task.settled", fixture.ownerPrincipal, fixture.organizationID),
		journalEvent("queued-second-run-finish", "run_attempt.finished", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	settled, err := repository.GetTask(ctx, before.ID)
	if err != nil || settled.Status != domain.TaskStatusUncertain || settled.CompletionBasis != "" || nullableValue(settled.Error) != "business_effect_unverified" {
		t.Fatalf("no pending message must remain uncertain: %+v err=%v", settled, err)
	}
	if err := finish(); err != nil {
		t.Fatalf("first finish replay: %v", err)
	}
	unchanged, err := repository.GetTask(ctx, before.ID)
	if err != nil || !reflect.DeepEqual(unchanged, settled) {
		t.Fatal("old finish replay overwrote latest Task")
	}
	noMore, err := repository.TryClaimMailbox(ctx, guard, 1, guard.CheckedAt.Add(time.Hour), journalEvent("queued-no-replay", "mailbox.claimed", fixture.ownerPrincipal, fixture.organizationID))
	if err != nil || noMore != nil {
		t.Fatalf("unexpected replay=%+v err=%v", noMore, err)
	}
}

func TestQueuedMessageDoesNotReviveUnconfirmedOrCanceledTurn(t *testing.T) {
	for _, name := range []string{"no-final", "truncated", "runtime-error", "actual-uncertain", "cancel-requested", "native-control", "no-message"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			repository, _ := openTestRepository(t, nil)
			fixture := seedRepository(t, repository)
			steer := openruntime.SteerQueued
			if name == "native-control" {
				steer = openruntime.SteerNative
			}
			descriptor := messageDescriptor(steer)
			worker, guard := registerMessageWorker(t, repository, fixture, descriptor)
			created, run := beginMessageRun(t, repository, fixture, worker, descriptor, "unconfirmed-"+name)
			version := int64(2)
			if name != "no-message" {
				if _, err := createRoutedMessage(t, repository, fixture, created.Task.ID, version, name); err != nil {
					t.Fatal(err)
				}
				version++
			}
			if name == "cancel-requested" {
				if _, _, err := repository.RequestTaskCancel(ctx, created.Task.ID, version, fixture.ownerPrincipal, &domain.MailboxItem{ID: "queued-cancel-control"}, journalEvent("queued-cancel-task", "task.cancel_requested", fixture.ownerPrincipal, fixture.organizationID), journalEvent("queued-cancel-mailbox", "mailbox.cancel_created", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
					t.Fatal(err)
				}
			}
			result := openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, FinalReply: true, Result: "returned content"}
			switch name {
			case "no-final":
				result.FinalReply = false
			case "truncated":
				result.FinalReply = false
				result.ResultTruncated = true
			case "runtime-error":
				result.FinalReply = false
				result.Error = "unconfirmed diagnostic"
			case "actual-uncertain":
				result.FinalReply = false
				result.Status = openruntime.TurnResultUncertain
			}
			if err := repository.FinishRun(ctx, guard, run.ID, 2, run.Version, result, nil, 0, nil, journalEvent("queued-reject-task", "task.settled", fixture.ownerPrincipal, fixture.organizationID), journalEvent("queued-reject-run", "run_attempt.finished", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
				t.Fatal(err)
			}
			task, err := repository.GetTask(ctx, created.Task.ID)
			if err != nil || task.Status != domain.TaskStatusUncertain || task.CanBeginAttempt() || task.CompletionBasis != "" {
				t.Fatalf("unconfirmed turn revived: %+v err=%v", task, err)
			}
		})
	}
}
