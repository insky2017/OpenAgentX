package domain

import (
	"encoding/json"
	"time"
)

type RunAttemptStatus string

const (
	RunAttemptStarting        RunAttemptStatus = "starting"
	RunAttemptRunning         RunAttemptStatus = "running"
	RunAttemptWaitingApproval RunAttemptStatus = "waiting_approval"
	RunAttemptFinishing       RunAttemptStatus = "finishing"
	RunAttemptSucceeded       RunAttemptStatus = "succeeded"
	RunAttemptFailed          RunAttemptStatus = "failed"
	RunAttemptCanceled        RunAttemptStatus = "canceled"
	RunAttemptUncertain       RunAttemptStatus = "uncertain"
)

func (s RunAttemptStatus) Valid() bool {
	switch s {
	case RunAttemptStarting, RunAttemptRunning, RunAttemptWaitingApproval, RunAttemptFinishing,
		RunAttemptSucceeded, RunAttemptFailed, RunAttemptCanceled, RunAttemptUncertain:
		return true
	default:
		return false
	}
}

func (s RunAttemptStatus) Active() bool {
	return s == RunAttemptStarting || s == RunAttemptRunning || s == RunAttemptWaitingApproval || s == RunAttemptFinishing
}

type RunAttempt struct {
	ID                     string           `json:"run_id"`
	TaskID                 string           `json:"task_id"`
	AgentID                string           `json:"agent_id"`
	Version                int64            `json:"version"`
	Status                 RunAttemptStatus `json:"status"`
	WorkerInstanceID       string           `json:"worker_instance_id"`
	FencingToken           int64            `json:"fencing_token"`
	LeaseUntil             time.Time        `json:"lease_until"`
	ExecutionSpecVersion   int64            `json:"execution_spec_version"`
	RequestedExecutionJSON string           `json:"requested_execution_json"`
	ResolvedExecutionJSON  string           `json:"resolved_execution_json"`
	AdapterID              string           `json:"adapter_id"`
	BackendID              string           `json:"backend_id"`
	Model                  string           `json:"model"`
	ReasoningMode          ReasoningMode    `json:"reasoning_mode"`
	ReasoningValue         string           `json:"reasoning_value"`
	StartedAt              time.Time        `json:"started_at"`
	FinishedAt             *time.Time       `json:"finished_at,omitempty"`
	ResultJSON             string           `json:"result_json,omitempty"`
	CreatedAt              time.Time        `json:"created_at"`
	UpdatedAt              time.Time        `json:"updated_at"`
}

func (r RunAttempt) Validate() error {
	if err := ValidateOpaqueID("run_id", r.ID); err != nil {
		return err
	}
	if err := ValidateOpaqueID("task_id", r.TaskID); err != nil {
		return err
	}
	if err := ValidateIdentifier("agent_id", r.AgentID); err != nil {
		return err
	}
	if err := ValidatePositiveVersion("run version", r.Version); err != nil {
		return err
	}
	if !r.Status.Valid() {
		return ErrInvalidInput("unsupported run attempt status")
	}
	if err := ValidateOpaqueID("worker_instance_id", r.WorkerInstanceID); err != nil {
		return err
	}
	if err := ValidatePositiveVersion("fencing_token", r.FencingToken); err != nil {
		return err
	}
	if r.LeaseUntil.IsZero() || r.StartedAt.IsZero() {
		return ErrInvalidInput("run lease_until and started_at are required")
	}
	if err := ValidatePositiveVersion("execution_spec_version", r.ExecutionSpecVersion); err != nil {
		return err
	}
	if !json.Valid([]byte(r.RequestedExecutionJSON)) || !json.Valid([]byte(r.ResolvedExecutionJSON)) {
		return ErrInvalidInput("run execution specs must be valid JSON")
	}
	if err := ValidateIdentifier("adapter_id", r.AdapterID); err != nil {
		return err
	}
	if err := ValidateIdentifier("backend_id", r.BackendID); err != nil {
		return err
	}
	if err := ValidateOpaqueID("model", r.Model); err != nil {
		return err
	}
	if !r.ReasoningMode.Valid() {
		return ErrInvalidInput("unsupported run reasoning_mode")
	}
	if r.ResultJSON != "" && !json.Valid([]byte(r.ResultJSON)) {
		return ErrInvalidInput("run result_json must be valid JSON")
	}
	return nil
}

func ValidateSingleActiveRun(runs []RunAttempt) error {
	activeByAgent := make(map[string]string)
	for _, run := range runs {
		if !run.Status.Active() {
			continue
		}
		if previous, exists := activeByAgent[run.AgentID]; exists {
			return ErrConflict("agent has multiple active runs: " + previous + " and " + run.ID)
		}
		activeByAgent[run.AgentID] = run.ID
	}
	return nil
}

type SessionBindingState string

const (
	SessionBindingActive  SessionBindingState = "active"
	SessionBindingInvalid SessionBindingState = "invalid"
)

func (s SessionBindingState) Valid() bool {
	return s == SessionBindingActive || s == SessionBindingInvalid
}

type SessionBinding struct {
	ID                string              `json:"session_binding_id"`
	ContextID         string              `json:"context_id"`
	AgentID           string              `json:"agent_id"`
	BackendID         string              `json:"backend_id"`
	ProviderSessionID string              `json:"provider_session_id"`
	State             SessionBindingState `json:"state"`
	Version           int64               `json:"version"`
	CreatedAt         time.Time           `json:"created_at"`
	UpdatedAt         time.Time           `json:"updated_at"`
}

func (s SessionBinding) Validate() error {
	if err := ValidateOpaqueID("session_binding_id", s.ID); err != nil {
		return err
	}
	if err := ValidateOpaqueID("context_id", s.ContextID); err != nil {
		return err
	}
	if err := ValidateIdentifier("agent_id", s.AgentID); err != nil {
		return err
	}
	if err := ValidateIdentifier("backend_id", s.BackendID); err != nil {
		return err
	}
	if err := ValidateOpaqueID("provider_session_id", s.ProviderSessionID); err != nil {
		return err
	}
	if !s.State.Valid() {
		return ErrInvalidInput("unsupported session binding state")
	}
	return ValidatePositiveVersion("session binding version", s.Version)
}
