package workerapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	openapi "openagentx/internal/api"
	"openagentx/internal/domain"
)

type Service interface {
	Register(context.Context, string, openapi.RegisterRequest) (*openapi.WorkerSession, error)
	Heartbeat(context.Context, string, string, openapi.HeartbeatRequest) error
	ClaimMailbox(context.Context, string, string, openapi.ClaimRequest) (*domain.MailboxItem, error)
	AcceptMailbox(context.Context, string, string, string, openapi.AcceptRequest) error
	BeginAttempt(context.Context, string, string, string, openapi.BeginAttemptRequest) (*openapi.BeginAttemptResponse, error)
	ResolveMailboxPayload(context.Context, string, string, string, openapi.MailboxPayloadRequest) (*openapi.MailboxPayloadResponse, error)
	AppendEvents(context.Context, string, string, string, openapi.EventBatch) error
	Finish(context.Context, string, string, string, openapi.FinishRunRequest) error
	ClaimWorkerCommand(context.Context, string, string, openapi.ControlClaimRequest) (*domain.WorkerCommand, error)
	AcknowledgeWorkerCommand(context.Context, string, string, string, openapi.ControlAckRequest) error
	Release(context.Context, string, string, openapi.WorkerReleaseRequest) error
	AcknowledgeReleasedWorkerCommand(context.Context, string, string, string, openapi.ControlAckRequest) error
}

type PrincipalResolver interface {
	ResolvePrincipal(*http.Request) (string, error)
}

// AgentBindingResolver adds the transport-specific registration boundary. It
// is intentionally optional so the local UDS resolver can keep using the
// operating-system socket identity while the remote resolver checks mTLS.
type AgentBindingResolver interface {
	ValidateAgent(*http.Request, string) error
}

type RegistrationBindingResolver interface {
	ValidateRegistration(*http.Request, openapi.RegisterRequest) error
}

type PrincipalResolverFunc func(*http.Request) (string, error)

func (f PrincipalResolverFunc) ResolvePrincipal(request *http.Request) (string, error) {
	return f(request)
}

func StaticPrincipal(principalID string) PrincipalResolver {
	return PrincipalResolverFunc(func(*http.Request) (string, error) {
		if err := domain.ValidateOpaqueID("principal_id", principalID); err != nil {
			return "", err
		}
		return principalID, nil
	})
}

type Handler struct {
	service   Service
	principal PrincipalResolver
	mux       *http.ServeMux
}

func NewHandler(service Service, principal PrincipalResolver) (*Handler, error) {
	if service == nil || principal == nil {
		return nil, fmt.Errorf("Worker API service and principal resolver are required")
	}
	handler := &Handler{service: service, principal: principal, mux: http.NewServeMux()}
	handler.registerRoutes()
	return handler, nil
}

func (h *Handler) registerRoutes() {
	h.mux.HandleFunc("POST /api/v1/workers/register", h.register)
	h.mux.HandleFunc("POST /api/v1/workers/{workerID}/heartbeat", h.heartbeat)
	h.mux.HandleFunc("POST /api/v1/workers/{workerID}/network-bindings/pull", h.pullNetworkBindings)
	h.mux.HandleFunc("POST /api/v1/workers/{workerID}/mailbox/claim", h.claimMailbox)
	h.mux.HandleFunc("POST /api/v1/mailbox/{itemID}/accept", h.acceptMailbox)
	h.mux.HandleFunc("POST /api/v1/mailbox/{itemID}/begin-attempt", h.beginAttempt)
	h.mux.HandleFunc("POST /api/v1/mailbox/{itemID}/payload", h.resolveMailboxPayload)
	h.mux.HandleFunc("POST /api/v1/run-attempts/{runID}/events", h.appendEvents)
	h.mux.HandleFunc("POST /api/v1/run-attempts/{runID}/finish", h.finishRun)
	h.mux.HandleFunc("POST /api/v1/workers/{workerID}/control/claim", h.claimCommand)
	h.mux.HandleFunc("POST /api/v1/workers/{workerID}/release", h.release)
	h.mux.HandleFunc("POST /api/v1/worker-commands/{commandID}/ack", h.ackCommand)
	h.mux.HandleFunc("POST /api/v1/worker-commands/{commandID}/released-ack", h.releasedAckCommand)
}

func (h *Handler) pullNetworkBindings(response http.ResponseWriter, request *http.Request) {
	service, ok := h.service.(interface {
		PullNetworkBindings(context.Context, string, string, openapi.NetworkBindingPullRequest) ([]domain.NetworkBinding, error)
	})
	if !ok {
		h.writeError(response, domain.ErrUnsupportedCapability)
		return
	}
	principal, token, ok := h.authenticate(response, request)
	if !ok {
		return
	}
	var body openapi.NetworkBindingPullRequest
	if err := openapi.DecodeStrictJSON(request.Body, &body); err != nil {
		h.writeError(response, domain.ErrInvalidInput("invalid JSON request body"))
		return
	}
	if !matchPathID(request.PathValue("workerID"), body.WorkerInstanceID) {
		h.writeError(response, domain.ErrInvalidInput("path Worker ID does not match request body"))
		return
	}
	bindings, err := service.PullNetworkBindings(request.Context(), principal, token, body)
	if err != nil {
		h.writeError(response, err)
		return
	}
	h.writeJSON(response, http.StatusOK, openapi.NetworkBindingPullResponse{Bindings: bindings})
}

func (h *Handler) release(w http.ResponseWriter, r *http.Request) {
	principal, token, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	var body openapi.WorkerReleaseRequest
	if err := openapi.DecodeStrictJSON(r.Body, &body); err != nil {
		h.writeError(w, domain.ErrInvalidInput("invalid JSON request body"))
		return
	}
	if !matchPathID(r.PathValue("workerID"), body.WorkerInstanceID) {
		h.writeError(w, domain.ErrInvalidInput("path Worker ID does not match request body"))
		return
	}
	if err := h.service.Release(r.Context(), principal, token, body); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) releasedAckCommand(w http.ResponseWriter, r *http.Request) {
	principal, token, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	var body openapi.ControlAckRequest
	if err := openapi.DecodeStrictJSON(r.Body, &body); err != nil {
		h.writeError(w, domain.ErrInvalidInput("invalid JSON request body"))
		return
	}
	if err := h.service.AcknowledgeReleasedWorkerCommand(r.Context(), principal, token, r.PathValue("commandID"), body); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) claimCommand(w http.ResponseWriter, r *http.Request) {
	principal, token, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	var body openapi.ControlClaimRequest
	if err := openapi.DecodeStrictJSON(r.Body, &body); err != nil {
		h.writeError(w, domain.ErrInvalidInput("invalid JSON request body"))
		return
	}
	if !matchPathID(r.PathValue("workerID"), body.WorkerInstanceID) {
		h.writeError(w, domain.ErrInvalidInput("path Worker ID does not match request body"))
		return
	}
	if err := applyWaitQuery(r, &body.WaitSeconds); err != nil {
		h.writeError(w, err)
		return
	}
	command, err := h.service.ClaimWorkerCommand(r.Context(), principal, token, body)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, openapi.ControlClaimResponse{Command: command})
}

func (h *Handler) ackCommand(w http.ResponseWriter, r *http.Request) {
	principal, token, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	var body openapi.ControlAckRequest
	if err := openapi.DecodeStrictJSON(r.Body, &body); err != nil {
		h.writeError(w, domain.ErrInvalidInput("invalid JSON request body"))
		return
	}
	if err := h.service.AcknowledgeWorkerCommand(r.Context(), principal, token, r.PathValue("commandID"), body); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Pragma", "no-cache")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Referrer-Policy", "no-referrer")
	response.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
	response.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	request.Body = http.MaxBytesReader(response, request.Body, 1<<20)
	h.mux.ServeHTTP(response, request)
}

func (h *Handler) register(response http.ResponseWriter, request *http.Request) {
	principal, err := h.principal.ResolvePrincipal(request)
	if err != nil {
		h.writeError(response, err)
		return
	}
	var body openapi.RegisterRequest
	if err := openapi.DecodeStrictJSON(request.Body, &body); err != nil {
		h.writeError(response, domain.ErrInvalidInput("invalid JSON request body"))
		return
	}
	if binding, ok := h.principal.(AgentBindingResolver); ok {
		if err := binding.ValidateAgent(request, body.AgentID); err != nil {
			h.writeError(response, err)
			return
		}
	}
	if binding, ok := h.principal.(RegistrationBindingResolver); ok {
		if err := binding.ValidateRegistration(request, body); err != nil {
			h.writeError(response, err)
			return
		}
	}
	session, err := h.service.Register(request.Context(), principal, body)
	if err != nil {
		h.writeError(response, err)
		return
	}
	h.writeJSON(response, http.StatusCreated, session)
}

func (h *Handler) heartbeat(response http.ResponseWriter, request *http.Request) {
	principal, token, ok := h.authenticate(response, request)
	if !ok {
		return
	}
	var body openapi.HeartbeatRequest
	if err := openapi.DecodeStrictJSON(request.Body, &body); err != nil {
		h.writeError(response, domain.ErrInvalidInput("invalid JSON request body"))
		return
	}
	if !matchPathID(request.PathValue("workerID"), body.WorkerInstanceID) {
		h.writeError(response, domain.ErrInvalidInput("path Worker ID does not match request body"))
		return
	}
	if err := h.service.Heartbeat(request.Context(), principal, token, body); err != nil {
		h.writeError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (h *Handler) claimMailbox(response http.ResponseWriter, request *http.Request) {
	principal, token, ok := h.authenticate(response, request)
	if !ok {
		return
	}
	var body openapi.ClaimRequest
	if err := openapi.DecodeStrictJSON(request.Body, &body); err != nil {
		h.writeError(response, domain.ErrInvalidInput("invalid JSON request body"))
		return
	}
	if !matchPathID(request.PathValue("workerID"), body.WorkerInstanceID) {
		h.writeError(response, domain.ErrInvalidInput("path Worker ID does not match request body"))
		return
	}
	if err := applyWaitQuery(request, &body.WaitSeconds); err != nil {
		h.writeError(response, err)
		return
	}
	item, err := h.service.ClaimMailbox(request.Context(), principal, token, body)
	if err != nil {
		h.writeError(response, err)
		return
	}
	h.writeJSON(response, http.StatusOK, openapi.ClaimResponse{Item: item})
}

func applyWaitQuery(request *http.Request, waitSeconds *int) error {
	rawWait := request.URL.Query().Get("wait")
	if rawWait == "" {
		return nil
	}
	wait, err := time.ParseDuration(rawWait)
	if err != nil || wait < 0 || wait > 30*time.Second || wait%time.Second != 0 {
		return domain.ErrInvalidInput("wait must be whole seconds between 0s and 30s")
	}
	*waitSeconds = int(wait / time.Second)
	return nil
}

func (h *Handler) acceptMailbox(response http.ResponseWriter, request *http.Request) {
	principal, token, ok := h.authenticate(response, request)
	if !ok {
		return
	}
	var body openapi.AcceptRequest
	if err := openapi.DecodeStrictJSON(request.Body, &body); err != nil {
		h.writeError(response, domain.ErrInvalidInput("invalid JSON request body"))
		return
	}
	if err := h.service.AcceptMailbox(request.Context(), principal, token, request.PathValue("itemID"), body); err != nil {
		h.writeError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (h *Handler) beginAttempt(response http.ResponseWriter, request *http.Request) {
	principal, token, ok := h.authenticate(response, request)
	if !ok {
		return
	}
	var body openapi.BeginAttemptRequest
	if err := openapi.DecodeStrictJSON(request.Body, &body); err != nil {
		h.writeError(response, domain.ErrInvalidInput("invalid JSON request body"))
		return
	}
	result, err := h.service.BeginAttempt(request.Context(), principal, token, request.PathValue("itemID"), body)
	if err != nil {
		h.writeError(response, err)
		return
	}
	h.writeJSON(response, http.StatusOK, result)
}

func (h *Handler) resolveMailboxPayload(response http.ResponseWriter, request *http.Request) {
	principal, token, ok := h.authenticate(response, request)
	if !ok {
		return
	}
	var body openapi.MailboxPayloadRequest
	if err := openapi.DecodeStrictJSON(request.Body, &body); err != nil {
		h.writeError(response, domain.ErrInvalidInput("invalid JSON request body"))
		return
	}
	result, err := h.service.ResolveMailboxPayload(request.Context(), principal, token, request.PathValue("itemID"), body)
	if err != nil {
		h.writeError(response, err)
		return
	}
	h.writeJSON(response, http.StatusOK, result)
}

func (h *Handler) appendEvents(response http.ResponseWriter, request *http.Request) {
	principal, token, ok := h.authenticate(response, request)
	if !ok {
		return
	}
	var body openapi.EventBatch
	if err := openapi.DecodeStrictJSON(request.Body, &body); err != nil {
		h.writeError(response, domain.ErrInvalidInput("invalid JSON request body"))
		return
	}
	if err := h.service.AppendEvents(request.Context(), principal, token, request.PathValue("runID"), body); err != nil {
		h.writeError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (h *Handler) finishRun(response http.ResponseWriter, request *http.Request) {
	principal, token, ok := h.authenticate(response, request)
	if !ok {
		return
	}
	var body openapi.FinishRunRequest
	if err := openapi.DecodeStrictJSON(request.Body, &body); err != nil {
		h.writeError(response, domain.ErrInvalidInput("invalid JSON request body"))
		return
	}
	if err := h.service.Finish(request.Context(), principal, token, request.PathValue("runID"), body); err != nil {
		h.writeError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (h *Handler) authenticate(response http.ResponseWriter, request *http.Request) (string, string, bool) {
	principal, err := h.principal.ResolvePrincipal(request)
	if err != nil {
		h.writeError(response, domain.ErrUnauthorized)
		return "", "", false
	}
	authorization := request.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, "Bearer ") || strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer ")) == "" {
		h.writeError(response, domain.ErrUnauthorized)
		return "", "", false
	}
	return principal, strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer ")), true
}

func matchPathID(pathID string, bodyID string) bool {
	return pathID != "" && pathID == bodyID
}

func (h *Handler) writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func (h *Handler) writeError(response http.ResponseWriter, err error) {
	status, code, message := classifyError(err)
	h.writeJSON(response, status, openapi.ErrorResponse{Code: code, Message: message})
}

func classifyError(err error) (int, string, string) {
	var domainError *domain.DomainError
	if errors.As(err, &domainError) {
		switch domainError.Code {
		case "INVALID_INPUT":
			return http.StatusBadRequest, openapi.ErrorInvalidRequest, domainError.Message
		case "FORBIDDEN":
			return http.StatusForbidden, openapi.ErrorForbidden, domainError.Message
		case "CONFLICT":
			return http.StatusConflict, openapi.ErrorConflict, domainError.Message
		}
	}
	switch {
	case errors.Is(err, domain.ErrUnauthorized):
		return http.StatusUnauthorized, openapi.ErrorUnauthorized, "worker authentication failed"
	case errors.Is(err, domain.ErrNotFound), errors.Is(err, domain.ErrTaskNotFound), errors.Is(err, domain.ErrAgentNotFound):
		return http.StatusNotFound, openapi.ErrorNotFound, "resource not found"
	case errors.Is(err, domain.ErrStaleVersion), errors.Is(err, domain.ErrSessionGenerationConflict):
		return http.StatusConflict, openapi.ErrorStaleVersion, "resource version is stale"
	case errors.Is(err, domain.ErrLeaseExpired):
		return http.StatusConflict, openapi.ErrorLeaseExpired, "worker or resource lease expired"
	case errors.Is(err, domain.ErrFencingRejected):
		return http.StatusConflict, openapi.ErrorFencingRejected, "fencing token rejected"
	case errors.Is(err, domain.ErrUnsupportedCapability):
		return http.StatusUnprocessableEntity, openapi.ErrorUnsupportedCapability, "runtime capability is unavailable"
	case errors.Is(err, domain.ErrAgentNotReady), errors.Is(err, domain.ErrWorkerBusy),
		errors.Is(err, domain.ErrInvalidState), errors.Is(err, domain.ErrTaskCancelRequested),
		errors.Is(err, domain.ErrInvalidTransition), errors.Is(err, domain.ErrTerminalState):
		return http.StatusConflict, openapi.ErrorConflict, "resource state does not allow this operation"
	default:
		return http.StatusInternalServerError, openapi.ErrorInternal, "internal server error"
	}
}
