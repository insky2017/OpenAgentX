// Package external exposes local message-only communication for agents whose
// execution remains in an existing host. It never starts a Worker or a turn.
package external

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	openapi "openagentx/internal/api"
	requestauth "openagentx/internal/auth"
	cliauth "openagentx/internal/auth/cli"
	"openagentx/internal/controlplane"
	"openagentx/internal/domain"
)

const Prefix = "/api/external/v1/"

type BindResponse struct {
	Binding *domain.ExternalSessionBinding `json:"binding"`
	Token   string                         `json:"token"`
}

type Handler struct {
	service *controlplane.ExternalSessionService
	auth    requestauth.RequestAuthorizer
	mux     *http.ServeMux
}

func NewHandler(service *controlplane.ExternalSessionService, auth *cliauth.Service) (*Handler, error) {
	a, err := requestauth.NewCLIAuthorizer(auth)
	if err != nil {
		return nil, err
	}
	h := &Handler{service: service, auth: a, mux: http.NewServeMux()}
	h.mux.HandleFunc("POST "+Prefix+"bindings", h.bind)
	h.mux.HandleFunc("GET "+Prefix+"bindings/{agent}", h.binding)
	h.mux.HandleFunc("POST "+Prefix+"bindings/{agent}/revoke", h.revoke)
	h.mux.HandleFunc("GET "+Prefix+"status", h.status)
	h.mux.HandleFunc("POST "+Prefix+"messages", h.send)
	h.mux.HandleFunc("GET "+Prefix+"messages/{message}", h.message)
	h.mux.HandleFunc("POST "+Prefix+"messages/{message}/ack", h.ack)
	h.mux.HandleFunc("GET "+Prefix+"inbox", h.inbox)
	return h, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// A 64 KiB decoded message may occupy six times as much JSON (\u0000).
	r.Body = http.MaxBytesReader(w, r.Body, 512<<10)
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) owner(w http.ResponseWriter, r *http.Request) (string, bool) {
	p, err := h.auth.Authorize(r, requestauth.Requirement{Role: domain.WebRoleOwner, Scope: domain.CLIScopeFleetLifecycle, Write: r.Method != http.MethodGet})
	if err != nil {
		h.auth.WriteFailure(w, err)
		return "", false
	}
	return p.ID, true
}

func (h *Handler) bind(w http.ResponseWriter, r *http.Request) {
	p, ok := h.owner(w, r)
	if !ok {
		return
	}
	var in domain.BindExternalSessionInput
	if err := openapi.DecodeStrictJSON(r.Body, &in); err != nil {
		fail(w, domain.ErrInvalidInput("invalid binding request"))
		return
	}
	b, token, err := h.service.Bind(r.Context(), p, in)
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, BindResponse{Binding: b, Token: token})
}

func (h *Handler) binding(w http.ResponseWriter, r *http.Request) {
	p, ok := h.owner(w, r)
	if !ok {
		return
	}
	b, err := h.service.Binding(r.Context(), p, r.PathValue("agent"))
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, b)
}

func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) {
	p, ok := h.owner(w, r)
	if !ok {
		return
	}
	var in struct {
		ExpectedGeneration int64 `json:"expected_generation"`
	}
	if err := openapi.DecodeStrictJSON(r.Body, &in); err != nil {
		fail(w, domain.ErrInvalidInput("invalid revocation request"))
		return
	}
	if err := h.service.Revoke(r.Context(), p, r.PathValue("agent"), in.ExpectedGeneration); err != nil {
		fail(w, err)
		return
	}
	respond(w, map[string]bool{"revoked": true})
}

func bearer(r *http.Request) string {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) == 2 && parts[0] == "Bearer" {
		return parts[1]
	}
	return ""
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	b, err := h.service.Status(r.Context(), bearer(r))
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, b)
}

func (h *Handler) send(w http.ResponseWriter, r *http.Request) {
	// Authenticate before parsing user content; never attribute it to an owner.
	if _, err := h.service.Status(r.Context(), bearer(r)); err != nil {
		fail(w, err)
		return
	}
	var in domain.SendExternalMessageInput
	if err := openapi.DecodeStrictJSON(r.Body, &in); err != nil {
		fail(w, domain.ErrInvalidInput("invalid message request"))
		return
	}
	if in.IdempotencyKey == "" || r.Header.Get("Idempotency-Key") != in.IdempotencyKey {
		fail(w, domain.ErrInvalidInput("Idempotency-Key must match request"))
		return
	}
	m, err := h.service.Send(r.Context(), bearer(r), in)
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, m)
}

func (h *Handler) message(w http.ResponseWriter, r *http.Request) {
	m, err := h.service.Get(r.Context(), bearer(r), r.PathValue("message"))
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, m)
}

func (h *Handler) ack(w http.ResponseWriter, r *http.Request) {
	m, err := h.service.Ack(r.Context(), bearer(r), r.PathValue("message"))
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, m)
}

func (h *Handler) inbox(w http.ResponseWriter, r *http.Request) {
	after, limit := int64(0), 100
	var err error
	if value := r.URL.Query().Get("after"); value != "" {
		after, err = strconv.ParseInt(value, 10, 64)
		if err != nil || after < 0 {
			fail(w, domain.ErrInvalidInput("invalid after cursor"))
			return
		}
	}
	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 100 {
			fail(w, domain.ErrInvalidInput("limit must be 1..100"))
			return
		}
	}
	messages, err := h.service.Inbox(r.Context(), bearer(r), after, limit)
	if err != nil {
		fail(w, err)
		return
	}
	if messages == nil {
		messages = []domain.ExternalMessage{}
	}
	respond(w, map[string]any{"messages": messages})
}

func respond(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func fail(w http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, "INTERNAL", "external message operation failed"
	var d *domain.DomainError
	switch {
	case errors.Is(err, domain.ErrUnauthorized):
		status, code, message = 401, "UNAUTHORIZED", "agent credential required or expired"
	case errors.Is(err, domain.ErrNotFound), errors.Is(err, domain.ErrAgentNotFound), errors.Is(err, domain.ErrTaskNotFound):
		status, code, message = 404, "NOT_FOUND", "resource not found"
	case errors.Is(err, domain.ErrIdempotencyConflict), errors.Is(err, domain.ErrStaleVersion), errors.Is(err, domain.ErrInvalidState), errors.Is(err, domain.ErrSessionGenerationConflict), errors.Is(err, domain.ErrAgentNotReady):
		status, code, message = 409, "CONFLICT", err.Error()
	case errors.As(err, &d):
		code, message = d.Code, d.Message
		switch code {
		case "INVALID_INPUT":
			status = 400
		case "FORBIDDEN":
			status = 403
		case "CONFLICT":
			status = 409
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(openapi.ErrorResponse{Code: code, Message: message})
}
