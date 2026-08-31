package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"openagentx/internal/domain"
)

func TestReconcileExpiredRequeuesClaimsAndMarksRunUncertain(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	daemon := &domain.Principal{ID: "daemon", Kind: domain.PrincipalSystem, DisplayName: "OpenAgentX daemon", Status: domain.IdentityActive}
	if err := repository.CreatePrincipal(context.Background(), daemon, journalEvent("event-principal-daemon-recovery", "principal.created", fixture.ownerPrincipal, "")); err != nil {
		t.Fatal(err)
	}
	created := createTask(t, repository, fixture, "recovery")
	worker := &domain.WorkerInstance{ID: "worker-recovery", AgentID: fixture.agentID, Generation: 1,
		Transport: domain.WorkerTransportUnix, AuthenticatedPrincipal: fixture.ownerPrincipal, Status: domain.WorkerStatusOnline,
		Capabilities: []string{"coding"}, LeaseUntil: repositoryTestTime.Add(-time.Minute), FencingToken: 1}
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
	events, err := repository.ListJournal(context.Background(), 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	expected := []struct {
		aggregateType string
		aggregateID   string
		eventType     string
	}{
		{"mailbox_item", created.MailboxItem.ID, "mailbox.reclaimed"},
		{"worker_instance", worker.ID, "worker.offline"},
		{"run_attempt", run.ID, "run_attempt.uncertain"},
		{"task", created.Task.ID, "task.uncertain"},
	}
	seen := make([]domain.JournalEvent, 0, len(expected))
	for _, event := range events {
		for _, want := range expected {
			if event.AggregateType == want.aggregateType && event.AggregateID == want.aggregateID && event.EventType == want.eventType {
				seen = append(seen, event)
			}
		}
	}
	if len(seen) != len(expected) {
		t.Fatalf("recovery journal events=%d, want %d: %+v", len(seen), len(expected), seen)
	}
	for index, event := range seen {
		if index > 0 && event.Sequence <= seen[index-1].Sequence {
			t.Fatalf("recovery journal sequence is not increasing: %+v", seen)
		}
		if event.ActorPrincipalID != daemon.ID {
			t.Fatalf("recovery event actor=%q, want %q", event.ActorPrincipalID, daemon.ID)
		}
	}

	// A cancellation intent remains authoritative when the expired run is
	// reconciled; the task transition is journaled as canceled.
	cancelTask := createTask(t, repository, fixture, "recovery-cancel")
	cancelWorker := &domain.WorkerInstance{ID: "worker-recovery-cancel", AgentID: fixture.agentID, Generation: 2,
		Transport: domain.WorkerTransportUnix, AuthenticatedPrincipal: fixture.ownerPrincipal, Status: domain.WorkerStatusOnline,
		Capabilities: []string{"coding"}, LeaseUntil: repositoryTestTime.Add(-time.Minute), FencingToken: 2}
	if err := repository.CreateWorkerInstance(context.Background(), cancelWorker, journalEvent("event-worker-recovery-cancel", "worker.registered", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	cancelRun := &domain.RunAttempt{ID: "run-recovery-cancel", TaskID: cancelTask.Task.ID, AgentID: fixture.agentID, Version: 1,
		Status: domain.RunAttemptRunning, WorkerInstanceID: cancelWorker.ID, FencingToken: 2, LeaseUntil: repositoryTestTime.Add(-time.Minute),
		ExecutionSpecVersion: 1, RequestedExecutionJSON: `{}`, ResolvedExecutionJSON: `{}`, AdapterID: "fake", BackendID: "local", Model: "model-1", ReasoningMode: domain.ReasoningBackendDefault}
	if _, err := repository.BeginRunAttempt(context.Background(), cancelTask.Task.Version, cancelRun,
		journalEvent("event-task-recovery-cancel-running", "task.running", fixture.ownerPrincipal, fixture.organizationID),
		journalEvent("event-run-recovery-cancel", "run_attempt.started", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.db.Exec(`UPDATE tasks SET status='cancel_requested' WHERE task_id=?`, cancelTask.Task.ID); err != nil {
		t.Fatal(err)
	}
	if err := repository.ReconcileExpired(context.Background()); err != nil {
		t.Fatal(err)
	}
	canceled, err := repository.GetTask(context.Background(), cancelTask.Task.ID)
	if err != nil || canceled.Status != domain.TaskStatusCanceled {
		t.Fatalf("canceled recovery task=%+v err=%v", canceled, err)
	}
	var canceledEvents int
	if err := repository.db.QueryRow(`SELECT COUNT(*) FROM event_journal WHERE aggregate_type='task' AND aggregate_id=? AND event_type='task.canceled'`, cancelTask.Task.ID).Scan(&canceledEvents); err != nil || canceledEvents != 1 {
		t.Fatalf("canceled recovery event count=%d err=%v", canceledEvents, err)
	}
}

func TestReconcileExpiredRollsBackStateAndJournalTogether(t *testing.T) {
	trigger := false
	fault := func(point FaultPoint) error {
		if trigger && point == FaultBeforeCommit {
			return errors.New("recovery commit fault")
		}
		return nil
	}
	repository, _ := openTestRepository(t, fault)
	fixture := seedRepository(t, repository)
	if err := repository.CreatePrincipal(context.Background(), &domain.Principal{ID: "daemon", Kind: domain.PrincipalSystem, DisplayName: "OpenAgentX daemon", Status: domain.IdentityActive}, journalEvent("event-principal-daemon-recovery-rollback", "principal.created", fixture.ownerPrincipal, "")); err != nil {
		t.Fatal(err)
	}
	created := createTask(t, repository, fixture, "recovery-rollback")
	worker := &domain.WorkerInstance{ID: "worker-recovery-rollback", AgentID: fixture.agentID, Generation: 1,
		Transport: domain.WorkerTransportUnix, AuthenticatedPrincipal: fixture.ownerPrincipal, Status: domain.WorkerStatusOnline,
		Capabilities: []string{"coding"}, LeaseUntil: repositoryTestTime.Add(-time.Minute), FencingToken: 1}
	if err := repository.CreateWorkerInstance(context.Background(), worker, journalEvent("event-worker-recovery-rollback", "worker.registered", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	run := &domain.RunAttempt{ID: "run-recovery-rollback", TaskID: created.Task.ID, AgentID: fixture.agentID, Version: 1,
		Status: domain.RunAttemptRunning, WorkerInstanceID: worker.ID, FencingToken: 1, LeaseUntil: repositoryTestTime.Add(-time.Minute),
		ExecutionSpecVersion: 1, RequestedExecutionJSON: `{}`, ResolvedExecutionJSON: `{}`, AdapterID: "fake", BackendID: "local", Model: "model-1", ReasoningMode: domain.ReasoningBackendDefault}
	if _, err := repository.BeginRunAttempt(context.Background(), created.Task.Version, run,
		journalEvent("event-task-recovery-rollback-running", "task.running", fixture.ownerPrincipal, fixture.organizationID),
		journalEvent("event-run-recovery-rollback", "run_attempt.started", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.db.Exec(`UPDATE mailbox_items SET state='claimed', worker_instance_id=?, fencing_token=1, lease_until=? WHERE mailbox_item_id=?`, worker.ID, formatTime(repositoryTestTime.Add(-time.Minute)), created.MailboxItem.ID); err != nil {
		t.Fatal(err)
	}
	trigger = true
	beforeEvents, err := repository.ListJournal(context.Background(), 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ReconcileExpired(context.Background()); err == nil {
		t.Fatal("expected injected recovery failure")
	}
	item, _ := repository.GetMailboxItem(context.Background(), created.MailboxItem.ID)
	gotWorker, _ := repository.GetWorkerInstance(context.Background(), worker.ID)
	gotRun, _ := repository.GetRunAttempt(context.Background(), run.ID)
	gotTask, _ := repository.GetTask(context.Background(), created.Task.ID)
	if item.State != domain.MailboxStateClaimed || gotWorker.Status != domain.WorkerStatusOnline || gotRun.Status != domain.RunAttemptRunning || gotTask.Status != domain.TaskStatusRunning {
		t.Fatalf("recovery rollback did not restore state: item=%+v worker=%+v run=%+v task=%+v", item, gotWorker, gotRun, gotTask)
	}
	afterEvents, err := repository.ListJournal(context.Background(), 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterEvents) != len(beforeEvents) {
		t.Fatalf("recovery rollback journal count=%d, before=%d", len(afterEvents), len(beforeEvents))
	}
}
