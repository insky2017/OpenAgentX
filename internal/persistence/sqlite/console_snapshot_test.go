package sqlite

import (
	"context"
	"fmt"
	"testing"
	"time"

	"openagentx/internal/domain"
)

func TestConsoleSnapshotReadsAgentWorkerBackendRunAndHighWaterTogether(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	createPublishedNetworkBinding(t, repository, fixture)
	worker, _, _ := registerNetworkWorker(t, repository, fixture)
	created := createTask(t, repository, fixture, "console-snapshot")
	run := &domain.RunAttempt{
		ID: "run-console-snapshot", TaskID: created.Task.ID, AgentID: fixture.agentID, Version: 1,
		Status: domain.RunAttemptRunning, WorkerInstanceID: worker.ID, FencingToken: worker.FencingToken,
		LeaseUntil: repositoryTestTime.Add(time.Hour), ExecutionSpecVersion: 1,
		RequestedExecutionJSON: `{}`, ResolvedExecutionJSON: `{}`, AdapterID: "fake", BackendID: "local",
		Model: "model-1", ReasoningMode: "effort", ReasoningValue: "high",
	}
	if _, err := repository.BeginRunAttempt(context.Background(), created.Task.Version, run,
		journalEvent("event-console-task-running", "task.running", fixture.ownerPrincipal, fixture.organizationID),
		journalEvent("event-console-run-started", "run_attempt.started", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	wantSequence, err := repository.LatestJournalSequence(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := repository.ConsoleSnapshot(context.Background(), fixture.agentID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Agent.ID != fixture.agentID || snapshot.Worker == nil || snapshot.Worker.ID != worker.ID ||
		snapshot.ActiveRun == nil || snapshot.ActiveRun.ID != run.ID || snapshot.SnapshotSequence != wantSequence ||
		snapshot.ActiveRunWorkerGeneration != worker.Generation || snapshot.BackendHealth["local"] != "unavailable" {
		t.Fatalf("incomplete Console snapshot: %+v", snapshot)
	}
}

func TestConsoleSnapshotConcurrentNPlusOneIsIncludedOrReplayable(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	worker := &domain.WorkerInstance{
		ID: "worker-console-race", AgentID: fixture.agentID, Generation: 1,
		Transport: domain.WorkerTransportUnix, AuthenticatedPrincipal: fixture.agentPrincipal,
		Capabilities: []string{"coding"}, Status: domain.WorkerStatusOnline,
		LeaseUntil: repositoryTestTime.Add(time.Hour), FencingToken: 1,
	}
	if err := repository.CreateWorkerInstance(context.Background(), worker,
		journalEvent("event-console-race-worker", "worker.registered", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	before, err := repository.LatestJournalSequence(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	writeAttempted := make(chan struct{})
	writeDone := make(chan error, 1)
	var committedSequence int64
	snapshot, err := repository.consoleSnapshot(context.Background(), fixture.agentID, func() {
		go func() {
			close(writeAttempted)
			tx, beginErr := repository.begin(context.Background())
			if beginErr != nil {
				writeDone <- beginErr
				return
			}
			defer tx.Rollback()
			changedAt := repositoryTestTime.Add(10 * time.Minute)
			if _, updateErr := tx.Exec(`UPDATE worker_instances SET status='draining', updated_at=?
				WHERE worker_instance_id=?`, formatTime(changedAt), worker.ID); updateErr != nil {
				writeDone <- updateErr
				return
			}
			result, insertErr := tx.Exec(`INSERT INTO event_journal (event_id, organization_id,
				aggregate_type, aggregate_id, event_type, actor_principal_id, payload_json, created_at)
				VALUES (?, ?, 'worker_instance', ?, 'worker.heartbeat', ?, '{}', ?)`,
				"event-console-race-n-plus-one", fixture.organizationID, worker.ID,
				fixture.ownerPrincipal, formatTime(changedAt))
			if insertErr != nil {
				writeDone <- insertErr
				return
			}
			committedSequence, insertErr = result.LastInsertId()
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
		t.Fatal("concurrent N+1 commit did not finish")
	}
	if committedSequence != before+1 {
		t.Fatalf("concurrent sequence=%d want=%d", committedSequence, before+1)
	}

	if snapshot.Worker != nil && snapshot.Worker.Status == domain.WorkerStatusDraining {
		if snapshot.SnapshotSequence < committedSequence {
			t.Fatalf("snapshot included draining state but cursor=%d is before event=%d", snapshot.SnapshotSequence, committedSequence)
		}
		return
	}
	events, listErr := repository.ListJournal(context.Background(), snapshot.SnapshotSequence, 100)
	if listErr != nil {
		t.Fatal(listErr)
	}
	found := false
	for _, event := range events {
		if event.Sequence == committedSequence && event.ID == "event-console-race-n-plus-one" {
			found = true
		}
	}
	if !found {
		t.Fatalf("snapshot cursor %d skipped concurrent event %d: %s", snapshot.SnapshotSequence, committedSequence, fmt.Sprint(events))
	}
	fresh, err := repository.ConsoleSnapshot(context.Background(), fixture.agentID)
	if err != nil || fresh.Worker == nil || fresh.Worker.Status != domain.WorkerStatusDraining || fresh.SnapshotSequence < committedSequence {
		t.Fatalf("fresh snapshot does not explain N+1: snapshot=%+v err=%v", fresh, err)
	}
}

func TestJournalSequenceBoundsReportsEmptyAndRetainedRange(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	bounds, err := repository.JournalSequenceBounds(context.Background())
	if err != nil || bounds.Earliest != 0 || bounds.Latest != 0 {
		t.Fatalf("empty bounds=%+v err=%v", bounds, err)
	}
	fixture := seedRepository(t, repository)
	events, err := repository.ListJournal(context.Background(), 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	bounds, err = repository.JournalSequenceBounds(context.Background())
	if err != nil || bounds.Earliest != events[0].Sequence || bounds.Latest != events[len(events)-1].Sequence {
		t.Fatalf("retained bounds=%+v events=%+v err=%v fixture=%+v", bounds, events, err, fixture)
	}
}
