package console

import (
	"context"
	"errors"
	"net/http"

	requestauth "openagentx/internal/auth"
	"openagentx/internal/domain"
)

const AgentSessionPath = "/api/console/v1/agents/{agentID}/session"

type agentSessionState interface {
	ReadAgentSession(context.Context, string, string) (*domain.AgentSession, error)
}

func (h *Handler) agentSession(w http.ResponseWriter, r *http.Request) {
	if _, err := h.auth.Authorize(r, requestauth.Requirement{Role: domain.WebRoleViewer, Scope: domain.CLIScopeConsoleRead}); err != nil {
		h.auth.WriteFailure(w, err)
		return
	}
	agentID, backendID := r.PathValue("agentID"), r.URL.Query().Get("backend_id")
	if domain.ValidateIdentifier("agent_id", agentID) != nil || domain.ValidateIdentifier("backend_id", backendID) != nil {
		http.Error(w, "invalid Agent or backend", http.StatusBadRequest)
		return
	}
	state, ok := h.state.(agentSessionState)
	if !ok {
		http.Error(w, "Agent sessions are unavailable", http.StatusNotImplemented)
		return
	}
	result, err := state.ReadAgentSession(r.Context(), agentID, backendID)
	if err != nil {
		status := http.StatusConflict
		if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrAgentNotFound) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, result)
}
