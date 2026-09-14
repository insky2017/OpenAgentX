package admin

import (
	"encoding/json"
	"fmt"
	"net/http"

	openapi "openagentx/internal/api"
	"openagentx/internal/auth/web"
	"openagentx/internal/controlplane"
	"openagentx/internal/domain"
)

type Handler struct {
	service *controlplane.WorkerAdminService
	auth    *web.Manager
	mux     *http.ServeMux
}

func NewHandler(service *controlplane.WorkerAdminService, auth *web.Manager) (*Handler, error) {
	if service == nil || auth == nil {
		return nil, fmt.Errorf("Worker admin service and Web Auth are required")
	}
	handler := &Handler{service: service, auth: auth, mux: http.NewServeMux()}
	handler.mux.HandleFunc("POST /api/admin/v1/workers/{workerID}/health-check", handler.healthCheck)
	handler.mux.HandleFunc("POST /api/admin/v1/workers/{workerID}/drain", handler.drain)
	handler.mux.HandleFunc("POST /api/admin/v1/workers/{workerID}/stop", handler.stop)
	handler.mux.HandleFunc("POST /api/admin/v1/workers/{workerID}/force-stop", handler.forceStop)
	return handler, nil
}

func (h *Handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Pragma", "no-cache")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Referrer-Policy", "no-referrer")
	response.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
	response.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	h.mux.ServeHTTP(response, request)
}

func (h *Handler) healthCheck(response http.ResponseWriter, request *http.Request) {
	h.createCommand(response, request, domain.WorkerCommandHealthCheck)
}

func (h *Handler) drain(response http.ResponseWriter, request *http.Request) {
	h.createCommand(response, request, domain.WorkerCommandDrain)
}

func (h *Handler) stop(response http.ResponseWriter, request *http.Request) {
	h.createCommand(response, request, domain.WorkerCommandStop)
}

func (h *Handler) createCommand(response http.ResponseWriter, request *http.Request, kind domain.WorkerCommandKind) {
	session, ok := h.authorize(response, request)
	if !ok {
		return
	}
	var command openapi.WorkerAdminRequest
	if err := openapi.DecodeStrictJSON(request.Body, &command); err != nil {
		http.Error(response, "invalid request", http.StatusBadRequest)
		return
	}
	if !requireIdempotencyHeader(response, request, command.Meta) {
		return
	}
	command.RequestedBy = session.User.ID
	created, err := h.service.Command(request.Context(), session.User.ID, request.PathValue("workerID"), kind, command)
	if err != nil {
		http.Error(response, err.Error(), http.StatusConflict)
		return
	}
	response.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(response).Encode(openapi.WorkerCommandResponse{Command: *created})
}

func (h *Handler) forceStop(response http.ResponseWriter, request *http.Request) {
	session, ok := h.authorize(response, request)
	if !ok {
		return
	}
	var command openapi.WorkerForceStopRequest
	if err := openapi.DecodeStrictJSON(request.Body, &command); err != nil {
		http.Error(response, "invalid request", http.StatusBadRequest)
		return
	}
	if !command.Confirm || request.Header.Get("X-Confirm-Dangerous") != "force-stop" {
		http.Error(response, "force stop requires explicit confirmation", http.StatusBadRequest)
		return
	}
	if !requireIdempotencyHeader(response, request, command.Meta) {
		return
	}
	created, err := h.service.Command(request.Context(), session.User.ID, request.PathValue("workerID"), domain.WorkerCommandForceStop, openapi.WorkerAdminRequest{
		Meta: command.Meta, RequestedBy: session.User.ID, ExpectedGeneration: command.ExpectedGeneration,
	})
	if err != nil {
		http.Error(response, err.Error(), http.StatusConflict)
		return
	}
	response.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(response).Encode(openapi.WorkerCommandResponse{Command: *created})
}

func (h *Handler) authorize(response http.ResponseWriter, request *http.Request) (*web.Session, bool) {
	session, err := h.auth.Authenticate(request)
	if err != nil {
		http.Error(response, "unauthorized", http.StatusUnauthorized)
		return nil, false
	}
	if err := web.RequireRole(session, web.RoleOwner); err != nil {
		http.Error(response, "forbidden", http.StatusForbidden)
		return nil, false
	}
	if err := web.ValidateCSRF(session, request.Header.Get("X-CSRF-Token")); err != nil {
		http.Error(response, "invalid csrf token", http.StatusForbidden)
		return nil, false
	}
	return session, true
}

func requireIdempotencyHeader(response http.ResponseWriter, request *http.Request, meta openapi.CommandMeta) bool {
	value := request.Header.Get("Idempotency-Key")
	if value == "" || value != meta.IdempotencyKey {
		http.Error(response, "Idempotency-Key header must match request meta", http.StatusBadRequest)
		return false
	}
	return true
}
