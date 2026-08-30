package domain

import (
	"fmt"
	"strings"
	"time"
)

const MaxOpaqueIDLength = 128

type AgentIdentityStatus string

const (
	AgentIdentityActive   AgentIdentityStatus = "active"
	AgentIdentityDisabled AgentIdentityStatus = "disabled"
)

func (s AgentIdentityStatus) Valid() bool {
	return s == AgentIdentityActive || s == AgentIdentityDisabled
}

type Connectivity string

const (
	ConnectivityBootstrapping Connectivity = "bootstrapping"
	ConnectivityOnline        Connectivity = "online"
	ConnectivityDegraded      Connectivity = "degraded"
	ConnectivityOffline       Connectivity = "offline"
)

func (s Connectivity) Valid() bool {
	return s == ConnectivityBootstrapping || s == ConnectivityOnline || s == ConnectivityDegraded || s == ConnectivityOffline
}

type Availability string

const (
	AvailabilityIdle            Availability = "idle"
	AvailabilityBusy            Availability = "busy"
	AvailabilityWaitingInput    Availability = "waiting_input"
	AvailabilityWaitingApproval Availability = "waiting_approval"
)

func (s Availability) Valid() bool {
	return s == AvailabilityIdle || s == AvailabilityBusy || s == AvailabilityWaitingInput || s == AvailabilityWaitingApproval
}

type DeliveryReadiness string

const (
	DeliveryReady         DeliveryReadiness = "ready"
	DeliveryBackpressured DeliveryReadiness = "backpressured"
	DeliveryUnavailable   DeliveryReadiness = "unavailable"
)

func (s DeliveryReadiness) Valid() bool {
	return s == DeliveryReady || s == DeliveryBackpressured || s == DeliveryUnavailable
}

// AgentIdentity is the target logical Agent contract. It deliberately contains
// no process, pane, socket, or Worker addressing fields.
type AgentIdentity struct {
	ID             string              `json:"agent_id"`
	PrincipalID    string              `json:"principal_id"`
	OrganizationID string              `json:"organization_id"`
	DisplayName    string              `json:"display_name"`
	Status         AgentIdentityStatus `json:"status"`
	Version        int64               `json:"version"`
	CreatedAt      time.Time           `json:"created_at"`
	UpdatedAt      time.Time           `json:"updated_at"`
}

func (a AgentIdentity) Validate() error {
	if err := ValidateIdentifier("agent_id", a.ID); err != nil {
		return err
	}
	if err := ValidateOpaqueID("principal_id", a.PrincipalID); err != nil {
		return err
	}
	if err := ValidateOpaqueID("organization_id", a.OrganizationID); err != nil {
		return err
	}
	if strings.TrimSpace(a.DisplayName) == "" {
		return ErrInvalidInput("agent display_name cannot be empty")
	}
	if !a.Status.Valid() {
		return ErrInvalidInput("unsupported agent identity status")
	}
	return ValidatePositiveVersion("agent version", a.Version)
}

func ValidateIdentifier(label string, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ErrInvalidInput(label + " cannot be empty")
	}
	if !identifierRegex.MatchString(trimmed) {
		return ErrInvalidInput(fmt.Sprintf("invalid %s '%s'", label, trimmed))
	}
	return nil
}

func ValidateOpaqueID(label string, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ErrInvalidInput(label + " cannot be empty")
	}
	if len(trimmed) > MaxOpaqueIDLength || strings.ContainsAny(trimmed, "\r\n\t") {
		return ErrInvalidInput(fmt.Sprintf("%s must be at most %d characters and cannot contain control characters", label, MaxOpaqueIDLength))
	}
	return nil
}

func ValidatePositiveVersion(label string, version int64) error {
	if version <= 0 {
		return ErrInvalidInput(label + " must be positive")
	}
	return nil
}
