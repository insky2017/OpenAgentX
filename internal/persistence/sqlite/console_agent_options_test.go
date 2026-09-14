package sqlite

import (
	"context"
	"testing"
	"time"

	"openagentx/internal/domain"
)

func TestConsoleAgentOptionsFenceActiveRunToDeterministicCurrentWorker(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	oldWorker := &domain.WorkerInstance{ID: "worker-selector-old", AgentID: fixture.agentID, Generation: 1,
		Transport: domain.WorkerTransportUnix, AuthenticatedPrincipal: fixture.agentPrincipal,
		Status: domain.WorkerStatusOnline, Capabilities: []string{"coding"},
		LeaseUntil: repositoryTestTime.Add(time.Hour), FencingToken: 1}
	if err := repository.CreateWorkerInstance(context.Background(), oldWorker,
		journalEvent("event-selector-old", "worker.registered", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	created := createTask(t, repository, fixture, "selector-old")
	run := &domain.RunAttempt{ID: "run-selector-old", TaskID: created.Task.ID, AgentID: fixture.agentID, Version: 1,
		Status: domain.RunAttemptRunning, WorkerInstanceID: oldWorker.ID, FencingToken: oldWorker.FencingToken,
		LeaseUntil: repositoryTestTime.Add(time.Hour), ExecutionSpecVersion: 1,
		RequestedExecutionJSON: `{}`, ResolvedExecutionJSON: `{}`, AdapterID: "fake", BackendID: "local",
		Model: "model-1", ReasoningMode: "effort", ReasoningValue: "high"}
	if _, err := repository.BeginRunAttempt(context.Background(), created.Task.Version, run,
		journalEvent("event-selector-task", "task.running", fixture.ownerPrincipal, fixture.organizationID),
		journalEvent("event-selector-run", "run_attempt.started", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	// Preserve the old active Run as delayed history while allowing the repository
	// to establish generation 2 as the sole current Worker for this read fixture.
	if _, err := repository.db.Exec(`UPDATE worker_instances SET status='offline' WHERE worker_instance_id=?`, oldWorker.ID); err != nil {
		t.Fatal(err)
	}
	currentWorker := &domain.WorkerInstance{ID: "worker-selector-current", AgentID: fixture.agentID, Generation: 2,
		Transport: domain.WorkerTransportUnix, AuthenticatedPrincipal: fixture.agentPrincipal,
		Status: domain.WorkerStatusDraining, Capabilities: []string{"coding"},
		LeaseUntil: repositoryTestTime.Add(2 * time.Hour), FencingToken: 2}
	if err := repository.CreateWorkerInstance(context.Background(), currentWorker,
		journalEvent("event-selector-current", "worker.registered", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}

	options, err := repository.ListConsoleAgentOptions(context.Background(), "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(options) != 1 {
		t.Fatalf("options=%+v", options)
	}
	option := options[0]
	if option.AgentID != fixture.agentID || option.OrganizationID != fixture.organizationID || option.DisplayName != "Quote Service" ||
		option.WorkerStatus != domain.WorkerStatusDraining || option.Generation != 2 || option.ActiveRunStatus != "" {
		t.Fatalf("unsafe or stale Console Agent projection: %+v", option)
	}
	if page, err := repository.ListConsoleAgentOptions(context.Background(), fixture.agentID, 100); err != nil || len(page) != 0 {
		t.Fatalf("cursor page=%+v err=%v", page, err)
	}
}
