package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	openapi "openagentx/internal/api"
	"openagentx/internal/auth/web"
	"openagentx/internal/controlplane"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

type testPanelState struct {
	agents            []domain.AgentIdentity
	workers           []domain.WorkerInstance
	tasks             []domain.Task
	journal           []domain.JournalEvent
	approvals         []domain.ApprovalRequest
	afterHistoryQuery func()
}

type backendPanelState struct {
	*testPanelState
	backends map[string][]openruntime.BackendRegistration
}

func (s *backendPanelState) ListWorkerBackends(_ context.Context, workerID string) ([]openruntime.BackendRegistration, error) {
	return s.backends[workerID], nil
}

func (s *testPanelState) ListAgents(context.Context, int) ([]domain.AgentIdentity, error) {
	return s.agents, nil
}

func (s *testPanelState) ListWorkers(context.Context, int) ([]domain.WorkerInstance, error) {
	return s.workers, nil
}

func (s *testPanelState) ListTasks(context.Context, string, int) ([]domain.Task, error) {
	return s.tasks, nil
}

func (s *testPanelState) QueryTasks(_ context.Context, agentID string, status domain.TaskStatus, _ time.Time, _ time.Time, text, cursorUpdatedAt, cursorTaskID string, limit int) ([]domain.Task, error) {
	result := make([]domain.Task, 0, limit)
	for _, task := range s.tasks {
		if agentID != "" && task.TargetAgentID != agentID || status != "" && task.Status != status {
			continue
		}
		if text != "" && !strings.Contains(strings.ToLower(task.ID+" "+task.TargetAgentID+" "+task.Content), strings.ToLower(text)) {
			continue
		}
		if cursorUpdatedAt != "" && !(task.UpdatedAt < cursorUpdatedAt || task.UpdatedAt == cursorUpdatedAt && task.ID < cursorTaskID) {
			continue
		}
		result = append(result, task)
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func (s *testPanelState) ListJournal(_ context.Context, after int64, limit int) ([]domain.JournalEvent, error) {
	result := make([]domain.JournalEvent, 0, limit)
	for _, event := range s.journal {
		if event.Sequence > after {
			result = append(result, event)
		}
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func (s *testPanelState) ListTaskJournal(_ context.Context, taskID string, after int64, limit int) ([]domain.JournalEvent, error) {
	result := make([]domain.JournalEvent, 0, limit)
	for _, event := range s.journal {
		if event.Sequence <= after || (event.AggregateType != "task" || event.AggregateID != taskID) {
			continue
		}
		result = append(result, event)
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func (s *testPanelState) ListTaskJournalBefore(_ context.Context, taskID string, before int64, limit int) ([]domain.JournalEvent, error) {
	result := make([]domain.JournalEvent, 0, limit)
	for index := len(s.journal) - 1; index >= 0; index-- {
		event := s.journal[index]
		if event.Sequence >= before || event.AggregateType != "task" || event.AggregateID != taskID {
			continue
		}
		result = append(result, event)
		if len(result) == limit {
			break
		}
	}
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	if s.afterHistoryQuery != nil {
		hook := s.afterHistoryQuery
		s.afterHistoryQuery = nil
		hook()
	}
	return result, nil
}

func (s *testPanelState) ListTaskJournalRange(_ context.Context, taskID string, after, through int64, limit int) ([]domain.JournalEvent, error) {
	result := make([]domain.JournalEvent, 0, limit)
	for _, event := range s.journal {
		if event.Sequence <= after || event.Sequence > through || event.AggregateType != "task" || event.AggregateID != taskID {
			continue
		}
		result = append(result, event)
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func (s *testPanelState) GetTask(_ context.Context, taskID string) (*domain.Task, error) {
	for index := range s.tasks {
		if s.tasks[index].ID == taskID {
			task := s.tasks[index]
			return &task, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (s *testPanelState) ListMessages(context.Context, string) ([]domain.Message, error) {
	return nil, nil
}

func (s *testPanelState) ListMailbox(context.Context, string, int64, int) ([]domain.MailboxItem, error) {
	return nil, nil
}

func (s *testPanelState) GetRunAttempt(context.Context, string) (*domain.RunAttempt, error) {
	return nil, domain.ErrNotFound
}

func (s *testPanelState) ListRunAttemptsForTask(context.Context, string, int) ([]domain.RunAttempt, error) {
	return []domain.RunAttempt{}, nil
}

func (s *testPanelState) ListPendingApprovals(context.Context, int) ([]domain.ApprovalRequest, error) {
	return s.approvals, nil
}

func (s *testPanelState) LatestJournalSequence(context.Context) (int64, error) {
	if len(s.journal) == 0 {
		return 0, nil
	}
	return s.journal[len(s.journal)-1].Sequence, nil
}

type rejectingCommandState struct{}

func (rejectingCommandState) CreateTask(context.Context, *domain.Task, *domain.Message, *domain.MailboxItem, *domain.JournalEvent) (*domain.CreateTaskResult, error) {
	return nil, errors.New("unexpected CreateTask call")
}

func (rejectingCommandState) GetTask(context.Context, string) (*domain.Task, error) {
	return nil, errors.New("unexpected GetTask call")
}

func (rejectingCommandState) CreateMessage(context.Context, int64, *domain.Message, *domain.MailboxItem, *domain.JournalEvent) (*domain.CreateMessageResult, error) {
	return nil, errors.New("unexpected CreateMessage call")
}

func (rejectingCommandState) RequestTaskCancel(context.Context, string, int64, string, *domain.MailboxItem, *domain.JournalEvent, *domain.JournalEvent) (*domain.Task, *domain.MailboxItem, error) {
	return nil, nil, errors.New("unexpected RequestTaskCancel call")
}

func (rejectingCommandState) GetApprovalRequest(context.Context, string) (*domain.ApprovalRequest, error) {
	return nil, errors.New("unexpected GetApprovalRequest call")
}

func (rejectingCommandState) DecideApproval(context.Context, string, *domain.ApprovalDecision, *domain.MailboxItem, *domain.JournalEvent, *domain.JournalEvent) (*domain.ApprovalDecision, *domain.MailboxItem, error) {
	return nil, nil, errors.New("unexpected DecideApproval call")
}

type authenticatedPanel struct {
	handler *Handler
	session *web.Session
	cookie  *http.Cookie
}

func newAuthenticatedPanel(t *testing.T, role web.Role, state State) authenticatedPanel {
	t.Helper()
	digest, err := web.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	manager := web.NewManager(web.Config{})
	if err := manager.AddUser(web.User{
		ID: "human-owner", WebUserID: "web-owner", Username: "owner", Roles: []web.Role{role}, PasswordDigest: digest,
	}); err != nil {
		t.Fatal(err)
	}
	session, err := manager.Login("owner", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	cookieResponse := httptest.NewRecorder()
	web.SetSessionCookie(cookieResponse, session)
	commands, err := controlplane.NewCommandService(rejectingCommandState{}, nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(state, commands, manager)
	if err != nil {
		t.Fatal(err)
	}
	return authenticatedPanel{handler: handler, session: session, cookie: cookieResponse.Result().Cookies()[0]}
}

func TestOverviewRequiresSessionAndExposesLatestSequence(t *testing.T) {
	state := &testPanelState{journal: []domain.JournalEvent{{Sequence: 7}}}
	panel := newAuthenticatedPanel(t, web.RoleOwner, state)

	unauthenticated := httptest.NewRecorder()
	panel.handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/observe/v1/overview", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d", unauthenticated.Code)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/observe/v1/overview", nil)
	request.AddCookie(panel.cookie)
	response := httptest.NewRecorder()
	panel.handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("overview status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		LatestSequence int64 `json:"latest_sequence"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.LatestSequence != 7 {
		t.Fatalf("overview body=%s err=%v", response.Body.String(), err)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("overview cache policy=%q", response.Header().Get("Cache-Control"))
	}
}

func TestTasksUsesBoundStableCursorAndSafeSummaryProjection(t *testing.T) {
	state := &testPanelState{tasks: []domain.Task{
		{ID: "task-c", TargetAgentID: "quote", Status: domain.TaskStatusRunning, Content: "third\nprivate body", UpdatedAt: "2026-09-05T12:00:00Z"},
		{ID: "task-b", TargetAgentID: "quote", Status: domain.TaskStatusRunning, Content: "second", UpdatedAt: "2026-09-05T12:00:00Z"},
		{ID: "task-a", TargetAgentID: "quote", Status: domain.TaskStatusRunning, Content: "first", UpdatedAt: "2026-09-05T11:00:00Z"},
	}}
	panel := newAuthenticatedPanel(t, web.RoleOwner, state)
	request := httptest.NewRequest(http.MethodGet, "/api/observe/v1/tasks?agent_id=quote&status=running&query=t&limit=2", nil)
	request.AddCookie(panel.cookie)
	response := httptest.NewRecorder()
	panel.handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("task list status=%d body=%s", response.Code, response.Body.String())
	}
	var first openapi.TaskListPage
	if err := json.Unmarshal(response.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Tasks) != 2 || first.Tasks[0].ID != "task-c" || first.Tasks[0].Summary != "third" || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("first task page=%+v", first)
	}
	if strings.Contains(response.Body.String(), "private body") || strings.Contains(response.Body.String(), "content") {
		t.Fatalf("task list leaked full content: %s", response.Body.String())
	}

	badCursorRequest := httptest.NewRequest(http.MethodGet, "/api/observe/v1/tasks?agent_id=other&status=running&query=t&limit=2&cursor="+first.NextCursor, nil)
	badCursorRequest.AddCookie(panel.cookie)
	badCursorResponse := httptest.NewRecorder()
	panel.handler.ServeHTTP(badCursorResponse, badCursorRequest)
	if badCursorResponse.Code != http.StatusBadRequest {
		t.Fatalf("cursor reused across filters status=%d body=%s", badCursorResponse.Code, badCursorResponse.Body.String())
	}
}

func TestExecutionOptionsRemovesWorkerConfigPath(t *testing.T) {
	state := &backendPanelState{
		testPanelState: &testPanelState{workers: []domain.WorkerInstance{{ID: "worker-1", AgentID: "quote"}}},
		backends: map[string][]openruntime.BackendRegistration{
			"worker-1": {{
				BackendID:  "local",
				Descriptor: openruntime.AdapterDescriptor{AdapterID: "agy", NetworkModes: []string{"named_profile"}},
				Health:     openruntime.BackendHealthy,
				Network:    domain.NetworkPolicy{Mode: domain.NetworkNamedProfile, ProfileID: "proxy-main", ProfileVersion: 3, ConfigFile: "/run/private/proxy.conf"},
			}},
		},
	}
	panel := newAuthenticatedPanel(t, web.RoleOwner, state)
	request := httptest.NewRequest(http.MethodGet, openapi.ObserveExecutionOptionsPath, nil)
	request.AddCookie(panel.cookie)
	response := httptest.NewRecorder()
	panel.handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("execution options status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "/run/private") || strings.Contains(response.Body.String(), "config_file") {
		t.Fatalf("execution options leaked worker config path: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "proxy-main") || !strings.Contains(response.Body.String(), "named_profile") {
		t.Fatalf("execution options lost safe capability data: %s", response.Body.String())
	}
}

func TestTaskDetailSeparatesHistoryAndLiveCursors(t *testing.T) {
	task := domain.Task{ID: "task-observe", TargetAgentID: "quote", Status: domain.TaskStatusRunning, Content: "observe", UpdatedAt: "2026-09-05T12:00:00Z"}
	state := &testPanelState{tasks: []domain.Task{task}}
	for sequence := int64(1); sequence <= 5; sequence++ {
		state.journal = append(state.journal, domain.JournalEvent{Sequence: sequence, ID: fmt.Sprintf("event-%d", sequence), AggregateType: "task", AggregateID: task.ID, EventType: "task.updated"})
	}
	panel := newAuthenticatedPanel(t, web.RoleOwner, state)
	request := httptest.NewRequest(http.MethodGet, "/api/observe/v1/tasks/task-observe?limit=2", nil)
	request.AddCookie(panel.cookie)
	response := httptest.NewRecorder()
	panel.handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("task detail status=%d body=%s", response.Code, response.Body.String())
	}
	var detail openapi.TaskReadModel
	if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Events) != 2 || detail.Events[0].Sequence != 4 || detail.Events[1].Sequence != 5 || !detail.HasOlderEvents || detail.HistoryBeforeSequence != 4 || detail.LiveAfterSequence != 5 {
		t.Fatalf("initial detail cursors=%+v", detail)
	}

	olderRequest := httptest.NewRequest(http.MethodGet, "/api/observe/v1/tasks/task-observe?before_sequence=4&limit=2", nil)
	olderRequest.AddCookie(panel.cookie)
	olderResponse := httptest.NewRecorder()
	panel.handler.ServeHTTP(olderResponse, olderRequest)
	if err := json.Unmarshal(olderResponse.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Events) != 2 || detail.Events[0].Sequence != 2 || detail.Events[1].Sequence != 3 || !detail.HasOlderEvents || detail.HistoryBeforeSequence != 2 {
		t.Fatalf("older detail cursors=%+v", detail)
	}
}

func TestTaskDetailWatermarkDoesNotSkipConcurrentTerminalEvent(t *testing.T) {
	task := domain.Task{ID: "task-race", TargetAgentID: "quote", Status: domain.TaskStatusRunning, Content: "race", Version: 1, UpdatedAt: "2026-09-05T12:00:00Z"}
	state := &testPanelState{tasks: []domain.Task{task}, journal: []domain.JournalEvent{{Sequence: 5, ID: "running", AggregateType: "task", AggregateID: task.ID, EventType: "task.running"}}}
	state.afterHistoryQuery = func() {
		state.tasks[0].Status = domain.TaskStatusSucceeded
		state.tasks[0].Version = 2
		state.journal = append(state.journal, domain.JournalEvent{Sequence: 6, ID: "succeeded", AggregateType: "task", AggregateID: task.ID, EventType: "task.succeeded"})
	}
	panel := newAuthenticatedPanel(t, web.RoleOwner, state)

	initialRequest := httptest.NewRequest(http.MethodGet, "/api/observe/v1/tasks/task-race?limit=2", nil)
	initialRequest.AddCookie(panel.cookie)
	initialResponse := httptest.NewRecorder()
	panel.handler.ServeHTTP(initialResponse, initialRequest)
	var initial openapi.TaskReadModel
	if err := json.Unmarshal(initialResponse.Body.Bytes(), &initial); err != nil {
		t.Fatal(err)
	}
	if initial.Task.Status != domain.TaskStatusSucceeded || initial.LiveAfterSequence != 5 {
		t.Fatalf("initial concurrent snapshot=%+v", initial)
	}

	liveRequest := httptest.NewRequest(http.MethodGet, "/api/observe/v1/tasks/task-race?after_sequence=5&limit=2", nil)
	liveRequest.AddCookie(panel.cookie)
	liveResponse := httptest.NewRecorder()
	panel.handler.ServeHTTP(liveResponse, liveRequest)
	var live openapi.TaskReadModel
	if err := json.Unmarshal(liveResponse.Body.Bytes(), &live); err != nil {
		t.Fatal(err)
	}
	if len(live.Events) != 1 || live.Events[0].Sequence != 6 || live.LiveAfterSequence != 6 || live.HasMoreLiveEvents {
		t.Fatalf("live catch-up=%+v", live)
	}
}

func TestTaskDetailRejectsLiveCursorAheadOfSnapshot(t *testing.T) {
	state := &testPanelState{tasks: []domain.Task{{ID: "task-reset", TargetAgentID: "quote", Status: domain.TaskStatusRunning}}}
	panel := newAuthenticatedPanel(t, web.RoleOwner, state)
	request := httptest.NewRequest(http.MethodGet, "/api/observe/v1/tasks/task-reset?after_sequence=9", nil)
	request.AddCookie(panel.cookie)
	response := httptest.NewRecorder()
	panel.handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("ahead cursor status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestHealthIsPublicMinimalJSONProbe(t *testing.T) {
	panel := newAuthenticatedPanel(t, web.RoleOwner, &testPanelState{})

	response := httptest.NewRecorder()
	panel.handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, openapi.ObserveHealthPath, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("health status=%d body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("health content type=%q", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("health cache policy=%q", got)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("health content type options=%q", got)
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("health body=%s err=%v", response.Body.String(), err)
	}
	if len(body) != 1 || body["status"] != "ok" {
		t.Fatalf("health body=%s", response.Body.String())
	}
	for _, sensitive := range []string{"database", "worker", "version", "path", "credential", "internal", "error"} {
		if strings.Contains(strings.ToLower(response.Body.String()), sensitive) {
			t.Fatalf("health body contains sensitive term %q: %s", sensitive, response.Body.String())
		}
	}

	for _, method := range []string{http.MethodPost, http.MethodPut} {
		t.Run(method, func(t *testing.T) {
			request := httptest.NewRequest(method, openapi.ObserveHealthPath, strings.NewReader(`{"status":"bad"}`))
			methodResponse := httptest.NewRecorder()
			panel.handler.ServeHTTP(methodResponse, request)
			if methodResponse.Code != http.StatusMethodNotAllowed {
				t.Fatalf("health %s status=%d body=%s", method, methodResponse.Code, methodResponse.Body.String())
			}
		})
	}

	unauthenticatedOverview := httptest.NewRecorder()
	panel.handler.ServeHTTP(unauthenticatedOverview, httptest.NewRequest(http.MethodGet, openapi.ObserveOverviewPath, nil))
	if unauthenticatedOverview.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated overview status=%d body=%s", unauthenticatedOverview.Code, unauthenticatedOverview.Body.String())
	}
}

func TestControlRejectsViewerCSRFAndMismatchedIdempotencyHeader(t *testing.T) {
	body := []byte(`{"meta":{"idempotency_key":"body-key"},"target_agent_id":"quote-service","organization_id":"default","dispatch_mode":"direct","content":"test"}`)

	viewer := newAuthenticatedPanel(t, web.RoleViewer, &testPanelState{})
	viewerRequest := httptest.NewRequest(http.MethodPost, "/api/control/v1/tasks", bytes.NewReader(body))
	viewerRequest.AddCookie(viewer.cookie)
	viewerRequest.Header.Set("X-CSRF-Token", viewer.session.CSRFToken)
	viewerRequest.Header.Set("Idempotency-Key", "body-key")
	viewerResponse := httptest.NewRecorder()
	viewer.handler.ServeHTTP(viewerResponse, viewerRequest)
	if viewerResponse.Code != http.StatusForbidden {
		t.Fatalf("viewer status=%d body=%s", viewerResponse.Code, viewerResponse.Body.String())
	}

	owner := newAuthenticatedPanel(t, web.RoleOwner, &testPanelState{})
	for name, testCase := range map[string]struct {
		csrf, idempotency string
		expected          int
	}{
		"missing csrf":      {idempotency: "body-key", expected: http.StatusForbidden},
		"incorrect csrf":    {csrf: "incorrect", idempotency: "body-key", expected: http.StatusForbidden},
		"missing header":    {csrf: owner.session.CSRFToken, expected: http.StatusBadRequest},
		"mismatched header": {csrf: owner.session.CSRFToken, idempotency: "other-key", expected: http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/control/v1/tasks", bytes.NewReader(body))
			request.AddCookie(owner.cookie)
			request.Header.Set("X-CSRF-Token", testCase.csrf)
			request.Header.Set("Idempotency-Key", testCase.idempotency)
			response := httptest.NewRecorder()
			owner.handler.ServeHTTP(response, request)
			if response.Code != testCase.expected {
				t.Fatalf("status=%d want=%d body=%s", response.Code, testCase.expected, response.Body.String())
			}
		})
	}
}

type cancelingRecorder struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
}

func (r *cancelingRecorder) Flush() {
	r.cancel()
}

func TestSSEReplaysAfterHighestClientSequence(t *testing.T) {
	state := &testPanelState{journal: []domain.JournalEvent{
		{Sequence: 1, ID: "event-1", EventType: "one"},
		{Sequence: 2, ID: "event-2", EventType: "two"},
		{Sequence: 3, ID: "event-3", EventType: "three"},
		{Sequence: 4, ID: "event-4", EventType: "four"},
	}}
	panel := newAuthenticatedPanel(t, web.RoleOwner, state)
	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodGet, "/api/observe/v1/events/stream?after_sequence=2", nil).WithContext(ctx)
	request.Header.Set("Last-Event-ID", "3")
	request.AddCookie(panel.cookie)
	response := &cancelingRecorder{ResponseRecorder: httptest.NewRecorder(), cancel: cancel}

	panel.handler.ServeHTTP(response, request)

	if !strings.Contains(response.Body.String(), "id: 4\n") || strings.Contains(response.Body.String(), "id: 3\n") {
		t.Fatalf("unexpected SSE replay: %q", response.Body.String())
	}
	if response.Header().Get("X-Accel-Buffering") != "no" {
		t.Fatalf("SSE buffering header=%q", response.Header().Get("X-Accel-Buffering"))
	}
}
