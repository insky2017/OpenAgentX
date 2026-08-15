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
	ID            string      `json:"id"`
	TaskID        string      `json:"task_id"`
	SenderAgentID string      `json:"sender_agent_id"`
	Kind          MessageKind `json:"kind"`
	Content       string      `json:"content"`
	CreatedAt     string      `json:"created_at"`
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
	if strings.TrimSpace(m.Content) == "" {
		return ErrInvalidInput("message content cannot be empty")
	}
	if m.CreatedAt == "" {
		m.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return nil
}
