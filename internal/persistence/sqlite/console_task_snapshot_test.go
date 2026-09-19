package sqlite

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"openagentx/internal/domain"
)

func TestListConsoleTasksUsesStableDescendingKeyset(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	for _, suffix := range []string{"a", "c", "b"} {
		createTask(t, repository, fixture, "console-list-"+suffix)
	}

	first, err := repository.ListConsoleTasks(context.Background(), fixture.agentID, domain.ConsoleTaskCursor{}, 2)
	if err != nil || len(first) != 2 || first[0].ID != "task-console-list-c" || first[1].ID != "task-console-list-b" {
		t.Fatalf("first Console Task page=%+v err=%v", first, err)
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, first[1].UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	second, err := repository.ListConsoleTasks(context.Background(), fixture.agentID,
		domain.ConsoleTaskCursor{UpdatedAt: updatedAt, TaskID: first[1].ID}, 2)
	if err != nil || len(second) != 1 || second[0].ID != "task-console-list-a" {
		t.Fatalf("second Console Task page=%+v err=%v", second, err)
	}
	if _, err := repository.ListConsoleTasks(context.Background(), "missing", domain.ConsoleTaskCursor{}, 2); !errors.Is(err, domain.ErrAgentNotFound) {
		t.Fatalf("missing Agent error=%v", err)
	}
}

func TestConsoleTaskSnapshotReadsOwnedStateAndHighWaterTogether(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	created := createTask(t, repository, fixture, "console-task-snapshot")
	worker := &domain.WorkerInstance{
		ID: "worker-console-task", AgentID: fixture.agentID, Generation: 7,
		Transport: domain.WorkerTransportUnix, AuthenticatedPrincipal: fixture.agentPrincipal,
		Capabilities: []string{"coding"}, Status: domain.WorkerStatusOnline,
		LeaseUntil: repositoryTestTime.Add(time.Hour), FencingToken: 7,
	}
	if err := repository.CreateWorkerInstance(context.Background(), worker,
		journalEvent("event-console-task-worker", "worker.registered", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	run := &domain.RunAttempt{
		ID: "run-console-task", TaskID: created.Task.ID, AgentID: fixture.agentID, Version: 1,
		Status: domain.RunAttemptRunning, WorkerInstanceID: worker.ID, FencingToken: worker.FencingToken,
		LeaseUntil: repositoryTestTime.Add(time.Hour), ExecutionSpecVersion: 1,
		RequestedExecutionJSON: `{}`, ResolvedExecutionJSON: `{}`, AdapterID: "fake", BackendID: "local",
		Model: "model-1", ReasoningMode: domain.ReasoningEffort, ReasoningValue: "high",
	}
	if _, err := repository.BeginRunAttempt(context.Background(), created.Task.Version, run,
		journalEvent("event-console-task-running", "task.running", fixture.ownerPrincipal, fixture.organizationID),
		journalEvent("event-console-task-run", "run_attempt.started", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	approval := &domain.ApprovalRequest{
		ID: "approval-console-task", TaskID: created.Task.ID, Mode: domain.ApprovalModeNative,
		TargetRunID: run.ID, ExpectedRunVersion: run.Version, ScopeDigest: "scope-console-task",
		State: domain.ApprovalRequestPending, ExpiresAt: repositoryTestTime.Add(time.Hour),
	}
	if err := repository.CreateApprovalRequest(context.Background(), approval,
		journalEvent("event-console-task-approval", "approval.created", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	wantSequence, err := repository.LatestJournalSequence(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := repository.ConsoleTaskSnapshot(context.Background(), fixture.agentID, created.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Task.ID != created.Task.ID || snapshot.WorkDelivery == nil || snapshot.WorkDelivery.ID != created.MailboxItem.ID ||
		snapshot.LatestRun == nil || snapshot.LatestRun.ID != run.ID || snapshot.LatestRunWorkerGeneration != worker.Generation ||
		snapshot.LatestMessage == nil || snapshot.LatestMessage.ID != "message-console-task-snapshot" ||
		snapshot.PendingApproval == nil || snapshot.PendingApproval.ID != approval.ID || snapshot.SnapshotSequence != wantSequence {
		t.Fatalf("incomplete Console Task snapshot: %+v", snapshot)
	}
	if _, err := repository.ConsoleTaskSnapshot(context.Background(), "other", created.Task.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-Agent snapshot error=%v", err)
	}
}

func TestConsoleTaskSnapshotConcurrentNPlusOneIsIncludedOrReplayable(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	created := createTask(t, repository, fixture, "console-task-race")
	before, err := repository.LatestJournalSequence(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	writeAttempted := make(chan struct{})
	writeDone := make(chan error, 1)
	var committedSequence int64
	snapshot, err := repository.consoleTaskSnapshot(context.Background(), fixture.agentID, created.Task.ID, func() {
		go func() {
			close(writeAttempted)
			tx, beginErr := repository.begin(context.Background())
			if beginErr != nil {
				writeDone <- beginErr
				return
			}
			defer tx.Rollback()
			changedAt := repositoryTestTime.Add(10 * time.Minute)
			if _, updateErr := tx.Exec(`UPDATE tasks SET status='running', version=version+1, updated_at=? WHERE task_id=?`,
				formatTime(changedAt), created.Task.ID); updateErr != nil {
				writeDone <- updateErr
				return
			}
			result, insertErr := tx.Exec(`INSERT INTO event_journal (event_id, organization_id,
				aggregate_type, aggregate_id, event_type, actor_principal_id, payload_json, created_at)
				VALUES (?, ?, 'task', ?, 'task.running', ?, '{}', ?)`, "event-console-task-race-n-plus-one",
				fixture.organizationID, created.Task.ID, fixture.ownerPrincipal, formatTime(changedAt))
			if insertErr == nil {
				committedSequence, insertErr = result.LastInsertId()
			}
			if insertErr == nil {
				insertErr = commit(tx)
			}
			writeDone <- insertErr
		}()
		<-writeAttempted
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent Console Task N+1 commit did not finish")
	}
	if committedSequence != before+1 {
		t.Fatalf("concurrent sequence=%d want=%d", committedSequence, before+1)
	}
	if snapshot.Task.Status == domain.TaskStatusRunning {
		if snapshot.Task.Version < 2 || snapshot.SnapshotSequence < committedSequence {
			t.Fatalf("snapshot included N+1 state without cursor: %+v", snapshot)
		}
		return
	}
	events, listErr := repository.ListJournal(context.Background(), snapshot.SnapshotSequence, 100)
	if listErr != nil {
		t.Fatal(listErr)
	}
	found := false
	for _, event := range events {
		if event.Sequence == committedSequence && event.ID == "event-console-task-race-n-plus-one" {
			found = true
		}
	}
	if !found {
		t.Fatalf("snapshot cursor %d skipped concurrent event %d: %s", snapshot.SnapshotSequence, committedSequence, fmt.Sprint(events))
	}
}
