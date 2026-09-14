package console

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"openagentx/internal/auth/web"
	"openagentx/internal/domain"
)

type testState struct {
	snapshot domain.ConsoleSnapshot
	agents   []domain.ConsoleAgentOption
	err      error
}

func (s testState) ConsoleSnapshot(context.Context, string) (domain.ConsoleSnapshot, error) {
	return s.snapshot, s.err
}

func (s testState) ListConsoleAgentOptions(_ context.Context, after string, limit int) ([]domain.ConsoleAgentOption, error) {
	options := make([]domain.ConsoleAgentOption, 0, limit)
	for _, option := range s.agents {
		if option.AgentID > after {
			options = append(options, option)
			if len(options) == limit {
				break
			}
		}
	}
	return options, s.err
}

type consoleFixture struct {
	handler *Handler
	cookie  *http.Cookie
}

func newConsoleFixture(t *testing.T, workers []domain.WorkerInstance) consoleFixture {
	snapshot := domain.ConsoleSnapshot{Agent: domain.AgentIdentity{ID: "quote", Status: domain.AgentIdentityActive}}
	if len(workers) > 0 {
		current := workers[0]
		for _, worker := range workers[1:] {
			if worker.Generation > current.Generation || worker.Generation == current.Generation && worker.ID > current.ID {
				current = worker
			}
		}
		snapshot.Worker = &current
	}
	return newConsoleFixtureWithState(t, testState{snapshot: snapshot})
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
		snapshot: domain.ConsoleSnapshot{Agent: domain.AgentIdentity{ID: "quote", Status: domain.AgentIdentityActive},
			Worker:                    &domain.WorkerInstance{ID: "worker-current", AgentID: "quote", Generation: 4, Status: domain.WorkerStatusOnline},
			ActiveRun:                 &domain.RunAttempt{ID: "run-current", TaskID: "task-current", AgentID: "quote", Status: domain.RunAttemptRunning, WorkerInstanceID: "worker-current", StartedAt: now, UpdatedAt: now},
			ActiveRunWorkerGeneration: 4, BackendHealth: map[string]string{"agy": "healthy"}, SnapshotSequence: 17},
	})
	response := fixture.request(AttachPath + "?agent_id=quote")
	var attached AttachResponse
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &attached) != nil {
		t.Fatalf("attach status=%d body=%s", response.Code, response.Body.String())
	}
	if attached.ActiveRun == nil || attached.ActiveRun.RunID != "run-current" ||
		attached.ActiveRun.WorkerInstanceID != "worker-current" || attached.ActiveRun.WorkerGeneration == nil ||
		*attached.ActiveRun.WorkerGeneration != 4 || attached.BackendHealth["agy"] != "healthy" || attached.SnapshotSequence != 17 {
		t.Fatalf("attach omitted active state: %+v", attached)
	}
}

func TestAttachFailsClosedOnInvalidWorkerOrRunIdentity(t *testing.T) {
	tests := map[string]domain.ConsoleSnapshot{
		"invalid Worker status": {Agent: domain.AgentIdentity{ID: "quote", Status: domain.AgentIdentityActive},
			Worker: &domain.WorkerInstance{ID: "worker-current", AgentID: "quote", Generation: 4, Status: "unknown"}},
		"Run bound to old Worker": {Agent: domain.AgentIdentity{ID: "quote", Status: domain.AgentIdentityActive},
			Worker:                    &domain.WorkerInstance{ID: "worker-current", AgentID: "quote", Generation: 4, Status: domain.WorkerStatusOnline},
			ActiveRun:                 &domain.RunAttempt{ID: "run-old", TaskID: "task-old", AgentID: "quote", Status: domain.RunAttemptRunning, WorkerInstanceID: "worker-old"},
			ActiveRunWorkerGeneration: 3},
	}
	for name, snapshot := range tests {
		t.Run(name, func(t *testing.T) {
			fixture := newConsoleFixtureWithState(t, testState{snapshot: snapshot})
			response := fixture.request(AttachPath + "?agent_id=quote")
			if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "worker-old") {
				t.Fatalf("invalid snapshot status=%d body=%q", response.Code, response.Body.String())
			}
		})
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

func TestCLIAttachScopeRequirements(t *testing.T) {
	normal := attachRequirement(ModeNormal)
	if normal.Role != domain.WebRoleViewer || normal.Scope != domain.CLIScopeConsoleRead {
		t.Fatalf("normal requirement=%+v", normal)
	}
	diagnostic := attachRequirement(ModeDiagnostic)
	if diagnostic.Role != domain.WebRoleOwner || diagnostic.Scope != domain.CLIScopeConsoleDiagnostic {
		t.Fatalf("diagnostic requirement=%+v", diagnostic)
	}
}

func TestAgentOptionsArePaginatedWithoutSensitiveWorkerFields(t *testing.T) {
	agents := make([]domain.ConsoleAgentOption, 101)
	for index := range agents {
		agents[index] = domain.ConsoleAgentOption{AgentID: fmt.Sprintf("agent-%03d", index), OrganizationID: "org-main",
			DisplayName: fmt.Sprintf("Agent %03d", index), WorkerStatus: domain.WorkerStatusOffline}
	}
	fixture := newConsoleFixtureWithState(t, testState{agents: agents})
	first := fixture.request(AgentsPath)
	var page AgentOptionsPage
	if first.Code != http.StatusOK || json.Unmarshal(first.Body.Bytes(), &page) != nil {
		t.Fatalf("first page status=%d body=%s", first.Code, first.Body.String())
	}
	if len(page.Agents) != 100 || !page.HasMore || page.NextCursor != "agent-099" {
		t.Fatalf("first page=%+v", page)
	}
	body := first.Body.String()
	for _, forbidden := range []string{"principal", "fencing", "transport", "runtime_payload", "descriptor", "network_policy"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("Agent option projection exposed %q: %s", forbidden, body)
		}
	}
	second := fixture.request(AgentsPath + "?after_agent_id=" + page.NextCursor)
	if second.Code != http.StatusOK || json.Unmarshal(second.Body.Bytes(), &page) != nil || len(page.Agents) != 1 || page.Agents[0].AgentID != "agent-100" || page.HasMore {
		t.Fatalf("second page status=%d page=%+v", second.Code, page)
	}
}

func TestAgentOptionsFailClosedOnInvalidOrUnorderedProjection(t *testing.T) {
	for name, agents := range map[string][]domain.ConsoleAgentOption{
		"unordered": {
			{AgentID: "risk", OrganizationID: "org-main", DisplayName: "Risk", WorkerStatus: domain.WorkerStatusOffline},
			{AgentID: "quote", OrganizationID: "org-main", DisplayName: "Quote", WorkerStatus: domain.WorkerStatusOffline},
		},
		"invalid": {{AgentID: "quote", OrganizationID: "", DisplayName: "Quote", WorkerStatus: domain.WorkerStatusOffline}},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newConsoleFixtureWithState(t, testState{agents: agents})
			response := fixture.request(AgentsPath)
			if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "org-main") {
				t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
			}
		})
	}
}
