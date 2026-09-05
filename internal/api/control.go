package api

import (
	"strings"
	"time"

	"openagentx/internal/domain"
)

const (
	ControlCreateTaskPath            = "/api/control/v1/tasks"
	ControlCreateMessagePath         = "/api/control/v1/tasks/{task-id}/messages"
	ControlCancelTaskPath            = "/api/control/v1/tasks/{task-id}/cancel"
	ControlApprovalDecisionPath      = "/api/control/v1/approvals/{approval-request-id}/decisions"
	ControlNetworkProfilePath        = "/api/control/v1/network-profiles"
	ControlNetworkProfilePublishPath = "/api/control/v1/network-profiles/{profileID}/publish"
	ControlNetworkBindingPath        = "/api/control/v1/network-bindings"
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

type CreateNetworkProfileRequest struct {
	Meta       CommandMeta `json:"meta"`
	ProfileID  string      `json:"profile_id"`
	Mode       string      `json:"mode"`
	Host       string      `json:"host"`
	Port       int         `json:"port"`
	ConfigFile string      `json:"config_file,omitempty"`
	SecretRef  string      `json:"secret_ref,omitempty"`
}

func (r CreateNetworkProfileRequest) Validate(actor string) (domain.ProxyProfile, error) {
	if err := r.Meta.Validate(false); err != nil {
		return domain.ProxyProfile{}, err
	}
	now := time.Now().UTC()
	p := domain.ProxyProfile{ID: r.ProfileID, Version: 1, Status: domain.NetworkProfileDraft, Mode: r.Mode, Host: r.Host, Port: r.Port, ConfigFile: r.ConfigFile, SecretRef: r.SecretRef, CreatedBy: actor, CreatedAt: now, UpdatedAt: now}
	return p, p.Validate()
}

type PublishNetworkProfileRequest struct {
	Meta CommandMeta `json:"meta"`
}

func (r PublishNetworkProfileRequest) Validate() error { return r.Meta.Validate(true) }

type BindNetworkProfileRequest struct {
	Meta           CommandMeta `json:"meta"`
	AgentID        string      `json:"agent_id"`
	BackendID      string      `json:"backend_id"`
	ProfileID      string      `json:"profile_id"`
	ProfileVersion int64       `json:"profile_version"`
}

func (r BindNetworkProfileRequest) Validate(now time.Time) (domain.NetworkBinding, error) {
	if err := r.Meta.Validate(false); err != nil {
		return domain.NetworkBinding{}, err
	}
	b := domain.NetworkBinding{AgentID: r.AgentID, BackendID: r.BackendID, ProfileID: r.ProfileID, ProfileVersion: r.ProfileVersion, Version: 1, DesiredStatus: "pending", UpdatedAt: now.UTC()}
	return b, b.Validate()
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
		if err := r.Execution.ValidateShape(); err != nil {
			return err
		}
		// CreateTask does not yet persist a requested execution override. In
		// particular, accepting a network policy here would make the API claim
		// a profile was selected while M1 planning still uses the registered
		// Backend policy. Reject it fail-closed until a transactional profile
		// binding is available.
		if !r.Execution.Network.IsZero() {
			return domain.ErrForbidden("network policy must be selected from the registered Backend profile")
		}
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
