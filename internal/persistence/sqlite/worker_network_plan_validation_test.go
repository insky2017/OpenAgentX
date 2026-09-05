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

func TestSelfReportedNetworkMetadataWithoutControlBindingIsNotTrusted(t *testing.T) {
	ctx := context.Background()
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	descriptor := messageDescriptor(openruntime.SteerQueued)
	modePolicy := domain.NetworkModePolicy{
		AgentID: fixture.agentID, BackendID: "local", PolicyVersion: 1, Mode: domain.NetworkInherit,
	}
	selfReported := domain.NetworkPolicy{
		Mode: domain.NetworkInherit, PolicyVersion: 1, BindingRevision: 1,
		ManifestDigest: modePolicy.ComputeManifestDigest(), RuntimeIdentity: descriptor.RuntimeIdentity,
	}
	registration := domain.WorkerRegistration{
		WorkerInstanceID: "worker-self-reported-network", AgentID: fixture.agentID, Transport: domain.WorkerTransportUnix,
		PrincipalID: fixture.agentPrincipal, Capabilities: []string{"coding"}, SessionTokenDigest: "self-reported-token-digest",
		TokenExpiresAt: repositoryTestTime.Add(time.Hour), LeaseUntil: repositoryTestTime.Add(time.Hour),
	}
	worker, _, err := repository.RegisterWorker(ctx, registration, []openruntime.BackendRegistration{{
		BackendID: "local", Descriptor: descriptor, Health: openruntime.BackendHealthy, Network: selfReported,
	}}, journalEvent("event-self-reported-network", "worker.registered", fixture.ownerPrincipal, fixture.organizationID))
	if err != nil {
		t.Fatal(err)
	}
	backends, err := repository.ListWorkerBackends(ctx, worker.ID)
	if err != nil || len(backends) != 1 || backends[0].Health != openruntime.BackendUnavailable {
		t.Fatalf("self-reported Backend became schedulable: backends=%+v err=%v", backends, err)
	}
	resolved, err := json.Marshal(domain.ResolvedExecutionSpec{Version: 1, Spec: domain.ExecutionSpec{
		AdapterID: descriptor.AdapterID, BackendID: "local", Model: descriptor.Models[0],
		Reasoning: domain.ReasoningSpec{Mode: domain.ReasoningBackendDefault},
		Session:   domain.SessionSpec{Mode: domain.SessionModeNew, ContextID: "task-self-reported-network"},
		Timeout:   time.Minute, Network: selfReported,
	}})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := repository.begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	guard := domain.WorkerWriteGuard{
		WorkerInstanceID: worker.ID, AgentID: fixture.agentID, PrincipalID: fixture.agentPrincipal,
		SessionTokenDigest: registration.SessionTokenDigest, Generation: worker.Generation,
		FencingToken: worker.FencingToken, CheckedAt: repositoryTestTime,
	}
	run := &domain.RunAttempt{BackendID: "local", AdapterID: descriptor.AdapterID, ResolvedExecutionJSON: string(resolved)}
	if err := validateRunNetworkSnapshot(ctx, tx, guard, run); !errors.Is(err, domain.ErrUnsupportedCapability) {
		t.Fatalf("self-reported network snapshot validation error=%v", err)
	}
}

// TestLegacyNetworkRegistrationRejectsClaimedBegin proves that a path-based
// pre-N2 acknowledgement cannot make a Backend schedulable or start a new Run.
func TestLegacyNetworkRegistrationRejectsClaimedBegin(t *testing.T) {
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
	if err != nil || len(backends) != 1 || backends[0].Health != openruntime.BackendUnavailable {
		t.Fatalf("legacy Backend scheduling state=%+v err=%v", backends, err)
	}
	created := createTask(t, repository, fixture, "legacy-network-begin")
	leaseUntil := guard.CheckedAt.Add(time.Hour)
	if _, err := repository.db.Exec(`UPDATE mailbox_items SET state='claimed',worker_instance_id=?,fencing_token=?,lease_until=? WHERE mailbox_item_id=?`, worker.ID, worker.FencingToken, formatTime(leaseUntil), created.MailboxItem.ID); err != nil {
		t.Fatal(err)
	}
	resolvedJSON, err := json.Marshal(domain.ResolvedExecutionSpec{Version: 1, Spec: domain.ExecutionSpec{
		AdapterID: backends[0].Descriptor.AdapterID, BackendID: backends[0].BackendID, Model: backends[0].Descriptor.Models[0],
		Reasoning: domain.ReasoningSpec{Mode: domain.ReasoningBackendDefault},
		Session:   domain.SessionSpec{Mode: domain.SessionModeNew, ContextID: created.Task.ID}, Timeout: time.Minute,
		Network: backends[0].Network,
	}})
	if err != nil {
		t.Fatal(err)
	}
	run := &domain.RunAttempt{
		ID: "run-legacy-network", TaskID: created.Task.ID, AgentID: fixture.agentID, Version: 1,
		Status: domain.RunAttemptStarting, WorkerInstanceID: worker.ID, FencingToken: worker.FencingToken,
		LeaseUntil: guard.CheckedAt.Add(time.Hour), ExecutionSpecVersion: 1, RequestedExecutionJSON: `{}`,
		ResolvedExecutionJSON: string(resolvedJSON), AdapterID: backends[0].Descriptor.AdapterID, BackendID: "local",
		Model: backends[0].Descriptor.Models[0], ReasoningMode: domain.ReasoningBackendDefault,
	}
	_, _, err = repository.BeginClaimedRunAttempt(context.Background(), guard, created.MailboxItem.ID, created.Task.Version, run, "", nil,
		journalEvent("event-n1-validation-task", "task.running", fixture.agentPrincipal, fixture.organizationID),
		journalEvent("event-n1-validation-run", "run_attempt.started", fixture.agentPrincipal, fixture.organizationID),
		journalEvent("event-n1-validation-mailbox", "mailbox.accepted", fixture.agentPrincipal, fixture.organizationID))
	if !errors.Is(err, domain.ErrUnsupportedCapability) {
		t.Fatalf("old network snapshot Begin error=%v", err)
	}
	persistedTask, taskErr := repository.GetTask(context.Background(), created.Task.ID)
	persistedMailbox, mailboxErr := repository.GetMailboxItem(context.Background(), created.MailboxItem.ID)
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
