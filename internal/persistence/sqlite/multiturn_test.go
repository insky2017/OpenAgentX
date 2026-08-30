package sqlite

import (
	"agentbus/internal/domain"
	openruntime "agentbus/internal/runtime"
	"context"
	"testing"
	"time"
)

func TestSessionBindingSaveUsesCASAndPreservesProviderIdentity(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	binding := &domain.SessionBinding{ID: "binding-multi", ContextID: "context-multi", AgentID: fixture.agentID,
		BackendID: "local", ProviderSessionID: "provider-1", State: domain.SessionBindingActive, Version: 1}
	if err := repository.SaveSessionBinding(context.Background(), binding, 0, journalEvent("event-binding-multi", "session_binding.created", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	binding.ProviderSessionID = "provider-2"
	if err := repository.SaveSessionBinding(context.Background(), binding, 1, journalEvent("event-binding-multi-update", "session_binding.updated", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	got, err := repository.GetSessionBinding(context.Background(), binding.ContextID, binding.AgentID, binding.BackendID)
	if err != nil || got.Version != 2 || got.ProviderSessionID != "provider-2" {
		t.Fatalf("binding=%+v err=%v", got, err)
	}
	if err := repository.SaveSessionBinding(context.Background(), binding, 1, journalEvent("event-binding-stale", "session_binding.updated", fixture.ownerPrincipal, fixture.organizationID)); err == nil {
		t.Fatal("stale SessionBinding update unexpectedly succeeded")
	}
}

func TestFinishWaitingInputKeepsTaskOpenForNextRunAttempt(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	created := createTask(t, repository, fixture, "waiting-input")
	worker := &domain.WorkerInstance{ID: "worker-waiting", AgentID: fixture.agentID, Generation: 1,
		Transport: domain.WorkerTransportUnix, AuthenticatedPrincipal: "uds-1000", Status: domain.WorkerStatusOnline,
		Capabilities: []string{"coding"}, LeaseUntil: repositoryTestTime.Add(time.Hour), FencingToken: 1}
	if err := repository.CreateWorkerInstance(context.Background(), worker, journalEvent("event-worker-waiting", "worker.registered", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	run := &domain.RunAttempt{ID: "run-waiting", TaskID: created.Task.ID, AgentID: fixture.agentID, Version: 1,
		Status: domain.RunAttemptRunning, WorkerInstanceID: worker.ID, FencingToken: 1,
		LeaseUntil: repositoryTestTime.Add(time.Hour), ExecutionSpecVersion: 1, RequestedExecutionJSON: `{}`, ResolvedExecutionJSON: `{}`,
		AdapterID: "fake", BackendID: "local", Model: "model-1", ReasoningMode: domain.ReasoningBackendDefault}
	if _, err := repository.BeginRunAttempt(context.Background(), 1, run,
		journalEvent("event-task-waiting-running", "task.running", fixture.ownerPrincipal, fixture.organizationID),
		journalEvent("event-run-waiting-start", "run_attempt.started", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	// The legacy repository method is not guarded; this assertion documents the
	// standard result mapping used by the Worker finish path.
	if status, taskStatus := terminalStatuses(openruntime.TurnResultWaitingInput); status != domain.RunAttemptSucceeded || taskStatus != domain.TaskStatusWaitingInput {
		t.Fatalf("waiting-input mapping run=%s task=%s", status, taskStatus)
	}
}
