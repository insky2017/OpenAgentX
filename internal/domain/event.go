package domain

import (
	"strings"
	"time"
)

type EventType string

const (
	EventTaskSubmitted      EventType = "task.submitted"
	EventTaskNotified       EventType = "task.notified"
	EventTaskDeliveryFailed EventType = "task.delivery_failed"
	EventTaskAcknowledged   EventType = "task.acknowledged"
	EventTaskStatusUpdated  EventType = "task.status_updated"
	EventTaskMessageSent    EventType = "task.message_sent"
	EventTaskSucceeded      EventType = "task.succeeded"
	EventTaskFailed         EventType = "task.failed"
	EventTaskCanceled       EventType = "task.canceled"
	EventRuntimeObserved    EventType = "runtime.event_observed"
)

const (
	AGYEventPreToolUse     = "PreToolUse"
	AGYEventPostToolUse    = "PostToolUse"
	AGYEventPreInvocation  = "PreInvocation"
	AGYEventPostInvocation = "PostInvocation"
	AGYEventStop           = "Stop"
)

var AllowedAGYEvents = map[string]bool{
	AGYEventPreToolUse:     true,
	AGYEventPostToolUse:    true,
	AGYEventPreInvocation:  true,
	AGYEventPostInvocation: true,
	AGYEventStop:           true,
}

func IsValidAGYEvent(ev string) bool {
	return AllowedAGYEvents[ev]
}

type Event struct {
	Sequence     int64     `json:"sequence"`
	ID           string    `json:"id"`
	TaskID       string    `json:"task_id"`
	ActorAgentID string    `json:"actor_agent_id"`
	Type         EventType `json:"type"`
	Payload      string    `json:"payload"`
	CreatedAt    string    `json:"created_at"`
}

func (e *Event) Validate() error {
	if strings.TrimSpace(e.ID) == "" {
		return ErrInvalidInput("event id cannot be empty")
	}
	if strings.TrimSpace(e.TaskID) == "" {
		return ErrInvalidInput("task_id cannot be empty")
	}
	if strings.TrimSpace(e.ActorAgentID) == "" {
		return ErrInvalidInput("actor_agent_id cannot be empty")
	}
	if e.Type == "" {
		return ErrInvalidInput("event type cannot be empty")
	}
	if e.CreatedAt == "" {
		e.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return nil
}
