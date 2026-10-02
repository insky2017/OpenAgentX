package domain

import (
	"fmt"
	"strings"
	"time"
)

type ExternalSessionBinding struct {
	ID                  string    `json:"binding_id"`
	AgentID             string    `json:"agent_id"`
	PrincipalID         string    `json:"principal_id"`
	OrganizationID      string    `json:"organization_id"`
	HostID              string    `json:"host_id"`
	ThreadID            string    `json:"thread_id"`
	Generation          int64     `json:"generation"`
	State               string    `json:"state"`
	AllowedPeerAgentIDs []string  `json:"allowed_peer_agent_ids"`
	TokenDigest         string    `json:"-"`
	TokenExpiresAt      time.Time `json:"token_expires_at"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type BindExternalSessionInput struct {
	AgentID             string   `json:"agent_id"`
	HostID              string   `json:"host_id"`
	ThreadID            string   `json:"thread_id"`
	AllowedPeerAgentIDs []string `json:"allowed_peer_agent_ids"`
	ExpectedGeneration  int64    `json:"expected_generation"`
}

func (v BindExternalSessionInput) Validate() error {
	for label, value := range map[string]string{"agent_id": v.AgentID, "host_id": v.HostID, "thread_id": v.ThreadID} {
		if value != strings.TrimSpace(value) {
			return ErrInvalidInput(label + " cannot contain surrounding whitespace")
		}
		if err := ValidateIdentifier(label, value); err != nil {
			return err
		}
	}
	if err := ValidateIdentifier("agent_id", v.AgentID); err != nil {
		return err
	}
	if v.ExpectedGeneration < 0 || len(v.AllowedPeerAgentIDs) == 0 || len(v.AllowedPeerAgentIDs) > 64 {
		return ErrInvalidInput("expected generation and allowed peers are invalid")
	}
	seen := map[string]bool{}
	for _, peer := range v.AllowedPeerAgentIDs {
		if err := ValidateIdentifier("peer agent_id", peer); err != nil {
			return err
		}
		if peer == v.AgentID || seen[peer] {
			return ErrInvalidInput("peers must be unique and exclude self")
		}
		seen[peer] = true
	}
	return nil
}

type ExternalMessageKind string

const (
	ExternalMessageConsultation ExternalMessageKind = "consultation"
	ExternalMessageRequest      ExternalMessageKind = "request"
	ExternalMessageResult       ExternalMessageKind = "result"
)

type SendExternalMessageInput struct {
	Scope                  string              `json:"scope,omitempty"`
	ForwardedFromMessageID string              `json:"forwarded_from_message_id,omitempty"`
	TargetAgentID          string              `json:"target_agent_id,omitempty"`
	Kind                   ExternalMessageKind `json:"kind"`
	ReplyToMessageID       string              `json:"reply_to_message_id,omitempty"`
	Content                string              `json:"content"`
	IdempotencyKey         string              `json:"idempotency_key"`
	OriginTaskID           string              `json:"origin_task_id,omitempty"`
}

func (v SendExternalMessageInput) Validate() error {
	if v.Scope != "" {
		if v.Scope != strings.TrimSpace(v.Scope) {
			return ErrInvalidInput("scope cannot contain surrounding whitespace")
		}
		if err := ValidateIdentifier("scope", v.Scope); err != nil {
			return err
		}
	}
	if v.ForwardedFromMessageID != "" {
		if err := ValidateOpaqueID("forwarded_from_message_id", v.ForwardedFromMessageID); err != nil {
			return err
		}
		if v.Kind != ExternalMessageRequest || v.OriginTaskID != "" {
			return ErrInvalidInput("only requests may forward a message; origin is derived from the source")
		}
	}
	if err := ValidateOpaqueID("idempotency_key", v.IdempotencyKey); err != nil {
		return err
	}
	if strings.TrimSpace(v.Content) == "" || len(v.Content) > 65536 {
		return ErrInvalidInput("message content must contain 1 to 65536 bytes")
	}
	if v.OriginTaskID != "" {
		if err := ValidateOpaqueID("origin_task_id", v.OriginTaskID); err != nil {
			return err
		}
	}
	switch v.Kind {
	case ExternalMessageConsultation, ExternalMessageRequest:
		if err := ValidateIdentifier("target_agent_id", v.TargetAgentID); err != nil {
			return err
		}
		if v.ReplyToMessageID != "" {
			return ErrInvalidInput("only results may reference a request")
		}
	case ExternalMessageResult:
		if v.TargetAgentID != "" || v.OriginTaskID != "" || v.Scope != "" || v.ForwardedFromMessageID != "" {
			return ErrInvalidInput("result target and origin are derived from the request")
		}
		if err := ValidateOpaqueID("reply_to_message_id", v.ReplyToMessageID); err != nil {
			return err
		}
	default:
		return ErrInvalidInput("unsupported external message kind")
	}
	return nil
}

type ExternalMessage struct {
	Scope                  string              `json:"scope,omitempty"`
	ForwardedFromMessageID string              `json:"forwarded_from_message_id,omitempty"`
	ProcessingState        string              `json:"processing_state"`
	ProcessingNote         string              `json:"processing_note,omitempty"`
	Sequence               int64               `json:"sequence"`
	ID                     string              `json:"message_id"`
	SenderAgentID          string              `json:"sender_agent_id"`
	TargetAgentID          string              `json:"target_agent_id"`
	SenderBindingID        string              `json:"sender_binding_id"`
	SenderGeneration       int64               `json:"sender_generation"`
	Kind                   ExternalMessageKind `json:"kind"`
	ReplyToMessageID       string              `json:"reply_to_message_id,omitempty"`
	Content                string              `json:"content"`
	IdempotencyKey         string              `json:"idempotency_key"`
	OriginTaskID           string              `json:"origin_task_id,omitempty"`
	PayloadDigest          string              `json:"-"`
	DeliveryState          string              `json:"delivery_state"`
	CreatedAt              time.Time           `json:"created_at"`
	AcknowledgedAt         *time.Time          `json:"acknowledged_at,omitempty"`
}

// ExternalRoleCatalog is owner-managed metadata, not a semantic body validator
// or a sandbox for tools running in the original external host.
type ExternalRoleRule struct {
	Scope        string `json:"scope"`
	OwnerAgentID string `json:"owner_agent_id"`
	Description  string `json:"description"`
}
type ExternalRoleCatalog struct {
	OrganizationID string             `json:"organization_id"`
	Revision       int64              `json:"revision"`
	Rules          []ExternalRoleRule `json:"rules"`
}
type ApplyExternalRolesInput struct {
	OrganizationID  string             `json:"organization_id"`
	ExpectedVersion int64              `json:"expected_version"`
	Rules           []ExternalRoleRule `json:"rules"`
}

func (v ApplyExternalRolesInput) Validate() error {
	if err := ValidateOpaqueID("organization_id", v.OrganizationID); err != nil {
		return err
	}
	if v.ExpectedVersion < 0 || len(v.Rules) == 0 || len(v.Rules) > 200 {
		return ErrInvalidInput("roles require 1..200 rules and nonnegative expected_version")
	}
	seen := map[string]bool{}
	for _, r := range v.Rules {
		if r.Scope != strings.TrimSpace(r.Scope) || r.OwnerAgentID != strings.TrimSpace(r.OwnerAgentID) {
			return ErrInvalidInput("scope and owner cannot contain surrounding whitespace")
		}
		if err := ValidateIdentifier("scope", r.Scope); err != nil {
			return err
		}
		if err := ValidateIdentifier("owner_agent_id", r.OwnerAgentID); err != nil {
			return err
		}
		if seen[r.Scope] || strings.TrimSpace(r.Description) == "" || len(r.Description) > 2048 {
			return ErrInvalidInput("role scopes must be unique and descriptions contain 1..2048 bytes")
		}
		seen[r.Scope] = true
	}
	return nil
}

type ExternalReceiptInput struct {
	State string `json:"state"`
	Note  string `json:"note"`
}

func (v ExternalReceiptInput) Validate() error {
	if v.State != "accepted" && v.State != "needs_clarification" && v.State != "out_of_scope" {
		return ErrInvalidInput("receipt state must be accepted, needs_clarification or out_of_scope")
	}
	if strings.TrimSpace(v.Note) == "" || len(v.Note) > 65536 {
		return ErrInvalidInput("receipt note must contain 1..65536 bytes; record unknown effects explicitly")
	}
	return nil
}

type ExternalScopeError struct {
	Code         string `json:"code"`
	Scope        string `json:"scope"`
	OwnerAgentID string `json:"owner_agent_id,omitempty"`
	Message      string `json:"message"`
}

func (e *ExternalScopeError) Error() string {
	return fmt.Sprintf("%s: %s (scope=%s owner=%s)", e.Code, e.Message, e.Scope, e.OwnerAgentID)
}
