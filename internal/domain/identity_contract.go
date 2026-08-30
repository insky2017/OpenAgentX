package domain

import (
	"sort"
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

type WebRole string

const (
	WebRoleOwner    WebRole = "owner"
	WebRoleOperator WebRole = "operator"
	WebRoleViewer   WebRole = "viewer"
)

func (r WebRole) Valid() bool {
	return r == WebRoleOwner || r == WebRoleOperator || r == WebRoleViewer
}

type WebUserRecord struct {
	ID             string         `json:"web_user_id"`
	PrincipalID    string         `json:"principal_id"`
	Username       string         `json:"username"`
	PasswordDigest string         `json:"-"`
	Roles          []WebRole      `json:"roles"`
	Status         IdentityStatus `json:"status"`
	PasswordSetAt  time.Time      `json:"password_changed_at"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

func (u WebUserRecord) Validate() error {
	if err := ValidateOpaqueID("web_user_id", u.ID); err != nil {
		return err
	}
	if err := ValidateOpaqueID("principal_id", u.PrincipalID); err != nil {
		return err
	}
	if strings.TrimSpace(u.Username) == "" || strings.ContainsAny(u.Username, "\r\n\t") {
		return ErrInvalidInput("web username cannot be empty or contain control characters")
	}
	if strings.TrimSpace(u.PasswordDigest) == "" {
		return ErrInvalidInput("web password digest cannot be empty")
	}
	if !u.Status.Valid() {
		return ErrInvalidInput("unsupported web user status")
	}
	if len(u.Roles) == 0 {
		return ErrInvalidInput("web user requires at least one role")
	}
	seen := make(map[WebRole]struct{}, len(u.Roles))
	for _, role := range u.Roles {
		if !role.Valid() {
			return ErrInvalidInput("unsupported web user role")
		}
		if _, exists := seen[role]; exists {
			return ErrInvalidInput("web user roles must be unique")
		}
		seen[role] = struct{}{}
	}
	return nil
}

func (u WebUserRecord) HasRole(role WebRole) bool {
	for _, candidate := range u.Roles {
		if candidate == role {
			return true
		}
	}
	return false
}

func SortWebRoles(roles []WebRole) []WebRole {
	result := append([]WebRole(nil), roles...)
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
