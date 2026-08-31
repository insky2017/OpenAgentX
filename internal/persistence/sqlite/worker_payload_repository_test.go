package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

func setupClaimedMessagePayload(t *testing.T) (*Repository, domain.WorkerWriteGuard, string, string) {
	t.Helper()
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	worker, guard := registerMessageWorker(t, repository, fixture, messageDescriptor(openruntime.SteerQueued))
	created := createTask(t, repository, fixture, "guarded-payload")
	messageID := "message-guarded-payload"
	leaseUntil := repositoryTestTime.Add(10 * time.Minute)
	if _, err := repository.db.Exec(`UPDATE mailbox_items SET kind='message', message_id=?, state='claimed',
		worker_instance_id=?, fencing_token=?, lease_until=? WHERE mailbox_item_id=?`,
		messageID, worker.ID, worker.FencingToken, formatTime(leaseUntil), created.MailboxItem.ID); err != nil {
		t.Fatal(err)
	}
	return repository, guard, created.MailboxItem.ID, messageID
}

func TestResolveMailboxPayloadGuardAuthority(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Repository, *domain.WorkerWriteGuard)
		want   error
	}{
		{name: "wrong principal", mutate: func(_ *Repository, guard *domain.WorkerWriteGuard) {
			guard.PrincipalID = "wrong-principal"
		}, want: domain.ErrUnauthorized},
		{name: "wrong token", mutate: func(_ *Repository, guard *domain.WorkerWriteGuard) {
			guard.SessionTokenDigest = "wrong-token-digest"
		}, want: domain.ErrUnauthorized},
		{name: "stale generation", mutate: func(_ *Repository, guard *domain.WorkerWriteGuard) {
			guard.Generation++
		}, want: domain.ErrSessionGenerationConflict},
		{name: "stale fencing", mutate: func(_ *Repository, guard *domain.WorkerWriteGuard) {
			guard.FencingToken++
		}, want: domain.ErrFencingRejected},
		{name: "expired token", mutate: func(repository *Repository, guard *domain.WorkerWriteGuard) {
			if _, err := repository.db.Exec(`UPDATE worker_instances SET session_token_expires_at=? WHERE worker_instance_id=?`,
				formatTime(guard.CheckedAt), guard.WorkerInstanceID); err != nil {
				t.Fatal(err)
			}
		}, want: domain.ErrUnauthorized},
		{name: "expired Worker lease", mutate: func(repository *Repository, guard *domain.WorkerWriteGuard) {
			if _, err := repository.db.Exec(`UPDATE worker_instances SET lease_until=? WHERE worker_instance_id=?`,
				formatTime(guard.CheckedAt), guard.WorkerInstanceID); err != nil {
				t.Fatal(err)
			}
		}, want: domain.ErrLeaseExpired},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository, guard, itemID, _ := setupClaimedMessagePayload(t)
			test.mutate(repository, &guard)
			_, err := repository.ResolveMailboxPayload(context.Background(), guard, itemID)
			if !errors.Is(err, test.want) {
				t.Fatalf("ResolveMailboxPayload error=%v want=%v", err, test.want)
			}
		})
	}
}

func TestResolveMailboxPayloadReturnsClaimedMessage(t *testing.T) {
	repository, guard, itemID, messageID := setupClaimedMessagePayload(t)
	payload, err := repository.ResolveMailboxPayload(context.Background(), guard, itemID)
	if err != nil {
		t.Fatal(err)
	}
	if payload == nil || payload.Message == nil || payload.Message.ID != messageID || payload.ApprovalDecision != nil {
		t.Fatalf("payload=%+v", payload)
	}
}

func TestResolveMailboxPayloadReturnsClaimedApprovalDecision(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	descriptor := messageDescriptor(openruntime.SteerNative)
	worker, guard := registerMessageWorker(t, repository, fixture, descriptor)
	created, run := beginMessageRun(t, repository, fixture, worker, descriptor, "guarded-approval")
	request := &domain.ApprovalRequest{
		ID: "approval-request-guarded-payload", TaskID: created.Task.ID, Mode: domain.ApprovalModeNative,
		TargetRunID: run.ID, ExpectedRunVersion: run.Version, ScopeDigest: "approval-scope-guarded-payload",
		State: domain.ApprovalRequestPending, ExpiresAt: repositoryTestTime.Add(time.Hour),
	}
	if err := repository.CreateApprovalRequest(context.Background(), request,
		journalEvent("event-approval-request-guarded-payload", "approval.created", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	decision := &domain.ApprovalDecision{
		ID: "approval-decision-guarded-payload", ApprovalRequestID: request.ID,
		DecidedBy: fixture.ownerPrincipal, Decision: domain.ApprovalDecisionApprove,
		State: domain.ApprovalDecisionPersisted, IdempotencyKey: "approval-decision-guarded-payload-key",
	}
	_, item, err := repository.DecideApproval(context.Background(), request.ID, decision,
		&domain.MailboxItem{ID: "mailbox-approval-guarded-payload", TargetAgentID: fixture.agentID},
		journalEvent("event-approval-decision-guarded-payload", "approval.decided", fixture.ownerPrincipal, fixture.organizationID),
		journalEvent("event-approval-mailbox-guarded-payload", "mailbox.created", fixture.ownerPrincipal, fixture.organizationID))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repository.TryClaimMailbox(context.Background(), guard, 0, repositoryTestTime.Add(10*time.Minute),
		journalEvent("event-approval-mailbox-claimed", "mailbox.claimed", fixture.ownerPrincipal, fixture.organizationID))
	if err != nil || claimed == nil || claimed.ID != item.ID {
		t.Fatalf("claimed=%+v item=%+v err=%v", claimed, item, err)
	}
	payload, err := repository.ResolveMailboxPayload(context.Background(), guard, claimed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if payload == nil || payload.ApprovalDecision == nil || payload.ApprovalDecision.ID != decision.ID || payload.Message != nil {
		t.Fatalf("payload=%+v", payload)
	}
}
