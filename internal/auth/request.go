package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	openapi "openagentx/internal/api"
	cliauth "openagentx/internal/auth/cli"
	webauth "openagentx/internal/auth/web"
	"openagentx/internal/domain"
)

type Principal struct {
	ID        string
	UserID    string
	Username  string
	Roles     []domain.WebRole
	ExpiresAt time.Time
}

func (p Principal) HasRole(required domain.WebRole) bool {
	requiredRank := roleRank(required)
	if requiredRank == 0 {
		return false
	}
	for _, role := range p.Roles {
		if roleRank(role) >= requiredRank {
			return true
		}
	}
	return false
}

type Requirement struct {
	Role  domain.WebRole
	Scope domain.CLIScope
	Write bool
}

type RequestAuthorizer interface {
	Authorize(*http.Request, Requirement) (Principal, error)
	WriteFailure(http.ResponseWriter, error)
}

type WebAuthorizer struct{ manager *webauth.Manager }

func NewWebAuthorizer(manager *webauth.Manager) (*WebAuthorizer, error) {
	if manager == nil {
		return nil, fmt.Errorf("Web Auth manager is required")
	}
	return &WebAuthorizer{manager: manager}, nil
}

func (a *WebAuthorizer) Authorize(request *http.Request, requirement Requirement) (Principal, error) {
	session, err := a.manager.Authenticate(request)
	if err != nil {
		return Principal{}, &authorizationError{status: http.StatusUnauthorized, message: "unauthorized"}
	}
	roles := make([]domain.WebRole, 0, len(session.User.Roles))
	for _, candidate := range session.User.Roles {
		roles = append(roles, domain.WebRole(candidate))
	}
	principal := Principal{ID: session.User.ID, UserID: session.User.WebUserID, Username: session.User.Username,
		Roles: roles, ExpiresAt: session.IdleExpiresAt}
	if !principal.HasRole(requirement.Role) {
		return Principal{}, &authorizationError{status: http.StatusForbidden, message: "forbidden"}
	}
	if requirement.Write {
		if err := webauth.ValidateCSRF(session, request.Header.Get("X-CSRF-Token")); err != nil {
			return Principal{}, &authorizationError{status: http.StatusForbidden, message: "invalid csrf token"}
		}
	}
	return principal, nil
}

func (a *WebAuthorizer) WriteFailure(response http.ResponseWriter, err error) {
	var failure *authorizationError
	if !errors.As(err, &failure) {
		failure = &authorizationError{status: http.StatusUnauthorized, message: "unauthorized"}
	}
	http.Error(response, failure.message, failure.status)
}

type CLIAuthorizer struct{ service *cliauth.Service }

func NewCLIAuthorizer(service *cliauth.Service) (*CLIAuthorizer, error) {
	if service == nil {
		return nil, fmt.Errorf("CLI Auth service is required")
	}
	return &CLIAuthorizer{service: service}, nil
}

func (a *CLIAuthorizer) Authorize(request *http.Request, requirement Requirement) (Principal, error) {
	principal, err := a.service.AuthenticateRequest(request)
	if err != nil {
		return Principal{}, cliauth.ErrUnauthenticated
	}
	if err := cliauth.Require(principal, requirement.Role, requirement.Scope); err != nil {
		return Principal{}, cliauth.ErrForbidden
	}
	return Principal{ID: principal.PrincipalID, UserID: principal.WebUserID, Username: principal.Username,
		Roles: append([]domain.WebRole(nil), principal.Roles...), ExpiresAt: principal.ExpiresAt}, nil
}

func (a *CLIAuthorizer) WriteFailure(response http.ResponseWriter, err error) {
	status := http.StatusUnauthorized
	code := openapi.ErrorCLIUnauthenticated
	message := "CLI authentication required"
	if errors.Is(err, cliauth.ErrForbidden) {
		status = http.StatusForbidden
		code = openapi.ErrorCLIForbidden
		message = "CLI authorization denied"
	}
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(openapi.ErrorResponse{Code: code, Message: message})
}

type authorizationError struct {
	status  int
	message string
}

func (e *authorizationError) Error() string { return e.message }

func roleRank(role domain.WebRole) int {
	switch role {
	case domain.WebRoleOwner:
		return 3
	case domain.WebRoleOperator:
		return 2
	case domain.WebRoleViewer:
		return 1
	default:
		return 0
	}
}
