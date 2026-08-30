package domain

import (
	"encoding/json"
	"time"
)

type JournalEvent struct {
	Sequence         int64           `json:"sequence"`
	ID               string          `json:"event_id"`
	OrganizationID   string          `json:"organization_id,omitempty"`
	AggregateType    string          `json:"aggregate_type"`
	AggregateID      string          `json:"aggregate_id"`
	EventType        string          `json:"event_type"`
	ActorPrincipalID string          `json:"actor_principal_id"`
	Payload          json.RawMessage `json:"payload"`
	CreatedAt        time.Time       `json:"created_at"`
}

func (e JournalEvent) Validate() error {
	if e.Sequence < 0 {
		return ErrInvalidInput("event sequence cannot be negative")
	}
	if err := ValidateOpaqueID("event_id", e.ID); err != nil {
		return err
	}
	if err := ValidateIdentifier("aggregate_type", e.AggregateType); err != nil {
		return err
	}
	if err := ValidateOpaqueID("aggregate_id", e.AggregateID); err != nil {
		return err
	}
	if err := ValidateIdentifier("event_type", e.EventType); err != nil {
		return err
	}
	if err := ValidateOpaqueID("actor_principal_id", e.ActorPrincipalID); err != nil {
		return err
	}
	if len(e.Payload) == 0 || !json.Valid(e.Payload) {
		return ErrInvalidInput("event payload must be valid JSON")
	}
	return nil
}
