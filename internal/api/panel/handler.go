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
	consoleapi "openagentx/internal/api/console"
	requestauth "openagentx/internal/auth"
	cliauth "openagentx/internal/auth/cli"
	webauth "openagentx/internal/auth/web"
	"openagentx/internal/controlplane"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
	"openagentx/internal/safeoutput"
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
	GetMessage(context.Context, string) (*domain.Message, error)
	ListMailbox(context.Context, string, int64, int) ([]domain.MailboxItem, error)
	GetMailboxItem(context.Context, string) (*domain.MailboxItem, error)
	GetRunAttempt(context.Context, string) (*domain.RunAttempt, error)
	GetWorkerInstance(context.Context, string) (*domain.WorkerInstance, error)
	ListRunAttemptsForTask(context.Context, string, int) ([]domain.RunAttempt, error)
	ListPendingApprovals(context.Context, int) ([]domain.ApprovalRequest, error)
	GetApprovalRequest(context.Context, string) (*domain.ApprovalRequest, error)
	LatestJournalSequence(context.Context) (int64, error)
	JournalSequenceBounds(context.Context) (domain.JournalSequenceBounds, error)
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
	state       State
	commands    *controlplane.CommandService
	network     *controlplane.NetworkWorkflowService
	auth        requestauth.RequestAuthorizer
	mux         *http.ServeMux
	now         func() time.Time
	expiryTimer func(time.Duration) (<-chan time.Time, func())
}

func NewHandler(state State, commands *controlplane.CommandService, manager *webauth.Manager, network ...*controlplane.NetworkWorkflowService) (*Handler, error) {
	authorizer, err := requestauth.NewWebAuthorizer(manager)
	if err != nil {
		return nil, err
	}
	return newHandler(state, commands, authorizer, network...)
}

func NewCLIHandler(state State, commands *controlplane.CommandService, service *cliauth.Service, network ...*controlplane.NetworkWorkflowService) (*Handler, error) {
	authorizer, err := requestauth.NewCLIAuthorizer(service)
	if err != nil {
		return nil, err
	}
	return newHandler(state, commands, authorizer, network...)
}

func newHandler(state State, commands *controlplane.CommandService, authorizer requestauth.RequestAuthorizer, network ...*controlplane.NetworkWorkflowService) (*Handler, error) {
	if state == nil || commands == nil || authorizer == nil {
		return nil, fmt.Errorf("panel state, commands and auth are required")
	}
	var networkService *controlplane.NetworkWorkflowService
	if len(network) > 0 {
		networkService = network[0]
	}
	h := &Handler{state: state, commands: commands, network: networkService, auth: authorizer,
		mux: http.NewServeMux(), now: time.Now, expiryTimer: func(delay time.Duration) (<-chan time.Time, func()) {
			timer := time.NewTimer(delay)
			return timer.C, func() { timer.Stop() }
		}}
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
	h.mux.HandleFunc("POST /api/control/v1/tasks/{taskID}/review", h.reviewTask)
	h.mux.HandleFunc("POST /api/control/v1/approvals/{approvalID}/decisions", h.decideApproval)
	h.mux.HandleFunc("POST "+openapi.ControlNetworkProfilePath, h.createNetworkProfile)
	h.mux.HandleFunc("POST "+openapi.ControlNetworkProfileDraftPath, h.editNetworkProfile)
	h.mux.HandleFunc("POST "+openapi.ControlNetworkProfileSecretPath, h.replaceNetworkSecret)
	h.mux.HandleFunc("POST "+openapi.ControlNetworkProfileTestPath, h.testNetworkProfile)
	h.mux.HandleFunc("POST "+openapi.ControlNetworkProfilePublishPath, h.publishNetworkProfile)
	h.mux.HandleFunc("POST "+openapi.ControlNetworkBindingPath, h.bindNetworkProfile)
	h.mux.HandleFunc("POST "+openapi.ControlNetworkRollbackPath, h.rollbackNetworkBinding)
	h.mux.HandleFunc("POST "+openapi.ControlNetworkModeTestPath, h.testNetworkMode)
	h.mux.HandleFunc("POST "+openapi.ControlNetworkModePublishPath, h.publishNetworkMode)
	h.mux.HandleFunc("POST "+openapi.ControlNetworkImportPath, h.importNetworkProfile)
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
func (h *Handler) session(w http.ResponseWriter, r *http.Request, write bool) (*requestauth.Principal, bool) {
	requirement := panelRequirement(r, write)
	s, err := h.auth.Authorize(r, requirement)
	if err != nil {
		h.auth.WriteFailure(w, err)
		return nil, false
	}
	return &s, true
}

func panelRequirement(r *http.Request, write bool) requestauth.Requirement {
	requirement := requestauth.Requirement{Role: domain.WebRoleViewer, Write: write}
	if write {
		switch {
		case r.URL.Path == openapi.ControlNetworkModeTestPath, r.URL.Path == openapi.ControlNetworkModePublishPath:
			requirement.Role = domain.WebRoleOwner
			requirement.Scope = domain.CLIScopeFleetLifecycle
		case r.URL.Path == "/api/control/v1/tasks",
			strings.HasPrefix(r.URL.Path, "/api/control/v1/tasks/"),
			strings.HasPrefix(r.URL.Path, "/api/control/v1/approvals/"):
			requirement.Role = domain.WebRoleOperator
			requirement.Scope = domain.CLIScopeConsoleControl
		default:
			requirement.Role = domain.WebRoleOwner
			requirement.Scope = ""
		}
		return requirement
	}
	switch r.URL.Path {
	case "/api/observe/v1/agents", openapi.ObserveOverviewPath, openapi.ObserveNetworkProfilesPath:
		requirement.Scope = domain.CLIScopeConsoleRead
	case openapi.ObserveEventsStreamPath:
		if r.URL.Query().Get("mode") == consoleapi.ModeDiagnostic {
			requirement.Role = domain.WebRoleOwner
			requirement.Scope = domain.CLIScopeConsoleDiagnostic
		} else {
			requirement.Scope = domain.CLIScopeConsoleRead
		}
	}
	return requirement
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
	agents, err := h.state.ListAgents(r.Context(), 100)
	if err != nil {
		http.Error(w, "overview temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	workers, err := h.state.ListWorkers(r.Context(), 100)
	if err != nil {
		http.Error(w, "overview temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	tasks, err := h.state.ListTasks(r.Context(), "", 100)
	if err != nil {
		http.Error(w, "overview temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	approvals, err := h.state.ListPendingApprovals(r.Context(), 100)
	if err != nil {
		http.Error(w, "overview temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	latestSequence, err := h.state.LatestJournalSequence(r.Context())
	if err != nil {
		http.Error(w, "overview temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	projectedWorkers := make([]openapi.WorkerReadModel, 0, len(workers))
	for _, worker := range workers {
		projectedWorkers = append(projectedWorkers, workerReadModel(worker))
	}
	writeJSON(w, map[string]any{"agents": h.projectAgentReadiness(r.Context(), agents, workers, tasks), "workers": projectedWorkers, "tasks": tasks, "approvals": approvals, "latest_sequence": latestSequence, "server_time": time.Now().UTC()})
}

func workerReadModel(worker domain.WorkerInstance) openapi.WorkerReadModel {
	return openapi.WorkerReadModel{WorkerInstanceID: worker.ID, AgentID: worker.AgentID, Generation: worker.Generation,
		Capabilities: append([]string(nil), worker.Capabilities...), Status: worker.Status, LastHeartbeatAt: worker.LastHeartbeatAt,
		LeaseUntil: worker.LeaseUntil, StartedAt: worker.StartedAt, UpdatedAt: worker.UpdatedAt}
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
	projected, err := projectEvents(events)
	if err != nil {
		http.Error(w, "failed to project task events", http.StatusInternalServerError)
		return
	}
	readModel := openapi.TaskReadModel{Task: taskReadModel(*t), Messages: m, Events: projected, SnapshotSequence: snapshotSequence}
	if reviews, ok := h.state.(taskReviewReader); ok {
		readModel.Review, err = reviews.GetTaskReview(r.Context(), taskID)
		if err != nil {
			http.Error(w, "failed to load result review", http.StatusInternalServerError)
			return
		}
	}

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
		worker, workerErr := h.state.GetWorkerInstance(r.Context(), run.WorkerInstanceID)
		if workerErr != nil && !errors.Is(workerErr, domain.ErrNotFound) {
			http.Error(w, "failed to load run worker", http.StatusInternalServerError)
			return
		}
		readModel.RunAttempts = append(readModel.RunAttempts, runReadModel(run, worker))
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
	summary := strings.TrimSpace(strings.SplitN(safeoutput.RedactText(task.Content), "\n", 2)[0])
	if summary == "" {
		summary = task.ID
	}
	runes := []rune(summary)
	if len(runes) > 160 {
		summary = string(runes[:159]) + "…"
	}
	return openapi.TaskListItem{ID: task.ID, TargetAgentID: task.TargetAgentID, Intent: task.Intent, Status: task.Status, Summary: summary, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt}
}

func projectEvents(events []domain.JournalEvent) ([]openapi.JournalEventReadModel, error) {
	projected := make([]openapi.JournalEventReadModel, 0, len(events))
	for _, event := range events {
		model, err := projectEvent(event)
		if err != nil {
			return nil, err
		}
		projected = append(projected, model)
	}
	return projected, nil
}

func projectEvent(event domain.JournalEvent) (openapi.JournalEventReadModel, error) {
	model := openapi.JournalEventReadModel{Sequence: event.Sequence, ID: event.ID, AggregateType: event.AggregateType,
		AggregateID: event.AggregateID, EventType: event.EventType, CreatedAt: event.CreatedAt}
	output, err := projectRuntimeOutput(event)
	if err != nil {
		return openapi.JournalEventReadModel{}, err
	}
	model.Output = output
	// Runtime output is persisted under its RunAttempt transaction aggregate.
	// The Console transport exposes it as a runtime event so a frame never
	// combines mutually exclusive Run and safe-output projections.
	if strings.HasPrefix(event.EventType, "runtime.") {
		model.AggregateType = "runtime"
	}
	return model, nil
}

func projectRuntimeOutput(event domain.JournalEvent) (*openapi.SafeOutputReadModel, error) {
	if !strings.HasPrefix(event.EventType, "runtime.") || event.EventType == "runtime.approval.requested" {
		return nil, nil
	}
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(event.Payload, &envelope); err != nil {
		return nil, fmt.Errorf("decode runtime event projection: %w", err)
	}
	if len(envelope.Payload) == 0 {
		return nil, nil
	}
	projected, err := safeoutput.ProjectRuntimePayload(envelope.Payload)
	if err != nil {
		return nil, err
	}
	var output openapi.SafeOutputReadModel
	if err := json.Unmarshal(projected, &output); err != nil {
		return nil, fmt.Errorf("decode safe runtime event projection: %w", err)
	}
	if output.Stage == "" && output.Status == "" && output.Text == "" && output.Diagnostic == "" && !output.HasOutput && !output.HasError {
		return nil, nil
	}
	return &output, nil
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

func runReadModel(run domain.RunAttempt, worker *domain.WorkerInstance) openapi.RunAttemptReadModel {
	policy := resolvedNetwork(run)
	readModel := openapi.RunAttemptReadModel{
		ID: run.ID, TaskID: run.TaskID, AgentID: run.AgentID, Version: run.Version,
		Status: run.Status, WorkerInstanceID: run.WorkerInstanceID,
		ExecutionSpecVersion: run.ExecutionSpecVersion, AdapterID: run.AdapterID,
		BackendID: run.BackendID, Model: run.Model, ReasoningMode: run.ReasoningMode,
		ReasoningValue: run.ReasoningValue, NetworkMode: policy.Mode, NetworkProfileID: policy.ProfileID,
		NetworkProfileVersion: policy.ProfileVersion, NetworkPolicyVersion: policy.PolicyVersion,
		NetworkBindingRevision: policy.BindingRevision, StartedAt: run.StartedAt,
		FinishedAt: run.FinishedAt, CreatedAt: run.CreatedAt, UpdatedAt: run.UpdatedAt,
	}
	if worker != nil && worker.ID == run.WorkerInstanceID {
		generation := worker.Generation
		readModel.WorkerGeneration = &generation
	}
	var resolved domain.ResolvedExecutionSpec
	if json.Unmarshal([]byte(run.ResolvedExecutionJSON), &resolved) == nil {
		if !resolved.DeadlineAt.IsZero() {
			readModel.DeadlineAt = &resolved.DeadlineAt
		}
		if resolved.AgentInput != nil {
			readModel.InstructionsSHA256 = resolved.AgentInput.InstructionsSHA256
			readModel.InstructionsPath = resolved.AgentInput.InstructionsPath
		}
	}
	readModel.TurnResult, readModel.TurnResultState = turnResultReadModel(run.ResultJSON)
	return readModel
}

func turnResultReadModel(resultJSON string) (*openapi.TurnResultReadModel, string) {
	result, state := safeoutput.DecodeTurnResult(resultJSON)
	if result == nil {
		return nil, state
	}
	source := "not_recorded"
	if result.RuntimeSideEffectsKnown != nil {
		source = "runtime_reported"
	}
	return &openapi.TurnResultReadModel{
		FinalReply:    result.FinalReply,
		RuntimeStatus: result.Status, Body: result.Result, BodyTruncated: result.ResultTruncated,
		Error: result.Error, ErrorTruncated: result.ErrorTruncated,
		RuntimeSideEffectsKnown: result.RuntimeSideEffectsKnown, SideEffectsSource: source,
		BusinessVerificationSource: "not_recorded",
	}, state
}

func taskReadModel(task domain.Task) openapi.TaskReadModelTask {
	outcome := safeoutput.ProjectOutcome(task.IsTerminal(), task.Result, task.Error)
	return openapi.TaskReadModelTask{ID: task.ID, Version: task.Version, TargetAgentID: task.TargetAgentID,
		CompletionBasis: task.CompletionBasis,
		OrganizationID:  task.OrganizationID, DispatchMode: task.DispatchMode, Intent: task.Intent,
		Content: safeoutput.RedactText(task.Content), Status: task.Status,
		Result: outcome.Result, ResultTruncated: outcome.ResultTruncated,
		Error: outcome.Error, ErrorTruncated: outcome.ErrorTruncated, OutcomeState: outcome.State,
		CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt}
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
	worker, workerErr := h.state.GetWorkerInstance(r.Context(), v.WorkerInstanceID)
	if workerErr != nil && !errors.Is(workerErr, domain.ErrNotFound) {
		http.Error(w, "failed to load run worker", http.StatusInternalServerError)
		return
	}
	writeJSON(w, runReadModel(*v, worker))
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
		WorkerID     string                          `json:"worker_id"`
		Generation   int64                           `json:"generation"`
		AgentID      string                          `json:"agent_id"`
		WorkerStatus string                          `json:"worker_status,omitempty"`
		Backend      openruntime.BackendRegistration `json:"backend"`
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
			options = append(options, option{
				WorkerID:     worker.ID,
				Generation:   worker.Generation,
				AgentID:      worker.AgentID,
				WorkerStatus: string(worker.Status),
				Backend:      backend,
			})
		}
	}
	writeJSON(w, map[string]any{"backends": options})
}

func (h *Handler) networkProfiles(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.session(w, r, false); !ok {
		return
	}
	if h.network != nil {
		response, err := h.network.Observe(r.Context(), r.URL.Query().Get("agent_id"))
		if err != nil {
			http.Error(w, "failed to load network profiles", http.StatusInternalServerError)
			return
		}
		writeJSON(w, response)
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
	if h.network != nil {
		var req openapi.CreateNetworkProfileRequest
		if openapi.DecodeStrictJSON(r.Body, &req) != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if !requireIdempotencyHeader(w, r, req.Meta) {
			return
		}
		result, err := h.network.CreateDraft(r.Context(), s.ID, req)
		writeNetworkCommandResult(w, result, err)
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
	p, err := req.Validate(s.ID)
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
	if h.network != nil {
		var req openapi.PublishNetworkProfileRequest
		if openapi.DecodeStrictJSON(r.Body, &req) != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if !requireIdempotencyHeader(w, r, req.Meta) {
			return
		}
		result, err := h.network.Publish(r.Context(), s.ID, r.PathValue("profileID"), req)
		writeNetworkCommandResult(w, result, err)
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
	p, err := state.PublishProxyProfile(r.Context(), r.PathValue("profileID"), req.Meta.ExpectedVersion, s.ID, time.Now().UTC())
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	p.SecretRef = ""
	writeJSON(w, p)
}

func (h *Handler) bindNetworkProfile(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r, true)
	if !ok {
		return
	}
	if h.network != nil {
		var req openapi.BindNetworkProfileRequest
		if openapi.DecodeStrictJSON(r.Body, &req) != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if !requireIdempotencyHeader(w, r, req.Meta) {
			return
		}
		result, err := h.network.Bind(r.Context(), s.ID, req)
		writeNetworkCommandResult(w, result, err)
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

func (h *Handler) editNetworkProfile(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r, true)
	if !ok || h.network == nil {
		if ok {
			http.Error(w, "network workflow unavailable", http.StatusNotImplemented)
		}
		return
	}
	var req openapi.EditNetworkProfileRequest
	if openapi.DecodeStrictJSON(r.Body, &req) != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if !requireIdempotencyHeader(w, r, req.Meta) {
		return
	}
	result, err := h.network.EditDraft(r.Context(), s.ID, r.PathValue("profileID"), req)
	writeNetworkCommandResult(w, result, err)
}

func (h *Handler) replaceNetworkSecret(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r, true)
	if !ok {
		return
	}
	if !s.HasRole(domain.WebRoleOwner) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if h.network == nil {
		http.Error(w, "network workflow unavailable", http.StatusNotImplemented)
		return
	}
	var req openapi.ReplaceNetworkSecretRequest
	if openapi.DecodeStrictJSON(r.Body, &req) != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if !requireIdempotencyHeader(w, r, req.Meta) {
		return
	}
	result, err := h.network.ReplaceSecret(r.Context(), s.ID, r.PathValue("profileID"), req)
	writeNetworkCommandResult(w, result, err)
}

func (h *Handler) testNetworkProfile(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r, true)
	if !ok || h.network == nil {
		if ok {
			http.Error(w, "network workflow unavailable", http.StatusNotImplemented)
		}
		return
	}
	var req openapi.TestNetworkProfileRequest
	if openapi.DecodeStrictJSON(r.Body, &req) != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if !requireIdempotencyHeader(w, r, req.Meta) {
		return
	}
	result, err := h.network.StartTest(r.Context(), s.ID, r.PathValue("profileID"), req)
	writeNetworkCommandResult(w, result, err)
}

func (h *Handler) rollbackNetworkBinding(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r, true)
	if !ok || h.network == nil {
		if ok {
			http.Error(w, "network workflow unavailable", http.StatusNotImplemented)
		}
		return
	}
	var req openapi.RollbackNetworkBindingRequest
	if openapi.DecodeStrictJSON(r.Body, &req) != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if !requireIdempotencyHeader(w, r, req.Meta) {
		return
	}
	result, err := h.network.Rollback(r.Context(), s.ID, req)
	writeNetworkCommandResult(w, result, err)
}

func (h *Handler) testNetworkMode(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r, true)
	if !ok || h.network == nil {
		if ok {
			http.Error(w, "network workflow unavailable", http.StatusNotImplemented)
		}
		return
	}
	var req openapi.TestNetworkModeRequest
	if openapi.DecodeStrictJSON(r.Body, &req) != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if !requireIdempotencyHeader(w, r, req.Meta) {
		return
	}
	result, err := h.network.StartModeTest(r.Context(), s.ID, req)
	writeNetworkCommandResult(w, result, err)
}

func (h *Handler) publishNetworkMode(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r, true)
	if !ok || h.network == nil {
		if ok {
			http.Error(w, "network workflow unavailable", http.StatusNotImplemented)
		}
		return
	}
	var req openapi.PublishNetworkModeRequest
	if openapi.DecodeStrictJSON(r.Body, &req) != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if !requireIdempotencyHeader(w, r, req.Meta) {
		return
	}
	result, err := h.network.PublishMode(r.Context(), s.ID, req)
	writeNetworkCommandResult(w, result, err)
}

func (h *Handler) importNetworkProfile(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r, true)
	if !ok {
		return
	}
	if !s.HasRole(domain.WebRoleOwner) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if h.network == nil {
		http.Error(w, "network workflow unavailable", http.StatusNotImplemented)
		return
	}
	var req openapi.ImportNetworkProfileRequest
	if openapi.DecodeStrictJSON(r.Body, &req) != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if !requireIdempotencyHeader(w, r, req.Meta) {
		return
	}
	result, err := h.network.StartImport(r.Context(), s.ID, req)
	writeNetworkCommandResult(w, result, err)
}

func writeNetworkCommandResult(w http.ResponseWriter, result json.RawMessage, err error) {
	if err == nil {
		writeJSON(w, openapi.NetworkCommandResponse{Receipt: result})
		return
	}
	status := http.StatusInternalServerError
	var domainError *domain.DomainError
	switch {
	case errors.As(err, &domainError) && domainError.Code == "INVALID_INPUT":
		status = http.StatusBadRequest
	case errors.As(err, &domainError) && domainError.Code == "FORBIDDEN", errors.Is(err, domain.ErrUnauthorized), errors.Is(err, domain.ErrFencingRejected):
		status = http.StatusForbidden
	case errors.Is(err, domain.ErrNotFound):
		status = http.StatusNotFound
	case errors.As(err, &domainError) && domainError.Code == "CONFLICT", errors.Is(err, domain.ErrStaleVersion), errors.Is(err, domain.ErrIdempotencyConflict), errors.Is(err, domain.ErrInvalidState):
		status = http.StatusConflict
	case errors.Is(err, domain.ErrUnsupportedCapability):
		status = http.StatusUnprocessableEntity
	}
	http.Error(w, err.Error(), status)
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
	modes, modeProvided := r.URL.Query()["mode"]
	mode := consoleapi.ModeNormal
	if modeProvided && (len(modes) != 1 || (modes[0] != consoleapi.ModeNormal && modes[0] != consoleapi.ModeDiagnostic)) {
		http.Error(w, "invalid Console event mode", http.StatusBadRequest)
		return
	}
	if modeProvided {
		mode = modes[0]
	}
	principal, err := h.auth.Authorize(r, panelRequirement(r, false))
	if err != nil {
		h.auth.WriteFailure(w, err)
		return
	}
	now := h.now().UTC()
	if principal.ExpiresAt.IsZero() || !now.Before(principal.ExpiresAt.UTC()) {
		h.auth.WriteFailure(w, errors.New("session expired"))
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", 500)
		return
	}
	var after int64
	agentID := strings.TrimSpace(r.URL.Query().Get("agent_id"))
	if agentID != "" {
		if err := domain.ValidateIdentifier("agent_id", agentID); err != nil {
			http.Error(w, "invalid agent_id", http.StatusBadRequest)
			return
		}
	}
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
	bounds, err := h.state.JournalSequenceBounds(r.Context())
	if err != nil {
		http.Error(w, "failed to inspect Event Journal cursor", http.StatusInternalServerError)
		return
	}
	if after > 0 && bounds.Earliest > 0 && after < bounds.Earliest-1 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(openapi.ErrorResponse{
			Code:    openapi.ErrorEventCursorExpired,
			Message: "Event Journal cursor is no longer retained; re-attach for a consistent snapshot",
		})
		return
	}
	if !h.now().UTC().Before(principal.ExpiresAt.UTC()) {
		h.auth.WriteFailure(w, errors.New("session expired"))
		return
	}
	streamContext, cancelStream := context.WithCancel(r.Context())
	expired, stopExpiryTimer := h.expiryTimer(principal.ExpiresAt.UTC().Sub(h.now().UTC()))
	defer cancelStream()
	defer stopExpiryTimer()
	go func() {
		select {
		case <-expired:
			cancelStream()
		case <-streamContext.Done():
		}
	}()
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ticker := time.NewTicker(time.Second)
	keepalive := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	defer keepalive.Stop()
	for {
		if streamContext.Err() != nil || !h.now().UTC().Before(principal.ExpiresAt.UTC()) {
			return
		}
		events, e := h.state.ListJournal(streamContext, after, 100)
		if e != nil {
			return
		}
		for _, ev := range events {
			if streamContext.Err() != nil || !h.now().UTC().Before(principal.ExpiresAt.UTC()) {
				return
			}
			if !observableAggregate(ev.AggregateType) {
				after = ev.Sequence
				continue
			}
			if agentID != "" {
				matched, matchErr := h.eventMatchesAgent(streamContext, ev, agentID)
				if matchErr != nil {
					return
				}
				if !matched {
					after = ev.Sequence
					continue
				}
			}
			// The panel currently runs in a single authenticated organization scope.
			// Stream only the browser-safe projection; Journal payloads never cross
			// the SSE boundary and therefore cannot expose runtime diagnostics or
			// credentials through a reconnecting client.
			model, projectionErr := projectEvent(ev)
			if projectionErr != nil {
				return
			}
			if mode == consoleapi.ModeNormal {
				stripDiagnostic(&model)
			}
			if model.AggregateType == "worker_instance" {
				worker, projectionErr := h.workerEventSnapshot(streamContext, ev.AggregateID)
				if projectionErr != nil {
					return
				}
				model.Worker = worker
			} else if model.AggregateType == "run_attempt" {
				run, projectionErr := h.runEventSnapshot(streamContext, ev.AggregateID)
				if projectionErr != nil {
					return
				}
				model.Run = run
			} else if model.AggregateType == "task" {
				task, projectionErr := h.taskEventSnapshot(streamContext, ev.AggregateID)
				if projectionErr != nil {
					return
				}
				model.Task = task
			} else if model.AggregateType == "mailbox_item" {
				task, mailbox, projectionErr := h.mailboxEventSnapshot(streamContext, ev.AggregateID)
				if projectionErr != nil {
					return
				}
				model.Task = task
				model.Mailbox = mailbox
			} else if model.AggregateType == "message" {
				task, message, projectionErr := h.messageEventSnapshot(streamContext, ev.AggregateID)
				if projectionErr != nil {
					return
				}
				model.Task = task
				model.Message = message
			} else if model.AggregateType == "approval_request" {
				task, approval, projectionErr := h.approvalEventSnapshot(streamContext, ev.AggregateID)
				if projectionErr != nil {
					return
				}
				model.Task = task
				model.Approval = approval
			}
			b, encodeErr := json.Marshal(model)
			if encodeErr != nil {
				return
			}
			if !h.now().UTC().Before(principal.ExpiresAt.UTC()) {
				return
			}
			if _, writeErr := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", ev.Sequence, b); writeErr != nil {
				return
			}
			after = ev.Sequence
		}
		fl.Flush()
		select {
		case <-streamContext.Done():
			return
		case <-ticker.C:
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			fl.Flush()
		}
	}
}

func stripDiagnostic(event *openapi.JournalEventReadModel) {
	if event == nil || event.Output == nil {
		return
	}
	output := *event.Output
	output.Diagnostic = ""
	output.DiagnosticTruncated = false
	if output.Stage == "" && output.Status == "" && output.Text == "" && !output.HasOutput && !output.HasError {
		event.Output = nil
		return
	}
	event.Output = &output
}

func (h *Handler) eventMatchesAgent(ctx context.Context, event domain.JournalEvent, agentID string) (bool, error) {
	switch event.AggregateType {
	case "task":
		task, err := h.state.GetTask(ctx, event.AggregateID)
		if err != nil {
			return false, fmt.Errorf("read Task event ownership: %w", err)
		}
		if task == nil || task.ID != event.AggregateID || domain.ValidateOpaqueID("task_id", task.ID) != nil ||
			domain.ValidateIdentifier("agent_id", task.TargetAgentID) != nil {
			return false, fmt.Errorf("invalid Task event ownership projection")
		}
		return task.TargetAgentID == agentID, nil
	case "run_attempt", "runtime":
		run, err := h.state.GetRunAttempt(ctx, event.AggregateID)
		if err != nil {
			return false, fmt.Errorf("read Run event ownership: %w", err)
		}
		if run == nil || run.ID != event.AggregateID || domain.ValidateOpaqueID("run_id", run.ID) != nil ||
			domain.ValidateIdentifier("agent_id", run.AgentID) != nil {
			return false, fmt.Errorf("invalid Run event ownership projection")
		}
		return run.AgentID == agentID, nil
	case "worker_instance":
		worker, err := h.state.GetWorkerInstance(ctx, event.AggregateID)
		if err != nil {
			return false, fmt.Errorf("read Worker event ownership: %w", err)
		}
		if worker == nil || worker.ID != event.AggregateID || domain.ValidateOpaqueID("worker_instance_id", worker.ID) != nil ||
			domain.ValidateIdentifier("agent_id", worker.AgentID) != nil {
			return false, fmt.Errorf("invalid Worker event ownership projection")
		}
		return worker.AgentID == agentID, nil
	case "mailbox_item":
		item, err := h.state.GetMailboxItem(ctx, event.AggregateID)
		if err != nil {
			return false, fmt.Errorf("read Mailbox event ownership: %w", err)
		}
		if item == nil || item.ID != event.AggregateID || domain.ValidateOpaqueID("mailbox_item_id", item.ID) != nil ||
			domain.ValidateOpaqueID("task_id", item.TaskID) != nil ||
			domain.ValidateIdentifier("agent_id", item.TargetAgentID) != nil {
			return false, fmt.Errorf("invalid Mailbox event ownership projection")
		}
		task, err := h.state.GetTask(ctx, item.TaskID)
		if err != nil {
			return false, fmt.Errorf("read Mailbox Task ownership: %w", err)
		}
		if task == nil || task.ID != item.TaskID || task.TargetAgentID != item.TargetAgentID ||
			domain.ValidateIdentifier("agent_id", task.TargetAgentID) != nil {
			return false, fmt.Errorf("invalid Mailbox Task ownership projection")
		}
		return task.TargetAgentID == agentID, nil
	case "message":
		message, err := h.state.GetMessage(ctx, event.AggregateID)
		if err != nil {
			return false, fmt.Errorf("read Message event ownership: %w", err)
		}
		if message == nil || message.ID != event.AggregateID || domain.ValidateOpaqueID("message_id", message.ID) != nil ||
			domain.ValidateOpaqueID("task_id", message.TaskID) != nil {
			return false, fmt.Errorf("invalid Message event ownership projection")
		}
		task, err := h.state.GetTask(ctx, message.TaskID)
		if err != nil {
			return false, fmt.Errorf("read Message Task ownership: %w", err)
		}
		if task == nil || task.ID != message.TaskID || domain.ValidateIdentifier("agent_id", task.TargetAgentID) != nil {
			return false, fmt.Errorf("invalid Message Task ownership projection")
		}
		return task.TargetAgentID == agentID, nil
	case "approval_request":
		approval, err := h.state.GetApprovalRequest(ctx, event.AggregateID)
		if err != nil {
			return false, fmt.Errorf("read Approval event ownership: %w", err)
		}
		if approval == nil || approval.ID != event.AggregateID || domain.ValidateOpaqueID("approval_id", approval.ID) != nil ||
			domain.ValidateOpaqueID("task_id", approval.TaskID) != nil {
			return false, fmt.Errorf("invalid Approval event ownership projection")
		}
		task, err := h.state.GetTask(ctx, approval.TaskID)
		if err != nil {
			return false, fmt.Errorf("read Approval Task ownership: %w", err)
		}
		if task == nil || task.ID != approval.TaskID || domain.ValidateIdentifier("agent_id", task.TargetAgentID) != nil {
			return false, fmt.Errorf("invalid Approval Task ownership projection")
		}
		return task.TargetAgentID == agentID, nil
	default:
		return false, nil
	}
}

func (h *Handler) taskEventSnapshot(ctx context.Context, taskID string) (*openapi.ConsoleTaskReadModel, error) {
	task, err := h.state.GetTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("read Task projection: %w", err)
	}
	if task == nil || task.ID != taskID {
		return nil, fmt.Errorf("invalid Task projection")
	}
	model, err := consoleapi.ProjectConsoleTask(*task)
	if err != nil {
		return nil, fmt.Errorf("invalid Task projection: %w", err)
	}
	return &model, nil
}

func (h *Handler) mailboxEventSnapshot(ctx context.Context, itemID string) (*openapi.ConsoleTaskReadModel, *openapi.ConsoleMailboxReadModel, error) {
	item, err := h.state.GetMailboxItem(ctx, itemID)
	if err != nil {
		return nil, nil, fmt.Errorf("read Mailbox projection: %w", err)
	}
	if item == nil || item.ID != itemID {
		return nil, nil, fmt.Errorf("invalid Mailbox projection")
	}
	mailbox, err := consoleapi.ProjectConsoleMailbox(*item)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid Mailbox projection: %w", err)
	}
	task, err := h.taskEventSnapshot(ctx, item.TaskID)
	if err != nil {
		return nil, nil, err
	}
	if task.AgentID != item.TargetAgentID || task.TaskID != item.TaskID {
		return nil, nil, fmt.Errorf("invalid Mailbox Task projection ownership")
	}
	return task, &mailbox, nil
}

func (h *Handler) messageEventSnapshot(ctx context.Context, messageID string) (*openapi.ConsoleTaskReadModel, *openapi.ConsoleMessageReadModel, error) {
	message, err := h.state.GetMessage(ctx, messageID)
	if err != nil {
		return nil, nil, fmt.Errorf("read Message projection: %w", err)
	}
	if message == nil || message.ID != messageID {
		return nil, nil, fmt.Errorf("invalid Message projection")
	}
	projected, err := consoleapi.ProjectConsoleMessage(*message)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid Message projection: %w", err)
	}
	task, err := h.taskEventSnapshot(ctx, message.TaskID)
	if err != nil {
		return nil, nil, err
	}
	if task.AgentID != message.TargetAgentID || task.TaskID != message.TaskID {
		return nil, nil, fmt.Errorf("invalid Message Task projection ownership")
	}
	return task, &projected, nil
}

func (h *Handler) approvalEventSnapshot(ctx context.Context, approvalID string) (*openapi.ConsoleTaskReadModel, *openapi.ConsoleApprovalReadModel, error) {
	approval, err := h.state.GetApprovalRequest(ctx, approvalID)
	if err != nil {
		return nil, nil, fmt.Errorf("read Approval projection: %w", err)
	}
	if approval == nil || approval.ID != approvalID {
		return nil, nil, fmt.Errorf("invalid Approval projection")
	}
	projected, err := consoleapi.ProjectConsoleApproval(*approval)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid Approval projection: %w", err)
	}
	task, err := h.taskEventSnapshot(ctx, approval.TaskID)
	if err != nil {
		return nil, nil, err
	}
	if task.TaskID != approval.TaskID {
		return nil, nil, fmt.Errorf("invalid Approval Task projection ownership")
	}
	return task, &projected, nil
}

func (h *Handler) workerEventSnapshot(ctx context.Context, workerID string) (*openapi.WorkerReadModel, error) {
	worker, err := h.state.GetWorkerInstance(ctx, workerID)
	if err != nil {
		return nil, fmt.Errorf("read Worker projection: %w", err)
	}
	if worker == nil || worker.ID != workerID || domain.ValidateOpaqueID("worker_instance_id", worker.ID) != nil ||
		domain.ValidateIdentifier("agent_id", worker.AgentID) != nil ||
		worker.Generation <= 0 || !worker.Status.Valid() {
		return nil, fmt.Errorf("invalid Worker projection")
	}
	provider, ok := h.state.(backendOptionsState)
	if !ok {
		return nil, fmt.Errorf("Worker Backend projection is unavailable")
	}
	backends, err := provider.ListWorkerBackends(ctx, workerID)
	if err != nil {
		return nil, fmt.Errorf("read Worker Backend projection: %w", err)
	}
	backendHealth := make(map[string]openruntime.BackendHealth, len(backends))
	for _, backend := range backends {
		if err := domain.ValidateIdentifier("backend_id", backend.BackendID); err != nil {
			return nil, fmt.Errorf("invalid Worker Backend projection: %w", err)
		}
		if !backend.Health.Valid() {
			return nil, fmt.Errorf("invalid Worker Backend health")
		}
		if _, exists := backendHealth[backend.BackendID]; exists {
			return nil, fmt.Errorf("duplicate Worker Backend projection")
		}
		backendHealth[backend.BackendID] = backend.Health
	}
	model := workerReadModel(*worker)
	model.BackendHealth = backendHealth
	return &model, nil
}

func (h *Handler) runEventSnapshot(ctx context.Context, runID string) (*openapi.RunAttemptReadModel, error) {
	run, err := h.state.GetRunAttempt(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("read Run projection: %w", err)
	}
	if run == nil || run.ID != runID || domain.ValidateOpaqueID("run_id", run.ID) != nil ||
		domain.ValidateOpaqueID("task_id", run.TaskID) != nil ||
		domain.ValidateIdentifier("agent_id", run.AgentID) != nil ||
		domain.ValidateOpaqueID("worker_instance_id", run.WorkerInstanceID) != nil || !run.Status.Valid() {
		return nil, fmt.Errorf("invalid Run projection")
	}
	worker, err := h.state.GetWorkerInstance(ctx, run.WorkerInstanceID)
	if err != nil {
		return nil, fmt.Errorf("read Run Worker projection: %w", err)
	}
	if worker == nil || worker.ID != run.WorkerInstanceID ||
		domain.ValidateOpaqueID("worker_instance_id", worker.ID) != nil ||
		domain.ValidateIdentifier("agent_id", worker.AgentID) != nil || worker.AgentID != run.AgentID ||
		worker.Generation <= 0 || !worker.Status.Valid() {
		return nil, fmt.Errorf("invalid Run Worker projection")
	}
	model := runReadModel(*run, worker)
	return &model, nil
}

func observableAggregate(aggregateType string) bool {
	switch aggregateType {
	case "", "task", "run_attempt", "runtime", "worker_instance", "mailbox_item", "message", "approval_request":
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
	req.SenderPrincipalID = s.ID
	v, e := h.commands.CreateTask(r.Context(), s.ID, req)
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
	req.SenderPrincipalID = s.ID
	v, e := h.commands.CreateMessage(r.Context(), s.ID, r.PathValue("taskID"), req)
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
	req.RequestedBy = s.ID
	v, e := h.commands.CancelTask(r.Context(), s.ID, r.PathValue("taskID"), req)
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
	req.DecidedBy = s.ID
	result, err := h.commands.DecideApproval(r.Context(), s.ID, r.PathValue("approvalID"), req)
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
