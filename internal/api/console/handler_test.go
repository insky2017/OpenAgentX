package console

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"openagentx/internal/auth/web"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

type testState struct {
	agents   []domain.AgentIdentity
	workers  []domain.WorkerInstance
	run      *domain.RunAttempt
	backends map[string][]openruntime.BackendRegistration
}

func (s testState) ListAgents(context.Context, int) ([]domain.AgentIdentity, error) {
	return s.agents, nil
}
func (s testState) ListWorkers(context.Context, int) ([]domain.WorkerInstance, error) {
	return s.workers, nil
}
func (s testState) GetActiveRunForAgent(context.Context, string) (*domain.RunAttempt, error) {
	if s.run == nil {
		return nil, domain.ErrNotFound
	}
	return s.run, nil
}
func (s testState) ListWorkerBackends(_ context.Context, workerID string) ([]openruntime.BackendRegistration, error) {
	return s.backends[workerID], nil
}

type consoleFixture struct {
	handler *Handler
	cookie  *http.Cookie
}

func newConsoleFixture(t *testing.T, workers []domain.WorkerInstance) consoleFixture {
	return newConsoleFixtureWithState(t, testState{agents: []domain.AgentIdentity{{ID: "quote"}}, workers: workers})
}

func newConsoleFixtureWithState(t *testing.T, state testState) consoleFixture {
	t.Helper()
	digest, err := web.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	manager := web.NewManager(web.Config{})
	if err := manager.AddUser(web.User{ID: "human-owner", Username: "owner", Roles: []web.Role{web.RoleOwner}, PasswordDigest: digest}); err != nil {
		t.Fatal(err)
	}
	session, err := manager.Login("owner", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	web.SetSessionCookie(recorder, session)
	handler, err := NewHandler(state, manager)
	if err != nil {
		t.Fatal(err)
	}
	return consoleFixture{handler: handler, cookie: recorder.Result().Cookies()[0]}
}

func TestAttachIncludesCurrentRunAndBackendHealth(t *testing.T) {
	now := time.Now().UTC()
	fixture := newConsoleFixtureWithState(t, testState{
		agents:  []domain.AgentIdentity{{ID: "quote"}},
		workers: []domain.WorkerInstance{{ID: "worker-current", AgentID: "quote", Generation: 4, Status: domain.WorkerStatusOnline}},
		run:     &domain.RunAttempt{ID: "run-current", TaskID: "task-current", AgentID: "quote", Status: domain.RunAttemptRunning, StartedAt: now, UpdatedAt: now},
		backends: map[string][]openruntime.BackendRegistration{
			"worker-current": {{BackendID: "agy", Health: openruntime.BackendHealthy}},
		},
	})
	response := fixture.request(AttachPath + "?agent_id=quote")
	var attached AttachResponse
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &attached) != nil {
		t.Fatalf("attach status=%d body=%s", response.Code, response.Body.String())
	}
	if attached.ActiveRun == nil || attached.ActiveRun.RunID != "run-current" || attached.BackendHealth["agy"] != openruntime.BackendHealthy {
		t.Fatalf("attach omitted active state: %+v", attached)
	}
}

func (f consoleFixture) request(path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.AddCookie(f.cookie)
	f.handler.ServeHTTP(response, request)
	return response
}

func TestAttachFollowsLatestGenerationAndReconnect(t *testing.T) {
	now := time.Now().UTC()
	fixture := newConsoleFixture(t, []domain.WorkerInstance{
		{ID: "worker-old", AgentID: "quote", Generation: 1, Status: domain.WorkerStatusOffline},
		{ID: "worker-new", AgentID: "quote", Generation: 3, Status: domain.WorkerStatusOnline, Capabilities: []string{"coding"}, UpdatedAt: now},
	})
	for attempt := 0; attempt < 2; attempt++ {
		response := fixture.request(AttachPath + "?agent_id=quote")
		if response.Code != http.StatusOK {
			t.Fatalf("attach status=%d body=%s", response.Code, response.Body.String())
		}
		var attached AttachResponse
		if err := json.Unmarshal(response.Body.Bytes(), &attached); err != nil {
			t.Fatal(err)
		}
		if attached.Generation != 3 || attached.WorkerInstanceID != "worker-new" || attached.Mode != ModeNormal {
			t.Fatalf("attach did not follow current generation: %+v", attached)
		}
	}
}

func TestAttachReportsOfflineWithoutInventingWorkerIdentity(t *testing.T) {
	fixture := newConsoleFixture(t, nil)
	response := fixture.request(AttachPath + "?agent_id=quote")
	var attached AttachResponse
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &attached) != nil {
		t.Fatalf("offline attach status=%d body=%s", response.Code, response.Body.String())
	}
	if attached.WorkerStatus != domain.WorkerStatusOffline || attached.Generation != 0 || attached.WorkerInstanceID != "" {
		t.Fatalf("unexpected offline projection: %+v", attached)
	}
}

func TestDiagnosticAttachIsAuthorizedRateLimitedAndDoesNotExposeFencing(t *testing.T) {
	fixture := newConsoleFixture(t, []domain.WorkerInstance{{ID: "worker-current", AgentID: "quote", Generation: 2,
		Status: domain.WorkerStatusDraining, FencingToken: 987654321, AuthenticatedPrincipal: "agent-secret-principal"}})
	for count := 0; count < diagnosticBurst; count++ {
		response := fixture.request(AttachPath + "?agent_id=quote&mode=diagnostic")
		if response.Code != http.StatusOK {
			t.Fatalf("diagnostic request %d status=%d", count, response.Code)
		}
		body := response.Body.String()
		if strings.Contains(body, "987654321") || strings.Contains(body, "agent-secret-principal") || strings.Contains(body, "fencing") {
			t.Fatalf("diagnostic projection leaked security material: %s", body)
		}
	}
	if response := fixture.request(AttachPath + "?agent_id=quote&mode=diagnostic"); response.Code != http.StatusTooManyRequests {
		t.Fatalf("diagnostic rate limit status=%d", response.Code)
	}

	unauthorized := httptest.NewRecorder()
	fixture.handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, AttachPath+"?agent_id=quote&mode=diagnostic", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized diagnostic status=%d", unauthorized.Code)
	}
}
