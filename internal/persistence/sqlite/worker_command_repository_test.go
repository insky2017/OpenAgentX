package sqlite

import (
	"context"
	"testing"
	"time"

	"openagentx/internal/domain"
)

func TestWorkerCommandLifecycleParsesPersistedTimestamps(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	worker := &domain.WorkerInstance{
		ID: "worker-command-lifecycle", AgentID: fixture.agentID, Generation: 1,
		Transport: domain.WorkerTransportUnix, AuthenticatedPrincipal: fixture.ownerPrincipal,
		Capabilities: []string{"fake"}, Status: domain.WorkerStatusOnline,
		LeaseUntil: repositoryTestTime.Add(time.Hour), FencingToken: 1,
	}
	if err := repository.CreateWorkerInstance(context.Background(), worker,
		journalEvent("event-worker-command", "worker.registered", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}

	command := &domain.WorkerCommand{
		ID: "worker-command-health-check", WorkerInstanceID: worker.ID, Generation: worker.Generation,
		Kind: domain.WorkerCommandHealthCheck, State: domain.WorkerCommandPending,
		RequestedBy: fixture.ownerPrincipal, IdempotencyKey: "health-check-command-key",
	}
	created, err := repository.CreateWorkerCommand(context.Background(), command,
		journalEvent("event-worker-command-created", "worker_command.created", fixture.ownerPrincipal, fixture.organizationID))
	if err != nil || created.CreatedAt.IsZero() {
		t.Fatalf("created command=%+v err=%v", created, err)
	}

	leaseUntil := repositoryTestTime.Add(time.Minute)
	claimed, err := repository.ClaimWorkerCommand(context.Background(), worker.ID, worker.Generation, leaseUntil,
		journalEvent("event-worker-command-claimed", "worker_command.claimed", fixture.ownerPrincipal, fixture.organizationID))
	if err != nil {
		t.Fatalf("claim Worker command: %v", err)
	}
	if claimed == nil || claimed.State != domain.WorkerCommandClaimed || claimed.Attempts != 1 ||
		claimed.CreatedAt.IsZero() || claimed.ClaimedAt == nil || claimed.LeaseUntil == nil {
		t.Fatalf("claimed command=%+v", claimed)
	}

	if err := repository.AcknowledgeWorkerCommand(context.Background(), worker.ID, worker.Generation,
		command.ID, domain.WorkerCommandApplied, "healthy",
		journalEvent("event-worker-command-applied", "worker_command.acknowledged", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatalf("acknowledge Worker command: %v", err)
	}
	var state domain.WorkerCommandState
	var attempts int
	var result string
	if err := repository.db.QueryRow(`SELECT state, attempts, result FROM worker_commands WHERE worker_command_id=?`, command.ID).
		Scan(&state, &attempts, &result); err != nil {
		t.Fatal(err)
	}
	if state != domain.WorkerCommandApplied || attempts != 1 || result != "healthy" {
		t.Fatalf("persisted command state=%s attempts=%d result=%q", state, attempts, result)
	}
}
