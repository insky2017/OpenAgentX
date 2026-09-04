package sqlite

import (
	"context"
	"errors"
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
	if _, err := repository.db.Exec(`UPDATE worker_instances SET session_token_digest=?, session_token_expires_at=? WHERE worker_instance_id=?`,
		"worker-command-token-digest", formatTime(repositoryTestTime.Add(time.Hour)), worker.ID); err != nil {
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
	claimed, err := repository.ClaimWorkerCommand(context.Background(), domain.WorkerWriteGuard{
		WorkerInstanceID: worker.ID, AgentID: worker.AgentID, PrincipalID: worker.AuthenticatedPrincipal,
		SessionTokenDigest: "worker-command-token-digest", Generation: worker.Generation,
		FencingToken: worker.FencingToken, CheckedAt: repositoryTestTime,
	}, leaseUntil,
		journalEvent("event-worker-command-claimed", "worker_command.claimed", fixture.ownerPrincipal, fixture.organizationID))
	if err != nil {
		t.Fatalf("claim Worker command: %v", err)
	}
	if claimed == nil || claimed.State != domain.WorkerCommandClaimed || claimed.Attempts != 1 ||
		claimed.CreatedAt.IsZero() || claimed.ClaimedAt == nil || claimed.LeaseUntil == nil {
		t.Fatalf("claimed command=%+v", claimed)
	}

	if err := repository.AcknowledgeWorkerCommand(context.Background(), domain.WorkerWriteGuard{
		WorkerInstanceID: worker.ID, AgentID: worker.AgentID, PrincipalID: worker.AuthenticatedPrincipal,
		SessionTokenDigest: "worker-command-token-digest", Generation: worker.Generation,
		FencingToken: worker.FencingToken, CheckedAt: repositoryTestTime,
	},
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
	if err := repository.AcknowledgeWorkerCommand(context.Background(), domain.WorkerWriteGuard{
		WorkerInstanceID: worker.ID, AgentID: worker.AgentID, PrincipalID: worker.AuthenticatedPrincipal,
		SessionTokenDigest: "worker-command-token-digest", Generation: worker.Generation,
		FencingToken: worker.FencingToken, CheckedAt: repositoryTestTime,
	}, command.ID, domain.WorkerCommandApplied, "healthy", journalEvent("event-worker-command-applied-retry", "worker_command.acknowledged", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatalf("duplicate Worker command ACK must replay idempotently: %v", err)
	}
	var journalCount int
	if err := repository.db.QueryRow(`SELECT COUNT(*) FROM event_journal WHERE aggregate_id=? AND event_type='worker_command.acknowledged'`, command.ID).Scan(&journalCount); err != nil {
		t.Fatal(err)
	}
	if journalCount != 1 {
		t.Fatalf("duplicate Worker command ACK appended journal count=%d", journalCount)
	}
}

func TestAcknowledgeWorkerCommandRejectsInvalidGuardWithoutUpdating(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Repository, *domain.WorkerWriteGuard)
		want   error
	}{
		{name: "wrong-token", mutate: func(_ *Repository, guard *domain.WorkerWriteGuard) {
			guard.SessionTokenDigest = "wrong-command-token-digest"
		}, want: domain.ErrUnauthorized},
		{name: "stale-fencing", mutate: func(_ *Repository, guard *domain.WorkerWriteGuard) {
			guard.FencingToken++
		}, want: domain.ErrFencingRejected},
		{name: "expired-Worker-lease", mutate: func(repository *Repository, guard *domain.WorkerWriteGuard) {
			if _, err := repository.db.Exec(`UPDATE worker_instances SET lease_until=? WHERE worker_instance_id=?`,
				formatTime(guard.CheckedAt), guard.WorkerInstanceID); err != nil {
				t.Fatal(err)
			}
		}, want: domain.ErrLeaseExpired},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository, fixture, worker, guard, command := setupClaimedWorkerCommand(t, test.name)
			test.mutate(repository, &guard)
			err := repository.AcknowledgeWorkerCommand(context.Background(), guard, command.ID,
				domain.WorkerCommandApplied, "healthy",
				journalEvent("event-worker-command-rejected-"+test.name, "worker_command.acknowledged", fixture.ownerPrincipal, fixture.organizationID))
			if !errors.Is(err, test.want) {
				t.Fatalf("AcknowledgeWorkerCommand error=%v want=%v", err, test.want)
			}
			var state domain.WorkerCommandState
			if err := repository.db.QueryRow(`SELECT state FROM worker_commands WHERE worker_command_id=?`, command.ID).Scan(&state); err != nil {
				t.Fatal(err)
			}
			if state != domain.WorkerCommandClaimed {
				t.Fatalf("rejected acknowledgement changed state to %s for Worker %+v", state, worker)
			}
		})
	}
}

func TestReleaseWorkerLeaseFencesAndAllowsOnlyReleasedStopAck(t *testing.T) {
	repository, fixture, worker, guard, command := setupClaimedWorkerCommand(t, "release")
	if _, err := repository.db.Exec(`UPDATE worker_commands SET kind='stop' WHERE worker_command_id=?`, command.ID); err != nil {
		t.Fatal(err)
	}
	released, err := repository.ReleaseWorkerLease(context.Background(), guard,
		journalEvent("event-worker-released", "worker.released", fixture.ownerPrincipal, fixture.organizationID))
	if err != nil {
		t.Fatal(err)
	}
	if released.Status != domain.WorkerStatusOffline || released.FencingToken != worker.FencingToken+1 || !released.LeaseUntil.Equal(repositoryTestTime) {
		t.Fatalf("released=%+v", released)
	}
	newGuard := guard
	newGuard.FencingToken++
	if err := repository.AcknowledgeReleasedWorkerCommand(context.Background(), newGuard, command.ID, domain.WorkerCommandApplied, "stopped",
		journalEvent("event-released-ack", "worker_command.acknowledged", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ReleaseWorkerLease(context.Background(), guard,
		journalEvent("event-worker-released-retry", "worker.released", fixture.ownerPrincipal, fixture.organizationID)); !errors.Is(err, domain.ErrFencingRejected) {
		t.Fatalf("duplicate release err=%v", err)
	}
	stale := newGuard
	stale.FencingToken++
	if err := repository.AcknowledgeReleasedWorkerCommand(context.Background(), stale, command.ID, domain.WorkerCommandApplied, "stopped",
		journalEvent("event-released-ack-stale", "worker_command.acknowledged", fixture.ownerPrincipal, fixture.organizationID)); !errors.Is(err, domain.ErrFencingRejected) {
		t.Fatalf("stale released ack err=%v", err)
	}
}

func setupClaimedWorkerCommand(t *testing.T, suffix string) (*Repository, repositoryFixture, *domain.WorkerInstance, domain.WorkerWriteGuard, *domain.WorkerCommand) {
	t.Helper()
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	worker := &domain.WorkerInstance{
		ID: "worker-command-" + suffix, AgentID: fixture.agentID, Generation: 1,
		Transport: domain.WorkerTransportUnix, AuthenticatedPrincipal: fixture.ownerPrincipal,
		Capabilities: []string{"fake"}, Status: domain.WorkerStatusOnline,
		LeaseUntil: repositoryTestTime.Add(time.Hour), FencingToken: 1,
	}
	if err := repository.CreateWorkerInstance(context.Background(), worker,
		journalEvent("event-worker-command-worker-"+suffix, "worker.registered", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	const digest = "worker-command-token-digest"
	if _, err := repository.db.Exec(`UPDATE worker_instances SET session_token_digest=?, session_token_expires_at=? WHERE worker_instance_id=?`,
		digest, formatTime(repositoryTestTime.Add(time.Hour)), worker.ID); err != nil {
		t.Fatal(err)
	}
	command := &domain.WorkerCommand{
		ID: "worker-command-guarded-" + suffix, WorkerInstanceID: worker.ID, Generation: worker.Generation,
		Kind: domain.WorkerCommandHealthCheck, State: domain.WorkerCommandPending,
		RequestedBy: fixture.ownerPrincipal, IdempotencyKey: "worker-command-guarded-key-" + suffix,
	}
	if _, err := repository.CreateWorkerCommand(context.Background(), command,
		journalEvent("event-worker-command-created-"+suffix, "worker_command.created", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	guard := domain.WorkerWriteGuard{
		WorkerInstanceID: worker.ID, AgentID: worker.AgentID, PrincipalID: worker.AuthenticatedPrincipal,
		SessionTokenDigest: digest, Generation: worker.Generation, FencingToken: worker.FencingToken,
		CheckedAt: repositoryTestTime,
	}
	claimed, err := repository.ClaimWorkerCommand(context.Background(), guard, repositoryTestTime.Add(time.Minute),
		journalEvent("event-worker-command-claimed-"+suffix, "worker_command.claimed", fixture.ownerPrincipal, fixture.organizationID))
	if err != nil {
		t.Fatal(err)
	}
	return repository, fixture, worker, guard, claimed
}
