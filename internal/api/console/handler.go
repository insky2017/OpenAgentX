// Package console contains the small, authenticated attach boundary used by
// terminal and diagnostic clients. Observation and mutation remain on the
// existing Panel API; this package never receives a Worker TurnHandle.
package console

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	openapi "openagentx/internal/api"
	requestauth "openagentx/internal/auth"
	cliauth "openagentx/internal/auth/cli"
	webauth "openagentx/internal/auth/web"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

const (
	ModeNormal      = "normal"
	ModeDiagnostic  = "diagnostic"
	AttachPath      = "/api/console/v1/attach"
	AgentsPath      = "/api/console/v1/agents"
	AgentTasksPath  = "/api/console/v1/agents/{agentID}/tasks"
	AgentTaskPath   = "/api/console/v1/agents/{agentID}/tasks/{taskID}"
	diagnosticBurst = 10
	agentPageSize   = 100
	taskPageSize    = 50
	taskPageMax     = 100
)

// ObserveState is intentionally narrow and does not expose repository handles
// or Worker internals to the Console transport.
type ObserveState interface {
	ConsoleSnapshot(context.Context, string) (domain.ConsoleSnapshot, error)
	ListConsoleAgentOptions(context.Context, string, int) ([]domain.ConsoleAgentOption, error)
	ListConsoleTasks(context.Context, string, domain.ConsoleTaskCursor, int) ([]domain.Task, error)
	ConsoleTaskSnapshot(context.Context, string, string) (domain.ConsoleTaskSnapshot, error)
}

type Handler struct {
	state   ObserveState
	auth    requestauth.RequestAuthorizer
	limiter *limiter
	mux     *http.ServeMux
}

func NewHandler(state ObserveState, manager *webauth.Manager) (*Handler, error) {
	authorizer, err := requestauth.NewWebAuthorizer(manager)
	if err != nil {
		return nil, err
	}
	return newHandler(state, authorizer)
}

func NewCLIHandler(state ObserveState, service *cliauth.Service) (*Handler, error) {
	authorizer, err := requestauth.NewCLIAuthorizer(service)
	if err != nil {
		return nil, err
	}
	return newHandler(state, authorizer)
}

func newHandler(state ObserveState, authorizer requestauth.RequestAuthorizer) (*Handler, error) {
	if state == nil || authorizer == nil {
		return nil, fmt.Errorf("console state and auth are required")
	}
	h := &Handler{state: state, auth: authorizer, limiter: newLimiter(), mux: http.NewServeMux()}
	h.mux.HandleFunc("GET "+AttachPath, h.attach)
	h.mux.HandleFunc("GET "+AgentsPath, h.agents)
	h.mux.HandleFunc("GET "+AgentModelSettingsPath, h.modelSettings)
	h.mux.HandleFunc("GET "+AgentSessionPath, h.agentSession)
	h.mux.HandleFunc("PUT "+AgentModelSettingsPath, h.modelSettings)
	h.mux.HandleFunc("GET "+AgentTasksPath, h.tasks)
	h.mux.HandleFunc("GET "+AgentTaskPath, h.task)
	return h, nil
}

type AgentOptionsPage struct {
	Agents     []domain.ConsoleAgentOption `json:"agents"`
	NextCursor string                      `json:"next_cursor,omitempty"`
	HasMore    bool                        `json:"has_more"`
}

func (h *Handler) agents(w http.ResponseWriter, r *http.Request) {
	if _, err := h.auth.Authorize(r, requestauth.Requirement{Role: domain.WebRoleViewer, Scope: domain.CLIScopeConsoleRead}); err != nil {
		h.auth.WriteFailure(w, err)
		return
	}
	after := r.URL.Query().Get("after_agent_id")
	if after != "" {
		if err := domain.ValidateIdentifier("after_agent_id", after); err != nil {
			http.Error(w, "invalid Agent cursor", http.StatusBadRequest)
			return
		}
	}
	options, err := h.state.ListConsoleAgentOptions(r.Context(), after, agentPageSize+1)
	if err != nil {
		http.Error(w, "failed to list Console Agents", http.StatusInternalServerError)
		return
	}
	page := AgentOptionsPage{Agents: options}
	if len(page.Agents) > agentPageSize {
		page.HasMore = true
		page.Agents = page.Agents[:agentPageSize]
	}
	previous := after
	for _, option := range page.Agents {
		if err := option.Validate(); err != nil {
			http.Error(w, "invalid Console Agent projection", http.StatusInternalServerError)
			return
		}
		if previous != "" && option.AgentID <= previous {
			http.Error(w, "invalid Console Agent ordering", http.StatusInternalServerError)
			return
		}
		previous = option.AgentID
	}
	if page.HasMore {
		page.NextCursor = page.Agents[len(page.Agents)-1].AgentID
	}
	for i := range page.Agents {
		option := &page.Agents[i]
		if _, ok := h.state.(consoleReadinessState); !ok {
			continue
		}
		snapshot, err := h.state.ConsoleSnapshot(r.Context(), option.AgentID)
		if err != nil {
			option.ReadinessReason = "暂时无法确认就绪状态"
			continue
		}
		if readiness := h.readiness(r.Context(), snapshot); readiness != nil {
			option.ReadinessReason = readiness.Reason
			option.NextAction = readiness.NextAction
		}
	}
	writeJSON(w, page)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	h.mux.ServeHTTP(w, r)
}

type AttachResponse struct {
	Readiness        *openapi.AgentReadiness              `json:"readiness,omitempty"`
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
	SuggestedTask    *openapi.ConsoleTaskOption           `json:"suggested_task,omitempty"`
	ObserveBasePath  string                               `json:"observe_base_path"`
	ControlBasePath  string                               `json:"control_base_path"`
	Diagnostic       *DiagnosticView                      `json:"diagnostic,omitempty"`
	SnapshotSequence int64                                `json:"snapshot_sequence"`
}

type RunSnapshot struct {
	RunID            string                  `json:"run_id"`
	TaskID           string                  `json:"task_id"`
	Status           domain.RunAttemptStatus `json:"status"`
	WorkerInstanceID string                  `json:"worker_instance_id"`
	WorkerGeneration *int64                  `json:"worker_generation,omitempty"`
	StartedAt        time.Time               `json:"started_at"`
	UpdatedAt        time.Time               `json:"updated_at"`
}

type DiagnosticView struct {
	LeaseUntil      time.Time `json:"lease_until,omitempty"`
	LastHeartbeatAt time.Time `json:"last_heartbeat_at,omitempty"`
	StartedAt       time.Time `json:"started_at,omitempty"`
	UpdatedAt       time.Time `json:"updated_at,omitempty"`
	Draining        bool      `json:"draining"`
}

func (h *Handler) attach(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = ModeNormal
	}
	if mode != ModeNormal && mode != ModeDiagnostic {
		http.Error(w, "unsupported attach mode", http.StatusBadRequest)
		return
	}
	requirement := attachRequirement(mode)
	principal, err := h.auth.Authorize(r, requirement)
	if err != nil {
		h.auth.WriteFailure(w, err)
		return
	}
	agentID := r.URL.Query().Get("agent_id")
	if err := domain.ValidateIdentifier("agent_id", agentID); err != nil {
		http.Error(w, "invalid agent_id", http.StatusBadRequest)
		return
	}
	if mode == ModeDiagnostic && !h.limiter.Allow(principal.ID) {
		http.Error(w, "diagnostic attach rate limited", http.StatusTooManyRequests)
		return
	}
	snapshot, err := h.state.ConsoleSnapshot(r.Context(), agentID)
	if err != nil {
		if errors.Is(err, domain.ErrAgentNotFound) || errors.Is(err, domain.ErrNotFound) {
			http.Error(w, "Agent not found", http.StatusNotFound)
		} else {
			http.Error(w, "failed to resolve Console snapshot", http.StatusInternalServerError)
		}
		return
	}
	if err := validateAttachSnapshot(snapshot, agentID); err != nil {
		http.Error(w, "invalid Console snapshot", http.StatusInternalServerError)
		return
	}
	response := AttachResponse{Readiness: h.readiness(r.Context(), snapshot), AgentID: agentID, Mode: mode, WorkerStatus: domain.WorkerStatusOffline,
		ObserveBasePath: "/api/observe/v1", ControlBasePath: "/api/control/v1",
		SnapshotSequence: snapshot.SnapshotSequence}
	if worker := snapshot.Worker; worker != nil {
		response.Generation = worker.Generation
		response.WorkerStatus = worker.Status
		response.WorkerInstanceID = worker.ID
		response.Capabilities = append([]string(nil), worker.Capabilities...)
		response.LastHeartbeatAt = worker.LastHeartbeatAt
		response.LeaseUntil = worker.LeaseUntil
		if mode == ModeDiagnostic && worker.Status != domain.WorkerStatusOffline {
			response.Diagnostic = &DiagnosticView{LeaseUntil: worker.LeaseUntil, LastHeartbeatAt: worker.LastHeartbeatAt,
				StartedAt: worker.StartedAt, UpdatedAt: worker.UpdatedAt, Draining: worker.Status == domain.WorkerStatusDraining}
		}
	}
	if len(snapshot.BackendHealth) > 0 && response.WorkerStatus != domain.WorkerStatusOffline {
		response.BackendHealth = make(map[string]openruntime.BackendHealth, len(snapshot.BackendHealth))
		for backendID, healthValue := range snapshot.BackendHealth {
			health := openruntime.BackendHealth(healthValue)
			if !health.Valid() {
				http.Error(w, "invalid Backend health in Console snapshot", http.StatusInternalServerError)
				return
			}
			response.BackendHealth[backendID] = health
		}
	}
	if snapshot.ActiveRun != nil {
		run := snapshot.ActiveRun
		generation := snapshot.ActiveRunWorkerGeneration
		response.ActiveRun = &RunSnapshot{RunID: run.ID, TaskID: run.TaskID, Status: run.Status,
			WorkerInstanceID: run.WorkerInstanceID, WorkerGeneration: &generation,
			StartedAt: run.StartedAt, UpdatedAt: run.UpdatedAt}
	}
	if snapshot.SuggestedTask != nil {
		projected, projectionErr := projectConsoleTaskOption(*snapshot.SuggestedTask)
		if projectionErr != nil {
			http.Error(w, "invalid suggested Console Task", http.StatusInternalServerError)
			return
		}
		response.SuggestedTask = &projected
	}
	writeJSON(w, response)
}

func attachRequirement(mode string) requestauth.Requirement {
	if mode == ModeDiagnostic {
		return requestauth.Requirement{Role: domain.WebRoleOwner, Scope: domain.CLIScopeConsoleDiagnostic}
	}
	return requestauth.Requirement{Role: domain.WebRoleViewer, Scope: domain.CLIScopeConsoleRead}
}

func validateAttachSnapshot(snapshot domain.ConsoleSnapshot, agentID string) error {
	if snapshot.SnapshotSequence < 0 || snapshot.Agent.ID != agentID || !snapshot.Agent.Status.Valid() {
		return fmt.Errorf("invalid Agent snapshot identity or status")
	}
	if snapshot.SuggestedTask != nil {
		if snapshot.SuggestedTask.TargetAgentID != agentID || snapshot.SuggestedTask.IsTerminal() {
			return fmt.Errorf("invalid suggested Console Task")
		}
		if _, err := projectConsoleTaskOption(*snapshot.SuggestedTask); err != nil {
			return err
		}
	}
	worker := snapshot.Worker
	if worker == nil {
		if len(snapshot.BackendHealth) != 0 || snapshot.ActiveRun != nil || snapshot.ActiveRunWorkerGeneration != 0 {
			return fmt.Errorf("worker-scoped state requires a Worker snapshot")
		}
		return nil
	}
	if err := domain.ValidateOpaqueID("worker_instance_id", worker.ID); err != nil {
		return err
	}
	if worker.AgentID != agentID || worker.Generation <= 0 || !worker.Status.Valid() {
		return fmt.Errorf("invalid Worker snapshot identity or status")
	}
	for _, capability := range worker.Capabilities {
		if err := domain.ValidateIdentifier("capability", capability); err != nil {
			return err
		}
	}
	for backendID, healthValue := range snapshot.BackendHealth {
		if err := domain.ValidateIdentifier("backend_id", backendID); err != nil {
			return err
		}
		if !openruntime.BackendHealth(healthValue).Valid() {
			return fmt.Errorf("invalid Backend health")
		}
	}
	if snapshot.ActiveRun == nil {
		if snapshot.ActiveRunWorkerGeneration != 0 {
			return fmt.Errorf("Run generation requires an active RunAttempt")
		}
		return nil
	}
	run := snapshot.ActiveRun
	if err := domain.ValidateOpaqueID("run_id", run.ID); err != nil {
		return err
	}
	if err := domain.ValidateOpaqueID("task_id", run.TaskID); err != nil {
		return err
	}
	if err := domain.ValidateOpaqueID("worker_instance_id", run.WorkerInstanceID); err != nil {
		return err
	}
	if run.AgentID != agentID || !run.Status.Active() || snapshot.ActiveRunWorkerGeneration <= 0 ||
		worker.ID != run.WorkerInstanceID || worker.Generation != snapshot.ActiveRunWorkerGeneration ||
		worker.Status == domain.WorkerStatusOffline {
		return fmt.Errorf("active RunAttempt is not fenced to the current Worker")
	}
	return nil
}

type consoleTaskCursorEnvelope struct {
	Version   int    `json:"v"`
	AgentID   string `json:"agent_id"`
	UpdatedAt string `json:"updated_at"`
	TaskID    string `json:"task_id"`
	Checksum  string `json:"checksum"`
}

func (h *Handler) tasks(w http.ResponseWriter, r *http.Request) {
	if _, err := h.auth.Authorize(r, requestauth.Requirement{Role: domain.WebRoleViewer, Scope: domain.CLIScopeConsoleRead}); err != nil {
		h.auth.WriteFailure(w, err)
		return
	}
	agentID := r.PathValue("agentID")
	if err := domain.ValidateIdentifier("agent_id", agentID); err != nil {
		http.Error(w, "invalid agent_id", http.StatusBadRequest)
		return
	}
	cursor, limit, err := parseConsoleTaskListQuery(r, agentID)
	if err != nil {
		http.Error(w, "invalid Console Task pagination", http.StatusBadRequest)
		return
	}
	tasks, err := h.state.ListConsoleTasks(r.Context(), agentID, cursor, limit+1)
	if err != nil {
		if errors.Is(err, domain.ErrAgentNotFound) || errors.Is(err, domain.ErrNotFound) {
			http.Error(w, "Agent not found", http.StatusNotFound)
		} else {
			http.Error(w, "failed to list Console Tasks", http.StatusInternalServerError)
		}
		return
	}
	projected := make([]openapi.ConsoleTaskOption, 0, len(tasks))
	previous := cursor
	for _, task := range tasks {
		option, projectionErr := projectConsoleTaskOption(task)
		if projectionErr != nil || task.TargetAgentID != agentID {
			http.Error(w, "invalid Console Task projection", http.StatusInternalServerError)
			return
		}
		updatedAt, parseErr := time.Parse(time.RFC3339Nano, option.UpdatedAt)
		if parseErr != nil || (!previous.UpdatedAt.IsZero() &&
			(updatedAt.After(previous.UpdatedAt) || updatedAt.Equal(previous.UpdatedAt) && option.TaskID >= previous.TaskID)) {
			http.Error(w, "invalid Console Task ordering", http.StatusInternalServerError)
			return
		}
		previous = domain.ConsoleTaskCursor{UpdatedAt: updatedAt, TaskID: option.TaskID}
		projected = append(projected, option)
	}
	page := openapi.ConsoleTaskPage{Tasks: projected}
	if len(page.Tasks) > limit {
		page.HasMore = true
		page.Tasks = page.Tasks[:limit]
		last := page.Tasks[len(page.Tasks)-1]
		page.NextCursor, err = encodeConsoleTaskCursor(agentID, last.UpdatedAt, last.TaskID)
		if err != nil {
			http.Error(w, "failed to encode Console Task cursor", http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, page)
}

func (h *Handler) task(w http.ResponseWriter, r *http.Request) {
	if _, err := h.auth.Authorize(r, requestauth.Requirement{Role: domain.WebRoleViewer, Scope: domain.CLIScopeConsoleRead}); err != nil {
		h.auth.WriteFailure(w, err)
		return
	}
	agentID, taskID := r.PathValue("agentID"), r.PathValue("taskID")
	if domain.ValidateIdentifier("agent_id", agentID) != nil || domain.ValidateOpaqueID("task_id", taskID) != nil {
		http.Error(w, "invalid Console Task identity", http.StatusBadRequest)
		return
	}
	snapshot, err := h.state.ConsoleTaskSnapshot(r.Context(), agentID, taskID)
	if err != nil {
		if errors.Is(err, domain.ErrAgentNotFound) || errors.Is(err, domain.ErrTaskNotFound) || errors.Is(err, domain.ErrNotFound) {
			http.Error(w, "Console Task not found", http.StatusNotFound)
		} else {
			http.Error(w, "failed to resolve Console Task snapshot", http.StatusInternalServerError)
		}
		return
	}
	projected, err := projectConsoleTaskSnapshot(snapshot, agentID, taskID)
	if err != nil {
		http.Error(w, "invalid Console Task snapshot", http.StatusInternalServerError)
		return
	}
	if reviews, ok := h.state.(interface {
		GetTaskReview(context.Context, string) (*domain.TaskReview, error)
	}); ok {
		review, err := reviews.GetTaskReview(r.Context(), taskID)
		if err != nil {
			http.Error(w, "failed to load result review", http.StatusInternalServerError)
			return
		}
		if review != nil {
			if review.TaskID != taskID || projected.LatestRun == nil || review.RunID != projected.LatestRun.ID || review.RunVersion != projected.LatestRun.Version || review.TaskVersion > projected.Task.Version {
				http.Error(w, "Task changed while loading result review; refresh", http.StatusConflict)
				return
			}
			projected.Task.Review = review
		}
	}
	writeJSON(w, projected)
}

func parseConsoleTaskListQuery(r *http.Request, agentID string) (domain.ConsoleTaskCursor, int, error) {
	query := r.URL.Query()
	limit := taskPageSize
	if values, exists := query["limit"]; exists {
		if len(values) != 1 || values[0] == "" {
			return domain.ConsoleTaskCursor{}, 0, fmt.Errorf("invalid limit")
		}
		parsed, err := strconv.Atoi(values[0])
		if err != nil || parsed < 1 || parsed > taskPageMax {
			return domain.ConsoleTaskCursor{}, 0, fmt.Errorf("invalid limit")
		}
		limit = parsed
	}
	values, exists := query["cursor"]
	if !exists {
		return domain.ConsoleTaskCursor{}, limit, nil
	}
	if len(values) != 1 || values[0] == "" {
		return domain.ConsoleTaskCursor{}, 0, fmt.Errorf("invalid cursor")
	}
	cursor, err := decodeConsoleTaskCursor(values[0], agentID)
	return cursor, limit, err
}

func encodeConsoleTaskCursor(agentID, updatedAt, taskID string) (string, error) {
	if domain.ValidateIdentifier("agent_id", agentID) != nil || domain.ValidateOpaqueID("task_id", taskID) != nil {
		return "", fmt.Errorf("invalid cursor identity")
	}
	parsed, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return "", err
	}
	envelope := consoleTaskCursorEnvelope{Version: 1, AgentID: agentID, UpdatedAt: parsed.UTC().Format(time.RFC3339Nano), TaskID: taskID}
	envelope.Checksum = consoleTaskCursorChecksum(envelope)
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeConsoleTaskCursor(value, agentID string) (domain.ConsoleTaskCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return domain.ConsoleTaskCursor{}, err
	}
	var envelope consoleTaskCursorEnvelope
	if err := json.Unmarshal(decoded, &envelope); err != nil {
		return domain.ConsoleTaskCursor{}, err
	}
	canonical, err := json.Marshal(envelope)
	if err != nil || !bytes.Equal(decoded, canonical) {
		return domain.ConsoleTaskCursor{}, fmt.Errorf("non-canonical cursor envelope")
	}
	if envelope.Version != 1 || envelope.AgentID != agentID || envelope.Checksum == "" ||
		envelope.Checksum != consoleTaskCursorChecksum(envelope) || domain.ValidateOpaqueID("task_id", envelope.TaskID) != nil {
		return domain.ConsoleTaskCursor{}, fmt.Errorf("invalid cursor envelope")
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, envelope.UpdatedAt)
	if err != nil {
		return domain.ConsoleTaskCursor{}, err
	}
	if updatedAt.UTC().Format(time.RFC3339Nano) != envelope.UpdatedAt {
		return domain.ConsoleTaskCursor{}, fmt.Errorf("non-canonical cursor timestamp")
	}
	return domain.ConsoleTaskCursor{UpdatedAt: updatedAt.UTC(), TaskID: envelope.TaskID}, nil
}

func consoleTaskCursorChecksum(envelope consoleTaskCursorEnvelope) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%s\x00%s", envelope.Version, envelope.AgentID, envelope.UpdatedAt, envelope.TaskID)))
	return base64.RawURLEncoding.EncodeToString(digest[:])
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

type consoleReadinessState interface {
	ListWorkerBackends(context.Context, string) ([]openruntime.BackendRegistration, error)
}

func (h *Handler) readiness(ctx context.Context, snapshot domain.ConsoleSnapshot) *openapi.AgentReadiness {
	reader, ok := h.state.(consoleReadinessState)
	if !ok {
		return nil
	}
	var backends []openruntime.BackendRegistration
	if snapshot.Worker != nil {
		var err error
		backends, err = reader.ListWorkerBackends(ctx, snapshot.Worker.ID)
		if err != nil {
			backends = nil
		}
	}
	var tasks []domain.Task
	if snapshot.SuggestedTask != nil {
		tasks = append(tasks, *snapshot.SuggestedTask)
	}
	if snapshot.ActiveRun != nil {
		tasks = append(tasks, domain.Task{TargetAgentID: snapshot.Agent.ID, Status: domain.TaskStatusRunning})
	}
	result := openapi.ProjectAgentReadiness(snapshot.Agent.ID, snapshot.Worker, tasks, backends, time.Now())
	return &result
}
