package domain

import (
	"fmt"
	"strings"
	"time"
)

type TaskStatus string

const (
	TaskStatusQueued    TaskStatus = "queued"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusSucceeded TaskStatus = "succeeded"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCanceled  TaskStatus = "canceled"
)

type Task struct {
	ID             string     `json:"id"`
	SenderAgentID  string     `json:"sender_agent_id"`
	TargetAgentID  string     `json:"target_agent_id"`
	IdempotencyKey string     `json:"idempotency_key"`
	Content        string     `json:"content"`
	Status         TaskStatus `json:"status"`
	Result         *string    `json:"result,omitempty"`
	Error          *string    `json:"error,omitempty"`
	CreatedAt      string     `json:"created_at"`
	UpdatedAt      string     `json:"updated_at"`
}

func (t *Task) IsTerminal() bool {
	return t.Status == TaskStatusSucceeded || t.Status == TaskStatusFailed || t.Status == TaskStatusCanceled
}

func (t *Task) IsActive() bool {
	return t.Status == TaskStatusQueued || t.Status == TaskStatusRunning
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
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if t.CreatedAt == "" {
		t.CreatedAt = now
	}
	if t.UpdatedAt == "" {
		t.UpdatedAt = now
	}
	return nil
}
