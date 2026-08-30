package sqlite

import (
	"context"
	"testing"
	"time"

	"openagentx/internal/domain"
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

func TestNativeApprovalBecomesStaleWhenRunVersionChanges(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	request := &domain.ApprovalRequest{ID: "approval-native-stale", TaskID: "task-approval-stale", Mode: domain.ApprovalModeNative,
		TargetRunID: "run-approval-stale", ExpectedRunVersion: 2, ScopeDigest: "scope-digest", State: domain.ApprovalRequestPending,
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
