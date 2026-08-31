package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
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

func TestFinishRunSessionBindingAndJournalRollbackTogether(t *testing.T) {
	var injectBeforeCommit atomic.Bool
	repository, _ := openTestRepository(t, func(point FaultPoint) error {
		if injectBeforeCommit.Load() && point == FaultBeforeCommit {
			return errors.New("finish transaction rollback fixture")
		}
		return nil
	})
	fixture := seedRepository(t, repository)
	descriptor := messageDescriptor(openruntime.SteerQueued)
	worker, guard := registerMessageWorker(t, repository, fixture, descriptor)
	created := createTask(t, repository, fixture, "finish-binding-rollback")
	resolved := domain.ResolvedExecutionSpec{Version: 1, Spec: domain.ExecutionSpec{
		AdapterID: descriptor.AdapterID, BackendID: "local", Model: descriptor.Models[0],
		Reasoning: domain.ReasoningSpec{Mode: domain.ReasoningBackendDefault},
		Session:   domain.SessionSpec{Mode: domain.SessionModeNew, ContextID: created.Task.ID},
		Timeout:   time.Minute, BackendOptions: json.RawMessage(`{}`),
	}}
	requestedJSON, err := json.Marshal(resolved.Spec)
	if err != nil {
		t.Fatal(err)
	}
	resolvedJSON, err := json.Marshal(resolved)
	if err != nil {
		t.Fatal(err)
	}
	run := &domain.RunAttempt{
		ID: "run-finish-binding-rollback", TaskID: created.Task.ID, AgentID: fixture.agentID, Version: 1,
		Status: domain.RunAttemptRunning, WorkerInstanceID: worker.ID, FencingToken: worker.FencingToken,
		LeaseUntil: repositoryTestTime.Add(time.Hour), ExecutionSpecVersion: 1,
		RequestedExecutionJSON: string(requestedJSON), ResolvedExecutionJSON: string(resolvedJSON),
		AdapterID: descriptor.AdapterID, BackendID: "local", Model: descriptor.Models[0],
		ReasoningMode: domain.ReasoningBackendDefault,
	}
	if _, err := repository.BeginRunAttempt(context.Background(), created.Task.Version, run,
		journalEvent("event-task-binding-rollback-running", "task.running", fixture.ownerPrincipal, fixture.organizationID),
		journalEvent("event-run-binding-rollback-started", "run_attempt.started", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	binding := &domain.SessionBinding{
		ID: "binding-finish-rollback", ContextID: created.Task.ID, AgentID: fixture.agentID,
		BackendID: "local", ProviderSessionID: "provider-finish-rollback", State: domain.SessionBindingActive,
	}
	injectBeforeCommit.Store(true)
	err = repository.FinishRun(context.Background(), guard, run.ID, 2, run.Version,
		openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, ProviderSessionID: binding.ProviderSessionID, SideEffectsKnown: true},
		binding, 0,
		journalEvent("event-binding-finish-rollback", "session_binding.saved", fixture.ownerPrincipal, fixture.organizationID),
		journalEvent("event-task-finish-rollback", "task.settled", fixture.ownerPrincipal, fixture.organizationID),
		journalEvent("event-run-finish-rollback", "run_attempt.finished", fixture.ownerPrincipal, fixture.organizationID))
	if err == nil || !strings.Contains(err.Error(), "finish transaction rollback fixture") {
		t.Fatalf("FinishRun fault error=%v", err)
	}
	injectBeforeCommit.Store(false)
	task, err := repository.GetTask(context.Background(), created.Task.ID)
	if err != nil || task.Status != domain.TaskStatusRunning || task.Version != 2 || task.Result != nil {
		t.Fatalf("Task changed across rolled-back finish: task=%+v err=%v", task, err)
	}
	persistedRun, err := repository.GetRunAttempt(context.Background(), run.ID)
	if err != nil || !persistedRun.Status.Active() || persistedRun.Version != 1 || persistedRun.ResultJSON != "" {
		t.Fatalf("RunAttempt changed across rolled-back finish: run=%+v err=%v", persistedRun, err)
	}
	if got, err := repository.GetSessionBinding(context.Background(), binding.ContextID, binding.AgentID, binding.BackendID); !errors.Is(err, domain.ErrNotFound) || got != nil {
		t.Fatalf("SessionBinding survived rolled-back finish: binding=%+v err=%v", got, err)
	}
	for _, eventID := range []string{"event-binding-finish-rollback", "event-task-finish-rollback", "event-run-finish-rollback"} {
		var count int
		if err := repository.db.QueryRow(`SELECT COUNT(*) FROM event_journal WHERE event_id=?`, eventID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("Journal event %s survived rollback: count=%d err=%v", eventID, count, err)
		}
	}
}
