package sqlite

import (
	"context"
	"testing"
	"time"

	"openagentx/internal/domain"
)

func TestReconcileExpiredRequeuesClaimsAndMarksRunUncertain(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	created := createTask(t, repository, fixture, "recovery")
	worker := &domain.WorkerInstance{ID: "worker-recovery", AgentID: fixture.agentID, Generation: 1,
		Transport: domain.WorkerTransportUnix, AuthenticatedPrincipal: fixture.ownerPrincipal, Status: domain.WorkerStatusOnline,
		Capabilities: []string{"coding"}, LeaseUntil: repositoryTestTime.Add(time.Hour), FencingToken: 1}
	if err := repository.CreateWorkerInstance(context.Background(), worker, journalEvent("event-worker-recovery", "worker.registered", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	run := &domain.RunAttempt{ID: "run-recovery", TaskID: created.Task.ID, AgentID: fixture.agentID, Version: 1,
		Status: domain.RunAttemptRunning, WorkerInstanceID: worker.ID, FencingToken: 1, LeaseUntil: repositoryTestTime.Add(-time.Minute),
		ExecutionSpecVersion: 1, RequestedExecutionJSON: `{}`, ResolvedExecutionJSON: `{}`, AdapterID: "fake", BackendID: "local", Model: "model-1", ReasoningMode: domain.ReasoningBackendDefault}
	if _, err := repository.BeginRunAttempt(context.Background(), created.Task.Version, run,
		journalEvent("event-task-recovery-running", "task.running", fixture.ownerPrincipal, fixture.organizationID),
		journalEvent("event-run-recovery", "run_attempt.started", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.db.Exec(`UPDATE mailbox_items SET state='claimed', worker_instance_id=?, fencing_token=1, lease_until=? WHERE mailbox_item_id=?`, worker.ID, formatTime(repositoryTestTime.Add(-time.Minute)), created.MailboxItem.ID); err != nil {
		t.Fatal(err)
	}
	if err := repository.ReconcileExpired(context.Background()); err != nil {
		t.Fatal(err)
	}
	item, err := repository.GetMailboxItem(context.Background(), created.MailboxItem.ID)
	if err != nil || item.State != domain.MailboxStatePending || item.WorkerInstanceID != "" {
		t.Fatalf("mailbox=%+v err=%v", item, err)
	}
	gotRun, err := repository.GetRunAttempt(context.Background(), run.ID)
	if err != nil || gotRun.Status != domain.RunAttemptUncertain {
		t.Fatalf("run=%+v err=%v", gotRun, err)
	}
	gotTask, err := repository.GetTask(context.Background(), created.Task.ID)
	if err != nil || gotTask.Status != domain.TaskStatusUncertain {
		t.Fatalf("task=%+v err=%v", gotTask, err)
	}
}
