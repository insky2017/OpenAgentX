package console

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	requestauth "openagentx/internal/auth"
	"openagentx/internal/domain"
)

const AgentModelSettingsPath = "/api/console/v1/agents/{agentID}/model-settings"

type modelSettingsState interface {
	GetAgentModelSettings(context.Context, string, string) (domain.AgentModelSettings, error)
	SetAgentModelSettings(context.Context, string, domain.AgentModelSettingsUpdate, string) (domain.AgentModelSettings, error)
}

func (h *Handler) modelSettings(w http.ResponseWriter, r *http.Request) {
	write := r.Method == http.MethodPut
	req := requestauth.Requirement{Role: domain.WebRoleViewer, Scope: domain.CLIScopeConsoleRead}
	if write {
		req = requestauth.Requirement{Role: domain.WebRoleOperator, Scope: domain.CLIScopeConsoleControl, Write: true}
	}
	principal, err := h.auth.Authorize(r, req)
	if err != nil {
		h.auth.WriteFailure(w, err)
		return
	}
	state, ok := h.state.(modelSettingsState)
	if !ok {
		http.Error(w, "Agent model settings are unavailable", http.StatusNotImplemented)
		return
	}
	var result domain.AgentModelSettings
	if write {
		var body domain.AgentModelSettingsUpdate
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&body); err != nil {
			http.Error(w, "invalid model settings", http.StatusBadRequest)
			return
		}
		if err = decoder.Decode(new(any)); err != io.EOF {
			http.Error(w, "invalid trailing input", http.StatusBadRequest)
			return
		}
		result, err = state.SetAgentModelSettings(r.Context(), r.PathValue("agentID"), body, principal.ID)
	} else {
		result, err = state.GetAgentModelSettings(r.Context(), r.PathValue("agentID"), r.URL.Query().Get("backend_id"))
	}
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, domain.ErrStaleVersion) {
			status = http.StatusConflict
		}
		if errors.Is(err, domain.ErrAgentNotFound) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, result)
}
