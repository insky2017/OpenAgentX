package api

import (
	"strings"

	"openagentx/internal/domain"
)

const (
	ControlCreateTaskPath       = "/api/control/v1/tasks"
	ControlCreateMessagePath    = "/api/control/v1/tasks/{task-id}/messages"
	ControlCancelTaskPath       = "/api/control/v1/tasks/{task-id}/cancel"
	ControlApprovalDecisionPath = "/api/control/v1/approvals/{approval-request-id}/decisions"
)

type CreateTaskRequest struct {
	Meta              CommandMeta           `json:"meta"`
	SenderPrincipalID string                `json:"sender_principal_id"`
	TargetAgentID     string                `json:"target_agent_id"`
	OrganizationID    string                `json:"organization_id"`
	DispatchMode      domain.DispatchMode   `json:"dispatch_mode"`
	ParentTaskID      string                `json:"parent_task_id,omitempty"`
	Content           string                `json:"content"`
	Execution         *domain.ExecutionSpec `json:"execution,omitempty"`
}

func (r CreateTaskRequest) Validate() error {
	if err := r.Meta.Validate(false); err != nil {
		return err
	}
	if err := domain.ValidateOpaqueID("sender_principal_id", r.SenderPrincipalID); err != nil {
		return err
	}
	if err := domain.ValidateIdentifier("target_agent_id", r.TargetAgentID); err != nil {
		return err
	}
	if err := domain.ValidateOpaqueID("organization_id", r.OrganizationID); err != nil {
		return err
	}
	if !r.DispatchMode.Valid() {
		return domain.ErrInvalidInput("unsupported dispatch_mode")
	}
	if strings.TrimSpace(r.Content) == "" {
		return domain.ErrInvalidInput("task content cannot be empty")
	}
	if r.ParentTaskID != "" {
		if err := domain.ValidateOpaqueID("parent_task_id", r.ParentTaskID); err != nil {
			return err
		}
	}
	if r.Execution != nil {
		return r.Execution.ValidateShape()
	}
	return nil
}

type CreateTaskResponse struct {
	TaskID   string `json:"task_id"`
	Sequence int64  `json:"sequence"`
}

type CreateMessageRequest struct {
	Meta              CommandMeta `json:"meta"`
	SenderPrincipalID string      `json:"sender_principal_id"`
	Content           string      `json:"content"`
}

func (r CreateMessageRequest) Validate() error {
	if err := r.Meta.Validate(true); err != nil {
		return err
	}
	if err := domain.ValidateOpaqueID("sender_principal_id", r.SenderPrincipalID); err != nil {
		return err
	}
	if strings.TrimSpace(r.Content) == "" {
		return domain.ErrInvalidInput("message content cannot be empty")
	}
	return nil
}

type CreateMessageResponse struct {
	MessageID string `json:"message_id"`
	Sequence  int64  `json:"sequence"`
}

type CancelTaskRequest struct {
	Meta        CommandMeta `json:"meta"`
	RequestedBy string      `json:"requested_by"`
}

func (r CancelTaskRequest) Validate() error {
	if err := r.Meta.Validate(true); err != nil {
		return err
	}
	return domain.ValidateOpaqueID("requested_by", r.RequestedBy)
}

type CancelTaskResponse struct {
	Task     domain.Task `json:"task"`
	Sequence int64       `json:"sequence"`
}

type DecideApprovalRequest struct {
	Meta      CommandMeta                  `json:"meta"`
	DecidedBy string                       `json:"decided_by"`
	Decision  domain.ApprovalDecisionValue `json:"decision"`
}

func (r DecideApprovalRequest) Validate() error {
	if err := r.Meta.Validate(true); err != nil {
		return err
	}
	if err := domain.ValidateOpaqueID("decided_by", r.DecidedBy); err != nil {
		return err
	}
	if !r.Decision.Valid() {
		return domain.ErrInvalidInput("unsupported approval decision")
	}
	return nil
}

type DecideApprovalResponse struct {
	Decision domain.ApprovalDecision `json:"decision"`
	Sequence int64                   `json:"sequence"`
}
