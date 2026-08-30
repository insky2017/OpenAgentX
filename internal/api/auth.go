package api

import (
	"fmt"
	"strings"
	"time"
)

const (
	AuthLoginPath   = "/api/auth/v1/login"
	AuthLogoutPath  = "/api/auth/v1/logout"
	AuthSessionPath = "/api/auth/v1/session"
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
