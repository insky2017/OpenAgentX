package api

import (
	"encoding/json"
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
	ControlNetworkProfileDraftPath   = "/api/control/v1/network-profiles/{profileID}/draft"
	ControlNetworkProfileSecretPath  = "/api/control/v1/network-profiles/{profileID}/secret"
	ControlNetworkProfileTestPath    = "/api/control/v1/network-profiles/{profileID}/tests"
	ControlNetworkBindingPath        = "/api/control/v1/network-bindings"
	ControlNetworkRollbackPath       = "/api/control/v1/network-bindings/rollback"
	ControlNetworkModeTestPath       = "/api/control/v1/network-bindings/mode/tests"
	ControlNetworkModePublishPath    = "/api/control/v1/network-bindings/mode/publish"
	ControlNetworkImportPath         = "/api/control/v1/network-imports"
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
	Meta      CommandMeta `json:"meta"`
	ProfileID string      `json:"profile_id"`
	Mode      string      `json:"mode"`
	Host      string      `json:"host"`
	Port      int         `json:"port"`
	DirectIPs []string    `json:"direct_ips,omitempty"`
}

func (r CreateNetworkProfileRequest) Validate(actor string) (domain.ProxyProfile, error) {
	if err := r.Meta.Validate(false); err != nil {
		return domain.ProxyProfile{}, err
	}
	now := time.Now().UTC()
	p := domain.ProxyProfile{ID: r.ProfileID, Version: 1, Status: domain.NetworkProfileDraft, Mode: r.Mode, Host: r.Host, Port: r.Port, DirectIPs: r.DirectIPs, CreatedBy: actor, CreatedAt: now, UpdatedAt: now}
	return p, p.Validate()
}

type EditNetworkProfileRequest struct {
	Meta      CommandMeta `json:"meta"`
	Mode      string      `json:"mode"`
	Host      string      `json:"host"`
	Port      int         `json:"port"`
	DirectIPs []string    `json:"direct_ips,omitempty"`
}

func (r EditNetworkProfileRequest) Validate() error { return r.Meta.Validate(true) }

type ReplaceNetworkSecretRequest struct {
	Meta     CommandMeta `json:"meta"`
	Username string      `json:"username,omitempty"`
	Password string      `json:"password"`
}

func (r ReplaceNetworkSecretRequest) Validate() error {
	if err := r.Meta.Validate(true); err != nil {
		return err
	}
	if r.Password == "" {
		return domain.ErrInvalidInput("secret password is required")
	}
	if strings.ContainsAny(r.Username, "\r\n") || strings.ContainsAny(r.Password, "\r\n") {
		return domain.ErrInvalidInput("secret fields contain control characters")
	}
	return nil
}

type TestNetworkProfileRequest struct {
	Meta             CommandMeta `json:"meta"`
	WorkerInstanceID string      `json:"worker_instance_id"`
	Generation       int64       `json:"generation"`
	BackendID        string      `json:"backend_id"`
}

func (r TestNetworkProfileRequest) Validate() error {
	if err := r.Meta.Validate(true); err != nil {
		return err
	}
	if err := domain.ValidateOpaqueID("worker_instance_id", r.WorkerInstanceID); err != nil {
		return err
	}
	if err := domain.ValidatePositiveVersion("generation", r.Generation); err != nil {
		return err
	}
	return domain.ValidateIdentifier("backend_id", r.BackendID)
}

type PublishNetworkProfileRequest struct {
	Meta CommandMeta `json:"meta"`
}

func (r PublishNetworkProfileRequest) Validate() error { return r.Meta.Validate(true) }

type BindNetworkProfileRequest struct {
	Meta             CommandMeta `json:"meta"`
	AgentID          string      `json:"agent_id"`
	BackendID        string      `json:"backend_id"`
	ProfileID        string      `json:"profile_id"`
	ProfileVersion   int64       `json:"profile_version"`
	WorkerInstanceID string      `json:"worker_instance_id"`
	Generation       int64       `json:"generation"`
}

func (r BindNetworkProfileRequest) Validate(now time.Time) (domain.NetworkBinding, error) {
	if err := r.Meta.Validate(false); err != nil {
		return domain.NetworkBinding{}, err
	}
	b := domain.NetworkBinding{AgentID: r.AgentID, BackendID: r.BackendID, Mode: domain.NetworkNamedProfile, ProfileID: r.ProfileID, ProfileVersion: r.ProfileVersion, Version: 1, DesiredStatus: "pending", UpdatedAt: now.UTC()}
	return b, b.Validate()
}

type RollbackNetworkBindingRequest struct {
	Meta                 CommandMeta `json:"meta"`
	AgentID              string      `json:"agent_id"`
	BackendID            string      `json:"backend_id"`
	TargetContentVersion int64       `json:"target_content_version"`
	WorkerInstanceID     string      `json:"worker_instance_id"`
	Generation           int64       `json:"generation"`
}

func (r RollbackNetworkBindingRequest) Validate() error {
	if err := r.Meta.Validate(true); err != nil {
		return err
	}
	if err := domain.ValidateIdentifier("agent_id", r.AgentID); err != nil {
		return err
	}
	if err := domain.ValidateIdentifier("backend_id", r.BackendID); err != nil {
		return err
	}
	if err := domain.ValidatePositiveVersion("target_content_version", r.TargetContentVersion); err != nil {
		return err
	}
	if err := domain.ValidateOpaqueID("worker_instance_id", r.WorkerInstanceID); err != nil {
		return err
	}
	return domain.ValidatePositiveVersion("generation", r.Generation)
}

type ImportNetworkProfileRequest struct {
	Meta             CommandMeta `json:"meta"`
	ProfileID        string      `json:"profile_id"`
	WorkerInstanceID string      `json:"worker_instance_id"`
	Generation       int64       `json:"generation"`
	BackendID        string      `json:"backend_id"`
}

type TestNetworkModeRequest struct {
	Meta             CommandMeta        `json:"meta"`
	AgentID          string             `json:"agent_id"`
	BackendID        string             `json:"backend_id"`
	Mode             domain.NetworkMode `json:"mode"`
	WorkerInstanceID string             `json:"worker_instance_id"`
	Generation       int64              `json:"generation"`
}

func (r TestNetworkModeRequest) Validate() error {
	if err := r.Meta.Validate(false); err != nil {
		return err
	}
	if err := domain.ValidateIdentifier("agent_id", r.AgentID); err != nil {
		return err
	}
	if err := domain.ValidateIdentifier("backend_id", r.BackendID); err != nil {
		return err
	}
	if r.Mode != domain.NetworkInherit && r.Mode != domain.NetworkDirect {
		return domain.ErrInvalidInput("mode must be inherit or direct")
	}
	if err := domain.ValidateOpaqueID("worker_instance_id", r.WorkerInstanceID); err != nil {
		return err
	}
	return domain.ValidatePositiveVersion("generation", r.Generation)
}

type PublishNetworkModeRequest struct {
	Meta             CommandMeta `json:"meta"`
	TestID           string      `json:"test_id"`
	WorkerInstanceID string      `json:"worker_instance_id"`
	Generation       int64       `json:"generation"`
}

func (r PublishNetworkModeRequest) Validate() error {
	if err := r.Meta.Validate(false); err != nil {
		return err
	}
	if err := domain.ValidateOpaqueID("test_id", r.TestID); err != nil {
		return err
	}
	if err := domain.ValidateOpaqueID("worker_instance_id", r.WorkerInstanceID); err != nil {
		return err
	}
	return domain.ValidatePositiveVersion("generation", r.Generation)
}

func (r ImportNetworkProfileRequest) Validate() error {
	if err := r.Meta.Validate(false); err != nil {
		return err
	}
	if err := domain.ValidateIdentifier("profile_id", r.ProfileID); err != nil {
		return err
	}
	if err := domain.ValidateOpaqueID("worker_instance_id", r.WorkerInstanceID); err != nil {
		return err
	}
	if err := domain.ValidatePositiveVersion("generation", r.Generation); err != nil {
		return err
	}
	return domain.ValidateIdentifier("backend_id", r.BackendID)
}

type NetworkCommandResponse struct {
	Receipt json.RawMessage `json:"receipt"`
}

type NetworkProfileSummary struct {
	ProfileID               string                     `json:"profile_id"`
	CurrentContentVersion   int64                      `json:"current_content_version"`
	State                   domain.NetworkProfileState `json:"state"`
	StateRevision           int64                      `json:"state_revision"`
	ReadyTestID             string                     `json:"ready_test_id,omitempty"`
	PublishedContentVersion int64                      `json:"published_content_version,omitempty"`
	UpdatedAt               time.Time                  `json:"updated_at"`
	Mode                    string                     `json:"mode"`
	Host                    string                     `json:"host"`
	Port                    int                        `json:"port"`
	DirectIPs               []string                   `json:"direct_ips,omitempty"`
	SecretPresent           bool                       `json:"secret_present"`
	ManifestDigest          string                     `json:"manifest_digest"`
}

type NetworkProfileVersionSummary struct {
	ProfileID        string     `json:"profile_id"`
	ContentVersion   int64      `json:"content_version"`
	Mode             string     `json:"mode"`
	Host             string     `json:"host"`
	Port             int        `json:"port"`
	DirectIPs        []string   `json:"direct_ips,omitempty"`
	ManifestDigest   string     `json:"manifest_digest"`
	CreatedBy        string     `json:"created_by"`
	CreatedAt        time.Time  `json:"created_at"`
	SecretPresent    bool       `json:"secret_present"`
	Published        bool       `json:"published"`
	CurrentPublished bool       `json:"current_published"`
	PublishedAt      *time.Time `json:"published_at,omitempty"`
}

type NetworkTestSummary struct {
	ID               string                      `json:"test_id"`
	ProfileID        string                      `json:"profile_id"`
	ContentVersion   int64                       `json:"content_version"`
	WorkerInstanceID string                      `json:"worker_instance_id"`
	Generation       int64                       `json:"generation"`
	BackendID        string                      `json:"backend_id"`
	RuntimeIdentity  domain.RuntimeIdentity      `json:"runtime_identity"`
	State            string                      `json:"state"`
	DiagnosticCode   string                      `json:"diagnostic_code,omitempty"`
	DurationMS       int64                       `json:"duration_ms,omitempty"`
	ProbeResults     []domain.NetworkProbeResult `json:"probe_results,omitempty"`
	CreatedBy        string                      `json:"created_by"`
	CreatedAt        time.Time                   `json:"created_at"`
	FinishedAt       *time.Time                  `json:"finished_at,omitempty"`
}

type NetworkOverviewResponse struct {
	Profiles   []NetworkProfileSummary           `json:"profiles"`
	Versions   []NetworkProfileVersionSummary    `json:"versions"`
	Tests      []NetworkTestSummary              `json:"tests"`
	ModeTests  []domain.NetworkModeTest          `json:"mode_tests"`
	Bindings   []domain.NetworkBinding           `json:"bindings"`
	ActiveRuns []domain.NetworkActiveRunSnapshot `json:"active_runs"`
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
