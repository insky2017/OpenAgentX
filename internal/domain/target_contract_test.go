package domain_test

import (
	"errors"
	"testing"
	"time"

	"agentbus/internal/domain"
)

func TestTargetEnumsRejectUnknownValues(t *testing.T) {
	t.Parallel()
	if domain.TaskStatus("mystery").Valid() {
		t.Fatal("unknown task status must be invalid")
	}
	if domain.MailboxKind("shutdown").Valid() {
		t.Fatal("shutdown must not be an Agent mailbox kind")
	}
	if domain.WorkerCommandKind("prompt").Valid() {
		t.Fatal("worker command must not accept business prompt kinds")
	}
	if domain.ApprovalMode("deferred").Valid() {
		t.Fatal("unknown approval mode must be invalid")
	}
	if domain.Connectivity("ready").Valid() || domain.Availability("offline").Valid() || domain.DeliveryReadiness("queued").Valid() {
		t.Fatal("orthogonal Agent read-model states must reject values from another dimension")
	}
}

func TestLogicalAgentIdentityHasNoRuntimeAddress(t *testing.T) {
	t.Parallel()
	agent := domain.AgentIdentity{
		ID: "quote", PrincipalID: "principal-quote", OrganizationID: "org-1",
		DisplayName: "Quote Service", Status: domain.AgentIdentityActive, Version: 1,
	}
	if err := agent.Validate(); err != nil {
		t.Fatalf("valid logical Agent rejected: %v", err)
	}
	agent.Status = domain.AgentIdentityStatus("connected-to-pane")
	if err := agent.Validate(); err == nil {
		t.Fatal("runtime addressing must not be representable as Agent identity status")
	}
}

func TestMailboxOrderingAndLaneConstraints(t *testing.T) {
	t.Parallel()
	items := []domain.MailboxItem{
		{Sequence: 1, ID: "item-1", TargetAgentID: "quote", Kind: domain.MailboxKindTask, Lane: domain.MailboxLaneWork, State: domain.MailboxStatePending},
		{Sequence: 9, ID: "item-9", TargetAgentID: "quote", Kind: domain.MailboxKindCancel, Lane: domain.MailboxLaneControl, TargetRunID: "run-1", ExpectedRunVersion: 2, State: domain.MailboxStatePending},
		{Sequence: 7, ID: "item-7", TargetAgentID: "quote", Kind: domain.MailboxKindMessage, Lane: domain.MailboxLaneControl, TargetRunID: "run-1", ExpectedRunVersion: 2, State: domain.MailboxStatePending},
		{Sequence: 2, ID: "item-2", TargetAgentID: "quote", Kind: domain.MailboxKindMessage, Lane: domain.MailboxLaneWork, State: domain.MailboxStatePending},
	}
	for _, item := range items {
		if err := item.Validate(); err != nil {
			t.Fatalf("valid mailbox item rejected: %v", err)
		}
	}
	domain.SortMailboxItems(items)
	got := []string{items[0].ID, items[1].ID, items[2].ID, items[3].ID}
	want := []string{"item-7", "item-9", "item-1", "item-2"}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("mailbox order = %v, want %v", got, want)
		}
	}

	invalidCancel := domain.MailboxItem{Sequence: 10, ID: "cancel-work", TargetAgentID: "quote", Kind: domain.MailboxKindCancel, Lane: domain.MailboxLaneWork, State: domain.MailboxStatePending}
	if err := invalidCancel.Validate(); err == nil {
		t.Fatal("cancel in work lane must be rejected")
	}
	invalidControl := domain.MailboxItem{Sequence: 11, ID: "control-no-run", TargetAgentID: "quote", Kind: domain.MailboxKindMessage, Lane: domain.MailboxLaneControl, State: domain.MailboxStatePending}
	if err := invalidControl.Validate(); err == nil {
		t.Fatal("control item without target run/version must be rejected")
	}
}

func TestSingleActiveRunInvariant(t *testing.T) {
	t.Parallel()
	runs := []domain.RunAttempt{
		{ID: "run-1", AgentID: "quote", Status: domain.RunAttemptRunning},
		{ID: "run-2", AgentID: "quote", Status: domain.RunAttemptSucceeded},
		{ID: "run-3", AgentID: "engine", Status: domain.RunAttemptStarting},
	}
	if err := domain.ValidateSingleActiveRun(runs); err != nil {
		t.Fatalf("valid active run set rejected: %v", err)
	}
	runs = append(runs, domain.RunAttempt{ID: "run-4", AgentID: "quote", Status: domain.RunAttemptWaitingApproval})
	if err := domain.ValidateSingleActiveRun(runs); err == nil {
		t.Fatal("two active runs for one agent must be rejected")
	}
}

func TestCancelRequestedPreventsBeginAttempt(t *testing.T) {
	t.Parallel()
	allowed := []domain.TaskStatus{domain.TaskStatusQueued, domain.TaskStatusDispatching, domain.TaskStatusWaitingInput}
	for _, status := range allowed {
		if !(&domain.Task{Status: status}).CanBeginAttempt() {
			t.Fatalf("status %s should allow begin attempt", status)
		}
	}
	blocked := []domain.TaskStatus{
		domain.TaskStatusCancelRequested,
		domain.TaskStatusRunning,
		domain.TaskStatusWaitingApproval,
		domain.TaskStatusSucceeded,
		domain.TaskStatusFailed,
		domain.TaskStatusCanceled,
		domain.TaskStatusUncertain,
	}
	for _, status := range blocked {
		if (&domain.Task{Status: status}).CanBeginAttempt() {
			t.Fatalf("status %s must block begin attempt", status)
		}
	}
}

func TestApprovalScopeContracts(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	native := domain.ApprovalRequest{
		ID: "approval-1", TaskID: "task-1", Mode: domain.ApprovalModeNative,
		TargetRunID: "run-1", ExpectedRunVersion: 3, ScopeDigest: "sha256-native",
		State: domain.ApprovalRequestPending, ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}
	if err := native.Validate(); err != nil {
		t.Fatalf("native approval rejected: %v", err)
	}
	native.TargetRunID = ""
	if err := native.Validate(); err == nil {
		t.Fatal("native approval without run binding must be rejected")
	}

	preflight := domain.ApprovalRequest{
		ID: "approval-2", TaskID: "task-1", Mode: domain.ApprovalModePreflight,
		ScopeDigest: "sha256-preflight", State: domain.ApprovalRequestPending,
		ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}
	if err := preflight.Validate(); err != nil {
		t.Fatalf("preflight approval rejected: %v", err)
	}
	preflight.TargetRunID = "run-2"
	preflight.ExpectedRunVersion = 1
	if err := preflight.Validate(); err == nil {
		t.Fatal("preflight approval must not bind an active run")
	}
}

func TestTargetTaskValidationRejectsUnknownStatus(t *testing.T) {
	t.Parallel()
	task := &domain.Task{
		ID: "task-1", SenderAgentID: "sender", TargetAgentID: "target",
		IdempotencyKey: "idem", Content: "work", Status: domain.TaskStatus("unknown"),
	}
	err := task.Validate()
	var domainErr *domain.DomainError
	if err == nil || !errors.As(err, &domainErr) {
		t.Fatalf("unknown task status must return a domain error, got %v", err)
	}
}

func TestTargetTaskAndMessageRequirePrincipalAddressing(t *testing.T) {
	t.Parallel()
	task := &domain.Task{
		ID: "task-1", Version: 1, SenderPrincipalID: "human-1", TargetAgentID: "quote",
		OrganizationID: "org-1", DispatchMode: domain.DispatchModeDirect,
		IdempotencyKey: "idem-1", Content: "work", Status: domain.TaskStatusQueued,
	}
	if err := task.ValidateTarget(); err != nil {
		t.Fatalf("valid target task rejected: %v", err)
	}
	task.SenderPrincipalID = ""
	if err := task.ValidateTarget(); err == nil {
		t.Fatal("target task without sender principal must be rejected")
	}

	message := &domain.Message{
		ID: "message-1", Version: 1, Sequence: 1, TaskID: "task-1",
		SenderPrincipalID: "human-1", TargetAgentID: "quote",
		Kind: domain.MessageKindSupplement, Content: "more",
	}
	if err := message.ValidateTarget(); err != nil {
		t.Fatalf("valid target message rejected: %v", err)
	}
	message.TargetAgentID = ""
	if err := message.ValidateTarget(); err == nil {
		t.Fatal("target message without logical Agent target must be rejected")
	}
}
