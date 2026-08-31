package sqlite

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"openagentx/internal/api"
	"openagentx/internal/controlplane"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

func TestRequestTaskCancelIsTaskLevelAndIdempotent(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	created := createTask(t, repository, fixture, "cancel-input")
	if err := repository.CreateWorkerInstance(context.Background(), &domain.WorkerInstance{ID: "worker-cancel-input", AgentID: fixture.agentID, Generation: 1, Transport: domain.WorkerTransportUnix, AuthenticatedPrincipal: fixture.ownerPrincipal, Status: domain.WorkerStatusOnline, Capabilities: []string{"coding"}, LeaseUntil: repositoryTestTime.Add(time.Hour), FencingToken: 1}, journalEvent("event-worker-cancel", "worker.registered", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	worker := &domain.RunAttempt{ID: "run-cancel-input", TaskID: created.Task.ID, AgentID: fixture.agentID, Version: 1,
		Status: domain.RunAttemptRunning, WorkerInstanceID: "worker-cancel-input", FencingToken: 1,
		LeaseUntil: repositoryTestTime.Add(time.Hour), ExecutionSpecVersion: 1, RequestedExecutionJSON: `{}`, ResolvedExecutionJSON: `{}`,
		AdapterID: "fake", BackendID: "local", Model: "model-1", ReasoningMode: domain.ReasoningBackendDefault}
	// The unguarded repository setup method is sufficient to establish an active
	// RunAttempt for testing the command transaction.
	if _, err := repository.BeginRunAttempt(context.Background(), created.Task.Version, worker,
		journalEvent("event-cancel-running", "task.running", fixture.ownerPrincipal, fixture.organizationID),
		journalEvent("event-cancel-run", "run_attempt.started", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	item := &domain.MailboxItem{ID: "cancel-control-input"}
	task, cancelItem, err := repository.RequestTaskCancel(context.Background(), created.Task.ID, 2, fixture.ownerPrincipal, item,
		journalEvent("event-cancel-request", "task.cancel_requested", fixture.ownerPrincipal, fixture.organizationID),
		journalEvent("event-cancel-mailbox", "mailbox.cancel_requested", fixture.ownerPrincipal, fixture.organizationID))
	if err != nil || task.Status != domain.TaskStatusCancelRequested || cancelItem == nil || cancelItem.TargetRunID != worker.ID {
		t.Fatalf("cancel task=%+v item=%+v err=%v", task, cancelItem, err)
	}
	// Repeating the same command does not create another control item.
	repeated, repeatedItem, err := repository.RequestTaskCancel(context.Background(), created.Task.ID, task.Version, fixture.ownerPrincipal,
		&domain.MailboxItem{ID: "mailbox-cancel-input-duplicate"}, nil, nil)
	if err != nil || repeatedItem != nil || repeated.Status != domain.TaskStatusCancelRequested {
		t.Fatalf("idempotent cancel task=%+v item=%+v err=%v", repeated, repeatedItem, err)
	}
	var count int
	if err := repository.db.QueryRow(`SELECT COUNT(*) FROM mailbox_items WHERE task_id=? AND kind='cancel'`, created.Task.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("cancel mailbox count=%d err=%v", count, err)
	}
}

func TestCommandServiceCancelHonorsExpectedVersionAndReturnsMailboxSequence(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	descriptor := messageDescriptor(openruntime.SteerNative)
	worker, _ := registerMessageWorker(t, repository, fixture, descriptor)
	created, _ := beginMessageRun(t, repository, fixture, worker, descriptor, "cancel-command-cas")
	service, err := controlplane.NewCommandService(repository, nil, func() time.Time { return repositoryTestTime })
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CancelTask(context.Background(), fixture.ownerPrincipal, created.Task.ID, api.CancelTaskRequest{
		Meta: api.CommandMeta{IdempotencyKey: "cancel-command-stale", ExpectedVersion: 1}, RequestedBy: fixture.ownerPrincipal,
	})
	if !errors.Is(err, domain.ErrStaleVersion) {
		t.Fatalf("stale Cancel expected version error=%v", err)
	}
	response, err := service.CancelTask(context.Background(), fixture.ownerPrincipal, created.Task.ID, api.CancelTaskRequest{
		Meta: api.CommandMeta{IdempotencyKey: "cancel-command-current", ExpectedVersion: 2}, RequestedBy: fixture.ownerPrincipal,
	})
	if err != nil || response.Sequence <= 0 || response.Task.Status != domain.TaskStatusCancelRequested {
		t.Fatalf("Cancel response=%+v err=%v", response, err)
	}
}

func TestNativeApprovalBecomesStaleWhenRunVersionChanges(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	request := &domain.ApprovalRequest{ID: "approval-native-stale", TaskID: "task-approval-stale", Mode: domain.ApprovalModeNative,
		TargetRunID: "run-approval-stale", ExpectedRunVersion: 1, ScopeDigest: "scope-digest", State: domain.ApprovalRequestPending,
		ExpiresAt: repositoryTestTime.Add(time.Hour)}
	created := createTask(t, repository, fixture, "approval-stale")
	request.TaskID = created.Task.ID
	if err := repository.CreateWorkerInstance(context.Background(), &domain.WorkerInstance{ID: "worker-approval-stale", AgentID: fixture.agentID, Generation: 1, Transport: domain.WorkerTransportUnix, AuthenticatedPrincipal: fixture.ownerPrincipal, Status: domain.WorkerStatusOnline, Capabilities: []string{"coding"}, LeaseUntil: repositoryTestTime.Add(time.Hour), FencingToken: 1}, journalEvent("event-worker-approval", "worker.registered", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	run := &domain.RunAttempt{ID: request.TargetRunID, TaskID: created.Task.ID, AgentID: fixture.agentID, Version: 1,
		Status: domain.RunAttemptRunning, WorkerInstanceID: "worker-approval-stale", FencingToken: 1,
		LeaseUntil: repositoryTestTime.Add(time.Hour), ExecutionSpecVersion: 1, RequestedExecutionJSON: `{}`, ResolvedExecutionJSON: `{}`,
		AdapterID: "fake", BackendID: "local", Model: "model-1", ReasoningMode: domain.ReasoningBackendDefault}
	if _, err := repository.BeginRunAttempt(context.Background(), created.Task.Version, run,
		journalEvent("event-approval-running", "task.running", fixture.ownerPrincipal, fixture.organizationID),
		journalEvent("event-approval-run", "run_attempt.started", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateApprovalRequest(context.Background(), request, journalEvent("event-approval-created", "approval.created", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.db.Exec(`UPDATE run_attempts SET version=version+1 WHERE run_id=?`, run.ID); err != nil {
		t.Fatal(err)
	}
	_, _, err := repository.DecideApproval(context.Background(), request.ID, &domain.ApprovalDecision{ID: "decision-stale", ApprovalRequestID: request.ID,
		DecidedBy: fixture.ownerPrincipal, Decision: domain.ApprovalDecisionApprove, State: domain.ApprovalDecisionPersisted, IdempotencyKey: "decision-stale-key"}, nil,
		journalEvent("event-approval-stale", "approval.stale", fixture.ownerPrincipal, fixture.organizationID), nil)
	if err != domain.ErrApprovalStale {
		t.Fatalf("expected stale approval, got %v", err)
	}
	got, getErr := repository.GetApprovalRequest(context.Background(), request.ID)
	if getErr != nil || got.State != domain.ApprovalRequestStale {
		t.Fatalf("approval=%+v err=%v", got, getErr)
	}
}

func TestNativeApprovalDecisionRollsBackStateMailboxAndJournalTogether(t *testing.T) {
	var failCommit atomic.Bool
	repository, _ := openTestRepository(t, func(point FaultPoint) error {
		if failCommit.Load() && point == FaultBeforeCommit {
			return errors.New("forced Approval rollback")
		}
		return nil
	})
	fixture := seedRepository(t, repository)
	descriptor := messageDescriptor(openruntime.SteerNative)
	worker, _ := registerMessageWorker(t, repository, fixture, descriptor)
	created, run := beginMessageRun(t, repository, fixture, worker, descriptor, "approval-rollback")
	request := &domain.ApprovalRequest{
		ID: "approval-rollback", TaskID: created.Task.ID, Mode: domain.ApprovalModeNative,
		TargetRunID: run.ID, ExpectedRunVersion: run.Version, ScopeDigest: "scope-rollback",
		State: domain.ApprovalRequestPending, ExpiresAt: repositoryTestTime.Add(time.Hour),
	}
	if err := repository.CreateApprovalRequest(context.Background(), request,
		journalEvent("event-approval-rollback-created", "approval.created", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	failCommit.Store(true)
	_, _, err := repository.DecideApproval(context.Background(), request.ID, &domain.ApprovalDecision{
		ID: "decision-approval-rollback", ApprovalRequestID: request.ID, DecidedBy: fixture.ownerPrincipal,
		Decision: domain.ApprovalDecisionApprove, State: domain.ApprovalDecisionPersisted, IdempotencyKey: "approval-rollback-idem",
	}, &domain.MailboxItem{ID: "control-approval-rollback"},
		journalEvent("event-decision-approval-rollback", "approval.decided", fixture.ownerPrincipal, fixture.organizationID),
		journalEvent("event-control-approval-rollback", "mailbox.approval_created", fixture.ownerPrincipal, fixture.organizationID))
	if err == nil {
		t.Fatal("Approval decision unexpectedly committed through fault")
	}
	failCommit.Store(false)
	persisted, err := repository.GetApprovalRequest(context.Background(), request.ID)
	if err != nil || persisted.State != domain.ApprovalRequestPending {
		t.Fatalf("rolled back Approval request=%+v err=%v", persisted, err)
	}
	var decisionCount, mailboxCount, eventCount int
	if err := repository.db.QueryRow(`SELECT COUNT(*) FROM approval_decisions WHERE approval_request_id=?`, request.ID).Scan(&decisionCount); err != nil || decisionCount != 0 {
		t.Fatalf("rolled back Approval decision count=%d err=%v", decisionCount, err)
	}
	if err := repository.db.QueryRow(`SELECT COUNT(*) FROM mailbox_items WHERE approval_request_id=?`, request.ID).Scan(&mailboxCount); err != nil || mailboxCount != 0 {
		t.Fatalf("rolled back Approval mailbox count=%d err=%v", mailboxCount, err)
	}
	if err := repository.db.QueryRow(`SELECT COUNT(*) FROM event_journal WHERE event_id IN ('event-decision-approval-rollback','event-control-approval-rollback')`).Scan(&eventCount); err != nil || eventCount != 0 {
		t.Fatalf("rolled back Approval event count=%d err=%v", eventCount, err)
	}
}

func TestCancelTransactionRollsBackAndBlocksFutureRun(t *testing.T) {
	var failCommit atomic.Bool
	repository, _ := openTestRepository(t, func(point FaultPoint) error {
		if failCommit.Load() && point == FaultBeforeCommit {
			return errors.New("forced cancel rollback")
		}
		return nil
	})
	fixture := seedRepository(t, repository)
	descriptor := messageDescriptor(openruntime.SteerNative)
	worker, _ := registerMessageWorker(t, repository, fixture, descriptor)
	created, run := beginMessageRun(t, repository, fixture, worker, descriptor, "cancel-rollback")

	failCommit.Store(true)
	_, _, err := repository.RequestTaskCancel(context.Background(), created.Task.ID, 2, fixture.ownerPrincipal,
		&domain.MailboxItem{ID: "control-cancel-rollback"},
		journalEvent("event-request-cancel-rollback", "task.cancel_requested", fixture.ownerPrincipal, fixture.organizationID),
		journalEvent("event-control-cancel-rollback", "mailbox.cancel_created", fixture.ownerPrincipal, fixture.organizationID))
	if err == nil {
		t.Fatal("cancel transaction unexpectedly committed through fault")
	}
	failCommit.Store(false)
	task, err := repository.GetTask(context.Background(), created.Task.ID)
	if err != nil || task.Status != domain.TaskStatusRunning || task.Version != 2 {
		t.Fatalf("rolled back cancel task=%+v err=%v", task, err)
	}
	var mailboxCount, eventCount int
	if err := repository.db.QueryRow(`SELECT COUNT(*) FROM mailbox_items WHERE mailbox_item_id='control-cancel-rollback'`).Scan(&mailboxCount); err != nil || mailboxCount != 0 {
		t.Fatalf("rolled back cancel mailbox count=%d err=%v", mailboxCount, err)
	}
	if err := repository.db.QueryRow(`SELECT COUNT(*) FROM event_journal WHERE event_id IN ('event-request-cancel-rollback','event-control-cancel-rollback')`).Scan(&eventCount); err != nil || eventCount != 0 {
		t.Fatalf("rolled back cancel event count=%d err=%v", eventCount, err)
	}

	canceled, item, err := repository.RequestTaskCancel(context.Background(), created.Task.ID, task.Version, fixture.ownerPrincipal,
		&domain.MailboxItem{ID: "control-cancel-committed"},
		journalEvent("event-request-cancel-committed", "task.cancel_requested", fixture.ownerPrincipal, fixture.organizationID),
		journalEvent("event-control-cancel-committed", "mailbox.cancel_created", fixture.ownerPrincipal, fixture.organizationID))
	if err != nil || item == nil || canceled.Status != domain.TaskStatusCancelRequested {
		t.Fatalf("committed cancel task=%+v item=%+v err=%v", canceled, item, err)
	}
	secondRun := *run
	secondRun.ID = "run-after-cancel"
	secondRun.Version = 1
	if _, err := repository.BeginRunAttempt(context.Background(), canceled.Version, &secondRun,
		journalEvent("event-task-after-cancel", "task.running", fixture.ownerPrincipal, fixture.organizationID),
		journalEvent("event-run-after-cancel", "run_attempt.started", fixture.ownerPrincipal, fixture.organizationID)); !errors.Is(err, domain.ErrTaskCancelRequested) {
		t.Fatalf("new RunAttempt after cancel error=%v", err)
	}
}

func TestCancelAndFinishLinearizeInBothOrders100Times(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	descriptor := messageDescriptor(openruntime.SteerNative)
	worker, guard := registerMessageWorker(t, repository, fixture, descriptor)

	for iteration := 0; iteration < 100; iteration++ {
		suffix := fmt.Sprintf("cancel-before-finish-%03d", iteration)
		created, run := beginMessageRun(t, repository, fixture, worker, descriptor, suffix)
		canceled, item, err := repository.RequestTaskCancel(context.Background(), created.Task.ID, 2, fixture.ownerPrincipal,
			&domain.MailboxItem{ID: "control-" + suffix},
			journalEvent("event-request-"+suffix, "task.cancel_requested", fixture.ownerPrincipal, fixture.organizationID),
			journalEvent("event-control-"+suffix, "mailbox.cancel_created", fixture.ownerPrincipal, fixture.organizationID))
		if err != nil || item == nil || canceled.Status != domain.TaskStatusCancelRequested {
			t.Fatalf("iteration %d cancel-before-finish task=%+v item=%+v err=%v", iteration, canceled, item, err)
		}
		if err := repository.FinishRun(context.Background(), guard, run.ID, 2, run.Version,
			openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "late success", SideEffectsKnown: true}, nil, 0, nil,
			journalEvent("event-finish-task-"+suffix, "task.canceled", fixture.ownerPrincipal, fixture.organizationID),
			journalEvent("event-finish-run-"+suffix, "run_attempt.finished", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
			t.Fatalf("iteration %d finish after cancel: %v", iteration, err)
		}
		settled, err := repository.GetTask(context.Background(), created.Task.ID)
		if err != nil || settled.Status != domain.TaskStatusCanceled {
			t.Fatalf("iteration %d settled cancel task=%+v err=%v", iteration, settled, err)
		}
	}

	for iteration := 0; iteration < 100; iteration++ {
		suffix := fmt.Sprintf("finish-before-cancel-%03d", iteration)
		created, run := beginMessageRun(t, repository, fixture, worker, descriptor, suffix)
		if err := repository.FinishRun(context.Background(), guard, run.ID, 2, run.Version,
			openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "done", SideEffectsKnown: true}, nil, 0, nil,
			journalEvent("event-finish-task-"+suffix, "task.succeeded", fixture.ownerPrincipal, fixture.organizationID),
			journalEvent("event-finish-run-"+suffix, "run_attempt.finished", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
			t.Fatalf("iteration %d finish-before-cancel: %v", iteration, err)
		}
		_, item, err := repository.RequestTaskCancel(context.Background(), created.Task.ID, 3, fixture.ownerPrincipal,
			&domain.MailboxItem{ID: "control-" + suffix},
			journalEvent("event-request-cancel-"+suffix, "task.cancel_requested", fixture.ownerPrincipal, fixture.organizationID),
			journalEvent("event-control-cancel-"+suffix, "mailbox.cancel_created", fixture.ownerPrincipal, fixture.organizationID))
		if !errors.Is(err, domain.ErrTerminalState) || item != nil {
			t.Fatalf("iteration %d cancel after finish item=%+v err=%v", iteration, item, err)
		}
	}
}

// The ordering loops above exercise the state machine serially. These tests
// add a real two-goroutine barrier around each pair so the competing commands
// are issued from concurrent callers while still making both linearization
// orders deterministic and auditable.
func TestConcurrentCancelAndFinishBarrierBothOrders100Times(t *testing.T) {
	for _, cancelFirst := range []bool{true, false} {
		name := "finish-first"
		if cancelFirst {
			name = "cancel-first"
		}
		t.Run(name, func(t *testing.T) {
			repository, _ := openTestRepository(t, nil)
			fixture := seedRepository(t, repository)
			descriptor := messageDescriptor(openruntime.SteerNative)
			worker, guard := registerMessageWorker(t, repository, fixture, descriptor)
			for iteration := 0; iteration < 100; iteration++ {
				suffix := fmt.Sprintf("barrier-%s-%03d", name, iteration)
				created, run := beginMessageRun(t, repository, fixture, worker, descriptor, suffix)
				start := make(chan struct{})
				ready := make(chan struct{}, 2)
				allowCancel, allowFinish := make(chan struct{}), make(chan struct{})
				cancelDone, finishDone := make(chan error, 1), make(chan error, 1)
				go func() {
					ready <- struct{}{}
					<-start
					<-allowCancel
					_, _, err := repository.RequestTaskCancel(context.Background(), created.Task.ID, 2, fixture.ownerPrincipal,
						&domain.MailboxItem{ID: "control-" + suffix},
						journalEvent("event-cancel-"+suffix, "task.cancel_requested", fixture.ownerPrincipal, fixture.organizationID),
						journalEvent("event-cancel-mailbox-"+suffix, "mailbox.cancel_created", fixture.ownerPrincipal, fixture.organizationID))
					cancelDone <- err
				}()
				go func() {
					ready <- struct{}{}
					<-start
					<-allowFinish
					err := repository.FinishRun(context.Background(), guard, run.ID, 2, run.Version,
						openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "concurrent finish", SideEffectsKnown: true}, nil, 0, nil,
						journalEvent("event-finish-task-"+suffix, "task.succeeded", fixture.ownerPrincipal, fixture.organizationID),
						journalEvent("event-finish-run-"+suffix, "run_attempt.finished", fixture.ownerPrincipal, fixture.organizationID))
					finishDone <- err
				}()
				<-ready
				<-ready
				close(start) // both callers are live before either transaction is released
				if cancelFirst {
					close(allowCancel)
					cancelErr := <-cancelDone
					if cancelErr != nil {
						t.Fatalf("iteration %d cancel-first error: %v", iteration, cancelErr)
					}
					close(allowFinish)
					if err := <-finishDone; err != nil {
						t.Fatalf("iteration %d finish after cancel: %v", iteration, err)
					}
					task, err := repository.GetTask(context.Background(), created.Task.ID)
					if err != nil || task.Status != domain.TaskStatusCanceled {
						t.Fatalf("iteration %d task=%+v err=%v", iteration, task, err)
					}
					claimed, err := repository.TryClaimMailbox(context.Background(), guard, 0, repositoryTestTime.Add(time.Hour),
						journalEvent("event-claim-"+suffix, "mailbox.claimed", fixture.ownerPrincipal, fixture.organizationID))
					if err != nil || claimed == nil || claimed.Kind != domain.MailboxKindCancel {
						t.Fatalf("iteration %d cancel claim=%+v err=%v", iteration, claimed, err)
					}
					if err := repository.AcceptMailboxItem(context.Background(), guard, claimed.ID, domain.MailboxStateClaimed, domain.MailboxStateAccepted,
						journalEvent("event-accept-"+suffix, "mailbox.accepted", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
						t.Fatalf("iteration %d cancel settle: %v", iteration, err)
					}
				} else {
					close(allowFinish)
					if err := <-finishDone; err != nil {
						t.Fatalf("iteration %d finish-first: %v", iteration, err)
					}
					close(allowCancel)
					if err := <-cancelDone; !errors.Is(err, domain.ErrTerminalState) && !errors.Is(err, domain.ErrStaleVersion) {
						t.Fatalf("iteration %d cancel after finish error=%v", iteration, err)
					}
					task, err := repository.GetTask(context.Background(), created.Task.ID)
					if err != nil || task.Status != domain.TaskStatusSucceeded {
						t.Fatalf("iteration %d task=%+v err=%v", iteration, task, err)
					}
					var cancelItems int
					if err := repository.db.QueryRow(`SELECT COUNT(*) FROM mailbox_items WHERE task_id=? AND kind='cancel'`, created.Task.ID).Scan(&cancelItems); err != nil || cancelItems != 0 {
						t.Fatalf("iteration %d finish-first cancel mailbox count=%d err=%v", iteration, cancelItems, err)
					}
				}
			}
		})
	}
}

func TestConcurrentDuplicateCancelCreatesOneControlItem(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	descriptor := messageDescriptor(openruntime.SteerNative)
	worker, _ := registerMessageWorker(t, repository, fixture, descriptor)
	created, _ := beginMessageRun(t, repository, fixture, worker, descriptor, "cancel-concurrent")

	const requests = 100
	start := make(chan struct{})
	results := make(chan error, requests)
	var group sync.WaitGroup
	for index := 0; index < requests; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			<-start
			_, _, err := repository.RequestTaskCancel(context.Background(), created.Task.ID, 2, fixture.ownerPrincipal,
				&domain.MailboxItem{ID: fmt.Sprintf("mailbox-cancel-concurrent-%03d", index)},
				journalEvent(fmt.Sprintf("event-task-cancel-concurrent-%03d", index), "task.cancel_requested", fixture.ownerPrincipal, fixture.organizationID),
				journalEvent(fmt.Sprintf("event-mailbox-cancel-concurrent-%03d", index), "mailbox.cancel_created", fixture.ownerPrincipal, fixture.organizationID))
			results <- err
		}(index)
	}
	close(start)
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("duplicate cancel failed: %v", err)
		}
	}
	var mailboxCount, taskEventCount int
	if err := repository.db.QueryRow(`SELECT COUNT(*) FROM mailbox_items WHERE task_id=? AND kind='cancel'`, created.Task.ID).Scan(&mailboxCount); err != nil || mailboxCount != 1 {
		t.Fatalf("cancel mailbox count=%d err=%v", mailboxCount, err)
	}
	if err := repository.db.QueryRow(`SELECT COUNT(*) FROM event_journal WHERE aggregate_type='task' AND aggregate_id=? AND event_type='task.cancel_requested'`, created.Task.ID).Scan(&taskEventCount); err != nil || taskEventCount != 1 {
		t.Fatalf("cancel task event count=%d err=%v", taskEventCount, err)
	}
}

func TestNativeApprovalAndFinishLinearizeInBothOrders100Times(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	descriptor := messageDescriptor(openruntime.SteerNative)
	worker, guard := registerMessageWorker(t, repository, fixture, descriptor)

	for iteration := 0; iteration < 100; iteration++ {
		suffix := fmt.Sprintf("approval-before-finish-%03d", iteration)
		created, run := beginMessageRun(t, repository, fixture, worker, descriptor, suffix)
		request := &domain.ApprovalRequest{
			ID: "approval-" + suffix, TaskID: created.Task.ID, Mode: domain.ApprovalModeNative,
			TargetRunID: run.ID, ExpectedRunVersion: run.Version, ScopeDigest: "scope-" + suffix,
			State: domain.ApprovalRequestPending, ExpiresAt: repositoryTestTime.Add(time.Hour),
		}
		if err := repository.CreateApprovalRequest(context.Background(), request,
			journalEvent("event-create-"+suffix, "approval.created", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
			t.Fatalf("iteration %d create Approval: %v", iteration, err)
		}
		decision, item, err := repository.DecideApproval(context.Background(), request.ID, &domain.ApprovalDecision{
			ID: "decision-" + suffix, ApprovalRequestID: request.ID, DecidedBy: fixture.ownerPrincipal,
			Decision: domain.ApprovalDecisionApprove, State: domain.ApprovalDecisionPersisted, IdempotencyKey: "idem-" + suffix,
		}, &domain.MailboxItem{ID: "control-" + suffix},
			journalEvent("event-decision-"+suffix, "approval.decided", fixture.ownerPrincipal, fixture.organizationID),
			journalEvent("event-control-"+suffix, "mailbox.approval_created", fixture.ownerPrincipal, fixture.organizationID))
		if err != nil || decision == nil || item == nil || item.TargetRunID != run.ID || item.ExpectedRunVersion != run.Version {
			t.Fatalf("iteration %d decide-before-finish decision=%+v item=%+v err=%v", iteration, decision, item, err)
		}
		if err := repository.FinishRun(context.Background(), guard, run.ID, 2, run.Version,
			openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "done", SideEffectsKnown: true}, nil, 0, nil,
			journalEvent("event-finish-task-"+suffix, "task.succeeded", fixture.ownerPrincipal, fixture.organizationID),
			journalEvent("event-finish-run-"+suffix, "run_attempt.finished", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
			t.Fatalf("iteration %d finish after Approval: %v", iteration, err)
		}
		claimed, err := repository.TryClaimMailbox(context.Background(), guard, 0, repositoryTestTime.Add(30*time.Minute),
			journalEvent("event-claim-"+suffix, "mailbox.claimed", fixture.ownerPrincipal, fixture.organizationID))
		if err != nil || claimed == nil || claimed.ID != item.ID {
			t.Fatalf("iteration %d claim stale Approval=%+v err=%v", iteration, claimed, err)
		}
		if err := repository.AcceptMailboxItem(context.Background(), guard, claimed.ID, domain.MailboxStateClaimed, domain.MailboxStateSuperseded,
			journalEvent("event-supersede-"+suffix, "mailbox.superseded", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
			t.Fatalf("iteration %d supersede stale Approval: %v", iteration, err)
		}
		persistedDecision, err := repository.GetApprovalDecision(context.Background(), decision.ID)
		if err != nil || persistedDecision.State != domain.ApprovalDecisionSuperseded {
			t.Fatalf("iteration %d stale decision=%+v err=%v", iteration, persistedDecision, err)
		}
		persistedRequest, err := repository.GetApprovalRequest(context.Background(), request.ID)
		if err != nil || persistedRequest.State != domain.ApprovalRequestStale {
			t.Fatalf("iteration %d stale request=%+v err=%v", iteration, persistedRequest, err)
		}
	}

	for iteration := 0; iteration < 100; iteration++ {
		suffix := fmt.Sprintf("finish-before-approval-%03d", iteration)
		created, run := beginMessageRun(t, repository, fixture, worker, descriptor, suffix)
		request := &domain.ApprovalRequest{
			ID: "approval-" + suffix, TaskID: created.Task.ID, Mode: domain.ApprovalModeNative,
			TargetRunID: run.ID, ExpectedRunVersion: run.Version, ScopeDigest: "scope-" + suffix,
			State: domain.ApprovalRequestPending, ExpiresAt: repositoryTestTime.Add(time.Hour),
		}
		if err := repository.CreateApprovalRequest(context.Background(), request,
			journalEvent("event-create-"+suffix, "approval.created", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
			t.Fatalf("iteration %d create Approval: %v", iteration, err)
		}
		if err := repository.FinishRun(context.Background(), guard, run.ID, 2, run.Version,
			openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "done", SideEffectsKnown: true}, nil, 0, nil,
			journalEvent("event-finish-task-"+suffix, "task.succeeded", fixture.ownerPrincipal, fixture.organizationID),
			journalEvent("event-finish-run-"+suffix, "run_attempt.finished", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
			t.Fatalf("iteration %d finish before Approval: %v", iteration, err)
		}
		decision, item, err := repository.DecideApproval(context.Background(), request.ID, &domain.ApprovalDecision{
			ID: "decision-" + suffix, ApprovalRequestID: request.ID, DecidedBy: fixture.ownerPrincipal,
			Decision: domain.ApprovalDecisionApprove, State: domain.ApprovalDecisionPersisted, IdempotencyKey: "idem-" + suffix,
		}, &domain.MailboxItem{ID: "control-" + suffix},
			journalEvent("event-stale-"+suffix, "approval.stale", fixture.ownerPrincipal, fixture.organizationID),
			journalEvent("event-control-"+suffix, "mailbox.approval_created", fixture.ownerPrincipal, fixture.organizationID))
		if !errors.Is(err, domain.ErrApprovalStale) || decision != nil || item != nil {
			t.Fatalf("iteration %d Approval after finish decision=%+v item=%+v err=%v", iteration, decision, item, err)
		}
		var decisionCount, mailboxCount int
		if err := repository.db.QueryRow(`SELECT COUNT(*) FROM approval_decisions WHERE approval_request_id=?`, request.ID).Scan(&decisionCount); err != nil || decisionCount != 0 {
			t.Fatalf("iteration %d stale decision count=%d err=%v", iteration, decisionCount, err)
		}
		if err := repository.db.QueryRow(`SELECT COUNT(*) FROM mailbox_items WHERE approval_request_id=?`, request.ID).Scan(&mailboxCount); err != nil || mailboxCount != 0 {
			t.Fatalf("iteration %d stale Approval mailbox count=%d err=%v", iteration, mailboxCount, err)
		}
	}
}

func TestConcurrentNativeApprovalAndFinishBarrierBothOrders100Times(t *testing.T) {
	for _, approvalFirst := range []bool{true, false} {
		name := "finish-first"
		if approvalFirst {
			name = "approval-first"
		}
		t.Run(name, func(t *testing.T) {
			repository, _ := openTestRepository(t, nil)
			fixture := seedRepository(t, repository)
			descriptor := messageDescriptor(openruntime.SteerNative)
			worker, guard := registerMessageWorker(t, repository, fixture, descriptor)
			for iteration := 0; iteration < 100; iteration++ {
				suffix := fmt.Sprintf("barrier-%s-%03d", name, iteration)
				created, run := beginMessageRun(t, repository, fixture, worker, descriptor, suffix)
				request := &domain.ApprovalRequest{ID: "approval-" + suffix, TaskID: created.Task.ID, Mode: domain.ApprovalModeNative,
					TargetRunID: run.ID, ExpectedRunVersion: run.Version, ScopeDigest: "scope-" + suffix,
					State: domain.ApprovalRequestPending, ExpiresAt: repositoryTestTime.Add(time.Hour)}
				if err := repository.CreateApprovalRequest(context.Background(), request,
					journalEvent("event-approval-create-"+suffix, "approval.created", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
					t.Fatal(err)
				}
				start := make(chan struct{})
				ready := make(chan struct{}, 2)
				allowApproval, allowFinish := make(chan struct{}), make(chan struct{})
				approvalDone := make(chan struct {
					decision *domain.ApprovalDecision
					item     *domain.MailboxItem
					err      error
				}, 1)
				finishDone := make(chan error, 1)
				go func() {
					ready <- struct{}{}
					<-start
					<-allowApproval
					decision, item, err := repository.DecideApproval(context.Background(), request.ID, &domain.ApprovalDecision{
						ID: "decision-" + suffix, ApprovalRequestID: request.ID, DecidedBy: fixture.ownerPrincipal,
						Decision: domain.ApprovalDecisionApprove, State: domain.ApprovalDecisionPersisted, IdempotencyKey: "idem-" + suffix,
					}, &domain.MailboxItem{ID: "control-" + suffix},
						journalEvent("event-approval-decide-"+suffix, "approval.decided", fixture.ownerPrincipal, fixture.organizationID),
						journalEvent("event-approval-mailbox-"+suffix, "mailbox.approval_created", fixture.ownerPrincipal, fixture.organizationID))
					approvalDone <- struct {
						decision *domain.ApprovalDecision
						item     *domain.MailboxItem
						err      error
					}{decision, item, err}
				}()
				go func() {
					ready <- struct{}{}
					<-start
					<-allowFinish
					finishDone <- repository.FinishRun(context.Background(), guard, run.ID, 2, run.Version,
						openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "concurrent finish", SideEffectsKnown: true}, nil, 0, nil,
						journalEvent("event-finish-task-"+suffix, "task.succeeded", fixture.ownerPrincipal, fixture.organizationID),
						journalEvent("event-finish-run-"+suffix, "run_attempt.finished", fixture.ownerPrincipal, fixture.organizationID))
				}()
				<-ready
				<-ready
				close(start)
				if approvalFirst {
					close(allowApproval)
					outcome := <-approvalDone
					if outcome.err != nil || outcome.decision == nil || outcome.item == nil {
						t.Fatalf("iteration %d approval-first outcome=%+v", iteration, outcome)
					}
					close(allowFinish)
					if err := <-finishDone; err != nil {
						t.Fatalf("iteration %d finish after approval: %v", iteration, err)
					}
					claimed, err := repository.TryClaimMailbox(context.Background(), guard, 0, repositoryTestTime.Add(time.Hour),
						journalEvent("event-claim-"+suffix, "mailbox.claimed", fixture.ownerPrincipal, fixture.organizationID))
					if err != nil || claimed == nil || claimed.ID != outcome.item.ID {
						t.Fatalf("iteration %d claim=%+v err=%v", iteration, claimed, err)
					}
					if err := repository.AcceptMailboxItem(context.Background(), guard, claimed.ID, domain.MailboxStateClaimed, domain.MailboxStateAccepted,
						journalEvent("event-accept-"+suffix, "mailbox.accepted", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
						t.Fatalf("iteration %d accept approval: %v", iteration, err)
					}
					decision, err := repository.GetApprovalDecision(context.Background(), outcome.decision.ID)
					if err != nil || decision.State != domain.ApprovalDecisionApplied {
						t.Fatalf("iteration %d decision=%+v err=%v", iteration, decision, err)
					}
				} else {
					close(allowFinish)
					if err := <-finishDone; err != nil {
						t.Fatalf("iteration %d finish-first: %v", iteration, err)
					}
					close(allowApproval)
					outcome := <-approvalDone
					if !errors.Is(outcome.err, domain.ErrApprovalStale) || outcome.decision != nil || outcome.item != nil {
						t.Fatalf("iteration %d stale approval outcome=%+v", iteration, outcome)
					}
					decision, err := repository.GetApprovalRequest(context.Background(), request.ID)
					if err != nil || decision.State != domain.ApprovalRequestStale {
						t.Fatalf("iteration %d request=%+v err=%v", iteration, decision, err)
					}
				}
			}
		})
	}
}

func TestConcurrentDuplicateNativeApprovalCreatesOneDecisionAndControlItem(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	descriptor := messageDescriptor(openruntime.SteerNative)
	worker, _ := registerMessageWorker(t, repository, fixture, descriptor)
	created, run := beginMessageRun(t, repository, fixture, worker, descriptor, "approval-concurrent")
	request := &domain.ApprovalRequest{
		ID: "approval-concurrent", TaskID: created.Task.ID, Mode: domain.ApprovalModeNative,
		TargetRunID: run.ID, ExpectedRunVersion: run.Version, ScopeDigest: "scope-concurrent",
		State: domain.ApprovalRequestPending, ExpiresAt: repositoryTestTime.Add(time.Hour),
	}
	if err := repository.CreateApprovalRequest(context.Background(), request,
		journalEvent("event-approval-concurrent", "approval.created", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}

	const requests = 100
	start := make(chan struct{})
	results := make(chan error, requests)
	var group sync.WaitGroup
	for index := 0; index < requests; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			<-start
			_, _, err := repository.DecideApproval(context.Background(), request.ID, &domain.ApprovalDecision{
				ID: fmt.Sprintf("decision-concurrent-%03d", index), ApprovalRequestID: request.ID,
				DecidedBy: fixture.ownerPrincipal, Decision: domain.ApprovalDecisionApprove,
				State: domain.ApprovalDecisionPersisted, IdempotencyKey: "approval-concurrent-idem",
			}, &domain.MailboxItem{ID: fmt.Sprintf("control-approval-concurrent-%03d", index)},
				journalEvent(fmt.Sprintf("event-decision-concurrent-%03d", index), "approval.decided", fixture.ownerPrincipal, fixture.organizationID),
				journalEvent(fmt.Sprintf("event-control-concurrent-%03d", index), "mailbox.approval_created", fixture.ownerPrincipal, fixture.organizationID))
			results <- err
		}(index)
	}
	close(start)
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("duplicate native Approval failed: %v", err)
		}
	}
	var decisionCount, mailboxCount int
	if err := repository.db.QueryRow(`SELECT COUNT(*) FROM approval_decisions WHERE approval_request_id=?`, request.ID).Scan(&decisionCount); err != nil || decisionCount != 1 {
		t.Fatalf("native Approval decision count=%d err=%v", decisionCount, err)
	}
	if err := repository.db.QueryRow(`SELECT COUNT(*) FROM mailbox_items WHERE approval_request_id=? AND kind='approval'`, request.ID).Scan(&mailboxCount); err != nil || mailboxCount != 1 {
		t.Fatalf("native Approval mailbox count=%d err=%v", mailboxCount, err)
	}
}

func TestNativeApprovalCannotOutliveTaskCancelAndRejectIsDelivered(t *testing.T) {
	t.Run("cancel makes pending native Approval stale", func(t *testing.T) {
		repository, _ := openTestRepository(t, nil)
		fixture := seedRepository(t, repository)
		descriptor := messageDescriptor(openruntime.SteerNative)
		worker, _ := registerMessageWorker(t, repository, fixture, descriptor)
		created, run := beginMessageRun(t, repository, fixture, worker, descriptor, "approval-after-cancel")
		request := &domain.ApprovalRequest{
			ID: "approval-after-cancel", TaskID: created.Task.ID, Mode: domain.ApprovalModeNative,
			TargetRunID: run.ID, ExpectedRunVersion: run.Version, ScopeDigest: "scope-after-cancel",
			State: domain.ApprovalRequestPending, ExpiresAt: repositoryTestTime.Add(time.Hour),
		}
		if err := repository.CreateApprovalRequest(context.Background(), request,
			journalEvent("event-approval-after-cancel", "approval.created", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
			t.Fatal(err)
		}
		if _, _, err := repository.RequestTaskCancel(context.Background(), created.Task.ID, 2, fixture.ownerPrincipal,
			&domain.MailboxItem{ID: "control-cancel-before-approval"},
			journalEvent("event-request-cancel-before-approval", "task.cancel_requested", fixture.ownerPrincipal, fixture.organizationID),
			journalEvent("event-control-cancel-before-approval", "mailbox.cancel_created", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
			t.Fatal(err)
		}
		decision, item, err := repository.DecideApproval(context.Background(), request.ID, &domain.ApprovalDecision{
			ID: "decision-after-cancel", ApprovalRequestID: request.ID, DecidedBy: fixture.ownerPrincipal,
			Decision: domain.ApprovalDecisionApprove, State: domain.ApprovalDecisionPersisted, IdempotencyKey: "approval-after-cancel-idem",
		}, &domain.MailboxItem{ID: "control-approval-after-cancel"},
			journalEvent("event-decision-after-cancel", "approval.decided", fixture.ownerPrincipal, fixture.organizationID),
			journalEvent("event-control-approval-after-cancel", "mailbox.approval_created", fixture.ownerPrincipal, fixture.organizationID))
		if !errors.Is(err, domain.ErrApprovalStale) || decision != nil || item != nil {
			t.Fatalf("Approval after cancel decision=%+v item=%+v err=%v", decision, item, err)
		}
		persisted, err := repository.GetApprovalRequest(context.Background(), request.ID)
		if err != nil || persisted.State != domain.ApprovalRequestStale {
			t.Fatalf("stale Approval after cancel=%+v err=%v", persisted, err)
		}
	})

	t.Run("native reject reaches current Run and becomes applied", func(t *testing.T) {
		repository, _ := openTestRepository(t, nil)
		fixture := seedRepository(t, repository)
		descriptor := messageDescriptor(openruntime.SteerNative)
		worker, guard := registerMessageWorker(t, repository, fixture, descriptor)
		created, run := beginMessageRun(t, repository, fixture, worker, descriptor, "approval-reject")
		request := &domain.ApprovalRequest{
			ID: "approval-reject", TaskID: created.Task.ID, Mode: domain.ApprovalModeNative,
			TargetRunID: run.ID, ExpectedRunVersion: run.Version, ScopeDigest: "scope-reject",
			State: domain.ApprovalRequestPending, ExpiresAt: repositoryTestTime.Add(time.Hour),
		}
		if err := repository.CreateApprovalRequest(context.Background(), request,
			journalEvent("event-approval-reject", "approval.created", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
			t.Fatal(err)
		}
		decision, item, err := repository.DecideApproval(context.Background(), request.ID, &domain.ApprovalDecision{
			ID: "decision-reject", ApprovalRequestID: request.ID, DecidedBy: fixture.ownerPrincipal,
			Decision: domain.ApprovalDecisionReject, State: domain.ApprovalDecisionPersisted, IdempotencyKey: "approval-reject-idem",
		}, &domain.MailboxItem{ID: "control-approval-reject"},
			journalEvent("event-decision-reject", "approval.decided", fixture.ownerPrincipal, fixture.organizationID),
			journalEvent("event-control-approval-reject", "mailbox.approval_created", fixture.ownerPrincipal, fixture.organizationID))
		if err != nil || item == nil || decision.Decision != domain.ApprovalDecisionReject {
			t.Fatalf("native reject decision=%+v item=%+v err=%v", decision, item, err)
		}
		claimed, err := repository.TryClaimMailbox(context.Background(), guard, 0, repositoryTestTime.Add(30*time.Minute),
			journalEvent("event-claim-approval-reject", "mailbox.claimed", fixture.ownerPrincipal, fixture.organizationID))
		if err != nil || claimed == nil || claimed.ID != item.ID {
			t.Fatalf("claim native reject=%+v err=%v", claimed, err)
		}
		payload, err := repository.ResolveMailboxPayload(context.Background(), guard, claimed.ID)
		if err != nil || payload.ApprovalDecision == nil || payload.ApprovalDecision.Decision != domain.ApprovalDecisionReject {
			t.Fatalf("native reject payload=%+v err=%v", payload, err)
		}
		if err := repository.AcceptMailboxItem(context.Background(), guard, claimed.ID, domain.MailboxStateClaimed, domain.MailboxStateAccepted,
			journalEvent("event-accept-approval-reject", "mailbox.accepted", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
			t.Fatal(err)
		}
		persisted, err := repository.GetApprovalDecision(context.Background(), decision.ID)
		if err != nil || persisted.State != domain.ApprovalDecisionApplied {
			t.Fatalf("applied native reject=%+v err=%v", persisted, err)
		}
	})
}

func TestPreflightApprovalIsScopedAndConsumedOnce(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	created := createTask(t, repository, fixture, "preflight-once")
	request := &domain.ApprovalRequest{
		ID: "approval-preflight-once", TaskID: created.Task.ID, Mode: domain.ApprovalModePreflight,
		ScopeDigest: "scope-preflight-once", State: domain.ApprovalRequestPending,
		ExpiresAt: repositoryTestTime.Add(time.Hour),
	}
	if err := repository.CreateApprovalRequest(context.Background(), request,
		journalEvent("event-preflight-created", "approval.created", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	decision, item, err := repository.DecideApproval(context.Background(), request.ID, &domain.ApprovalDecision{
		ID: "decision-preflight-once", ApprovalRequestID: request.ID, DecidedBy: fixture.ownerPrincipal,
		Decision: domain.ApprovalDecisionApprove, State: domain.ApprovalDecisionPersisted, IdempotencyKey: "preflight-once-idem",
	}, nil, journalEvent("event-preflight-decided", "approval.decided", fixture.ownerPrincipal, fixture.organizationID), nil)
	if err != nil || decision == nil || item != nil {
		t.Fatalf("preflight decision=%+v item=%+v err=%v", decision, item, err)
	}
	if _, err := repository.ConsumePreflightApproval(context.Background(), created.Task.ID, "wrong-scope",
		journalEvent("event-preflight-wrong-scope", "approval.consumed", fixture.ownerPrincipal, fixture.organizationID)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("wrong scope consumption error=%v", err)
	}

	const consumers = 100
	start := make(chan struct{})
	results := make(chan error, consumers)
	var group sync.WaitGroup
	for index := 0; index < consumers; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			<-start
			_, err := repository.ConsumePreflightApproval(context.Background(), created.Task.ID, request.ScopeDigest,
				journalEvent(fmt.Sprintf("event-preflight-consume-%03d", index), "approval.consumed", fixture.ownerPrincipal, fixture.organizationID))
			results <- err
		}(index)
	}
	close(start)
	group.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		if !errors.Is(err, domain.ErrNotFound) && !errors.Is(err, domain.ErrApprovalStale) {
			t.Fatalf("unexpected preflight consume error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("preflight consume successes=%d want=1", successes)
	}
	persisted, err := repository.GetApprovalRequest(context.Background(), request.ID)
	if err != nil || persisted.State != domain.ApprovalRequestConsumed {
		t.Fatalf("consumed preflight=%+v err=%v", persisted, err)
	}
	var consumeEvents int
	if err := repository.db.QueryRow(`SELECT COUNT(*) FROM event_journal WHERE aggregate_type='approval_request' AND aggregate_id=? AND event_type='approval.consumed'`, request.ID).Scan(&consumeEvents); err != nil || consumeEvents != 1 {
		t.Fatalf("preflight consume event count=%d err=%v", consumeEvents, err)
	}
}

func TestMailboxClaimPrioritizesControlAndPreservesSequenceWithinEachLane(t *testing.T) {
	t.Run("control before earlier work and control sequence ascending", func(t *testing.T) {
		repository, _ := openTestRepository(t, nil)
		fixture := seedRepository(t, repository)
		descriptor := messageDescriptor(openruntime.SteerNative)
		worker, guard := registerMessageWorker(t, repository, fixture, descriptor)
		created, run := beginMessageRun(t, repository, fixture, worker, descriptor, "lane-control")
		workSequence := created.MailboxItem.Sequence
		controlItems := make([]*domain.MailboxItem, 0, 2)
		for index := 0; index < 2; index++ {
			suffix := fmt.Sprintf("lane-control-%d", index)
			request := &domain.ApprovalRequest{
				ID: "approval-" + suffix, TaskID: created.Task.ID, Mode: domain.ApprovalModeNative,
				TargetRunID: run.ID, ExpectedRunVersion: run.Version, ScopeDigest: "scope-" + suffix,
				State: domain.ApprovalRequestPending, ExpiresAt: repositoryTestTime.Add(time.Hour),
			}
			if err := repository.CreateApprovalRequest(context.Background(), request,
				journalEvent("event-create-"+suffix, "approval.created", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
				t.Fatal(err)
			}
			_, item, err := repository.DecideApproval(context.Background(), request.ID, &domain.ApprovalDecision{
				ID: "decision-" + suffix, ApprovalRequestID: request.ID, DecidedBy: fixture.ownerPrincipal,
				Decision: domain.ApprovalDecisionApprove, State: domain.ApprovalDecisionPersisted, IdempotencyKey: "idem-" + suffix,
			}, &domain.MailboxItem{ID: "control-" + suffix},
				journalEvent("event-decision-"+suffix, "approval.decided", fixture.ownerPrincipal, fixture.organizationID),
				journalEvent("event-control-"+suffix, "mailbox.approval_created", fixture.ownerPrincipal, fixture.organizationID))
			if err != nil {
				t.Fatal(err)
			}
			controlItems = append(controlItems, item)
		}
		if !(workSequence < controlItems[0].Sequence && controlItems[0].Sequence < controlItems[1].Sequence) {
			t.Fatalf("unexpected inserted sequences work=%d controls=%d,%d", workSequence, controlItems[0].Sequence, controlItems[1].Sequence)
		}
		for index, expected := range controlItems {
			claimed, err := repository.TryClaimMailbox(context.Background(), guard, 1, repositoryTestTime.Add(30*time.Minute),
				journalEvent(fmt.Sprintf("event-claim-lane-control-%d", index), "mailbox.claimed", fixture.ownerPrincipal, fixture.organizationID))
			if err != nil || claimed == nil || claimed.ID != expected.ID {
				t.Fatalf("control claim %d=%+v expected=%+v err=%v", index, claimed, expected, err)
			}
			if err := repository.AcceptMailboxItem(context.Background(), guard, claimed.ID, domain.MailboxStateClaimed, domain.MailboxStateAccepted,
				journalEvent(fmt.Sprintf("event-accept-lane-control-%d", index), "mailbox.accepted", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
				t.Fatal(err)
			}
		}
	})

	t.Run("work sequence ascending", func(t *testing.T) {
		repository, _ := openTestRepository(t, nil)
		fixture := seedRepository(t, repository)
		descriptor := messageDescriptor(openruntime.SteerNative)
		_, guard := registerMessageWorker(t, repository, fixture, descriptor)
		first := createTask(t, repository, fixture, "lane-work-first")
		second := createTask(t, repository, fixture, "lane-work-second")
		for index, expected := range []domain.MailboxItem{first.MailboxItem, second.MailboxItem} {
			claimed, err := repository.TryClaimMailbox(context.Background(), guard, 1, repositoryTestTime.Add(30*time.Minute),
				journalEvent(fmt.Sprintf("event-claim-lane-work-%d", index), "mailbox.claimed", fixture.ownerPrincipal, fixture.organizationID))
			if err != nil || claimed == nil || claimed.ID != expected.ID {
				t.Fatalf("work claim %d=%+v expected=%+v err=%v", index, claimed, expected, err)
			}
			if err := repository.AcceptMailboxItem(context.Background(), guard, claimed.ID, domain.MailboxStateClaimed, domain.MailboxStateSuperseded,
				journalEvent(fmt.Sprintf("event-supersede-lane-work-%d", index), "mailbox.superseded", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
				t.Fatal(err)
			}
		}
	})
}
