package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"openagentx/internal/domain"
)

var (
	ErrSteerUnsupported    = errors.New("runtime steering is unsupported")
	ErrApprovalUnsupported = errors.New("runtime approval is unsupported")
	ErrCancelUnsupported   = errors.New("runtime cancellation is unsupported")
	ErrSessionUnsupported  = errors.New("runtime session mode is unsupported")
	ErrBackendCrashed      = errors.New("runtime backend crashed")
)

type SteerMode string

const (
	SteerNative      SteerMode = "native"
	SteerQueued      SteerMode = "queued"
	SteerUnsupported SteerMode = "unsupported"
)

func (m SteerMode) Valid() bool {
	return m == SteerNative || m == SteerQueued || m == SteerUnsupported
}

type ApprovalMode string

const (
	ApprovalNative      ApprovalMode = "native"
	ApprovalPreflight   ApprovalMode = "preflight"
	ApprovalUnsupported ApprovalMode = "unsupported"
)

func (m ApprovalMode) Valid() bool {
	return m == ApprovalNative || m == ApprovalPreflight || m == ApprovalUnsupported
}

type CancelMode string

const (
	CancelNative        CancelMode = "native"
	CancelProcessSignal CancelMode = "process_signal"
	CancelUnsupported   CancelMode = "unsupported"
)

func (m CancelMode) Valid() bool {
	return m == CancelNative || m == CancelProcessSignal || m == CancelUnsupported
}

type AdapterDescriptor struct {
	AdapterID          string                 `json:"adapter_id"`
	BackendType        string                 `json:"backend_type"`
	Version            string                 `json:"version"`
	LaunchProtocol     string                 `json:"launch_protocol"`
	Models             []string               `json:"models"`
	ReasoningModes     []domain.ReasoningMode `json:"reasoning_modes"`
	SessionModes       []domain.SessionMode   `json:"session_modes"`
	Steer              SteerMode              `json:"steer"`
	Approval           ApprovalMode           `json:"approval"`
	Cancel             CancelMode             `json:"cancel"`
	Permissions        []string               `json:"permissions"`
	Sandboxes          []string               `json:"sandboxes"`
	NetworkModes       []string               `json:"network_modes"`
	Streams            bool                   `json:"streams"`
	BackendOptionsJSON json.RawMessage        `json:"backend_options_schema"`
	MaxConcurrency     int                    `json:"max_concurrency"`
}

type BackendHealth string

const (
	BackendHealthy     BackendHealth = "healthy"
	BackendDegraded    BackendHealth = "degraded"
	BackendUnavailable BackendHealth = "unavailable"
)

func (h BackendHealth) Valid() bool {
	return h == BackendHealthy || h == BackendDegraded || h == BackendUnavailable
}

type BackendRegistration struct {
	BackendID  string            `json:"backend_id"`
	Descriptor AdapterDescriptor `json:"descriptor"`
	Health     BackendHealth     `json:"health"`
}

func (r BackendRegistration) Validate() error {
	if err := domain.ValidateIdentifier("backend_id", r.BackendID); err != nil {
		return err
	}
	if err := r.Descriptor.Validate(); err != nil {
		return err
	}
	if !r.Health.Valid() {
		return domain.ErrInvalidInput("unsupported Backend health")
	}
	return nil
}

func (d AdapterDescriptor) Validate() error {
	if err := domain.ValidateIdentifier("adapter_id", d.AdapterID); err != nil {
		return err
	}
	if err := domain.ValidateIdentifier("backend_type", d.BackendType); err != nil {
		return err
	}
	if err := domain.ValidateOpaqueID("adapter version", d.Version); err != nil {
		return err
	}
	if err := domain.ValidateIdentifier("launch_protocol", d.LaunchProtocol); err != nil {
		return err
	}
	if len(d.Models) == 0 {
		return domain.ErrInvalidInput("adapter must declare at least one model")
	}
	seenModels := make(map[string]struct{}, len(d.Models))
	for _, model := range d.Models {
		if err := domain.ValidateIdentifier("model", model); err != nil {
			return err
		}
		if _, exists := seenModels[model]; exists {
			return domain.ErrInvalidInput("adapter models must be unique")
		}
		seenModels[model] = struct{}{}
	}
	for _, mode := range d.ReasoningModes {
		if !mode.Valid() {
			return domain.ErrInvalidInput("adapter has unsupported reasoning mode")
		}
	}
	for _, mode := range d.SessionModes {
		if !mode.Valid() {
			return domain.ErrInvalidInput("adapter has unsupported session mode")
		}
	}
	if !d.Steer.Valid() || !d.Approval.Valid() || !d.Cancel.Valid() {
		return domain.ErrInvalidInput("adapter has unsupported control capability")
	}
	if len(d.BackendOptionsJSON) != 0 && !json.Valid(d.BackendOptionsJSON) {
		return domain.ErrInvalidInput("backend options schema must be valid JSON")
	}
	if d.MaxConcurrency <= 0 {
		return domain.ErrInvalidInput("max_concurrency must be positive")
	}
	return nil
}

type RuntimeEvent struct {
	Type       string          `json:"type"`
	Payload    json.RawMessage `json:"payload"`
	OccurredAt time.Time       `json:"occurred_at"`
}

func (e RuntimeEvent) Validate() error {
	if err := domain.ValidateIdentifier("runtime event type", e.Type); err != nil {
		return err
	}
	if len(e.Payload) == 0 || !json.Valid(e.Payload) {
		return domain.ErrInvalidInput("runtime event payload must be valid JSON")
	}
	if e.OccurredAt.IsZero() {
		return domain.ErrInvalidInput("runtime event occurred_at is required")
	}
	return nil
}

type EventSink interface {
	Emit(context.Context, RuntimeEvent) error
}

type EventSinkFunc func(context.Context, RuntimeEvent) error

func (f EventSinkFunc) Emit(ctx context.Context, event RuntimeEvent) error {
	return f(ctx, event)
}

type TurnRequest struct {
	Task           domain.Task                  `json:"task"`
	RunAttempt     domain.RunAttempt            `json:"run_attempt"`
	SessionBinding *domain.SessionBinding       `json:"session_binding,omitempty"`
	Messages       []domain.Message             `json:"messages"`
	Execution      domain.ResolvedExecutionSpec `json:"execution"`
}

type TurnResultStatus string

const (
	TurnResultSucceeded    TurnResultStatus = "succeeded"
	TurnResultFailed       TurnResultStatus = "failed"
	TurnResultCanceled     TurnResultStatus = "canceled"
	TurnResultUncertain    TurnResultStatus = "uncertain"
	TurnResultWaitingInput TurnResultStatus = "waiting_input"
)

func (s TurnResultStatus) Valid() bool {
	return s == TurnResultSucceeded || s == TurnResultFailed || s == TurnResultCanceled || s == TurnResultUncertain || s == TurnResultWaitingInput
}

type TurnResult struct {
	Status            TurnResultStatus `json:"status"`
	ProviderSessionID string           `json:"provider_session_id,omitempty"`
	Result            string           `json:"result,omitempty"`
	Error             string           `json:"error,omitempty"`
	UsageJSON         json.RawMessage  `json:"usage,omitempty"`
	SideEffectsKnown  bool             `json:"side_effects_known"`
}

func (r TurnResult) Validate() error {
	if !r.Status.Valid() {
		return domain.ErrInvalidInput("unsupported turn result status")
	}
	if len(r.UsageJSON) != 0 && !json.Valid(r.UsageJSON) {
		return domain.ErrInvalidInput("turn usage must be valid JSON")
	}
	return nil
}

type AgentRuntimeAdapter interface {
	Descriptor(context.Context) (AdapterDescriptor, error)
	Validate(context.Context, domain.ExecutionSpec) error
	Health(context.Context) error
	StartTurn(context.Context, TurnRequest, EventSink) (TurnHandle, error)
}

type TurnHandle interface {
	Wait(context.Context) (TurnResult, error)
	Steer(context.Context, domain.Message) error
	DecideApproval(context.Context, domain.ApprovalDecision) error
	RequestCancel(context.Context) error
}
