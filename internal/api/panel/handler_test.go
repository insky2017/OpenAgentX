package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	openapi "openagentx/internal/api"
	"openagentx/internal/auth/web"
	"openagentx/internal/controlplane"
	"openagentx/internal/domain"
)

type testPanelState struct {
	agents    []domain.AgentIdentity
	workers   []domain.WorkerInstance
	tasks     []domain.Task
	journal   []domain.JournalEvent
	approvals []domain.ApprovalRequest
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

func (s *testPanelState) GetTask(context.Context, string) (*domain.Task, error) {
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

func newAuthenticatedPanel(t *testing.T, role web.Role, state *testPanelState) authenticatedPanel {
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
