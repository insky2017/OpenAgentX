package domain

import (
	"strings"
	"time"
)

type MessageKind string

const (
	MessageKindInstruction  MessageKind = "instruction"
	MessageKindSupplement   MessageKind = "supplement"
	MessageKindStatusUpdate MessageKind = "status_update"
)

type Message struct {
	ID                string      `json:"id"`
	Version           int64       `json:"version"`
	Sequence          int64       `json:"sequence,omitempty"`
	TaskID            string      `json:"task_id"`
	SenderAgentID     string      `json:"sender_agent_id"`
	SenderPrincipalID string      `json:"sender_principal_id,omitempty"`
	TargetAgentID     string      `json:"target_agent_id,omitempty"`
	Kind              MessageKind `json:"kind"`
	Content           string      `json:"content"`
	CreatedAt         string      `json:"created_at"`
}

func (k MessageKind) Valid() bool {
	switch k {
	case MessageKindInstruction, MessageKindSupplement, MessageKindStatusUpdate:
		return true
	default:
		return false
	}
}

func (m *Message) Validate() error {
	if strings.TrimSpace(m.ID) == "" {
		return ErrInvalidInput("message id cannot be empty")
	}
	if strings.TrimSpace(m.TaskID) == "" {
		return ErrInvalidInput("task_id cannot be empty")
	}
	if strings.TrimSpace(m.SenderAgentID) == "" {
		return ErrInvalidInput("sender_agent_id cannot be empty")
	}
	if m.Kind == "" {
		return ErrInvalidInput("message kind cannot be empty")
	}
	if !m.Kind.Valid() {
		return ErrInvalidInput("unsupported message kind")
	}
	if m.Version < 0 || m.Sequence < 0 {
		return ErrInvalidInput("message version and sequence cannot be negative")
	}
	if strings.TrimSpace(m.Content) == "" {
		return ErrInvalidInput("message content cannot be empty")
	}
	if m.CreatedAt == "" {
		m.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return nil
}

// ValidateTarget validates the ADR-001 persistent Message contract.
func (m *Message) ValidateTarget() error {
	if err := ValidateOpaqueID("message_id", m.ID); err != nil {
		return err
	}
	if err := ValidatePositiveVersion("message version", m.Version); err != nil {
		return err
	}
	if m.Sequence <= 0 {
		return ErrInvalidInput("message sequence must be positive")
	}
	if err := ValidateOpaqueID("task_id", m.TaskID); err != nil {
		return err
	}
	if err := ValidateOpaqueID("sender_principal_id", m.SenderPrincipalID); err != nil {
		return err
	}
	if err := ValidateIdentifier("target_agent_id", m.TargetAgentID); err != nil {
		return err
	}
	if !m.Kind.Valid() {
		return ErrInvalidInput("unsupported message kind")
	}
	if strings.TrimSpace(m.Content) == "" {
		return ErrInvalidInput("message content cannot be empty")
	}
	return nil
}
