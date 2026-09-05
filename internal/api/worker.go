package api

import (
	"context"
	"encoding/hex"
	"strings"
	"time"

	"openagentx/internal/domain"
	"openagentx/internal/runtime"
)

const (
	WorkerRegisterPath            = "/api/v1/workers/register"
	WorkerHeartbeatPath           = "/api/v1/workers/{worker-id}/heartbeat"
	WorkerNetworkBindingsPullPath = "/api/v1/workers/{worker-id}/network-bindings/pull"
	WorkerNetworkWorkPullPath     = "/api/v1/workers/{worker-id}/network-work/pull"
	WorkerNetworkWorkAckPath      = "/api/v1/network-work/{work-id}/ack"
	WorkerMailboxClaimPath        = "/api/v1/workers/{worker-id}/mailbox/claim"
	WorkerControlClaimPath        = "/api/v1/workers/{worker-id}/control/claim"
	WorkerReleasePath             = "/api/v1/workers/{worker-id}/release"
	MailboxAcceptPath             = "/api/v1/mailbox/{item-id}/accept"
	MailboxBeginAttemptPath       = "/api/v1/mailbox/{item-id}/begin-attempt"
	MailboxPayloadPath            = "/api/v1/mailbox/{item-id}/payload"
	WorkerCommandAckPath          = "/api/v1/worker-commands/{command-id}/ack"
	WorkerReleasedCommandAckPath  = "/api/v1/worker-commands/{command-id}/released-ack"
	RunEventsPath                 = "/api/v1/run-attempts/{run-id}/events"
	RunFinishPath                 = "/api/v1/run-attempts/{run-id}/finish"
)

type RegisterRequest struct {
	ContractVersion  string                        `json:"contract_version"`
	AgentID          string                        `json:"agent_id"`
	WorkerInstanceID string                        `json:"worker_instance_id"`
	Transport        domain.WorkerTransport        `json:"transport"`
	Capabilities     []string                      `json:"capabilities"`
	Backends         []runtime.BackendRegistration `json:"backends"`
}

func (r RegisterRequest) Validate() error {
	if err := ValidateContractVersion(r.ContractVersion); err != nil {
		return err
	}
	if err := domain.ValidateIdentifier("agent_id", r.AgentID); err != nil {
		return err
	}
	if err := domain.ValidateOpaqueID("worker_instance_id", r.WorkerInstanceID); err != nil {
		return err
	}
	if !r.Transport.Valid() {
		return domain.ErrInvalidInput("unsupported worker transport")
	}
	for _, capability := range r.Capabilities {
		if err := domain.ValidateIdentifier("capability", capability); err != nil {
			return err
		}
	}
	if len(r.Backends) == 0 {
		return domain.ErrInvalidInput("worker must register at least one Backend")
	}
	for _, backend := range r.Backends {
		if err := backend.Validate(); err != nil {
			return err
		}
	}
	return nil
}

type WorkerSession struct {
	Worker          domain.WorkerInstance   `json:"worker"`
	SessionToken    string                  `json:"session_token"`
	TokenExpiresAt  time.Time               `json:"token_expires_at"`
	NetworkBindings []domain.NetworkBinding `json:"network_bindings,omitempty"`
}

type NetworkBindingAck struct {
	BackendID       string `json:"backend_id"`
	ProfileID       string `json:"profile_id"`
	ProfileVersion  int64  `json:"profile_version"`
	BindingRevision int64  `json:"binding_revision"`
	State           string `json:"state"`
	Diagnostic      string `json:"diagnostic,omitempty"`
}

type NetworkBindingPullRequest struct {
	WorkerInstanceID string `json:"worker_instance_id"`
	Generation       int64  `json:"generation"`
	FencingToken     int64  `json:"fencing_token"`
}

func (r NetworkBindingPullRequest) Validate() error {
	if err := domain.ValidateOpaqueID("worker_instance_id", r.WorkerInstanceID); err != nil {
		return err
	}
	if err := domain.ValidatePositiveVersion("generation", r.Generation); err != nil {
		return err
	}
	return domain.ValidatePositiveVersion("fencing_token", r.FencingToken)
}

type NetworkBindingPullResponse struct {
	Bindings []domain.NetworkBinding `json:"bindings"`
}

type NetworkWorkPullRequest struct {
	WorkerInstanceID string `json:"worker_instance_id"`
	Generation       int64  `json:"generation"`
	FencingToken     int64  `json:"fencing_token"`
}

func (r NetworkWorkPullRequest) Validate() error {
	return (NetworkBindingPullRequest{WorkerInstanceID: r.WorkerInstanceID, Generation: r.Generation, FencingToken: r.FencingToken}).Validate()
}

// NetworkSecretPayload is deliberately confined to the authenticated Worker
// response. It must never be embedded in domain profiles, receipts or events.
type NetworkSecretPayload struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}
type NetworkWorkEnvelope struct {
	Work   domain.NetworkWork    `json:"work"`
	Secret *NetworkSecretPayload `json:"secret,omitempty"`
}
type NetworkWorkPullResponse struct {
	Work *NetworkWorkEnvelope `json:"work,omitempty"`
}
type ImportedNetworkProfile struct {
	Mode           string                `json:"mode"`
	Host           string                `json:"host"`
	Port           int                   `json:"port"`
	DirectIPs      []string              `json:"direct_ips,omitempty"`
	Secret         *NetworkSecretPayload `json:"secret,omitempty"`
	SourceIdentity string                `json:"source_identity"`
}

func (p ImportedNetworkProfile) Validate() error {
	content := domain.NetworkProfileContent{
		ProfileID: "import-validation", ContentVersion: 1, Mode: p.Mode,
		Host: p.Host, Port: p.Port, DirectIPs: p.DirectIPs,
		CreatedBy: "import-validator", CreatedAt: time.Unix(1, 0).UTC(),
	}
	content.ManifestDigest = content.ComputeManifestDigest()
	if err := content.Validate(); err != nil {
		return err
	}
	if len(p.SourceIdentity) != 64 {
		return domain.ErrInvalidInput("import source_identity must be sha256")
	}
	if _, err := hex.DecodeString(p.SourceIdentity); err != nil || strings.ToLower(p.SourceIdentity) != p.SourceIdentity {
		return domain.ErrInvalidInput("import source_identity must be sha256")
	}
	if p.Secret != nil {
		if p.Mode == "only_http_proxy" && (p.Secret.Username != "" || p.Secret.Password != "") {
			return domain.ErrUnsupportedCapability
		}
		if strings.ContainsAny(p.Secret.Username, "\r\n") || strings.ContainsAny(p.Secret.Password, "\r\n") {
			return domain.ErrInvalidInput("imported secret contains control characters")
		}
	}
	return nil
}

type NetworkWorkAckRequest struct {
	WorkerInstanceID string                      `json:"worker_instance_id"`
	Generation       int64                       `json:"generation"`
	FencingToken     int64                       `json:"fencing_token"`
	State            string                      `json:"state"`
	DiagnosticCode   string                      `json:"diagnostic_code,omitempty"`
	DurationMS       int64                       `json:"duration_ms,omitempty"`
	Imported         *ImportedNetworkProfile     `json:"imported,omitempty"`
	Policy           *domain.NetworkPolicy       `json:"policy,omitempty"`
	ProbeResults     []domain.NetworkProbeResult `json:"probe_results,omitempty"`
}

func (r NetworkWorkAckRequest) Validate() error {
	if err := (NetworkBindingPullRequest{WorkerInstanceID: r.WorkerInstanceID, Generation: r.Generation, FencingToken: r.FencingToken}).Validate(); err != nil {
		return err
	}
	if r.State != "succeeded" && r.State != "failed" {
		return domain.ErrInvalidInput("network work state must be succeeded or failed")
	}
	if !domain.ValidNetworkDiagnostic(r.DiagnosticCode) || (r.State == "succeeded" && r.DiagnosticCode != "") {
		return domain.ErrInvalidInput("invalid network work diagnostic")
	}
	if r.DurationMS < 0 {
		return domain.ErrInvalidInput("network work duration cannot be negative")
	}
	if len(r.ProbeResults) != 0 {
		if err := domain.ValidateNetworkProbeResults(r.State, r.ProbeResults); err != nil {
			return err
		}
	}
	if r.Imported != nil {
		if r.State != "succeeded" {
			return domain.ErrInvalidInput("failed network work cannot include imported profile")
		}
		if err := r.Imported.Validate(); err != nil {
			return err
		}
	}
	return nil
}

type HeartbeatRequest struct {
	WorkerInstanceID string                           `json:"worker_instance_id"`
	Generation       int64                            `json:"generation"`
	FencingToken     int64                            `json:"fencing_token"`
	Status           domain.WorkerStatus              `json:"status"`
	BackendHealth    map[string]runtime.BackendHealth `json:"backend_health"`
	NetworkBindings  map[string]NetworkBindingAck     `json:"network_bindings,omitempty"`
}

type WorkerReleaseRequest struct {
	WorkerInstanceID string `json:"worker_instance_id"`
	Generation       int64  `json:"generation"`
	FencingToken     int64  `json:"fencing_token"`
}

func (r WorkerReleaseRequest) Validate() error {
	if err := domain.ValidateOpaqueID("worker_instance_id", r.WorkerInstanceID); err != nil {
		return err
	}
	if err := domain.ValidatePositiveVersion("generation", r.Generation); err != nil {
		return err
	}
	return domain.ValidatePositiveVersion("fencing_token", r.FencingToken)
}

func (r HeartbeatRequest) Validate() error {
	if err := domain.ValidateOpaqueID("worker_instance_id", r.WorkerInstanceID); err != nil {
		return err
	}
	if err := domain.ValidatePositiveVersion("generation", r.Generation); err != nil {
		return err
	}
	if err := domain.ValidatePositiveVersion("fencing_token", r.FencingToken); err != nil {
		return err
	}
	if !r.Status.Valid() {
		return domain.ErrInvalidInput("unsupported worker status")
	}
	for backendID, health := range r.BackendHealth {
		if err := domain.ValidateIdentifier("backend_id", backendID); err != nil {
			return err
		}
		if !health.Valid() {
			return domain.ErrInvalidInput("unsupported Backend health")
		}
	}
	for backendID, binding := range r.NetworkBindings {
		if err := domain.ValidateIdentifier("network binding backend_id", backendID); err != nil {
			return err
		}
		if binding.BackendID != backendID || binding.ProfileID == "" {
			return domain.ErrInvalidInput("network binding acknowledgement does not match map key")
		}
		if err := domain.ValidateIdentifier("network binding profile_id", binding.ProfileID); err != nil {
			return err
		}
		if err := domain.ValidatePositiveVersion("network binding profile_version", binding.ProfileVersion); err != nil {
			return err
		}
		if err := domain.ValidatePositiveVersion("network binding revision", binding.BindingRevision); err != nil {
			return err
		}
		if binding.State != "applied" && binding.State != "failed" {
			return domain.ErrInvalidInput("unsupported network binding acknowledgement state")
		}
		if len(binding.Diagnostic) > 4096 {
			return domain.ErrInvalidInput("network binding diagnostic is too long")
		}
	}
	return nil
}

type ClaimRequest struct {
	WorkerInstanceID string `json:"worker_instance_id"`
	AgentID          string `json:"agent_id"`
	Generation       int64  `json:"generation"`
	FencingToken     int64  `json:"fencing_token"`
	WorkCapacity     int    `json:"work_capacity"`
	WaitSeconds      int    `json:"wait_seconds"`
}

func (r ClaimRequest) Validate() error {
	if err := domain.ValidateOpaqueID("worker_instance_id", r.WorkerInstanceID); err != nil {
		return err
	}
	if err := domain.ValidateIdentifier("agent_id", r.AgentID); err != nil {
		return err
	}
	if err := domain.ValidatePositiveVersion("generation", r.Generation); err != nil {
		return err
	}
	if err := domain.ValidatePositiveVersion("fencing_token", r.FencingToken); err != nil {
		return err
	}
	if r.WorkCapacity < 0 || r.WaitSeconds < 0 || r.WaitSeconds > 30 {
		return domain.ErrInvalidInput("invalid work_capacity or wait_seconds")
	}
	return nil
}

type ClaimResponse struct {
	Item *domain.MailboxItem `json:"item,omitempty"`
}

type AcceptRequest struct {
	WorkerInstanceID  string              `json:"worker_instance_id"`
	Generation        int64               `json:"generation"`
	FencingToken      int64               `json:"fencing_token"`
	ExpectedItemState domain.MailboxState `json:"expected_item_state"`
	Outcome           domain.MailboxState `json:"outcome"`
	Result            string              `json:"result,omitempty"`
}

func (r AcceptRequest) Validate() error {
	if err := domain.ValidateOpaqueID("worker_instance_id", r.WorkerInstanceID); err != nil {
		return err
	}
	if err := domain.ValidatePositiveVersion("generation", r.Generation); err != nil {
		return err
	}
	if err := domain.ValidatePositiveVersion("fencing_token", r.FencingToken); err != nil {
		return err
	}
	if !r.ExpectedItemState.Valid() {
		return domain.ErrInvalidInput("unsupported expected mailbox state")
	}
	if r.Outcome != domain.MailboxStateAccepted && r.Outcome != domain.MailboxStateSuperseded && r.Outcome != domain.MailboxStateFailed {
		return domain.ErrInvalidInput("mailbox outcome must be accepted, superseded, or failed")
	}
	return nil
}

type BeginAttemptRequest struct {
	WorkerInstanceID  string              `json:"worker_instance_id"`
	AgentID           string              `json:"agent_id"`
	Generation        int64               `json:"generation"`
	FencingToken      int64               `json:"fencing_token"`
	ExpectedItemState domain.MailboxState `json:"expected_item_state"`
}

func (r BeginAttemptRequest) Validate() error {
	if err := domain.ValidateOpaqueID("worker_instance_id", r.WorkerInstanceID); err != nil {
		return err
	}
	if err := domain.ValidateIdentifier("agent_id", r.AgentID); err != nil {
		return err
	}
	if err := domain.ValidatePositiveVersion("generation", r.Generation); err != nil {
		return err
	}
	if err := domain.ValidatePositiveVersion("fencing_token", r.FencingToken); err != nil {
		return err
	}
	if r.ExpectedItemState != domain.MailboxStateClaimed {
		return domain.ErrInvalidInput("begin attempt requires a claimed mailbox item")
	}
	return nil
}

type BeginAttemptResponse struct {
	MailboxItem domain.MailboxItem  `json:"mailbox_item"`
	Turn        runtime.TurnRequest `json:"turn"`
}

type MailboxPayloadRequest struct {
	WorkerInstanceID string `json:"worker_instance_id"`
	Generation       int64  `json:"generation"`
	FencingToken     int64  `json:"fencing_token"`
}

func (r MailboxPayloadRequest) Validate() error {
	if err := domain.ValidateOpaqueID("worker_instance_id", r.WorkerInstanceID); err != nil {
		return err
	}
	if err := domain.ValidatePositiveVersion("generation", r.Generation); err != nil {
		return err
	}
	return domain.ValidatePositiveVersion("fencing_token", r.FencingToken)
}

type MailboxPayloadResponse struct {
	Message          *domain.Message          `json:"message,omitempty"`
	ApprovalDecision *domain.ApprovalDecision `json:"approval_decision,omitempty"`
}

type ControlClaimRequest struct {
	WorkerInstanceID string `json:"worker_instance_id"`
	Generation       int64  `json:"generation"`
	FencingToken     int64  `json:"fencing_token"`
	WaitSeconds      int    `json:"wait_seconds"`
}

func (r ControlClaimRequest) Validate() error {
	if err := domain.ValidateOpaqueID("worker_instance_id", r.WorkerInstanceID); err != nil {
		return err
	}
	if err := domain.ValidatePositiveVersion("generation", r.Generation); err != nil {
		return err
	}
	if err := domain.ValidatePositiveVersion("fencing_token", r.FencingToken); err != nil {
		return err
	}
	if r.WaitSeconds < 0 || r.WaitSeconds > 30 {
		return domain.ErrInvalidInput("wait_seconds must be between 0 and 30")
	}
	return nil
}

type ControlClaimResponse struct {
	Command *domain.WorkerCommand `json:"command,omitempty"`
}

type ControlAckRequest struct {
	WorkerInstanceID string                    `json:"worker_instance_id"`
	Generation       int64                     `json:"generation"`
	FencingToken     int64                     `json:"fencing_token"`
	State            domain.WorkerCommandState `json:"state"`
	Result           string                    `json:"result,omitempty"`
}

func (r ControlAckRequest) Validate() error {
	if err := domain.ValidateOpaqueID("worker_instance_id", r.WorkerInstanceID); err != nil {
		return err
	}
	if err := domain.ValidatePositiveVersion("generation", r.Generation); err != nil {
		return err
	}
	if err := domain.ValidatePositiveVersion("fencing_token", r.FencingToken); err != nil {
		return err
	}
	if r.State != domain.WorkerCommandApplied && r.State != domain.WorkerCommandFailed {
		return domain.ErrInvalidInput("worker command acknowledgement must be applied or failed")
	}
	return nil
}

type EventBatch struct {
	WorkerInstanceID   string                 `json:"worker_instance_id"`
	Generation         int64                  `json:"generation"`
	FencingToken       int64                  `json:"fencing_token"`
	ExpectedRunVersion int64                  `json:"expected_run_version"`
	Events             []runtime.RuntimeEvent `json:"events"`
}

func (r EventBatch) Validate() error {
	if err := domain.ValidateOpaqueID("worker_instance_id", r.WorkerInstanceID); err != nil {
		return err
	}
	if err := domain.ValidatePositiveVersion("generation", r.Generation); err != nil {
		return err
	}
	if err := domain.ValidatePositiveVersion("fencing_token", r.FencingToken); err != nil {
		return err
	}
	if err := domain.ValidatePositiveVersion("expected_run_version", r.ExpectedRunVersion); err != nil {
		return err
	}
	if len(r.Events) == 0 {
		return domain.ErrInvalidInput("event batch cannot be empty")
	}
	for _, event := range r.Events {
		if err := event.Validate(); err != nil {
			return err
		}
	}
	return nil
}

type FinishRunRequest struct {
	WorkerInstanceID    string             `json:"worker_instance_id"`
	Generation          int64              `json:"generation"`
	FencingToken        int64              `json:"fencing_token"`
	ExpectedTaskVersion int64              `json:"expected_task_version"`
	ExpectedRunVersion  int64              `json:"expected_run_version"`
	Result              runtime.TurnResult `json:"result"`
}

func (r FinishRunRequest) Validate() error {
	if err := domain.ValidateOpaqueID("worker_instance_id", r.WorkerInstanceID); err != nil {
		return err
	}
	versions := []struct {
		label string
		value int64
	}{
		{label: "generation", value: r.Generation},
		{label: "fencing_token", value: r.FencingToken},
		{label: "expected_task_version", value: r.ExpectedTaskVersion},
		{label: "expected_run_version", value: r.ExpectedRunVersion},
	}
	for _, version := range versions {
		if err := domain.ValidatePositiveVersion(version.label, version.value); err != nil {
			return err
		}
	}
	return r.Result.Validate()
}

type WorkerControlClient interface {
	RegisterWorker(context.Context, RegisterRequest) (*WorkerSession, error)
	Heartbeat(context.Context, HeartbeatRequest) error
	PullNetworkWork(context.Context, NetworkWorkPullRequest) (*NetworkWorkEnvelope, error)
	AcknowledgeNetworkWork(context.Context, string, NetworkWorkAckRequest) error
	ClaimMailbox(context.Context, ClaimRequest) (*domain.MailboxItem, error)
	BeginAttempt(context.Context, string, BeginAttemptRequest) (*BeginAttemptResponse, error)
	ResolveMailboxPayload(context.Context, string, MailboxPayloadRequest) (*MailboxPayloadResponse, error)
	AcceptMailboxItem(context.Context, string, AcceptRequest) error
	ClaimWorkerCommand(context.Context, ControlClaimRequest) (*domain.WorkerCommand, error)
	AcknowledgeWorkerCommand(context.Context, string, ControlAckRequest) error
	ReleaseWorker(context.Context, WorkerReleaseRequest) error
	AcknowledgeReleasedWorkerCommand(context.Context, string, ControlAckRequest) error
	AppendRunEvents(context.Context, string, EventBatch) error
	FinishRun(context.Context, string, FinishRunRequest) error
}
