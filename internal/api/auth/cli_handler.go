package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	openapi "openagentx/internal/api"
	cliauth "openagentx/internal/auth/cli"
)

type CLIHandler struct {
	service *cliauth.Service
	mux     *http.ServeMux
}

func NewCLIHandler(service *cliauth.Service) (*CLIHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("CLI Auth service is required")
	}
	h := &CLIHandler{service: service, mux: http.NewServeMux()}
	h.mux.HandleFunc("GET "+openapi.CLIInstallationProbePath, h.installation)
	h.mux.HandleFunc("POST "+openapi.CLIAuthLoginPath, h.login)
	h.mux.HandleFunc("GET "+openapi.CLIAuthSessionPath, h.session)
	h.mux.HandleFunc("POST "+openapi.CLIAuthLogoutPath, h.logout)
	return h, nil
}

func (h *CLIHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Pragma", "no-cache")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	h.mux.ServeHTTP(response, request)
}

func (h *CLIHandler) installation(response http.ResponseWriter, request *http.Request) {
	installationID, err := h.service.InstallationID(request.Context())
	if err != nil {
		writeCLIError(response, http.StatusInternalServerError, openapi.ErrorInternal, "installation identity unavailable")
		return
	}
	writeCLIJSON(response, openapi.CLIInstallationResponse{InstallationID: installationID})
}

func (h *CLIHandler) login(response http.ResponseWriter, request *http.Request) {
	var login openapi.LoginRequest
	if openapi.DecodeStrictJSON(request.Body, &login) != nil || login.Validate() != nil {
		writeCLIError(response, http.StatusUnauthorized, openapi.ErrorCLIUnauthenticated, "invalid CLI credentials")
		return
	}
	issued, err := h.service.Login(request.Context(), login.Username, login.Password)
	if err != nil {
		if errors.Is(err, cliauth.ErrUnauthenticated) {
			writeCLIError(response, http.StatusUnauthorized, openapi.ErrorCLIUnauthenticated, "invalid CLI credentials")
			return
		}
		if errors.Is(err, cliauth.ErrForbidden) {
			writeCLIError(response, http.StatusForbidden, openapi.ErrorCLIForbidden, "CLI authorization denied")
			return
		}
		writeCLIError(response, http.StatusInternalServerError, openapi.ErrorInternal, "CLI login failed")
		return
	}
	result := openapi.CLILoginResponse{CLISessionResponse: cliSessionResponse(issued.Principal), Token: issued.Token}
	writeCLIJSON(response, result)
}

func (h *CLIHandler) session(response http.ResponseWriter, request *http.Request) {
	principal, err := h.service.AuthenticateRequest(request)
	if err != nil {
		writeCLIError(response, http.StatusUnauthorized, openapi.ErrorCLIUnauthenticated, "CLI authentication required")
		return
	}
	writeCLIJSON(response, cliSessionResponse(principal))
}

func (h *CLIHandler) logout(response http.ResponseWriter, request *http.Request) {
	if err := h.service.RevokeRequest(request); err != nil {
		writeCLIError(response, http.StatusUnauthorized, openapi.ErrorCLIUnauthenticated, "CLI authentication required")
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func cliSessionResponse(principal cliauth.Principal) openapi.CLISessionResponse {
	roles := make([]string, 0, len(principal.Roles))
	for _, role := range principal.Roles {
		roles = append(roles, string(role))
	}
	scopes := make([]string, 0, len(principal.Scopes))
	for _, scope := range principal.Scopes {
		scopes = append(scopes, string(scope))
	}
	return openapi.CLISessionResponse{Principal: openapi.CLIPrincipal{TokenID: principal.TokenID,
		UserID: principal.WebUserID, PrincipalID: principal.PrincipalID, Username: principal.Username,
		Roles: roles, Scopes: scopes}, InstallationID: principal.InstallationID, AbsoluteExpiresAt: principal.ExpiresAt}
}

func writeCLIJSON(response http.ResponseWriter, value any) {
	response.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(response).Encode(value)
}

func writeCLIError(response http.ResponseWriter, status int, code, message string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(openapi.ErrorResponse{Code: code, Message: message})
}
