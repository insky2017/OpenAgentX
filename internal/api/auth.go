package api

import (
	"fmt"
	"strings"
	"time"
)

const (
	AuthLoginPath            = "/api/auth/v1/login"
	AuthLogoutPath           = "/api/auth/v1/logout"
	AuthSessionPath          = "/api/auth/v1/session"
	CLIAuthLoginPath         = "/api/auth/v1/cli/login"
	CLIAuthLogoutPath        = "/api/auth/v1/cli/logout"
	CLIAuthSessionPath       = "/api/auth/v1/cli/session"
	CLIInstallationProbePath = "/api/auth/v1/cli/installation"
)

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (r LoginRequest) Validate() error {
	if strings.TrimSpace(r.Username) == "" || r.Password == "" {
		return fmt.Errorf("username and password are required")
	}
	return nil
}

type WebPrincipal struct {
	UserID   string   `json:"user_id"`
	Username string   `json:"username"`
	Roles    []string `json:"roles"`
}

type WebSessionResponse struct {
	Principal         WebPrincipal `json:"principal"`
	CSRFToken         string       `json:"csrf_token"`
	IdleExpiresAt     time.Time    `json:"idle_expires_at"`
	AbsoluteExpiresAt time.Time    `json:"absolute_expires_at"`
}

type CLIInstallationResponse struct {
	InstallationID string `json:"installation_id"`
}

type CLIPrincipal struct {
	TokenID     string   `json:"token_id"`
	UserID      string   `json:"user_id"`
	PrincipalID string   `json:"principal_id"`
	Username    string   `json:"username"`
	Roles       []string `json:"roles"`
	Scopes      []string `json:"scopes"`
}

type CLISessionResponse struct {
	Principal         CLIPrincipal `json:"principal"`
	InstallationID    string       `json:"installation_id"`
	AbsoluteExpiresAt time.Time    `json:"absolute_expires_at"`
}

type CLILoginResponse struct {
	CLISessionResponse
	Token string `json:"token"`
}
