// Package console contains the small, authenticated attach boundary used by
// terminal and diagnostic clients. Observation and mutation remain on the
// existing Panel API; this package never receives a Worker TurnHandle.
package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"openagentx/internal/auth/web"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

const (
	ModeNormal      = "normal"
	ModeDiagnostic  = "diagnostic"
	AttachPath      = "/api/console/v1/attach"
	diagnosticBurst = 10
)

// ObserveState is intentionally narrow and does not expose repository handles
// or Worker internals to the Console transport.
type ObserveState interface {
	ListAgents(context.Context, int) ([]domain.AgentIdentity, error)
	ListWorkers(context.Context, int) ([]domain.WorkerInstance, error)
	GetActiveRunForAgent(context.Context, string) (*domain.RunAttempt, error)
	ListWorkerBackends(context.Context, string) ([]openruntime.BackendRegistration, error)
}

type Handler struct {
	state   ObserveState
	auth    *web.Manager
	limiter *limiter
	mux     *http.ServeMux
}

func NewHandler(state ObserveState, auth *web.Manager) (*Handler, error) {
	if state == nil || auth == nil {
		return nil, fmt.Errorf("console state and auth are required")
	}
	h := &Handler{state: state, auth: auth, limiter: newLimiter(), mux: http.NewServeMux()}
	h.mux.HandleFunc("GET "+AttachPath, h.attach)
	return h, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	h.mux.ServeHTTP(w, r)
}

type AttachResponse struct {
	AgentID          string                               `json:"agent_id"`
	Mode             string                               `json:"mode"`
	Generation       int64                                `json:"generation"`
	WorkerStatus     domain.WorkerStatus                  `json:"worker_status"`
	WorkerInstanceID string                               `json:"worker_instance_id,omitempty"`
	Capabilities     []string                             `json:"capabilities,omitempty"`
	LastHeartbeatAt  time.Time                            `json:"last_heartbeat_at,omitempty"`
	LeaseUntil       time.Time                            `json:"lease_until,omitempty"`
	BackendHealth    map[string]openruntime.BackendHealth `json:"backend_health,omitempty"`
	ActiveRun        *RunSnapshot                         `json:"active_run,omitempty"`
	ObserveBasePath  string                               `json:"observe_base_path"`
	ControlBasePath  string                               `json:"control_base_path"`
	Diagnostic       *DiagnosticView                      `json:"diagnostic,omitempty"`
}

type RunSnapshot struct {
	RunID     string                  `json:"run_id"`
	TaskID    string                  `json:"task_id"`
	Status    domain.RunAttemptStatus `json:"status"`
	StartedAt time.Time               `json:"started_at"`
	UpdatedAt time.Time               `json:"updated_at"`
}

type DiagnosticView struct {
	LeaseUntil      time.Time `json:"lease_until,omitempty"`
	LastHeartbeatAt time.Time `json:"last_heartbeat_at,omitempty"`
	StartedAt       time.Time `json:"started_at,omitempty"`
	UpdatedAt       time.Time `json:"updated_at,omitempty"`
	Draining        bool      `json:"draining"`
}

func (h *Handler) attach(w http.ResponseWriter, r *http.Request) {
	session, err := h.auth.Authenticate(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := web.RequireRole(session, web.RoleOperator); err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	agentID := r.URL.Query().Get("agent_id")
	if err := domain.ValidateIdentifier("agent_id", agentID); err != nil {
		http.Error(w, "invalid agent_id", http.StatusBadRequest)
		return
	}
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = ModeNormal
	}
	if mode != ModeNormal && mode != ModeDiagnostic {
		http.Error(w, "unsupported attach mode", http.StatusBadRequest)
		return
	}
	if mode == ModeDiagnostic && !h.limiter.Allow(session.User.ID) {
		http.Error(w, "diagnostic attach rate limited", http.StatusTooManyRequests)
		return
	}
	agents, err := h.state.ListAgents(r.Context(), 1000)
	if err != nil {
		http.Error(w, "failed to resolve Agent", http.StatusInternalServerError)
		return
	}
	found := false
	for _, agent := range agents {
		if agent.ID == agentID {
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "Agent not found", http.StatusNotFound)
		return
	}
	workers, err := h.state.ListWorkers(r.Context(), 1000)
	if err != nil {
		http.Error(w, "failed to resolve Worker", http.StatusInternalServerError)
		return
	}
	response := AttachResponse{AgentID: agentID, Mode: mode, WorkerStatus: domain.WorkerStatusOffline,
		ObserveBasePath: "/api/observe/v1", ControlBasePath: "/api/control/v1"}
	for _, worker := range workers {
		if worker.AgentID != agentID || worker.Generation < response.Generation {
			continue
		}
		response.Generation = worker.Generation
		response.WorkerStatus = worker.Status
		response.WorkerInstanceID = worker.ID
		response.Capabilities = append([]string(nil), worker.Capabilities...)
		response.LastHeartbeatAt = worker.LastHeartbeatAt
		response.LeaseUntil = worker.LeaseUntil
		if mode == ModeDiagnostic {
			response.Diagnostic = &DiagnosticView{LeaseUntil: worker.LeaseUntil, LastHeartbeatAt: worker.LastHeartbeatAt,
				StartedAt: worker.StartedAt, UpdatedAt: worker.UpdatedAt, Draining: worker.Status == domain.WorkerStatusDraining}
		}
	}
	if response.WorkerInstanceID != "" {
		backends, backendErr := h.state.ListWorkerBackends(r.Context(), response.WorkerInstanceID)
		if backendErr != nil {
			http.Error(w, "failed to resolve Backend health", http.StatusInternalServerError)
			return
		}
		response.BackendHealth = make(map[string]openruntime.BackendHealth, len(backends))
		for _, backend := range backends {
			response.BackendHealth[backend.BackendID] = backend.Health
		}
	}
	if run, runErr := h.state.GetActiveRunForAgent(r.Context(), agentID); runErr == nil {
		response.ActiveRun = &RunSnapshot{RunID: run.ID, TaskID: run.TaskID, Status: run.Status, StartedAt: run.StartedAt, UpdatedAt: run.UpdatedAt}
	} else if !errors.Is(runErr, domain.ErrNotFound) {
		http.Error(w, "failed to resolve active RunAttempt", http.StatusInternalServerError)
		return
	}
	writeJSON(w, response)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

type limiter struct {
	mu   sync.Mutex
	seen map[string][]time.Time
}

func newLimiter() *limiter { return &limiter{seen: make(map[string][]time.Time)} }

func (l *limiter) Allow(key string) bool {
	now := time.Now()
	cutoff := now.Add(-time.Second)
	l.mu.Lock()
	defer l.mu.Unlock()
	values := l.seen[key][:0]
	for _, value := range l.seen[key] {
		if value.After(cutoff) {
			values = append(values, value)
		}
	}
	if len(values) >= diagnosticBurst {
		l.seen[key] = values
		return false
	}
	l.seen[key] = append(values, now)
	return true
}
