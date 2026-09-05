package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

func TestBeginClaimedRunConsumesPreflightApprovalInSameTransaction(t *testing.T) {
	var failCommit atomic.Bool
	repository, _ := openTestRepository(t, func(point FaultPoint) error {
		if failCommit.Load() && point == FaultBeforeCommit {
			return errors.New("forced preflight BeginAttempt rollback")
		}
		return nil
	})
	fixture := seedRepository(t, repository)
	descriptor := messageDescriptor(openruntime.SteerQueued)
	worker, guard := registerMessageWorker(t, repository, fixture, descriptor)
	resolvedJSON, err := json.Marshal(domain.ResolvedExecutionSpec{Version: 1, Spec: domain.ExecutionSpec{
		AdapterID: descriptor.AdapterID, BackendID: "local", Model: descriptor.Models[0],
		Reasoning: domain.ReasoningSpec{Mode: domain.ReasoningBackendDefault},
		Session:   domain.SessionSpec{Mode: domain.SessionModeNew, ContextID: "task-preflight-begin-atomic"},
		Timeout:   time.Minute, Network: trustedMessageNetwork(descriptor),
	}})
	if err != nil {
		t.Fatal(err)
	}
	created := createTask(t, repository, fixture, "preflight-begin-atomic")
	claimed, err := repository.TryClaimMailbox(context.Background(), guard, 1, repositoryTestTime.Add(30*time.Minute),
		journalEvent("event-claim-preflight-begin-atomic", "mailbox.claimed", fixture.ownerPrincipal, fixture.organizationID))
	if err != nil || claimed == nil || claimed.ID != created.MailboxItem.ID {
		t.Fatalf("claim preflight work=%+v err=%v", claimed, err)
	}
	approval := &domain.ApprovalRequest{
		ID: "approval-preflight-begin-atomic", TaskID: created.Task.ID, Mode: domain.ApprovalModePreflight,
		ScopeDigest: "scope-preflight-begin-atomic", State: domain.ApprovalRequestPending,
		ExpiresAt: repositoryTestTime.Add(time.Hour),
	}
	if err := repository.CreateApprovalRequest(context.Background(), approval,
		journalEvent("event-create-preflight-begin-atomic", "approval.created", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.DecideApproval(context.Background(), approval.ID, &domain.ApprovalDecision{
		ID: "decision-preflight-begin-atomic", ApprovalRequestID: approval.ID, DecidedBy: fixture.ownerPrincipal,
		Decision: domain.ApprovalDecisionApprove, State: domain.ApprovalDecisionPersisted,
		IdempotencyKey: "preflight-begin-atomic-idem",
	}, nil, journalEvent("event-decide-preflight-begin-atomic", "approval.decided", fixture.ownerPrincipal, fixture.organizationID), nil); err != nil {
		t.Fatal(err)
	}
	run := &domain.RunAttempt{
		ID: "run-preflight-begin-atomic", TaskID: created.Task.ID, AgentID: fixture.agentID, Version: 1,
		Status: domain.RunAttemptStarting, WorkerInstanceID: worker.ID, FencingToken: worker.FencingToken,
		LeaseUntil: repositoryTestTime.Add(time.Hour), ExecutionSpecVersion: 1,
		RequestedExecutionJSON: `{}`, ResolvedExecutionJSON: string(resolvedJSON), AdapterID: descriptor.AdapterID,
		BackendID: "local", Model: descriptor.Models[0], ReasoningMode: domain.ReasoningBackendDefault,
	}
	begin := func(prefix string) error {
		_, _, err := repository.BeginClaimedRunAttempt(context.Background(), guard, claimed.ID, created.Task.Version,
			run, approval.ScopeDigest,
			journalEvent("event-approval-consume-"+prefix, "approval.consumed", fixture.agentPrincipal, fixture.organizationID),
			journalEvent("event-task-running-"+prefix, "task.running", fixture.agentPrincipal, fixture.organizationID),
			journalEvent("event-run-started-"+prefix, "run_attempt.started", fixture.agentPrincipal, fixture.organizationID),
			journalEvent("event-mailbox-accepted-"+prefix, "mailbox.accepted", fixture.agentPrincipal, fixture.organizationID))
		return err
	}
	failCommit.Store(true)
	if err := begin("rollback"); err == nil {
		t.Fatal("preflight BeginAttempt unexpectedly committed through fault")
	}
	failCommit.Store(false)
	persistedApproval, err := repository.GetApprovalRequest(context.Background(), approval.ID)
	if err != nil || persistedApproval.State != domain.ApprovalRequestApproved {
		t.Fatalf("rolled back preflight Approval=%+v err=%v", persistedApproval, err)
	}
	persistedTask, err := repository.GetTask(context.Background(), created.Task.ID)
	if err != nil || persistedTask.Status != domain.TaskStatusQueued || persistedTask.Version != created.Task.Version {
		t.Fatalf("rolled back preflight Task=%+v err=%v", persistedTask, err)
	}
	persistedItem, err := repository.GetMailboxItem(context.Background(), claimed.ID)
	if err != nil || persistedItem.State != domain.MailboxStateClaimed {
		t.Fatalf("rolled back preflight mailbox=%+v err=%v", persistedItem, err)
	}
	if _, err := repository.GetRunAttempt(context.Background(), run.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("rolled back preflight RunAttempt error=%v", err)
	}
	if err := begin("commit"); err != nil {
		t.Fatalf("commit preflight BeginAttempt: %v", err)
	}
	persistedApproval, err = repository.GetApprovalRequest(context.Background(), approval.ID)
	if err != nil || persistedApproval.State != domain.ApprovalRequestConsumed {
		t.Fatalf("committed preflight Approval=%+v err=%v", persistedApproval, err)
	}
}
