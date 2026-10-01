package panel

import (
	"context"
	openapi "openagentx/internal/api"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

type readyAgent struct {
	domain.AgentIdentity
	Readiness openapi.AgentReadiness `json:"readiness"`
}

// Use the same generation-bound backend filtering as scheduling, not presence
// alone. Busy Agents can accept queued work; diagnostics stay available.
func (h *Handler) projectAgentReadiness(ctx context.Context, agents []domain.AgentIdentity, workers []domain.WorkerInstance, tasks []domain.Task) []readyAgent {
	latest := map[string]domain.WorkerInstance{}
	for _, w := range workers {
		if old, ok := latest[w.AgentID]; !ok || w.Generation > old.Generation {
			latest[w.AgentID] = w
		}
	}
	result := make([]readyAgent, 0, len(agents))
	for _, a := range agents {
		var worker *domain.WorkerInstance
		var registered []openruntime.BackendRegistration
		if w, ok := latest[a.ID]; ok {
			worker = &w
			if backends, ok := h.state.(backendOptionsState); ok {
				registered, _ = backends.ListWorkerBackends(ctx, w.ID)
			}
		}
		state := openapi.ProjectAgentReadiness(a.ID, worker, tasks, registered, h.now())
		result = append(result, readyAgent{AgentIdentity: a, Readiness: state})
	}
	return result
}
