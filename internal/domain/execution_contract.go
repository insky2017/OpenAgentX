package domain

import (
	"encoding/json"
	"time"
)

type ReasoningMode string

const (
	ReasoningBackendDefault ReasoningMode = "backend_default"
	ReasoningEffort         ReasoningMode = "effort"
	ReasoningBudgetTokens   ReasoningMode = "budget_tokens"
)

func (m ReasoningMode) Valid() bool {
	return m == ReasoningBackendDefault || m == ReasoningEffort || m == ReasoningBudgetTokens
}

type SessionMode string

const (
	SessionModeNew    SessionMode = "new"
	SessionModeResume SessionMode = "resume"
	SessionModeFork   SessionMode = "fork"
)

func (m SessionMode) Valid() bool {
	return m == SessionModeNew || m == SessionModeResume || m == SessionModeFork
}

type ReasoningSpec struct {
	Mode  ReasoningMode `json:"mode"`
	Value string        `json:"value,omitempty"`
}

type SessionSpec struct {
	ForceNew  bool        `json:"force_new,omitempty"`
	Mode      SessionMode `json:"mode"`
	ContextID string      `json:"context_id,omitempty"`
}

type ExecutionBudget struct {
	MaxTokens int64 `json:"max_tokens,omitempty"`
}

type ExecutionSpec struct {
	AdapterID      string          `json:"adapter_id"`
	BackendID      string          `json:"backend_id"`
	Model          string          `json:"model"`
	Reasoning      ReasoningSpec   `json:"reasoning"`
	Session        SessionSpec     `json:"session"`
	ApprovalPolicy string          `json:"approval_policy"`
	Sandbox        string          `json:"sandbox"`
	Timeout        time.Duration   `json:"timeout"`
	Budget         ExecutionBudget `json:"budget"`
	BackendOptions json.RawMessage `json:"backend_options"`
	Network        NetworkPolicy   `json:"network,omitempty"`
}

func (s ExecutionSpec) ValidateShape() error {
	if err := ValidateIdentifier("adapter_id", s.AdapterID); err != nil {
		return err
	}
	if err := ValidateIdentifier("backend_id", s.BackendID); err != nil {
		return err
	}
	if err := ValidateIdentifier("model", s.Model); err != nil {
		return err
	}
	if !s.Reasoning.Mode.Valid() || !s.Session.Mode.Valid() {
		return ErrInvalidInput("unsupported reasoning or session mode")
	}
	if s.Session.ForceNew && (s.Session.Mode != SessionModeNew || s.AdapterID != "codex-app-server") {
		return ErrInvalidInput("force_new requires a new Codex session")
	}
	if s.Timeout < 0 || (s.Timeout == 0 && s.AdapterID != "codex-app-server") {
		return ErrInvalidInput("execution timeout must be positive; Codex permits zero for no execution deadline")
	}
	if s.Budget.MaxTokens < 0 {
		return ErrInvalidInput("max_tokens cannot be negative")
	}
	if len(s.BackendOptions) != 0 && !json.Valid(s.BackendOptions) {
		return ErrInvalidInput("backend_options must be valid JSON")
	}
	if err := s.Network.Validate(); err != nil {
		return err
	}
	return nil
}

// AgentExecutionInput freezes the local Agent profile and role for one Run.
// Content is captured once; later file edits apply only to subsequent Runs.
type AgentExecutionInput struct {
	ProfileVersion      int64  `json:"profile_version"`
	InstructionsPath    string `json:"instructions_path"`
	InstructionsSHA256  string `json:"instructions_sha256"`
	InstructionsContent string `json:"instructions_content"`
	WorkspaceRoot       string `json:"workspace_root"`
}

type ResolvedExecutionSpec struct {
	AgentInput *AgentExecutionInput `json:"agent_input,omitempty"`
	DeadlineAt time.Time            `json:"deadline_at,omitempty"`
	Version    int64                `json:"version"`
	Spec       ExecutionSpec        `json:"spec"`
	Sources    map[string]string    `json:"sources"`
}
