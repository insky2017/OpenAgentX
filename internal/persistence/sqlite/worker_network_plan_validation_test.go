package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

// TestN1ValidationRebindAfterPlanRejectsClaimedBegin proves that a network
// snapshot produced before a binding rebind cannot commit a RunAttempt.
func TestN1ValidationRebindAfterPlanRejectsClaimedBegin(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	createPublishedNetworkBinding(t, repository, fixture)
	worker, guard, bindings := registerNetworkWorker(t, repository, fixture)
	application := networkApplication(bindings[0], "applied", "event-n1-validation-applied")
	if _, err := repository.HeartbeatWorker(context.Background(), guard, domain.WorkerStatusOnline,
		map[string]openruntime.BackendHealth{"local": openruntime.BackendHealthy}, guard.CheckedAt.Add(time.Hour),
		guard.CheckedAt.Add(time.Hour), []domain.NetworkBindingApplication{application},
		journalEvent("event-n1-validation-heartbeat", "worker.heartbeat", fixture.agentPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	backends, err := repository.ListWorkerBackends(context.Background(), worker.ID)
	if err != nil || len(backends) != 1 || backends[0].Health != openruntime.BackendHealthy {
		t.Fatalf("planned Backend=%+v err=%v", backends, err)
	}
	created := createTask(t, repository, fixture, "n1-validation-rebind")
	claimed, err := repository.TryClaimMailbox(context.Background(), guard, 1, guard.CheckedAt.Add(time.Hour),
		journalEvent("event-n1-validation-claim", "mailbox.claimed", fixture.agentPrincipal, fixture.organizationID))
	if err != nil || claimed == nil {
		t.Fatalf("claim work item=%+v err=%v", claimed, err)
	}
	resolvedJSON, err := json.Marshal(domain.ResolvedExecutionSpec{Version: 1, Spec: domain.ExecutionSpec{
		AdapterID: backends[0].Descriptor.AdapterID, BackendID: backends[0].BackendID, Model: backends[0].Descriptor.Models[0],
		Reasoning: domain.ReasoningSpec{Mode: domain.ReasoningBackendDefault},
		Session: domain.SessionSpec{Mode: domain.SessionModeNew, ContextID: created.Task.ID}, Timeout: time.Minute,
		Network: backends[0].Network,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.BindNetworkProfile(context.Background(), &domain.NetworkBinding{
		AgentID: fixture.agentID, BackendID: "local", ProfileID: bindings[0].ProfileID,
		ProfileVersion: bindings[0].ProfileVersion, Version: 1, DesiredStatus: "pending", UpdatedAt: guard.CheckedAt.Add(time.Minute),
	}, bindings[0].Version); err != nil {
		t.Fatal(err)
	}
	run := &domain.RunAttempt{
		ID: "run-n1-validation-rebind", TaskID: created.Task.ID, AgentID: fixture.agentID, Version: 1,
		Status: domain.RunAttemptStarting, WorkerInstanceID: worker.ID, FencingToken: worker.FencingToken,
		LeaseUntil: guard.CheckedAt.Add(time.Hour), ExecutionSpecVersion: 1, RequestedExecutionJSON: `{}`,
		ResolvedExecutionJSON: string(resolvedJSON), AdapterID: backends[0].Descriptor.AdapterID, BackendID: "local",
		Model: backends[0].Descriptor.Models[0], ReasoningMode: domain.ReasoningBackendDefault,
	}
	_, _, err = repository.BeginClaimedRunAttempt(context.Background(), guard, claimed.ID, created.Task.Version, run, "", nil,
		journalEvent("event-n1-validation-task", "task.running", fixture.agentPrincipal, fixture.organizationID),
		journalEvent("event-n1-validation-run", "run_attempt.started", fixture.agentPrincipal, fixture.organizationID),
		journalEvent("event-n1-validation-mailbox", "mailbox.accepted", fixture.agentPrincipal, fixture.organizationID))
	if !errors.Is(err, domain.ErrUnsupportedCapability) {
		t.Fatalf("old network snapshot Begin error=%v", err)
	}
	persistedTask, taskErr := repository.GetTask(context.Background(), created.Task.ID)
	persistedMailbox, mailboxErr := repository.GetMailboxItem(context.Background(), claimed.ID)
	if taskErr != nil || persistedTask.Status != domain.TaskStatusQueued || persistedTask.Version != created.Task.Version {
		t.Fatalf("rejected Begin changed Task=%+v err=%v", persistedTask, taskErr)
	}
	if mailboxErr != nil || persistedMailbox.State != domain.MailboxStateClaimed {
		t.Fatalf("rejected Begin changed mailbox=%+v err=%v", persistedMailbox, mailboxErr)
	}
	if _, runErr := repository.GetRunAttempt(context.Background(), run.ID); !errors.Is(runErr, domain.ErrNotFound) {
		t.Fatalf("rejected Begin persisted RunAttempt: %v", runErr)
	}
}
