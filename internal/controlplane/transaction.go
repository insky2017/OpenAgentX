package controlplane

import (
	"context"

	"agentbus/internal/domain"
)

// TransactionalState is the M1 authoritative-state boundary. Implementations
// commit each method's state changes and Journal/Mailbox side effects in one
// database transaction; callers never reconstruct current state by replay.
type TransactionalState interface {
	CreatePrincipal(context.Context, *domain.Principal, *domain.JournalEvent) error
	CreateOrganization(context.Context, *domain.Organization, *domain.JournalEvent) error
	CreateAgent(context.Context, *domain.AgentIdentity, *domain.AgentProfileRecord, *domain.JournalEvent) error
	GetAgent(context.Context, string) (*domain.AgentIdentity, *domain.AgentProfileRecord, error)

	CreateTask(context.Context, *domain.Task, *domain.Message, *domain.MailboxItem, *domain.JournalEvent) (*domain.CreateTaskResult, error)
	GetTask(context.Context, string) (*domain.Task, error)
	ListTasks(context.Context, string, int) ([]domain.Task, error)
	CreateMessage(context.Context, int64, *domain.Message, *domain.MailboxItem, *domain.JournalEvent) (*domain.CreateMessageResult, error)
	RequestTaskCancel(context.Context, string, int64, string, *domain.MailboxItem, *domain.JournalEvent, *domain.JournalEvent) (*domain.Task, *domain.MailboxItem, error)
	ListMessages(context.Context, string) ([]domain.Message, error)
	TransitionTask(context.Context, domain.TaskTransition) (*domain.Task, error)
	CreateApprovalRequest(context.Context, *domain.ApprovalRequest, *domain.JournalEvent) error
	GetApprovalRequest(context.Context, string) (*domain.ApprovalRequest, error)
	DecideApproval(context.Context, string, *domain.ApprovalDecision, *domain.MailboxItem, *domain.JournalEvent, *domain.JournalEvent) (*domain.ApprovalDecision, *domain.MailboxItem, error)
	ConsumePreflightApproval(context.Context, string, string, *domain.JournalEvent) (*domain.ApprovalRequest, error)

	CreateWorkerInstance(context.Context, *domain.WorkerInstance, *domain.JournalEvent) error
	GetWorkerInstance(context.Context, string) (*domain.WorkerInstance, error)
	GetMailboxItem(context.Context, string) (*domain.MailboxItem, error)
	ListMailbox(context.Context, string, int64, int) ([]domain.MailboxItem, error)
	BeginRunAttempt(context.Context, int64, *domain.RunAttempt, *domain.JournalEvent, *domain.JournalEvent) (*domain.Task, error)
	GetRunAttempt(context.Context, string) (*domain.RunAttempt, error)
	CreateSessionBinding(context.Context, *domain.SessionBinding, *domain.JournalEvent) error
	GetSessionBinding(context.Context, string, string, string) (*domain.SessionBinding, error)
	ListJournal(context.Context, int64, int) ([]domain.JournalEvent, error)
}
