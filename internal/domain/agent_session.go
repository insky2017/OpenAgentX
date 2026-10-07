package domain

import "time"

// AgentSession selects future work without changing any historical Task binding.
// Version zero is a durable legacy bootstrap; only a completed handoff advances it.
type AgentSession struct {
	AgentID       string    `json:"agent_id"`
	BackendID     string    `json:"backend_id"`
	ThreadID      string    `json:"thread_id"`
	ContextTaskID string    `json:"context_task_id"`
	Version       int64     `json:"version"`
	PendingTaskID string    `json:"pending_task_id,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type NewSessionRequest struct {
	BackendID        string `json:"backend_id"`
	ExpectedThreadID string `json:"expected_thread_id"`
	ExpectedVersion  int64  `json:"expected_version"`
}

func (r NewSessionRequest) Validate() error {
	if err := ValidateIdentifier("backend_id", r.BackendID); err != nil {
		return err
	}
	if r.ExpectedThreadID != "" {
		if err := ValidateOpaqueID("expected_thread_id", r.ExpectedThreadID); err != nil {
			return err
		}
	}
	if r.ExpectedVersion < 0 {
		return ErrInvalidInput("expected session version cannot be negative")
	}
	return nil
}
