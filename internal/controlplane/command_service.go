package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"openagentx/internal/api"
	"openagentx/internal/domain"
)

// CommandState is the transactional input surface used by the human/API control plane.
type CommandState interface {
	CreateTask(context.Context, *domain.Task, *domain.Message, *domain.MailboxItem, *domain.JournalEvent) (*domain.CreateTaskResult, error)
	GetTask(context.Context, string) (*domain.Task, error)
	CreateMessage(context.Context, int64, *domain.Message, *domain.MailboxItem, *domain.JournalEvent) (*domain.CreateMessageResult, error)
	RequestTaskCancel(context.Context, string, int64, string, *domain.MailboxItem, *domain.JournalEvent, *domain.JournalEvent) (*domain.Task, *domain.MailboxItem, error)
	GetApprovalRequest(context.Context, string) (*domain.ApprovalRequest, error)
	DecideApproval(context.Context, string, *domain.ApprovalDecision, *domain.MailboxItem, *domain.JournalEvent, *domain.JournalEvent) (*domain.ApprovalDecision, *domain.MailboxItem, error)
}

type CommandService struct {
	state  CommandState
	broker WakeupBroker
	now    func() time.Time
}

func NewCommandService(state CommandState, broker WakeupBroker, now func() time.Time) (*CommandService, error) {
	if state == nil {
		return nil, fmt.Errorf("command state is required")
	}
	if broker == nil {
		broker = NewMemoryWakeupBroker()
	}
	if now == nil {
		now = time.Now
	}
	return &CommandService{state: state, broker: broker, now: now}, nil
}

func commandID(prefix string) string { return prefix + "-" + uuid.NewString() }
func commandEvent(id, typ, actor, aggregate string, payload any, now time.Time) *domain.JournalEvent {
	b, _ := json.Marshal(payload)
	return &domain.JournalEvent{ID: commandID("event"), AggregateType: aggregate, AggregateID: id, EventType: typ, ActorPrincipalID: actor, Payload: b, CreatedAt: now.UTC()}
}

func (s *CommandService) CreateTask(ctx context.Context, principal string, req api.CreateTaskRequest) (*api.CreateTaskResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	taskID := commandID("task")
	msgID := commandID("message")
	itemID := commandID("mailbox")
	task := &domain.Task{ID: taskID, Version: 1, SenderAgentID: "command-center", SenderPrincipalID: principal, TargetAgentID: req.TargetAgentID, OrganizationID: req.OrganizationID, DispatchMode: req.DispatchMode, Content: req.Content, IdempotencyKey: req.Meta.IdempotencyKey, Status: domain.TaskStatusQueued, CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}
	if req.ParentTaskID != "" {
		task.ParentTaskID = &req.ParentTaskID
	}
	message := &domain.Message{ID: msgID, Version: 1, Sequence: 1, TaskID: taskID, SenderAgentID: "command-center", SenderPrincipalID: principal, TargetAgentID: req.TargetAgentID, Kind: domain.MessageKindInstruction, Content: req.Content, CreatedAt: now.Format(time.RFC3339Nano)}
	item := &domain.MailboxItem{ID: itemID, TargetAgentID: req.TargetAgentID, Kind: domain.MailboxKindTask, Lane: domain.MailboxLaneWork, TaskID: taskID, State: domain.MailboxStatePending, CreatedAt: now}
	result, err := s.state.CreateTask(ctx, task, message, item, commandEvent(taskID, "task.created", principal, "task", map[string]any{"target_agent_id": req.TargetAgentID}, now))
	if err != nil {
		return nil, err
	}
	s.broker.Publish(AgentMailboxTopic(req.TargetAgentID))
	return &api.CreateTaskResponse{TaskID: result.Task.ID, Sequence: result.MailboxItem.Sequence}, nil
}

func (s *CommandService) CreateMessage(ctx context.Context, principal, taskID string, req api.CreateMessageRequest) (*api.CreateMessageResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	task, err := s.state.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	message := &domain.Message{ID: commandID("message"), TaskID: taskID, SenderAgentID: "command-center", SenderPrincipalID: principal, Kind: domain.MessageKindSupplement, Content: req.Content}
	item := &domain.MailboxItem{ID: commandID("mailbox"), State: domain.MailboxStatePending, CreatedAt: s.now().UTC()}
	result, err := s.state.CreateMessage(ctx, task.Version, message, item, commandEvent(taskID, "task.message_created", principal, "task", nil, s.now()))
	if err != nil {
		return nil, err
	}
	s.broker.Publish(AgentMailboxTopic(task.TargetAgentID))
	return &api.CreateMessageResponse{MessageID: result.Message.ID, Sequence: result.MailboxItem.Sequence}, nil
}

func (s *CommandService) CancelTask(ctx context.Context, principal, taskID string, req api.CancelTaskRequest) (*api.CancelTaskResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	task, err := s.state.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	item := &domain.MailboxItem{ID: commandID("mailbox")}
	updated, _, err := s.state.RequestTaskCancel(ctx, taskID, task.Version, principal, item, commandEvent(taskID, "task.cancel_requested", principal, "task", nil, s.now()), commandEvent(item.ID, "mailbox.cancel_created", principal, "mailbox_item", nil, s.now()))
	if err != nil {
		return nil, err
	}
	s.broker.Publish(AgentMailboxTopic(task.TargetAgentID))
	return &api.CancelTaskResponse{Task: *updated}, nil
}

func (s *CommandService) DecideApproval(ctx context.Context, principal, requestID string, req api.DecideApprovalRequest) (*api.DecideApprovalResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	request, err := s.state.GetApprovalRequest(ctx, requestID)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	decision := &domain.ApprovalDecision{ID: commandID("approval-decision"), ApprovalRequestID: requestID, DecidedBy: principal, Decision: req.Decision, IdempotencyKey: req.Meta.IdempotencyKey}
	item := &domain.MailboxItem{ID: commandID("mailbox"), State: domain.MailboxStatePending, CreatedAt: now}
	result, _, err := s.state.DecideApproval(ctx, requestID, decision, item, commandEvent(requestID, "approval.decided", principal, "approval_request", nil, now), commandEvent(item.ID, "mailbox.approval_created", principal, "mailbox_item", nil, now))
	if err != nil {
		return nil, err
	}
	if task, taskErr := s.state.GetTask(ctx, request.TaskID); taskErr == nil {
		s.broker.Publish(AgentMailboxTopic(task.TargetAgentID))
	}
	return &api.DecideApprovalResponse{Decision: *result}, nil
}
