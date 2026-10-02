package domain

import (
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
	TargetAgentID    string              `json:"target_agent_id,omitempty"`
	Kind             ExternalMessageKind `json:"kind"`
	ReplyToMessageID string              `json:"reply_to_message_id,omitempty"`
	Content          string              `json:"content"`
	IdempotencyKey   string              `json:"idempotency_key"`
	OriginTaskID     string              `json:"origin_task_id,omitempty"`
}

func (v SendExternalMessageInput) Validate() error {
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
		if v.TargetAgentID != "" || v.OriginTaskID != "" {
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
	Sequence         int64               `json:"sequence"`
	ID               string              `json:"message_id"`
	SenderAgentID    string              `json:"sender_agent_id"`
	TargetAgentID    string              `json:"target_agent_id"`
	SenderBindingID  string              `json:"sender_binding_id"`
	SenderGeneration int64               `json:"sender_generation"`
	Kind             ExternalMessageKind `json:"kind"`
	ReplyToMessageID string              `json:"reply_to_message_id,omitempty"`
	Content          string              `json:"content"`
	IdempotencyKey   string              `json:"idempotency_key"`
	OriginTaskID     string              `json:"origin_task_id,omitempty"`
	PayloadDigest    string              `json:"-"`
	DeliveryState    string              `json:"delivery_state"`
	CreatedAt        time.Time           `json:"created_at"`
	AcknowledgedAt   *time.Time          `json:"acknowledged_at,omitempty"`
}
