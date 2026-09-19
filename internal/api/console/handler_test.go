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

	openapi "openagentx/internal/api"
	cliauth "openagentx/internal/auth/cli"
	"openagentx/internal/auth/web"
	"openagentx/internal/domain"
)

type testState struct {
	snapshot      domain.ConsoleSnapshot
	agents        []domain.ConsoleAgentOption
	tasks         []domain.Task
	taskSnapshots map[string]domain.ConsoleTaskSnapshot
	err           error
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

func (s testState) ListConsoleTasks(_ context.Context, agentID string, cursor domain.ConsoleTaskCursor, limit int) ([]domain.Task, error) {
	if s.err != nil {
		return nil, s.err
	}
	result := make([]domain.Task, 0, limit)
	for _, task := range s.tasks {
		if task.TargetAgentID != agentID {
			continue
		}
		updatedAt, err := time.Parse(time.RFC3339Nano, task.UpdatedAt)
		if err != nil {
			return nil, err
		}
		if !cursor.UpdatedAt.IsZero() && !(updatedAt.Before(cursor.UpdatedAt) || updatedAt.Equal(cursor.UpdatedAt) && task.ID < cursor.TaskID) {
			continue
		}
		result = append(result, task)
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func (s testState) ConsoleTaskSnapshot(_ context.Context, agentID, taskID string) (domain.ConsoleTaskSnapshot, error) {
	if s.err != nil {
		return domain.ConsoleTaskSnapshot{}, s.err
	}
	snapshot, ok := s.taskSnapshots[taskID]
	if !ok || snapshot.Task.TargetAgentID != agentID {
		return domain.ConsoleTaskSnapshot{}, domain.ErrNotFound
	}
	return snapshot, nil
}

type consoleFixture struct {
	handler *Handler
	cookie  *http.Cookie
}

type consoleCLIRepository struct {
	installationID string
	user           domain.WebUserRecord
	tokens         map[string]domain.CLITokenRecord
}

func (r *consoleCLIRepository) InstallationID(context.Context) (string, error) {
	return r.installationID, nil
}
func (r *consoleCLIRepository) GetWebUserByUsername(_ context.Context, username string) (*domain.WebUserRecord, error) {
	if username != r.user.Username {
		return nil, domain.ErrNotFound
	}
	user := r.user
	return &user, nil
}
func (r *consoleCLIRepository) GetWebUserByID(_ context.Context, id string) (*domain.WebUserRecord, error) {
	if id != r.user.ID {
		return nil, domain.ErrNotFound
	}
	user := r.user
	return &user, nil
}
func (r *consoleCLIRepository) ReplaceCLIToken(_ context.Context, record *domain.CLITokenRecord) error {
	r.tokens[record.TokenDigest] = *record
	return nil
}
func (r *consoleCLIRepository) GetCLITokenByDigest(_ context.Context, digest string) (*domain.CLITokenRecord, error) {
	record, ok := r.tokens[digest]
	if !ok {
		return nil, domain.ErrNotFound
	}
	copy := record
	return &copy, nil
}
func (r *consoleCLIRepository) TouchCLIToken(_ context.Context, digest string, usedAt time.Time) error {
	record, ok := r.tokens[digest]
	if !ok {
		return domain.ErrNotFound
	}
	record.LastUsedAt = usedAt
	r.tokens[digest] = record
	return nil
}
func (r *consoleCLIRepository) RevokeCLIToken(context.Context, string, string, time.Time) error {
	return nil
}
func (r *consoleCLIRepository) RevokeCLITokensByWebUser(context.Context, string, time.Time) error {
	return nil
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

func consoleTask(id, agentID, updatedAt, content string, version int64, status domain.TaskStatus) domain.Task {
	return domain.Task{
		ID: id, Version: version, SenderPrincipalID: "human-owner", TargetAgentID: agentID,
		OrganizationID: "org-main", DispatchMode: domain.DispatchModeDirect,
		IdempotencyKey: "idem-" + id, Content: content, Status: status,
		CreatedAt: updatedAt, UpdatedAt: updatedAt,
	}
}

func TestAttachIncludesSafeSuggestedTask(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	task := consoleTask("task-current", "quote", now, "check token=private", 3, domain.TaskStatusRunning)
	fixture := newConsoleFixtureWithState(t, testState{snapshot: domain.ConsoleSnapshot{
		Agent: domain.AgentIdentity{ID: "quote", Status: domain.AgentIdentityActive}, SuggestedTask: &task,
	}})
	response := fixture.request(AttachPath + "?agent_id=quote")
	var attached AttachResponse
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &attached) != nil {
		t.Fatalf("attach status=%d body=%s", response.Code, response.Body.String())
	}
	if attached.SuggestedTask == nil || attached.SuggestedTask.TaskID != task.ID || attached.SuggestedTask.Version != 3 ||
		strings.Contains(attached.SuggestedTask.Summary, "private") || !strings.Contains(attached.SuggestedTask.Summary, "[REDACTED]") {
		t.Fatalf("unsafe or missing suggested Task: %+v", attached.SuggestedTask)
	}
}

func TestConsoleTasksListUsesOpaquePaginationAndRejectsTampering(t *testing.T) {
	newer := time.Date(2026, 9, 19, 10, 0, 1, 0, time.UTC).Format(time.RFC3339Nano)
	older := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	tasks := []domain.Task{
		consoleTask("task-c", "quote", newer, "new token=private", 3, domain.TaskStatusRunning),
		consoleTask("task-b", "quote", older, "second", 2, domain.TaskStatusQueued),
		consoleTask("task-a", "quote", older, "third", 1, domain.TaskStatusSucceeded),
	}
	fixture := newConsoleFixtureWithState(t, testState{tasks: tasks})
	first := fixture.request("/api/console/v1/agents/quote/tasks?limit=2")
	var page openapi.ConsoleTaskPage
	if first.Code != http.StatusOK || json.Unmarshal(first.Body.Bytes(), &page) != nil {
		t.Fatalf("first page status=%d body=%s", first.Code, first.Body.String())
	}
	if len(page.Tasks) != 2 || !page.HasMore || page.NextCursor == "" || page.Tasks[0].TaskID != "task-c" ||
		page.Tasks[1].TaskID != "task-b" || strings.Contains(first.Body.String(), "private") {
		t.Fatalf("first Console Task page=%+v body=%s", page, first.Body.String())
	}
	second := fixture.request("/api/console/v1/agents/quote/tasks?limit=2&cursor=" + page.NextCursor)
	if second.Code != http.StatusOK || json.Unmarshal(second.Body.Bytes(), &page) != nil || len(page.Tasks) != 1 || page.Tasks[0].TaskID != "task-a" || page.HasMore {
		t.Fatalf("second page status=%d page=%+v", second.Code, page)
	}
	badCursor := page.NextCursor
	if badCursor == "" {
		badCursor = "eyJ2IjoxfQ"
	}
	for _, path := range []string{
		"/api/console/v1/agents/quote/tasks?cursor=",
		"/api/console/v1/agents/quote/tasks?cursor=" + badCursor + "x",
		"/api/console/v1/agents/quote/tasks?limit=0",
		"/api/console/v1/agents/quote/tasks?limit=1&limit=2",
	} {
		if response := fixture.request(path); response.Code != http.StatusBadRequest {
			t.Fatalf("invalid pagination %q status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}

func TestConsoleTaskDetailIsOwnedConsistentAndSafe(t *testing.T) {
	now := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	nowText := now.Format(time.RFC3339Nano)
	result := "answer password=hunter2"
	task := consoleTask("task-detail", "quote", nowText, "question token=private", 4, domain.TaskStatusWaitingApproval)
	task.Result = &result
	mailbox := &domain.MailboxItem{Sequence: 1, ID: "mailbox-detail", TargetAgentID: "quote", Kind: domain.MailboxKindTask,
		Lane: domain.MailboxLaneWork, TaskID: task.ID, State: domain.MailboxStateAccepted,
		WorkerInstanceID: "worker-detail", FencingToken: 7, Attempts: 1, CreatedAt: now, AcceptedAt: &now}
	message := &domain.Message{ID: "message-detail", Version: 2, Sequence: 2, TaskID: task.ID,
		SenderPrincipalID: "human-owner", TargetAgentID: "quote", Kind: domain.MessageKindSupplement,
		Content: "continue api_key=private", CreatedAt: nowText}
	approval := &domain.ApprovalRequest{ID: "approval-detail", TaskID: task.ID, Mode: domain.ApprovalModePreflight,
		ScopeDigest: "must-not-leak", State: domain.ApprovalRequestPending, ExpiresAt: now.Add(time.Hour), CreatedAt: now}
	run := &domain.RunAttempt{ID: "run-detail", TaskID: task.ID, AgentID: "quote", Version: 3,
		Status: domain.RunAttemptWaitingApproval, WorkerInstanceID: "worker-detail", FencingToken: 998877,
		LeaseUntil: now.Add(time.Hour), ExecutionSpecVersion: 1,
		RequestedExecutionJSON: `{"secret":"must-not-leak"}`, ResolvedExecutionJSON: `{}`,
		AdapterID: "agy", BackendID: "local", Model: "model-1", ReasoningMode: domain.ReasoningEffort,
		ReasoningValue: "high", StartedAt: now, CreatedAt: now, UpdatedAt: now}
	fixture := newConsoleFixtureWithState(t, testState{taskSnapshots: map[string]domain.ConsoleTaskSnapshot{
		task.ID: {Task: task, WorkDelivery: mailbox, LatestRun: run, LatestRunWorkerGeneration: 7,
			LatestMessage: message, PendingApproval: approval, SnapshotSequence: 42},
	}})
	response := fixture.request("/api/console/v1/agents/quote/tasks/task-detail")
	var snapshot openapi.ConsoleTaskSnapshot
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &snapshot) != nil {
		t.Fatalf("detail status=%d body=%s", response.Code, response.Body.String())
	}
	if snapshot.Task.TaskID != task.ID || snapshot.Task.Version != 4 || snapshot.WorkDelivery == nil ||
		snapshot.LatestRun == nil || snapshot.LatestRun.WorkerGeneration == nil || *snapshot.LatestRun.WorkerGeneration != 7 ||
		snapshot.LatestMessage == nil || snapshot.PendingApproval == nil || snapshot.SnapshotSequence != 42 {
		t.Fatalf("incomplete Task detail: %+v", snapshot)
	}
	body := response.Body.String()
	for _, forbidden := range []string{"hunter2", "private", "must-not-leak", "998877", "sender_principal", "idempotency", "fencing", "requested_execution"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("Task detail leaked %q: %s", forbidden, body)
		}
	}
	if cross := fixture.request("/api/console/v1/agents/risk/tasks/task-detail"); cross.Code != http.StatusNotFound {
		t.Fatalf("cross-Agent detail status=%d body=%s", cross.Code, cross.Body.String())
	}
}

func TestCLITaskRoutesRequireViewerAndConsoleReadScope(t *testing.T) {
	now := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	digest, err := web.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	repository := &consoleCLIRepository{installationID: "installation-test", tokens: make(map[string]domain.CLITokenRecord),
		user: domain.WebUserRecord{ID: "web-user", PrincipalID: "human-user", Username: "owner",
			PasswordDigest: digest, Roles: []domain.WebRole{domain.WebRoleOwner}, Status: domain.IdentityActive,
			PasswordSetAt: now, CreatedAt: now, UpdatedAt: now}}
	service, err := cliauth.NewService(repository, cliauth.Config{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := service.Login(context.Background(), "owner", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	task := consoleTask("task-cli", "quote", now.Format(time.RFC3339Nano), "safe", 1, domain.TaskStatusQueued)
	handler, err := NewCLIHandler(testState{tasks: []domain.Task{task}}, service)
	if err != nil {
		t.Fatal(err)
	}
	request := func(token string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/console/v1/agents/quote/tasks", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		handler.ServeHTTP(response, req)
		return response
	}
	if response := request(issued.Token); response.Code != http.StatusOK {
		t.Fatalf("authorized CLI Task list status=%d body=%s", response.Code, response.Body.String())
	}
	if response := request(""); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated CLI Task list status=%d body=%s", response.Code, response.Body.String())
	}
	for digest, record := range repository.tokens {
		record.Scopes = []domain.CLIScope{domain.CLIScopeConsoleControl}
		repository.tokens[digest] = record
	}
	if response := request(issued.Token); response.Code != http.StatusForbidden {
		t.Fatalf("wrong-scope CLI Task list status=%d body=%s", response.Code, response.Body.String())
	}
}
