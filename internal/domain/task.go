package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type TaskStatus string

const (
	TaskStatusQueued          TaskStatus = "queued"
	TaskStatusDispatching     TaskStatus = "dispatching"
	TaskStatusRunning         TaskStatus = "running"
	TaskStatusWaitingInput    TaskStatus = "waiting_input"
	TaskStatusWaitingApproval TaskStatus = "waiting_approval"
	TaskStatusCancelRequested TaskStatus = "cancel_requested"
	TaskStatusSucceeded       TaskStatus = "succeeded"
	TaskStatusFailed          TaskStatus = "failed"
	TaskStatusCanceled        TaskStatus = "canceled"
	TaskStatusUncertain       TaskStatus = "uncertain"
)

type DispatchMode string

const (
	DispatchModeCoordinated DispatchMode = "coordinated"
	DispatchModeDirect      DispatchMode = "direct"
)

func (s TaskStatus) Valid() bool {
	switch s {
	case TaskStatusQueued,
		TaskStatusDispatching,
		TaskStatusRunning,
		TaskStatusWaitingInput,
		TaskStatusWaitingApproval,
		TaskStatusCancelRequested,
		TaskStatusSucceeded,
		TaskStatusFailed,
		TaskStatusCanceled,
		TaskStatusUncertain:
		return true
	default:
		return false
	}
}

// TaskIntent is declared by the authenticated task creator. It describes the
// requested contract, not a Runtime capability or terminal-state shortcut.
type TaskIntent string

const (
	TaskIntentMutation TaskIntent = "mutation"
	TaskIntentQuery    TaskIntent = "query"
)

func (i TaskIntent) Valid() bool {
	return i == TaskIntentMutation || i == TaskIntentQuery
}

// NormalizeTaskIntent preserves compatibility for Go fixtures and legacy
// callers that did not have an intent field. HTTP JSON uses UnmarshalJSON,
// which rejects an explicit null, empty, or unknown value.
func NormalizeTaskIntent(intent TaskIntent) (TaskIntent, error) {
	if intent == "" {
		return TaskIntentMutation, nil
	}
	if !intent.Valid() {
		return "", ErrInvalidInput("task intent must be query or mutation")
	}
	return intent, nil
}

func (i *TaskIntent) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		return ErrInvalidInput("task intent cannot be null")
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return ErrInvalidInput("task intent must be query or mutation")
	}
	parsed := TaskIntent(value)
	if !parsed.Valid() {
		return ErrInvalidInput("task intent must be query or mutation")
	}
	*i = parsed
	return nil
}

// MarshalJSON makes a zero-valued Go request equivalent to an omitted intent.
// It does not weaken decoding: clients that explicitly send null or "" are
// still rejected by UnmarshalJSON.
func (i TaskIntent) MarshalJSON() ([]byte, error) {
	normalized, err := NormalizeTaskIntent(i)
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(normalized))
}

func (m DispatchMode) Valid() bool {
	return m == DispatchModeCoordinated || m == DispatchModeDirect
}

type Task struct {
	ID                string              `json:"id"`
	Version           int64               `json:"version"`
	SenderAgentID     string              `json:"sender_agent_id"`
	SenderPrincipalID string              `json:"sender_principal_id,omitempty"`
	TargetAgentID     string              `json:"target_agent_id"`
	OrganizationID    string              `json:"organization_id,omitempty"`
	DispatchMode      DispatchMode        `json:"dispatch_mode,omitempty"`
	Intent            TaskIntent          `json:"intent"`
	CompletionBasis   TaskCompletionBasis `json:"completion_basis"`
	ParentTaskID      *string             `json:"parent_task_id,omitempty"`
	IdempotencyKey    string              `json:"idempotency_key"`
	Content           string              `json:"content"`
	Status            TaskStatus          `json:"status"`
	Result            *string             `json:"result,omitempty"`
	Error             *string             `json:"error,omitempty"`
	CancelRequestedBy *string             `json:"cancel_requested_by,omitempty"`
	CancelRequestedAt *string             `json:"cancel_requested_at,omitempty"`
	CreatedAt         string              `json:"created_at"`
	UpdatedAt         string              `json:"updated_at"`
}

func (t *Task) IsTerminal() bool {
	return t.Status == TaskStatusSucceeded || t.Status == TaskStatusFailed || t.Status == TaskStatusCanceled || t.Status == TaskStatusUncertain
}

func (t *Task) IsActive() bool {
	return !t.IsTerminal()
}

func (t *Task) CanBeginAttempt() bool {
	switch t.Status {
	case TaskStatusQueued, TaskStatusDispatching, TaskStatusWaitingInput:
		return true
	default:
		return false
	}
}

func (t *Task) Validate() error {
	t.ID = strings.TrimSpace(t.ID)
	if t.ID == "" {
		return ErrInvalidInput("task id cannot be empty")
	}
	if strings.ContainsAny(t.ID, "\r\n\t") {
		return ErrInvalidInput("task id cannot contain newlines or control characters")
	}

	t.SenderAgentID = strings.TrimSpace(t.SenderAgentID)
	if t.SenderAgentID == "" {
		return ErrInvalidInput("sender_agent_id cannot be empty")
	}
	if !identifierRegex.MatchString(t.SenderAgentID) {
		return ErrInvalidInput(fmt.Sprintf("invalid sender_agent_id '%s'", t.SenderAgentID))
	}

	t.TargetAgentID = strings.TrimSpace(t.TargetAgentID)
	if t.TargetAgentID == "" {
		return ErrInvalidInput("target_agent_id cannot be empty")
	}
	if !identifierRegex.MatchString(t.TargetAgentID) {
		return ErrInvalidInput(fmt.Sprintf("invalid target_agent_id '%s'", t.TargetAgentID))
	}

	t.IdempotencyKey = strings.TrimSpace(t.IdempotencyKey)
	if t.IdempotencyKey == "" {
		return ErrInvalidInput("idempotency_key cannot be empty")
	}
	if len(t.IdempotencyKey) > 128 || strings.ContainsAny(t.IdempotencyKey, "\r\n\t") {
		return ErrInvalidInput("idempotency_key must be at most 128 characters and cannot contain control characters")
	}

	if strings.TrimSpace(t.Content) == "" {
		return ErrInvalidInput("task content cannot be empty")
	}

	if t.Status == "" {
		t.Status = TaskStatusQueued
	}
	if !t.Status.Valid() {
		return ErrInvalidInput(fmt.Sprintf("unsupported task status '%s'", t.Status))
	}
	if t.Version < 0 {
		return ErrInvalidInput("task version cannot be negative")
	}
	if t.DispatchMode != "" && !t.DispatchMode.Valid() {
		return ErrInvalidInput(fmt.Sprintf("unsupported dispatch_mode '%s'", t.DispatchMode))
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if t.CreatedAt == "" {
		t.CreatedAt = now
	}
	if t.UpdatedAt == "" {
		t.UpdatedAt = now
	}
	return nil
}

// ValidateTarget validates the ADR-001 persistent Task contract. Validate is
// retained only while the staged implementation still compiles the V0 path.
func (t *Task) ValidateTarget() error {
	if err := ValidateOpaqueID("task_id", t.ID); err != nil {
		return err
	}
	if err := ValidatePositiveVersion("task version", t.Version); err != nil {
		return err
	}
	if err := ValidateOpaqueID("sender_principal_id", t.SenderPrincipalID); err != nil {
		return err
	}
	if err := ValidateIdentifier("target_agent_id", t.TargetAgentID); err != nil {
		return err
	}
	if err := ValidateOpaqueID("organization_id", t.OrganizationID); err != nil {
		return err
	}
	if !t.DispatchMode.Valid() {
		return ErrInvalidInput("unsupported dispatch_mode")
	}
	intent, err := NormalizeTaskIntent(t.Intent)
	if err != nil {
		return err
	}
	t.Intent = intent
	if !t.CompletionBasis.Valid() || (t.CompletionBasis != "" && t.Status != TaskStatusSucceeded) {
		return ErrInvalidInput("invalid task completion basis")
	}
	if (t.CompletionBasis == TaskCompletionQueryResultDelivered && t.Intent != TaskIntentQuery) ||
		(t.CompletionBasis == TaskCompletionMutationEffectsKnown && t.Intent != TaskIntentMutation) {
		return ErrInvalidInput("task completion basis does not match intent")
	}
	if !t.Status.Valid() {
		return ErrInvalidInput("unsupported task status")
	}
	if err := ValidateOpaqueID("idempotency_key", t.IdempotencyKey); err != nil {
		return err
	}
	if strings.TrimSpace(t.Content) == "" {
		return ErrInvalidInput("task content cannot be empty")
	}
	if t.ParentTaskID != nil {
		if err := ValidateOpaqueID("parent_task_id", *t.ParentTaskID); err != nil {
			return err
		}
	}
	return nil
}
