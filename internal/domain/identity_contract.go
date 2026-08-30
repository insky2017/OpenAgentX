package domain

import (
	"strings"
	"time"
)

type PrincipalKind string

const (
	PrincipalHuman  PrincipalKind = "human"
	PrincipalAgent  PrincipalKind = "agent"
	PrincipalWorker PrincipalKind = "worker"
	PrincipalSystem PrincipalKind = "system"
)

func (k PrincipalKind) Valid() bool {
	return k == PrincipalHuman || k == PrincipalAgent || k == PrincipalWorker || k == PrincipalSystem
}

type IdentityStatus string

const (
	IdentityActive   IdentityStatus = "active"
	IdentityDisabled IdentityStatus = "disabled"
)

func (s IdentityStatus) Valid() bool {
	return s == IdentityActive || s == IdentityDisabled
}

type Principal struct {
	ID          string         `json:"principal_id"`
	Kind        PrincipalKind  `json:"kind"`
	DisplayName string         `json:"display_name"`
	Status      IdentityStatus `json:"status"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

func (p Principal) Validate() error {
	if err := ValidateOpaqueID("principal_id", p.ID); err != nil {
		return err
	}
	if !p.Kind.Valid() || !p.Status.Valid() {
		return ErrInvalidInput("unsupported principal kind or status")
	}
	if strings.TrimSpace(p.DisplayName) == "" {
		return ErrInvalidInput("principal display_name cannot be empty")
	}
	return nil
}

type Organization struct {
	ID        string         `json:"organization_id"`
	Name      string         `json:"name"`
	Status    IdentityStatus `json:"status"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

func (o Organization) Validate() error {
	if err := ValidateOpaqueID("organization_id", o.ID); err != nil {
		return err
	}
	if strings.TrimSpace(o.Name) == "" {
		return ErrInvalidInput("organization name cannot be empty")
	}
	if !o.Status.Valid() {
		return ErrInvalidInput("unsupported organization status")
	}
	return nil
}
