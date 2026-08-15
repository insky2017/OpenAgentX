package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	identifierRegex = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
)

const (
	ConnectorTmux = "tmux"
	ConnectorNone = "none"

	AgentStatusRegistered = "registered"
	AgentStatusActive     = "active"
)

type Agent struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Connector string `json:"connector"`
	Address   string `json:"address"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func (a *Agent) Validate() error {
	trimmedID := strings.TrimSpace(a.ID)
	if trimmedID == "" {
		return ErrInvalidInput("agent id cannot be empty")
	}
	if !identifierRegex.MatchString(trimmedID) {
		return ErrInvalidInput(fmt.Sprintf("invalid agent id '%s': must match pattern ^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$", trimmedID))
	}
	a.ID = trimmedID

	trimmedRole := strings.TrimSpace(a.Role)
	if trimmedRole == "" {
		return ErrInvalidInput("agent role cannot be empty")
	}
	if !identifierRegex.MatchString(trimmedRole) {
		return ErrInvalidInput(fmt.Sprintf("invalid agent role '%s': must match pattern ^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$", trimmedRole))
	}
	a.Role = trimmedRole

	if a.Connector == "" {
		a.Connector = ConnectorNone
	}
	if a.Connector != ConnectorTmux && a.Connector != ConnectorNone {
		return ErrInvalidInput("unsupported connector: must be 'tmux' or 'none'")
	}

	a.Address = strings.TrimSpace(a.Address)
	if a.Connector == ConnectorTmux && a.Address == "" {
		return ErrInvalidInput("tmux address cannot be empty when connector is 'tmux'")
	}
	if strings.ContainsAny(a.Address, "\r\n\t") {
		return ErrInvalidInput("connector address cannot contain newlines or control characters")
	}

	if a.Status == "" {
		a.Status = AgentStatusRegistered
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if a.CreatedAt == "" {
		a.CreatedAt = now
	}
	if a.UpdatedAt == "" {
		a.UpdatedAt = now
	}
	return nil
}
