package console

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	consoleclient "openagentx/internal/client/console"
	"openagentx/internal/credentialstore"
	"openagentx/internal/domain"
	fleetmodel "openagentx/internal/fleet"
)

type fakeConsoleClient struct {
	mu          sync.Mutex
	operations  []string
	probe       openapi.CLIInstallationResponse
	login       openapi.CLILoginResponse
	session     openapi.CLISessionResponse
	agents      []domain.ConsoleAgentOption
	tasks       []openapi.ConsoleTaskOption
	taskDetails map[string]openapi.ConsoleTaskSnapshot
	sessionErr  error
	dispatches  []openapi.CreateTaskRequest
	steers      []openapi.CreateMessageRequest
	cancels     []openapi.CancelTaskRequest
	decisions   []openapi.DecideApprovalRequest
	targets     []string
	followAgent string
	followMode  string
	followFunc  func(context.Context, string, string, func(consoleapi.AttachResponse) error,
		func(openapi.JournalEventReadModel) error, func(consoleclient.FollowState) error) error
	followCursor int64
	logoutCalls  int
}

func (c *fakeConsoleClient) record(value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.operations = append(c.operations, value)
}
func (c *fakeConsoleClient) ProbeInstallation(context.Context) (openapi.CLIInstallationResponse, error) {
	c.record("probe")
	return c.probe, nil
}
func (c *fakeConsoleClient) LoginCredential(_ context.Context, username, _ string) (openapi.CLILoginResponse, error) {
	c.record("login:" + username)
	return c.login, nil
}
func (c *fakeConsoleClient) UseCredential(_ context.Context, installationID, _ string) error {
	c.record("credential:" + installationID)
	return nil
}
func (c *fakeConsoleClient) Session(context.Context) (openapi.CLISessionResponse, error) {
	c.record("session")
	return c.session, c.sessionErr
}
func (c *fakeConsoleClient) Logout(context.Context) error {
	c.record("logout")
	c.logoutCalls++
	return nil
}
func (c *fakeConsoleClient) ListAgentOptions(context.Context) ([]domain.ConsoleAgentOption, error) {
	c.record("agents")
	return append([]domain.ConsoleAgentOption(nil), c.agents...), nil
}
func (c *fakeConsoleClient) ListTaskOptions(context.Context, string) ([]openapi.ConsoleTaskOption, error) {
	c.record("tasks")
	return append([]openapi.ConsoleTaskOption(nil), c.tasks...), nil
}
func (c *fakeConsoleClient) TaskSnapshot(_ context.Context, _, taskID string) (openapi.ConsoleTaskSnapshot, error) {
	c.record("task:" + taskID)
	snapshot, ok := c.taskDetails[taskID]
	if !ok {
		return openapi.ConsoleTaskSnapshot{}, fmt.Errorf("Task detail not found")
	}
	return snapshot, nil
}
func (c *fakeConsoleClient) Attach(context.Context, string, string) (consoleapi.AttachResponse, error) {
	return consoleapi.AttachResponse{}, nil
}
func (c *fakeConsoleClient) Follow(ctx context.Context, agentID, mode string, onAttach func(consoleapi.AttachResponse) error,
	onEvent func(openapi.JournalEventReadModel) error, onState func(consoleclient.FollowState) error) error {
	c.record("follow")
	c.followAgent, c.followMode = agentID, mode
	if c.followFunc != nil {
		return c.followFunc(ctx, agentID, mode, onAttach, onEvent, onState)
	}
	if err := onState(consoleclient.FollowState{State: consoleclient.ConnectionConnected}); err != nil {
		return err
	}
	return onAttach(consoleapi.AttachResponse{AgentID: agentID, Mode: mode, WorkerStatus: domain.WorkerStatusOffline})
}
func (c *fakeConsoleClient) Dispatch(_ context.Context, request openapi.CreateTaskRequest) (openapi.CreateTaskResponse, error) {
	c.record("dispatch")
	c.dispatches = append(c.dispatches, request)
	return openapi.CreateTaskResponse{TaskID: "task-created", TaskVersion: 1,
		TaskStatus: domain.TaskStatusQueued, Sequence: 11}, nil
}
func (c *fakeConsoleClient) Steer(_ context.Context, target string, request openapi.CreateMessageRequest) (openapi.CreateMessageResponse, error) {
	c.record("steer")
	c.targets = append(c.targets, target)
	c.steers = append(c.steers, request)
	return openapi.CreateMessageResponse{MessageID: "message-created", MessageVersion: 1,
		TaskID: target, TaskVersion: request.Meta.ExpectedVersion + 1,
		TaskStatus: domain.TaskStatusRunning, Sequence: 12}, nil
}
func (c *fakeConsoleClient) Cancel(_ context.Context, target string, request openapi.CancelTaskRequest) (openapi.CancelTaskResponse, error) {
	c.record("cancel")
	c.targets = append(c.targets, target)
	c.cancels = append(c.cancels, request)
	return openapi.CancelTaskResponse{Task: domain.Task{ID: target, Version: request.Meta.ExpectedVersion + 1,
		Status: domain.TaskStatusCancelRequested}, Sequence: 13}, nil
}
func (c *fakeConsoleClient) DecideApproval(_ context.Context, target string, request openapi.DecideApprovalRequest) (openapi.DecideApprovalResponse, error) {
	c.record("approval")
	c.targets = append(c.targets, target)
	c.decisions = append(c.decisions, request)
	return openapi.DecideApprovalResponse{Decision: domain.ApprovalDecision{ID: "decision-1",
		ApprovalRequestID: target, Decision: request.Decision, State: domain.ApprovalDecisionPersisted}, Sequence: 14}, nil
}

type fakeCredentialStore struct {
	credential credentialstore.Credential
	loadErr    error
	deleted    []credentialstore.Credential
}

func (s *fakeCredentialStore) Save(credential credentialstore.Credential) error {
	s.credential = credential
	return nil
}
func (s *fakeCredentialStore) Replace(issue func() (credentialstore.Credential, error)) (credentialstore.Credential, error) {
	credential, err := issue()
	if err == nil {
		s.credential = credential
	}
	return credential, err
}
func (s *fakeCredentialStore) Load(_, _, _ string) (credentialstore.Credential, error) {
	if s.loadErr != nil {
		return credentialstore.Credential{}, s.loadErr
	}
	return s.credential, nil
}
func (s *fakeCredentialStore) LoadCurrentForSocket(string) (credentialstore.Credential, error) {
	if s.loadErr != nil {
		return credentialstore.Credential{}, s.loadErr
	}
	return s.credential, nil
}
func (s *fakeCredentialStore) Delete(credential credentialstore.Credential) (bool, error) {
	s.deleted = append(s.deleted, credential)
	s.credential = credentialstore.Credential{}
	return true, nil
}

type applicationTmuxRunner struct {
	name    string
	managed bool
	agentID string
	calls   []string
}

func (r *applicationTmuxRunner) Run(_ context.Context, args ...string) (string, error) {
	call := strings.Join(args, " ")
	r.calls = append(r.calls, call)
	switch call {
	case "display-message -p -F #{window_id}":
		return "@1\n", nil
	case "display-message -p -F #{session_name}":
		return fleetmodel.SessionName + "\n", nil
	case "display-message -p -F #{window_name}":
		return r.name + "\n", nil
	case "display-message -p -F #{pane_index}":
		return "0\n", nil
	case "has-session -t =OAX":
		return "", nil
	case "list-windows -t =OAX -F #{window_id}":
		return "@1\n", nil
	case "display-message -p -t @1 -F #{window_name}":
		return r.name + "\n", nil
	case "list-panes -t @1 -F #{pane_index}":
		return "0\n1\n", nil
	case "display-message -p -t @1.0 -F #{pane_dead}":
		return "0\n", nil
	case "show-options -w -t @1":
		if r.managed {
			return fleetmodel.ManagedOption + " 1\n" + fleetmodel.AgentIDOption + " " + r.agentID + "\n", nil
		}
		return "", nil
	case "set-option -w -t @1 " + fleetmodel.ManagedOption + " 1":
		r.managed = true
		return "", nil
	case "set-option -w -t @1 " + fleetmodel.AgentIDOption + " quote":
		r.agentID = "quote"
		return "", nil
	case "set-option -w -t @1 " + fleetmodel.AgentIDOption + " risk":
		r.agentID = "risk"
		return "", nil
	case "rename-window -t @1 quote":
		r.name = "quote"
		return "", nil
	case "rename-window -t @1 risk":
		r.name = "risk"
		return "", nil
	default:
		return "", fmt.Errorf("unexpected tmux call %q", call)
	}
}

func applicationFixture(t *testing.T, managed bool) (*consoleApplication, *fakeConsoleClient, *fakeCredentialStore, *applicationTmuxRunner) {
	t.Helper()
	expires := fixedNow().Add(time.Hour)
	credential := credentialstore.Credential{SocketPath: "/tmp/oax.sock", InstallationID: "installation-main", Username: "owner",
		TokenID: "token-main", Token: "opaque-value", AbsoluteExpires: expires}
	client := &fakeConsoleClient{probe: openapi.CLIInstallationResponse{InstallationID: credential.InstallationID},
		login: openapi.CLILoginResponse{CLISessionResponse: openapi.CLISessionResponse{InstallationID: credential.InstallationID,
			Principal: openapi.CLIPrincipal{TokenID: credential.TokenID, Username: credential.Username}, AbsoluteExpiresAt: expires}, Token: credential.Token},
		session: openapi.CLISessionResponse{InstallationID: credential.InstallationID,
			Principal: openapi.CLIPrincipal{TokenID: credential.TokenID, Username: credential.Username}, AbsoluteExpiresAt: expires},
		agents: []domain.ConsoleAgentOption{
			{AgentID: "quote", OrganizationID: "org-main", DisplayName: "Quote", WorkerStatus: domain.WorkerStatusOnline, Generation: 4},
			{AgentID: "risk", OrganizationID: "org-main", DisplayName: "Risk", WorkerStatus: domain.WorkerStatusOffline},
		}, taskDetails: make(map[string]openapi.ConsoleTaskSnapshot)}
	store := &fakeCredentialStore{credential: credential}
	runner := &applicationTmuxRunner{name: "scratch"}
	if managed {
		runner.name, runner.managed, runner.agentID = "quote", true, "quote"
	}
	deps := Dependencies{Now: fixedNow, NewClient: func(string) (Client, error) { return client, nil }, Tmux: runner}
	return newConsoleApplication(context.Background(), credential.SocketPath, store, deps), client, store, runner
}

func TestLoginAndLogoutUseSharedApplicationLifecycle(t *testing.T) {
	application, client, store, _ := applicationFixture(t, true)
	status, err := application.login("owner", "password supplied outside argv")
	if err != nil || !status.Authenticated || status.Username != "owner" || store.credential.TokenID != "token-main" {
		t.Fatalf("login authenticated=%v username=%q token_id=%q err=%v", status.Authenticated, status.Username, store.credential.TokenID, err)
	}
	message, err := application.logout()
	if err != nil || !strings.Contains(message, "revoked") || client.logoutCalls != 1 || len(store.deleted) != 1 {
		t.Fatalf("logout message=%q calls=%d deleted=%d err=%v", message, client.logoutCalls, len(store.deleted), err)
	}
}

func TestPrepareAttachUsesExplicitManagedWindowAndSelectorSources(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		managed   bool
		requested string
		selected  string
		source    string
	}{
		{name: "explicit", managed: true, requested: "risk", selected: "risk", source: "explicit"},
		{name: "managed window", managed: true, selected: "quote", source: "managed-window"},
		{name: "selector", selected: "", source: "selector"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			application, client, _, runner := applicationFixture(t, testCase.managed)
			prepared, err := application.prepareAttach(consoleapi.ModeNormal, testCase.requested)
			if err != nil {
				t.Fatal(err)
			}
			if prepared.SelectedAgent != testCase.selected || prepared.Source != testCase.source || len(prepared.Agents) != 2 {
				t.Fatalf("preparation=%+v", prepared)
			}
			if !reflect.DeepEqual(client.operations, []string{"probe", "credential:installation-main", "session", "agents"}) {
				t.Fatalf("official auth/list order=%v", client.operations)
			}
			for _, call := range runner.calls {
				if strings.HasPrefix(call, "set-option") || strings.HasPrefix(call, "rename-window") {
					t.Fatalf("prepare mutated tmux before selection: %v", runner.calls)
				}
			}
		})
	}
}

func TestBindUsesTask05ServiceAndRequiresConfirmation(t *testing.T) {
	application, _, _, runner := applicationFixture(t, true)
	prepared, err := application.prepareAttach(consoleapi.ModeNormal, "risk")
	if err != nil {
		t.Fatal(err)
	}
	if _, confirmation, err := application.bind(prepared.ID, "risk", false); err != nil || !confirmation {
		t.Fatalf("unconfirmed bind confirmation=%v err=%v", confirmation, err)
	}
	before := append([]string(nil), runner.calls...)
	if runner.name != "quote" || runner.agentID != "quote" {
		t.Fatalf("unconfirmed bind mutated tmux: name=%s marker=%s calls before=%v after=%v", runner.name, runner.agentID, before, runner.calls)
	}
	if _, confirmation, err := application.bind(prepared.ID, "risk", true); err != nil || confirmation {
		t.Fatalf("confirmed bind confirmation=%v err=%v", confirmation, err)
	}
	if runner.name != "risk" || runner.agentID != "risk" || !runner.managed {
		t.Fatalf("confirmed bind did not use Task 05 state machine: name=%s marker=%s managed=%v", runner.name, runner.agentID, runner.managed)
	}
}

func TestOnlyLatestAttachPreparationRetainsAuthenticatedClient(t *testing.T) {
	application, _, _, _ := applicationFixture(t, true)
	first, err := application.prepareAttach(consoleapi.ModeNormal, "quote")
	if err != nil {
		t.Fatal(err)
	}
	second, err := application.prepareAttach(consoleapi.ModeDiagnostic, "quote")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := application.preparation(first.ID); ok {
		t.Fatal("superseded Attach preparation retained an authenticated client")
	}
	if prepared, ok := application.preparation(second.ID); !ok || prepared.mode != consoleapi.ModeDiagnostic {
		t.Fatalf("latest preparation=%+v ok=%v", prepared, ok)
	}
}

func TestControlCmdUsesOfficialClientExactlyOnceWithCAS(t *testing.T) {
	application, client, _, _ := applicationFixture(t, true)
	application.prepared[9] = preparedAttach{client: client, agents: map[string]domain.ConsoleAgentOption{
		"quote": {AgentID: "quote", OrganizationID: "org-main", DisplayName: "Quote", WorkerStatus: domain.WorkerStatusOnline, Generation: 4},
	}}
	requests := []controlRequest{
		{Kind: controlDispatch, AgentID: "quote", Content: "work"},
		{Kind: controlSteer, AgentID: "quote", TargetID: "task-1", ExpectedVersion: 7, Content: "focus"},
		{Kind: controlCancel, AgentID: "quote", TargetID: "task-1", ExpectedVersion: 8},
		{Kind: controlApprove, AgentID: "quote", TargetID: "approval-1", ExpectedVersion: 9},
		{Kind: controlReject, AgentID: "quote", TargetID: "approval-2", ExpectedVersion: 10},
	}
	wantOutcome := map[controlKind][]string{
		controlDispatch: {"task task-created", "version 1", "status queued", "sequence 11"},
		controlSteer:    {"task task-1", "task version 8", "status running", "message message-created", "sequence 12"},
		controlCancel:   {"task task-1", "version 9", "status cancel_requested", "sequence 13"},
		controlApprove:  {"approval approval-1", "decision approve", "decision id decision-1", "status persisted", "sequence 14"},
		controlReject:   {"approval approval-2", "decision reject", "decision id decision-1", "status persisted", "sequence 14"},
	}
	for _, request := range requests {
		message := application.controlCmd(9, request)()
		result, ok := message.(controlResultMsg)
		if !ok || result.Err != nil {
			t.Fatalf("control %s result=%+v", request.Kind, message)
		}
		summary := result.Outcome.summary(request.Kind)
		for _, fragment := range wantOutcome[request.Kind] {
			if !strings.Contains(summary, fragment) {
				t.Fatalf("control %s outcome %q omitted %q", request.Kind, summary, fragment)
			}
		}
	}
	if len(client.dispatches) != 1 || client.dispatches[0].OrganizationID != "org-main" || client.dispatches[0].TargetAgentID != "quote" {
		t.Fatalf("dispatch requests=%+v", client.dispatches)
	}
	if len(client.steers) != 1 || client.steers[0].Meta.ExpectedVersion != 7 || len(client.cancels) != 1 || client.cancels[0].Meta.ExpectedVersion != 8 ||
		len(client.decisions) != 2 || client.decisions[0].Meta.ExpectedVersion != 9 || client.decisions[1].Meta.ExpectedVersion != 10 ||
		client.decisions[0].Decision != domain.ApprovalDecisionApprove || client.decisions[1].Decision != domain.ApprovalDecisionReject {
		t.Fatalf("CAS controls steer=%+v cancel=%+v decisions=%+v", client.steers, client.cancels, client.decisions)
	}
}

func TestTaskReadsUsePreparedAuthenticatedClient(t *testing.T) {
	application, client, _, _ := applicationFixture(t, true)
	client.tasks = []openapi.ConsoleTaskOption{{TaskID: "task-1", Version: 2, Status: domain.TaskStatusRunning,
		Summary: "safe task", UpdatedAt: fixedNow().Format(time.RFC3339Nano)}}
	client.taskDetails["task-1"] = taskSnapshot("task-1", 2, domain.TaskStatusRunning)
	application.prepared[9] = preparedAttach{client: client, agents: map[string]domain.ConsoleAgentOption{
		"quote": {AgentID: "quote", OrganizationID: "org-main", DisplayName: "Quote",
			WorkerStatus: domain.WorkerStatusOnline, Generation: 4},
	}}

	listResult, ok := application.taskOptionsCmd(9, "quote")().(taskOptionsResultMsg)
	if !ok || listResult.Err != nil || len(listResult.Options) != 1 || listResult.Options[0].TaskID != "task-1" {
		t.Fatalf("Task list result=%+v", listResult)
	}
	detailResult, ok := application.taskSnapshotCmd(9, "quote", "task-1", taskSnapshotFocus)().(taskSnapshotResultMsg)
	if !ok || detailResult.Err != nil || detailResult.Snapshot.Task.TaskID != "task-1" ||
		detailResult.Purpose != taskSnapshotFocus {
		t.Fatalf("Task detail result=%+v", detailResult)
	}
	if got := client.operations[len(client.operations)-2:]; !reflect.DeepEqual(got, []string{"tasks", "task:task-1"}) {
		t.Fatalf("Task reads did not use prepared authenticated client: %v", client.operations)
	}

	unauthorized := application.taskOptionsCmd(9, "risk")().(taskOptionsResultMsg)
	if unauthorized.Err == nil || len(client.operations) != 2 {
		t.Fatalf("unauthorized Task read err=%v operations=%v", unauthorized.Err, client.operations)
	}
}

func TestFollowWaitsForReducerAckBeforeAdvancingClientCursor(t *testing.T) {
	application, client, _, _ := applicationFixture(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	application.ctx = ctx
	application.prepared[17] = preparedAttach{client: client, mode: consoleapi.ModeNormal,
		agents: map[string]domain.ConsoleAgentOption{"quote": {AgentID: "quote"}}}
	client.followFunc = func(_ context.Context, agentID, mode string,
		onAttach func(consoleapi.AttachResponse) error, onEvent func(openapi.JournalEventReadModel) error,
		onState func(consoleclient.FollowState) error) error {
		if err := onState(consoleclient.FollowState{State: consoleclient.ConnectionConnected}); err != nil {
			return err
		}
		if err := onAttach(consoleapi.AttachResponse{AgentID: agentID, Mode: mode,
			WorkerInstanceID: "worker-current", Generation: 4, WorkerStatus: domain.WorkerStatusOnline,
			SnapshotSequence: 4}); err != nil {
			return err
		}
		client.mu.Lock()
		client.followCursor = 4
		client.mu.Unlock()
		invalid := openapi.JournalEventReadModel{Sequence: 5, ID: "event-invalid",
			AggregateType: "worker_instance", AggregateID: "worker-current", EventType: "worker.heartbeat",
			Worker: &openapi.WorkerReadModel{WorkerInstanceID: "worker-current", AgentID: "quote",
				Generation: 4, Status: "invalid"}}
		if err := onEvent(invalid); err != nil {
			return err
		}
		client.mu.Lock()
		client.followCursor = invalid.Sequence
		client.mu.Unlock()
		return nil
	}

	started, ok := application.startFollowCmd(17, "quote")().(followStartedMsg)
	if !ok {
		t.Fatal("Follow did not return its typed update channel")
	}
	m := newTUIModel(application, fixedNow, nil)
	m.screen = screenAttach
	m.mode = consoleapi.ModeNormal
	m.selectedAgent = "quote"
	m.session = sessionStatus{Authenticated: true, ExpiresAt: fixedNow().Add(time.Hour)}
	m.follow = started.Updates
	m.followID = started.FollowID
	m.followEpoch = 1

	receive := func() tea.Msg {
		t.Helper()
		select {
		case message, open := <-started.Updates:
			if !open {
				t.Fatal("Follow update channel closed before terminal result")
			}
			return message
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for acknowledged Follow update")
			return nil
		}
	}
	for range 4 {
		message := receive()
		m, _ = updateModel(t, m, message)
		if _, done := message.(followDoneMsg); done {
			break
		}
	}
	if m.reducer == nil || m.reducer.Cursor() != 4 || m.overlay != overlayError {
		t.Fatalf("rejected event state cursor=%v overlay=%v notice=%q", m.reducer, m.overlay, m.notice)
	}
	client.mu.Lock()
	cursor := client.followCursor
	client.mu.Unlock()
	if cursor != 4 {
		t.Fatalf("client cursor crossed rejected event: %d", cursor)
	}
	select {
	case _, open := <-started.Updates:
		if open {
			t.Fatal("Follow goroutine left updates open after reducer rejection")
		}
	case <-time.After(time.Second):
		t.Fatal("Follow goroutine did not terminate after reducer rejection")
	}
}

func TestFollowCancellationEndsOldStreamBeforeStartingNewMode(t *testing.T) {
	application, client, _, _ := applicationFixture(t, true)
	application.prepared[21] = preparedAttach{client: client, mode: consoleapi.ModeNormal,
		agents: map[string]domain.ConsoleAgentOption{"quote": {AgentID: "quote"}}}
	startedModes := make(chan string, 2)
	client.followFunc = func(ctx context.Context, _, mode string, _ func(consoleapi.AttachResponse) error,
		_ func(openapi.JournalEventReadModel) error, _ func(consoleclient.FollowState) error) error {
		startedModes <- mode
		<-ctx.Done()
		return ctx.Err()
	}

	first := application.startFollowCmd(21, "quote")().(followStartedMsg)
	if got := <-startedModes; got != consoleapi.ModeNormal || first.FollowID == 0 {
		t.Fatalf("first Follow mode=%q id=%d", got, first.FollowID)
	}
	canceled := application.cancelFollowCmd(first.FollowID)().(followCancelResultMsg)
	if !canceled.Found {
		t.Fatal("active Follow was not found for cancellation")
	}
	select {
	case message := <-first.Updates:
		done, ok := message.(followDoneMsg)
		if !ok || done.FollowID != first.FollowID || !errors.Is(done.Err, context.Canceled) {
			t.Fatalf("old Follow terminal message=%+v", message)
		}
	case <-time.After(time.Second):
		t.Fatal("old Follow did not terminate after cancellation")
	}
	select {
	case _, open := <-first.Updates:
		if open {
			t.Fatal("old Follow updates remained open")
		}
	case <-time.After(time.Second):
		t.Fatal("old Follow updates did not close")
	}

	second := application.startFollowModeCmd(21, "quote", consoleapi.ModeDiagnostic)().(followStartedMsg)
	if got := <-startedModes; got != consoleapi.ModeDiagnostic || second.FollowID == first.FollowID {
		t.Fatalf("second Follow mode=%q ids=%d/%d", got, first.FollowID, second.FollowID)
	}
	_ = application.cancelFollowCmd(second.FollowID)()
	select {
	case <-second.Updates:
	case <-time.After(time.Second):
		t.Fatal("second Follow did not terminate")
	}
	application.mu.Lock()
	active := len(application.follows)
	application.mu.Unlock()
	if active != 0 {
		t.Fatalf("Follow registry retained %d active entries", active)
	}
}

func TestAuthenticatedClientDeletesExpiredAndStructuredUnauthorizedCredentials(t *testing.T) {
	application, client, store, _ := applicationFixture(t, true)
	store.credential.AbsoluteExpires = fixedNow().Add(-time.Second)
	if _, _, _, err := application.authenticatedClient(); !errors.Is(err, errLoginRequired) || len(store.deleted) != 1 {
		t.Fatalf("expired credential err=%v deleted=%d", err, len(store.deleted))
	}

	application, client, store, _ = applicationFixture(t, true)
	client.sessionErr = &consoleclient.APIError{StatusCode: 401, Code: openapi.ErrorCLIUnauthenticated, Message: "not exposed"}
	if _, _, _, err := application.authenticatedClient(); !errors.Is(err, errLoginRequired) || len(store.deleted) != 1 {
		t.Fatalf("revoked credential err=%v deleted=%d", err, len(store.deleted))
	}
}
