package panel

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	openapi "openagentx/internal/api"
	"openagentx/internal/auth/web"
	"openagentx/internal/controlplane"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

type State interface {
	ListAgents(context.Context, int) ([]domain.AgentIdentity, error)
	ListWorkers(context.Context, int) ([]domain.WorkerInstance, error)
	ListTasks(context.Context, string, int) ([]domain.Task, error)
	QueryTasks(context.Context, string, domain.TaskStatus, time.Time, time.Time, string, string, string, int) ([]domain.Task, error)
	ListJournal(context.Context, int64, int) ([]domain.JournalEvent, error)
	ListTaskJournal(context.Context, string, int64, int) ([]domain.JournalEvent, error)
	ListTaskJournalBefore(context.Context, string, int64, int) ([]domain.JournalEvent, error)
	ListTaskJournalRange(context.Context, string, int64, int64, int) ([]domain.JournalEvent, error)
	GetTask(context.Context, string) (*domain.Task, error)
	ListMessages(context.Context, string) ([]domain.Message, error)
	ListMailbox(context.Context, string, int64, int) ([]domain.MailboxItem, error)
	GetRunAttempt(context.Context, string) (*domain.RunAttempt, error)
	ListRunAttemptsForTask(context.Context, string, int) ([]domain.RunAttempt, error)
	ListPendingApprovals(context.Context, int) ([]domain.ApprovalRequest, error)
	LatestJournalSequence(context.Context) (int64, error)
}

// backendOptionsState is optional to keep the observe contract compatible with
// lightweight fixtures. The SQLite repository implements it; when unavailable
// the endpoint returns an empty set rather than inventing profile data.
type backendOptionsState interface {
	ListWorkerBackends(context.Context, string) ([]openruntime.BackendRegistration, error)
}

type networkProfileState interface {
	CreateProxyProfile(context.Context, *domain.ProxyProfile) error
	GetProxyProfile(context.Context, string, int64) (*domain.ProxyProfile, error)
	ListProxyProfiles(context.Context, int) ([]domain.ProxyProfile, error)
	PublishProxyProfile(context.Context, string, int64, string, time.Time) (*domain.ProxyProfile, error)
	BindNetworkProfile(context.Context, *domain.NetworkBinding, int64) error
	ListNetworkBindings(context.Context, string) ([]domain.NetworkBinding, error)
}

type Handler struct {
	state    State
	commands *controlplane.CommandService
	auth     *web.Manager
	mux      *http.ServeMux
}

func NewHandler(state State, commands *controlplane.CommandService, auth *web.Manager) (*Handler, error) {
	if state == nil || commands == nil || auth == nil {
		return nil, fmt.Errorf("panel state, commands and auth are required")
	}
	h := &Handler{state: state, commands: commands, auth: auth, mux: http.NewServeMux()}
	h.mux.HandleFunc("GET "+openapi.ObserveHealthPath, h.health)
	h.mux.HandleFunc("GET /api/observe/v1/overview", h.overview)
	h.mux.HandleFunc("GET /api/observe/v1/agents", h.agents)
	h.mux.HandleFunc("GET /api/observe/v1/tasks", h.tasks)
	h.mux.HandleFunc("GET /api/observe/v1/tasks/{taskID}", h.task)
	h.mux.HandleFunc("GET /api/observe/v1/mailboxes", h.mailboxes)
	h.mux.HandleFunc("GET /api/observe/v1/run-attempts/{runID}", h.run)
	h.mux.HandleFunc("GET "+openapi.ObserveExecutionOptionsPath, h.executionOptions)
	h.mux.HandleFunc("GET "+openapi.ObserveNetworkProfilesPath, h.networkProfiles)
	h.mux.HandleFunc("GET /api/observe/v1/events/stream", h.events)
	h.mux.HandleFunc("POST /api/control/v1/tasks", h.createTask)
	h.mux.HandleFunc("POST /api/control/v1/tasks/{taskID}/messages", h.createMessage)
	h.mux.HandleFunc("POST /api/control/v1/tasks/{taskID}/cancel", h.cancelTask)
	h.mux.HandleFunc("POST /api/control/v1/approvals/{approvalID}/decisions", h.decideApproval)
	h.mux.HandleFunc("POST "+openapi.ControlNetworkProfilePath, h.createNetworkProfile)
	h.mux.HandleFunc("POST "+openapi.ControlNetworkProfilePublishPath, h.publishNetworkProfile)
	h.mux.HandleFunc("POST "+openapi.ControlNetworkBindingPath, h.bindNetworkProfile)
	return h, nil
}
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	h.mux.ServeHTTP(w, r)
}
func (h *Handler) session(w http.ResponseWriter, r *http.Request, write bool) (*web.Session, bool) {
	s, err := h.auth.Authenticate(r)
	if err != nil {
		http.Error(w, "unauthorized", 401)
		return nil, false
	}
	if write {
		if err := web.RequireRole(s, web.RoleOperator); err != nil {
			http.Error(w, "forbidden", 403)
			return nil, false
		}
		if err := web.ValidateCSRF(s, r.Header.Get("X-CSRF-Token")); err != nil {
			http.Error(w, "invalid csrf token", 403)
			return nil, false
		}
	}
	return s, true
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, struct {
		Status string `json:"status"`
	}{Status: "ok"})
}
func (h *Handler) overview(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.session(w, r, false); !ok {
		return
	}
	agents, _ := h.state.ListAgents(r.Context(), 100)
	workers, _ := h.state.ListWorkers(r.Context(), 100)
	tasks, _ := h.state.ListTasks(r.Context(), "", 100)
	approvals, _ := h.state.ListPendingApprovals(r.Context(), 100)
	latestSequence, _ := h.state.LatestJournalSequence(r.Context())
	writeJSON(w, map[string]any{"agents": agents, "workers": workers, "tasks": tasks, "approvals": approvals, "latest_sequence": latestSequence, "server_time": time.Now().UTC()})
}
func (h *Handler) agents(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.session(w, r, false); !ok {
		return
	}
	a, e := h.state.ListAgents(r.Context(), 100)
	if e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	writeJSON(w, a)
}
func (h *Handler) tasks(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.session(w, r, false); !ok {
		return
	}
	query, err := parseTaskQuery(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	tasks, err := h.state.QueryTasks(r.Context(), query.AgentID, query.Status, query.UpdatedAfter, query.UpdatedBefore, query.Text, query.CursorUpdatedAt, query.CursorTaskID, query.Limit+1)
	if err != nil {
		http.Error(w, "failed to load tasks", http.StatusInternalServerError)
		return
	}
	hasMore := len(tasks) > query.Limit
	if hasMore {
		tasks = tasks[:query.Limit]
	}
	page := openapi.TaskListPage{Tasks: make([]openapi.TaskListItem, 0, len(tasks)), HasMore: hasMore}
	for _, task := range tasks {
		page.Tasks = append(page.Tasks, taskListItem(task))
	}
	if hasMore && len(tasks) > 0 {
		last := tasks[len(tasks)-1]
		page.NextCursor = encodeTaskCursor(taskCursor{UpdatedAt: last.UpdatedAt, TaskID: last.ID, Filter: query.Filter})
	}
	writeJSON(w, page)
}
func (h *Handler) task(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.session(w, r, false); !ok {
		return
	}
	cursor, err := observeCursor(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	snapshotSequence, err := h.state.LatestJournalSequence(r.Context())
	if err != nil {
		http.Error(w, "failed to read observation snapshot", http.StatusInternalServerError)
		return
	}
	if cursor.Mode == observeAfter && cursor.Sequence > snapshotSequence {
		http.Error(w, "observation cursor is ahead of the current snapshot", http.StatusConflict)
		return
	}
	taskID := r.PathValue("taskID")
	var events []domain.JournalEvent
	if cursor.Mode == observeAfter {
		events, err = h.state.ListTaskJournalRange(r.Context(), taskID, cursor.Sequence, snapshotSequence, cursor.Limit+1)
	} else {
		before := cursor.Sequence
		if before == 0 || before > snapshotSequence+1 {
			before = snapshotSequence + 1
		}
		events, err = h.state.ListTaskJournalBefore(r.Context(), taskID, before, cursor.Limit+1)
	}
	if err != nil {
		http.Error(w, "failed to load task events", http.StatusInternalServerError)
		return
	}
	// Read projections after the event watermark is fixed. A concurrent commit
	// is either visible in these projections and remains after the live cursor,
	// or is picked up by the next range request; it can never be skipped.
	t, err := h.state.GetTask(r.Context(), taskID)
	if err != nil {
		if errors.Is(err, domain.ErrTaskNotFound) || errors.Is(err, domain.ErrNotFound) {
			http.Error(w, "task not found", http.StatusNotFound)
		} else if isForbidden(err) {
			http.Error(w, "forbidden", http.StatusForbidden)
		} else {
			http.Error(w, "failed to load task", http.StatusInternalServerError)
		}
		return
	}
	m, err := h.state.ListMessages(r.Context(), t.ID)
	if err != nil {
		http.Error(w, "failed to load task messages", http.StatusInternalServerError)
		return
	}
	runs, err := h.state.ListRunAttemptsForTask(r.Context(), t.ID, 100)
	if err != nil {
		http.Error(w, "failed to load task runs", http.StatusInternalServerError)
		return
	}
	hasMore := len(events) > cursor.Limit
	if hasMore {
		if cursor.Mode == observeAfter {
			events = events[:cursor.Limit]
		} else {
			events = events[1:]
		}
	}
	projected := projectEvents(events)
	readModel := openapi.TaskReadModel{Task: taskReadModel(*t), Messages: m, Events: projected, SnapshotSequence: snapshotSequence}
	if cursor.Mode == observeAfter {
		readModel.HasMoreLiveEvents = hasMore
		readModel.LiveAfterSequence = snapshotSequence
		if hasMore {
			readModel.LiveAfterSequence = lastEventSequence(projected)
		}
	} else {
		readModel.HasOlderEvents = hasMore
		readModel.LiveAfterSequence = snapshotSequence
		if hasMore && len(projected) > 0 {
			readModel.HistoryBeforeSequence = projected[0].Sequence
		}
	}
	for _, run := range runs {
		readModel.RunAttempts = append(readModel.RunAttempts, runReadModel(run))
	}
	if readModel.RunAttempts == nil {
		readModel.RunAttempts = []openapi.RunAttemptReadModel{}
	}
	if readModel.Events == nil {
		readModel.Events = []openapi.JournalEventReadModel{}
	}
	writeJSON(w, readModel)
}

func isForbidden(err error) bool {
	var domainErr *domain.DomainError
	return errors.As(err, &domainErr) && domainErr.Code == "FORBIDDEN"
}

type observeCursorMode uint8

const (
	observeHistory observeCursorMode = iota
	observeAfter
)

type observationCursor struct {
	Mode     observeCursorMode
	Sequence int64
	Limit    int
}

func observeCursor(r *http.Request) (observationCursor, error) {
	afterValue := r.URL.Query().Get("after_sequence")
	beforeValue := r.URL.Query().Get("before_sequence")
	if afterValue != "" && beforeValue != "" {
		return observationCursor{}, fmt.Errorf("after_sequence and before_sequence are mutually exclusive")
	}
	cursor := observationCursor{Mode: observeHistory, Limit: 200}
	value := beforeValue
	if afterValue != "" {
		cursor.Mode = observeAfter
		value = afterValue
	}
	if value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed < 0 {
			return observationCursor{}, fmt.Errorf("invalid observation sequence")
		}
		cursor.Sequence = parsed
	}
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 499 {
			return observationCursor{}, fmt.Errorf("invalid limit")
		}
		cursor.Limit = parsed
	}
	return cursor, nil
}

type taskQuery struct {
	AgentID, Text, CursorUpdatedAt, CursorTaskID, Filter string
	Status                                               domain.TaskStatus
	UpdatedAfter, UpdatedBefore                          time.Time
	Limit                                                int
}

type taskCursor struct {
	UpdatedAt string `json:"updated_at"`
	TaskID    string `json:"task_id"`
	Filter    string `json:"filter"`
}

func parseTaskQuery(r *http.Request) (taskQuery, error) {
	values := r.URL.Query()
	query := taskQuery{AgentID: strings.TrimSpace(values.Get("agent_id")), Text: strings.TrimSpace(values.Get("query")), Limit: 50}
	if utf8.RuneCountInString(query.Text) > 200 {
		return taskQuery{}, fmt.Errorf("query is too long")
	}
	if value := strings.TrimSpace(values.Get("status")); value != "" {
		query.Status = domain.TaskStatus(value)
		if !query.Status.Valid() {
			return taskQuery{}, fmt.Errorf("invalid task status")
		}
	}
	var err error
	if value := values.Get("updated_after"); value != "" {
		query.UpdatedAfter, err = time.Parse(time.RFC3339, value)
		if err != nil {
			return taskQuery{}, fmt.Errorf("invalid updated_after")
		}
	}
	if value := values.Get("updated_before"); value != "" {
		query.UpdatedBefore, err = time.Parse(time.RFC3339, value)
		if err != nil {
			return taskQuery{}, fmt.Errorf("invalid updated_before")
		}
	}
	if !query.UpdatedAfter.IsZero() && !query.UpdatedBefore.IsZero() && !query.UpdatedAfter.Before(query.UpdatedBefore) {
		return taskQuery{}, fmt.Errorf("updated_after must be before updated_before")
	}
	if value := values.Get("limit"); value != "" {
		query.Limit, err = strconv.Atoi(value)
		if err != nil || query.Limit < 1 || query.Limit > 100 {
			return taskQuery{}, fmt.Errorf("invalid limit")
		}
	}
	filterBytes := sha256.Sum256([]byte(strings.Join([]string{query.AgentID, string(query.Status), query.UpdatedAfter.UTC().Format(time.RFC3339Nano), query.UpdatedBefore.UTC().Format(time.RFC3339Nano), query.Text}, "\x00")))
	query.Filter = base64.RawURLEncoding.EncodeToString(filterBytes[:])
	if value := values.Get("cursor"); value != "" {
		cursor, decodeErr := decodeTaskCursor(value)
		if decodeErr != nil || cursor.Filter != query.Filter || strings.TrimSpace(cursor.UpdatedAt) == "" || strings.TrimSpace(cursor.TaskID) == "" {
			return taskQuery{}, fmt.Errorf("invalid task cursor")
		}
		if _, parseErr := time.Parse(time.RFC3339Nano, cursor.UpdatedAt); parseErr != nil {
			return taskQuery{}, fmt.Errorf("invalid task cursor")
		}
		query.CursorUpdatedAt, query.CursorTaskID = cursor.UpdatedAt, cursor.TaskID
	}
	return query, nil
}

func encodeTaskCursor(cursor taskCursor) string {
	encoded, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func decodeTaskCursor(value string) (taskCursor, error) {
	var cursor taskCursor
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return cursor, err
	}
	if err := json.Unmarshal(decoded, &cursor); err != nil {
		return cursor, err
	}
	return cursor, nil
}

func taskListItem(task domain.Task) openapi.TaskListItem {
	summary := strings.TrimSpace(strings.SplitN(task.Content, "\n", 2)[0])
	if summary == "" {
		summary = task.ID
	}
	runes := []rune(summary)
	if len(runes) > 160 {
		summary = string(runes[:159]) + "…"
	}
	return openapi.TaskListItem{ID: task.ID, TargetAgentID: task.TargetAgentID, Status: task.Status, Summary: summary, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt}
}

func projectEvents(events []domain.JournalEvent) []openapi.JournalEventReadModel {
	projected := make([]openapi.JournalEventReadModel, 0, len(events))
	for _, event := range events {
		projected = append(projected, openapi.JournalEventReadModel{Sequence: event.Sequence, ID: event.ID, AggregateType: event.AggregateType, AggregateID: event.AggregateID, EventType: event.EventType, CreatedAt: event.CreatedAt})
	}
	return projected
}

func lastEventSequence(events []openapi.JournalEventReadModel) int64 {
	var sequence int64
	for _, event := range events {
		if event.Sequence > sequence {
			sequence = event.Sequence
		}
	}
	return sequence
}

func runReadModel(run domain.RunAttempt) openapi.RunAttemptReadModel {
	return openapi.RunAttemptReadModel{
		ID: run.ID, TaskID: run.TaskID, AgentID: run.AgentID, Version: run.Version,
		Status: run.Status, WorkerInstanceID: run.WorkerInstanceID,
		ExecutionSpecVersion: run.ExecutionSpecVersion, AdapterID: run.AdapterID,
		BackendID: run.BackendID, Model: run.Model, ReasoningMode: run.ReasoningMode,
		ReasoningValue: run.ReasoningValue, NetworkMode: networkMode(run), NetworkProfileID: networkProfileID(run), NetworkProfileVersion: networkProfileVersion(run), StartedAt: run.StartedAt,
		FinishedAt: run.FinishedAt, CreatedAt: run.CreatedAt, UpdatedAt: run.UpdatedAt,
	}
}

func taskReadModel(task domain.Task) openapi.TaskReadModelTask {
	return openapi.TaskReadModelTask{ID: task.ID, Version: task.Version, TargetAgentID: task.TargetAgentID, OrganizationID: task.OrganizationID, DispatchMode: task.DispatchMode, Content: task.Content, Status: task.Status, Result: task.Result, Error: task.Error, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt}
}

func resolvedNetwork(run domain.RunAttempt) domain.NetworkPolicy {
	var resolved domain.ResolvedExecutionSpec
	if err := json.Unmarshal([]byte(run.ResolvedExecutionJSON), &resolved); err != nil {
		return domain.NetworkPolicy{}
	}
	return resolved.Spec.Network
}

func networkMode(run domain.RunAttempt) domain.NetworkMode { return resolvedNetwork(run).Mode }
func networkProfileID(run domain.RunAttempt) string        { return resolvedNetwork(run).ProfileID }
func networkProfileVersion(run domain.RunAttempt) int64    { return resolvedNetwork(run).ProfileVersion }
func (h *Handler) mailboxes(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.session(w, r, false); !ok {
		return
	}
	items, e := h.state.ListMailbox(r.Context(), r.URL.Query().Get("agent_id"), 0, 100)
	if e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	writeJSON(w, items)
}
func (h *Handler) run(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.session(w, r, false); !ok {
		return
	}
	v, e := h.state.GetRunAttempt(r.Context(), r.PathValue("runID"))
	if e != nil {
		http.Error(w, e.Error(), 404)
		return
	}
	writeJSON(w, runReadModel(*v))
}

func (h *Handler) executionOptions(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.session(w, r, false); !ok {
		return
	}
	provider, ok := h.state.(backendOptionsState)
	if !ok {
		writeJSON(w, map[string]any{"backends": []any{}})
		return
	}
	workers, err := h.state.ListWorkers(r.Context(), 100)
	if err != nil {
		http.Error(w, "failed to load workers", http.StatusInternalServerError)
		return
	}
	type option struct {
		WorkerID string                          `json:"worker_id"`
		AgentID  string                          `json:"agent_id"`
		Backend  openruntime.BackendRegistration `json:"backend"`
	}
	options := make([]option, 0)
	for _, worker := range workers {
		backends, listErr := provider.ListWorkerBackends(r.Context(), worker.ID)
		if listErr != nil {
			continue
		}
		for _, backend := range backends {
			// BackendRegistration only carries a profile reference and health;
			// it never exposes credentials or config file contents.
			backend.Network.ConfigFile = ""
			options = append(options, option{WorkerID: worker.ID, AgentID: worker.AgentID, Backend: backend})
		}
	}
	writeJSON(w, map[string]any{"backends": options})
}

func (h *Handler) networkProfiles(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.session(w, r, false); !ok {
		return
	}
	state, ok := h.state.(networkProfileState)
	if !ok {
		writeJSON(w, map[string]any{"profiles": []any{}, "bindings": []any{}})
		return
	}
	profiles, err := state.ListProxyProfiles(r.Context(), 200)
	if err != nil {
		http.Error(w, "failed to load network profiles", 500)
		return
	}
	bindings, err := state.ListNetworkBindings(r.Context(), r.URL.Query().Get("agent_id"))
	if err != nil {
		http.Error(w, "failed to load network bindings", 500)
		return
	}
	// Never expose the secret reference itself to the browser; presence is
	// enough for operators to distinguish configured from unconfigured.
	for i := range profiles {
		profiles[i].SecretRef = ""
		profiles[i].ConfigFile = ""
	}
	for i := range bindings {
		if bindings[i].Profile != nil {
			bindings[i].Profile.SecretRef = ""
			bindings[i].Profile.ConfigFile = ""
		}
	}
	writeJSON(w, map[string]any{"profiles": profiles, "bindings": bindings})
}

func (h *Handler) createNetworkProfile(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r, true)
	if !ok {
		return
	}
	state, ok := h.state.(networkProfileState)
	if !ok {
		http.Error(w, "network profiles unavailable", 501)
		return
	}
	var req openapi.CreateNetworkProfileRequest
	if openapi.DecodeStrictJSON(r.Body, &req) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	if !requireIdempotencyHeader(w, r, req.Meta) {
		return
	}
	p, err := req.Validate(s.User.ID)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if err := state.CreateProxyProfile(r.Context(), &p); err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	p.SecretRef = ""
	writeJSON(w, p)
}

func (h *Handler) publishNetworkProfile(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r, true)
	if !ok {
		return
	}
	state, ok := h.state.(networkProfileState)
	if !ok {
		http.Error(w, "network profiles unavailable", 501)
		return
	}
	var req openapi.PublishNetworkProfileRequest
	if openapi.DecodeStrictJSON(r.Body, &req) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	if !requireIdempotencyHeader(w, r, req.Meta) {
		return
	}
	if err := req.Validate(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	p, err := state.PublishProxyProfile(r.Context(), r.PathValue("profileID"), req.Meta.ExpectedVersion, s.User.ID, time.Now().UTC())
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	p.SecretRef = ""
	writeJSON(w, p)
}

func (h *Handler) bindNetworkProfile(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.session(w, r, true); !ok {
		return
	}
	state, ok := h.state.(networkProfileState)
	if !ok {
		http.Error(w, "network profiles unavailable", 501)
		return
	}
	var req openapi.BindNetworkProfileRequest
	if openapi.DecodeStrictJSON(r.Body, &req) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	if !requireIdempotencyHeader(w, r, req.Meta) {
		return
	}
	b, err := req.Validate(time.Now().UTC())
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if provider, ok := h.state.(backendOptionsState); ok {
		workers, listErr := h.state.ListWorkers(r.Context(), 100)
		if listErr != nil {
			http.Error(w, "failed to load workers", http.StatusInternalServerError)
			return
		}
		supported := false
		for _, worker := range workers {
			if worker.AgentID != b.AgentID {
				continue
			}
			backends, backendErr := provider.ListWorkerBackends(r.Context(), worker.ID)
			if backendErr != nil {
				continue
			}
			for _, backend := range backends {
				if backend.BackendID == b.BackendID && containsString(backend.Descriptor.NetworkModes, "named_profile") {
					supported = true
					break
				}
			}
		}
		if !supported {
			http.Error(w, "backend does not support named_profile network bindings", http.StatusConflict)
			return
		}
	}
	if err := state.BindNetworkProfile(r.Context(), &b, req.Meta.ExpectedVersion); err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	writeJSON(w, b)
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
func (h *Handler) events(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.session(w, r, false); !ok {
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", 500)
		return
	}
	var after int64
	for _, afterValue := range []string{r.URL.Query().Get("after_sequence"), r.Header.Get("Last-Event-ID")} {
		if afterValue == "" {
			continue
		}
		parsed, err := strconv.ParseInt(afterValue, 10, 64)
		if err != nil || parsed < 0 {
			http.Error(w, "invalid after_sequence", http.StatusBadRequest)
			return
		}
		if parsed > after {
			after = parsed
		}
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ticker := time.NewTicker(time.Second)
	keepalive := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	defer keepalive.Stop()
	for {
		events, e := h.state.ListJournal(r.Context(), after, 100)
		if e != nil {
			return
		}
		for _, ev := range events {
			if !observableAggregate(ev.AggregateType) {
				after = ev.Sequence
				continue
			}
			// The panel currently runs in a single authenticated organization scope.
			// Stream only the browser-safe projection; Journal payloads never cross
			// the SSE boundary and therefore cannot expose runtime diagnostics or
			// credentials through a reconnecting client.
			b, _ := json.Marshal(projectEvents([]domain.JournalEvent{ev})[0])
			fmt.Fprintf(w, "id: %d\ndata: %s\n\n", ev.Sequence, b)
			after = ev.Sequence
		}
		fl.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			fl.Flush()
		}
	}
}

func observableAggregate(aggregateType string) bool {
	switch aggregateType {
	case "", "task", "run_attempt", "message", "approval_request":
		return true
	default:
		return false
	}
}
func (h *Handler) createTask(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r, true)
	if !ok {
		return
	}
	var req openapi.CreateTaskRequest
	if openapi.DecodeStrictJSON(r.Body, &req) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	if !requireIdempotencyHeader(w, r, req.Meta) {
		return
	}
	req.SenderPrincipalID = s.User.ID
	v, e := h.commands.CreateTask(r.Context(), s.User.ID, req)
	if e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	writeJSON(w, v)
}
func (h *Handler) createMessage(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r, true)
	if !ok {
		return
	}
	var req openapi.CreateMessageRequest
	if openapi.DecodeStrictJSON(r.Body, &req) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	if !requireIdempotencyHeader(w, r, req.Meta) {
		return
	}
	req.SenderPrincipalID = s.User.ID
	v, e := h.commands.CreateMessage(r.Context(), s.User.ID, r.PathValue("taskID"), req)
	if e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	writeJSON(w, v)
}
func (h *Handler) cancelTask(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r, true)
	if !ok {
		return
	}
	var req openapi.CancelTaskRequest
	if openapi.DecodeStrictJSON(r.Body, &req) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	if !requireIdempotencyHeader(w, r, req.Meta) {
		return
	}
	req.RequestedBy = s.User.ID
	v, e := h.commands.CancelTask(r.Context(), s.User.ID, r.PathValue("taskID"), req)
	if e != nil {
		status := 400
		if errors.Is(e, domain.ErrTerminalState) {
			status = 409
		}
		http.Error(w, e.Error(), status)
		return
	}
	writeJSON(w, v)
}

func (h *Handler) decideApproval(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r, true)
	if !ok {
		return
	}
	var req openapi.DecideApprovalRequest
	if openapi.DecodeStrictJSON(r.Body, &req) != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if !requireIdempotencyHeader(w, r, req.Meta) {
		return
	}
	req.DecidedBy = s.User.ID
	result, err := h.commands.DecideApproval(r.Context(), s.User.ID, r.PathValue("approvalID"), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, result)
}

func requireIdempotencyHeader(w http.ResponseWriter, r *http.Request, meta openapi.CommandMeta) bool {
	value := r.Header.Get("Idempotency-Key")
	if value == "" || value != meta.IdempotencyKey {
		http.Error(w, "Idempotency-Key header must match request meta", http.StatusBadRequest)
		return false
	}
	return true
}
