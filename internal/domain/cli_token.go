package domain

import (
	"strings"
	"time"
)

type CLIScope string

const (
	CLIScopeConsoleRead       CLIScope = "console.read"
	CLIScopeConsoleControl    CLIScope = "console.control"
	CLIScopeConsoleDiagnostic CLIScope = "console.diagnostic"
	CLIScopeFleetLifecycle    CLIScope = "fleet.lifecycle"
)

func (s CLIScope) Valid() bool {
	switch s {
	case CLIScopeConsoleRead, CLIScopeConsoleControl, CLIScopeConsoleDiagnostic, CLIScopeFleetLifecycle:
		return true
	default:
		return false
	}
}

type CLITokenRecord struct {
	ID                string     `json:"token_id"`
	TokenDigest       string     `json:"-"`
	WebUserID         string     `json:"web_user_id"`
	PrincipalID       string     `json:"principal_id"`
	Scopes            []CLIScope `json:"scopes"`
	InstallationID    string     `json:"installation_id"`
	CreatedAt         time.Time  `json:"created_at"`
	LastUsedAt        time.Time  `json:"last_used_at"`
	AbsoluteExpiresAt time.Time  `json:"absolute_expires_at"`
	RevokedAt         *time.Time `json:"revoked_at,omitempty"`
}

func (r CLITokenRecord) Validate() error {
	if err := ValidateOpaqueID("cli_token_id", r.ID); err != nil {
		return err
	}
	if strings.TrimSpace(r.TokenDigest) == "" {
		return ErrInvalidInput("CLI Token digest is required")
	}
	if err := ValidateOpaqueID("web_user_id", r.WebUserID); err != nil {
		return err
	}
	if err := ValidateOpaqueID("principal_id", r.PrincipalID); err != nil {
		return err
	}
	if err := ValidateOpaqueID("installation_id", r.InstallationID); err != nil {
		return err
	}
	if len(r.Scopes) == 0 {
		return ErrInvalidInput("CLI Token requires at least one scope")
	}
	seen := make(map[CLIScope]struct{}, len(r.Scopes))
	for _, scope := range r.Scopes {
		if !scope.Valid() {
			return ErrInvalidInput("unsupported CLI Token scope")
		}
		if _, ok := seen[scope]; ok {
			return ErrInvalidInput("CLI Token scopes must be unique")
		}
		seen[scope] = struct{}{}
	}
	if r.CreatedAt.IsZero() || r.LastUsedAt.Before(r.CreatedAt) || !r.AbsoluteExpiresAt.After(r.CreatedAt) {
		return ErrInvalidInput("CLI Token timestamps are invalid")
	}
	if r.RevokedAt != nil && r.RevokedAt.Before(r.CreatedAt) {
		return ErrInvalidInput("CLI Token revocation timestamp is invalid")
	}
	return nil
}

func (r CLITokenRecord) HasScope(required CLIScope) bool {
	for _, scope := range r.Scopes {
		if scope == required {
			return true
		}
	}
	return false
}
