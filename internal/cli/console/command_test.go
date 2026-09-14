package console

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	"openagentx/internal/credentialstore"
	"openagentx/internal/domain"
	"openagentx/internal/localprofile"
)

type testClient struct {
	mu                sync.Mutex
	attached          consoleapi.AttachResponse
	loginCount        int
	attachCount       int
	followCount       int
	dispatches        []openapi.CreateTaskRequest
	steers            []openapi.CreateMessageRequest
	cancels           []openapi.CancelTaskRequest
	approvals         []openapi.DecideApprovalRequest
	workerCommands    []domain.WorkerCommandKind
	workerIDs         []string
	workerGenerations []int64
	attachedAgentID   string
	followEvents      []openapi.JournalEventReadModel
	eventsDelivered   chan struct{}
	installationID    string
	tokenSent         bool
	logoutCount       int
}

func (c *testClient) ProbeInstallation(context.Context) (openapi.CLIInstallationResponse, error) {
	if c.installationID == "" {
		c.installationID = "installation-test"
	}
	return openapi.CLIInstallationResponse{InstallationID: c.installationID}, nil
}

func (c *testClient) LoginCredential(context.Context, string, string) (openapi.CLILoginResponse, error) {
	c.loginCount++
	return openapi.CLILoginResponse{CLISessionResponse: openapi.CLISessionResponse{
		Principal: openapi.CLIPrincipal{TokenID: "token-id", Username: "owner"}, InstallationID: c.installationID,
		AbsoluteExpiresAt: time.Now().Add(time.Hour)}, Token: "opaque-token"}, nil
}

func (c *testClient) UseCredential(_ context.Context, installationID, token string) error {
	c.loginCount++
	if installationID != c.installationID || token == "" {
		return fmt.Errorf("credential mismatch")
	}
	c.tokenSent = true
	return nil
}

func (c *testClient) Logout(context.Context) error {
	c.logoutCount++
	return nil
}

func (c *testClient) Attach(_ context.Context, agentID, _ string) (consoleapi.AttachResponse, error) {
	c.attachCount++
	c.attachedAgentID = agentID
	response := c.attached
	response.AgentID = agentID
	return response, nil
}

func (c *testClient) Follow(ctx context.Context, agentID, _ string, onAttach func(consoleapi.AttachResponse) error, onEvent func(openapi.JournalEventReadModel) error) error {
	c.followCount++
	response := c.attached
	response.AgentID = agentID
	if err := onAttach(response); err != nil {
		return err
	}
	events := c.followEvents
	if events == nil {
		events = []openapi.JournalEventReadModel{{Sequence: 1, ID: "event-1", AggregateType: "task", AggregateID: "task-1", EventType: "task.created"}}
	}
	for _, event := range events {
		if err := onEvent(event); err != nil {
			return err
		}
	}
	if c.eventsDelivered != nil {
		close(c.eventsDelivered)
	}
	<-ctx.Done()
	return nil
}

func (c *testClient) Dispatch(_ context.Context, request openapi.CreateTaskRequest) (openapi.CreateTaskResponse, error) {
	c.dispatches = append(c.dispatches, request)
	return openapi.CreateTaskResponse{}, nil
}

func (c *testClient) Steer(_ context.Context, _ string, request openapi.CreateMessageRequest) (openapi.CreateMessageResponse, error) {
	c.steers = append(c.steers, request)
	return openapi.CreateMessageResponse{}, nil
}

func (c *testClient) Cancel(_ context.Context, _ string, request openapi.CancelTaskRequest) (openapi.CancelTaskResponse, error) {
	c.cancels = append(c.cancels, request)
	return openapi.CancelTaskResponse{}, nil
}

func (c *testClient) DecideApproval(_ context.Context, _ string, request openapi.DecideApprovalRequest) (openapi.DecideApprovalResponse, error) {
	c.approvals = append(c.approvals, request)
	return openapi.DecideApprovalResponse{}, nil
}

func (c *testClient) WorkerCommand(_ context.Context, workerID string, generation int64, kind domain.WorkerCommandKind, _ string, _ bool) (openapi.WorkerCommandResponse, error) {
	c.workerCommands = append(c.workerCommands, kind)
	c.workerIDs = append(c.workerIDs, workerID)
	c.workerGenerations = append(c.workerGenerations, generation)
	return openapi.WorkerCommandResponse{}, nil
}

type gatedReader struct {
	ready <-chan struct{}
	once  sync.Once
	inner io.Reader
}

func (r *gatedReader) Read(p []byte) (int, error) {
	r.once.Do(func() { <-r.ready })
	return r.inner.Read(p)
}

type testTmux struct {
	current string
	windows string
	err     error
	calls   []string
}

type testCredentialStore struct {
	credential credentialstore.Credential
	missing    bool
	deleted    bool
	saved      bool
}

func (s *testCredentialStore) Save(value credentialstore.Credential) error {
	s.credential = value
	s.saved = true
	return nil
}
func (s *testCredentialStore) Load(_, installationID, username string) (credentialstore.Credential, error) {
	if s.missing || (username != "" && username != s.credential.Username) || installationID != s.credential.InstallationID {
		return credentialstore.Credential{}, credentialstore.ErrNotFound
	}
	return s.credential, nil
}
func (s *testCredentialStore) LoadCurrentForSocket(string) (credentialstore.Credential, error) {
	if s.missing {
		return credentialstore.Credential{}, credentialstore.ErrNotFound
	}
	return s.credential, nil
}
func (s *testCredentialStore) Delete(credentialstore.Credential) (bool, error) {
	s.deleted = true
	return true, nil
}

func (t *testTmux) Run(_ context.Context, args ...string) (string, error) {
	t.calls = append(t.calls, strings.Join(args, " "))
	if t.err != nil {
		return "", t.err
	}
	if args[0] == "display-message" {
		return t.current, nil
	}
	if args[0] == "list-windows" {
		return t.windows, nil
	}
	return "", fmt.Errorf("unexpected tmux command")
}

func consoleDeps(client Client, input string, interactive bool) (Dependencies, *bytes.Buffer, *bytes.Buffer) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	store := &testCredentialStore{credential: credentialstore.Credential{SocketPath: "/run/openagentx.sock",
		InstallationID: "installation-test", Username: "owner", TokenID: "token-id", Token: "opaque-token", AbsoluteExpires: time.Now().Add(time.Hour)}}
	return Dependencies{
		Out: out, Err: errOut, In: strings.NewReader(input), IsInteractive: func() bool { return interactive },
		ReadPassword:       func(string) (string, error) { return "password", nil },
		NewClient:          func(string) (Client, error) { return client, nil },
		NewCredentialStore: func(string) (CredentialStore, error) { return store, nil },
	}, out, errOut
}

func TestInteractiveAttachUsesOfficialClientForCommandsWhileFollowing(t *testing.T) {
	client := &testClient{attached: consoleapi.AttachResponse{
		WorkerInstanceID: "worker-quote", Generation: 7, WorkerStatus: domain.WorkerStatusOnline,
	}}
	input := strings.Join([]string{
		"/dispatch inspect repository",
		"/steer task-1 2 continue carefully",
		"/cancel task-1 3",
		"/approve approval-1 4",
		"/reject approval-2 5",
		"/down",
		"/foreground",
		"/quit",
	}, "\n") + "\n"
	deps, out, stderr := consoleDeps(client, input, true)
	if code := Execute([]string{"attach", "--socket", "/run/openagentx.sock", "--agent", "quote", "--organization", "org-1"}, deps); code != 0 {
		t.Fatalf("attach code=%d stderr=%s", code, stderr.String())
	}
	if client.followCount != 1 || len(client.dispatches) != 1 || len(client.steers) != 1 || len(client.cancels) != 1 || len(client.approvals) != 2 ||
		!reflect.DeepEqual(client.workerCommands, []domain.WorkerCommandKind{domain.WorkerCommandStop}) {
		t.Fatalf("official client calls follow=%d dispatch=%d steer=%d cancel=%d approvals=%d worker=%v", client.followCount, len(client.dispatches), len(client.steers), len(client.cancels), len(client.approvals), client.workerCommands)
	}
	if client.dispatches[0].TargetAgentID != "quote" || client.dispatches[0].OrganizationID != "org-1" || client.steers[0].Meta.ExpectedVersion != 2 ||
		client.cancels[0].Meta.ExpectedVersion != 3 || client.approvals[0].Decision != domain.ApprovalDecisionApprove || client.approvals[1].Decision != domain.ApprovalDecisionReject {
		t.Fatalf("unexpected API requests dispatch=%+v steer=%+v cancel=%+v approvals=%+v", client.dispatches, client.steers, client.cancels, client.approvals)
	}
	if !strings.Contains(out.String(), foregroundUnavailable) || !strings.Contains(out.String(), replPrompt) || !strings.Contains(out.String(), "task.created") {
		t.Fatalf("interactive output missing command bar or Follow event: %s", out.String())
	}
}

func TestInteractiveAttachUsesReducerIdentityAndSuppressesOldHeartbeats(t *testing.T) {
	now := time.Now().UTC()
	delivered := make(chan struct{})
	client := &testClient{
		attached: consoleapi.AttachResponse{AgentID: "quote", WorkerInstanceID: "worker-48",
			Generation: 48, WorkerStatus: domain.WorkerStatusOnline, LeaseUntil: now.Add(time.Minute), SnapshotSequence: 100},
		followEvents: []openapi.JournalEventReadModel{
			{Sequence: 101, ID: "old-generation-heartbeat", AggregateType: "worker_instance", AggregateID: "worker-42", EventType: "worker.heartbeat",
				Worker: &openapi.WorkerReadModel{WorkerInstanceID: "worker-42", AgentID: "quote", Generation: 42, Status: domain.WorkerStatusOffline}},
			{Sequence: 102, ID: "current-heartbeat", AggregateType: "worker_instance", AggregateID: "worker-48", EventType: "worker.heartbeat",
				Worker: &openapi.WorkerReadModel{WorkerInstanceID: "worker-48", AgentID: "quote", Generation: 48, Status: domain.WorkerStatusOnline, LeaseUntil: now.Add(2 * time.Minute)}},
			{Sequence: 103, ID: "worker-replacement", AggregateType: "worker_instance", AggregateID: "worker-49", EventType: "worker.registered",
				Worker: &openapi.WorkerReadModel{WorkerInstanceID: "worker-49", AgentID: "quote", Generation: 49, Status: domain.WorkerStatusOnline, LeaseUntil: now.Add(3 * time.Minute)}},
		},
		eventsDelivered: delivered,
	}
	deps, out, stderr := consoleDeps(client, "", true)
	deps.In = &gatedReader{ready: delivered, inner: strings.NewReader("/down\n/quit\n")}
	if code := Execute([]string{"attach", "--socket", "/run/openagentx.sock", "--agent", "quote"}, deps); code != 0 {
		t.Fatalf("attach code=%d stderr=%s", code, stderr.String())
	}
	if !reflect.DeepEqual(client.workerIDs, []string{"worker-49"}) || !reflect.DeepEqual(client.workerGenerations, []int64{49}) {
		t.Fatalf("down used stale Worker identity ids=%v generations=%v", client.workerIDs, client.workerGenerations)
	}
	if strings.Contains(out.String(), "old-generation-heartbeat") || strings.Contains(out.String(), "current-heartbeat") ||
		!strings.Contains(out.String(), "worker-replacement") {
		t.Fatalf("heartbeat/replacement Timeline mismatch: %s", out.String())
	}
}

func TestAttachOnceIsExplicitNonInteractiveAndDoesNotReadCommands(t *testing.T) {
	client := &testClient{attached: consoleapi.AttachResponse{WorkerStatus: domain.WorkerStatusOffline}}
	deps, _, stderr := consoleDeps(client, "/dispatch must-not-run\n", false)
	if code := Execute([]string{"attach", "--socket", "/run/openagentx.sock", "--agent", "quote", "--once"}, deps); code != 0 {
		t.Fatalf("once code=%d stderr=%s", code, stderr.String())
	}
	if client.attachCount != 1 || client.followCount != 0 || len(client.dispatches) != 0 {
		t.Fatalf("once behavior attach=%d follow=%d dispatch=%d", client.attachCount, client.followCount, len(client.dispatches))
	}

	client = &testClient{}
	deps, _, stderr = consoleDeps(client, "", false)
	if code := Execute([]string{"attach", "--socket", "/run/openagentx.sock", "--agent", "quote"}, deps); code != 2 {
		t.Fatalf("non-interactive continuous attach code=%d", code)
	}
	if client.loginCount != 0 || !strings.Contains(stderr.String(), "requires an interactive TTY") {
		t.Fatalf("non-interactive attach reached API or lacked guidance: login=%d stderr=%s", client.loginCount, stderr.String())
	}
}

func TestAttachDefaultsAgentFromExactManagedTmuxWindow(t *testing.T) {
	client := &testClient{attached: consoleapi.AttachResponse{WorkerStatus: domain.WorkerStatusOffline}}
	tmux := &testTmux{current: "agentx\tquote\t0\t1\n", windows: "overview\nquote\nrisk\n"}
	deps, _, stderr := consoleDeps(client, "", false)
	deps.Tmux = tmux
	if code := Execute([]string{"attach", "--socket", "/run/openagentx.sock", "--once"}, deps); code != 0 {
		t.Fatalf("tmux default attach code=%d stderr=%s", code, stderr.String())
	}
	if client.attachedAgentID != "quote" || len(tmux.calls) != 2 || !strings.HasPrefix(tmux.calls[0], "display-message -p -F") {
		t.Fatalf("resolved Agent=%q tmux calls=%v", client.attachedAgentID, tmux.calls)
	}

	explicit := &testTmux{err: fmt.Errorf("must not be called")}
	deps, _, stderr = consoleDeps(client, "", false)
	deps.Tmux = explicit
	if code := Execute([]string{"attach", "--socket", "/run/openagentx.sock", "--agent", "risk", "--once"}, deps); code != 0 {
		t.Fatalf("explicit Agent attach code=%d stderr=%s", code, stderr.String())
	}
	if len(explicit.calls) != 0 {
		t.Fatalf("explicit --agent unexpectedly queried tmux: %v", explicit.calls)
	}
}

func TestTmuxAgentResolutionFailsClosedForWrongOrAmbiguousWindow(t *testing.T) {
	tests := map[string]*testTmux{
		"wrong-session": {current: "other\tquote\t0\t1\n", windows: "quote\n"},
		"overview":      {current: "agentx\toverview\t0\t1\n", windows: "overview\n"},
		"wrong-pane":    {current: "agentx\tquote\t1\t1\n", windows: "quote\n"},
		"unmanaged":     {current: "agentx\tquote\t0\t\n", windows: "quote\n"},
		"duplicate":     {current: "agentx\tquote\t0\t1\n", windows: "quote\nquote\n"},
	}
	for name, tmux := range tests {
		t.Run(name, func(t *testing.T) {
			if agentID, err := resolveAgentFromTmux(context.Background(), tmux); err == nil || agentID != "" {
				t.Fatalf("resolved ambiguous Agent %q err=%v", agentID, err)
			}
		})
	}
}

func TestConsoleUsesDefaultSocketAndRejectsInvalidOverrides(t *testing.T) {
	client := &testClient{attached: consoleapi.AttachResponse{WorkerStatus: domain.WorkerStatusOffline}}
	defaultSocket := filepath.Join(t.TempDir(), "run", "openagentx.sock")
	t.Setenv(localprofile.EnvSocketPath, defaultSocket)
	deps, _, stderr := consoleDeps(client, "", false)
	var usedSocket string
	deps.NewClient = func(socketPath string) (Client, error) {
		usedSocket = socketPath
		return client, nil
	}
	if code := Execute([]string{"attach", "--agent", "quote", "--once"}, deps); code != 0 {
		t.Fatalf("default socket attach code=%d stderr=%s", code, stderr.String())
	}
	if usedSocket != defaultSocket {
		t.Fatalf("Console socket=%q want=%q", usedSocket, defaultSocket)
	}

	for _, value := range []string{"", "relative/openagentx.sock", "~/openagentx.sock"} {
		clientCalls := 0
		deps, _, stderr = consoleDeps(client, "", false)
		deps.NewClient = func(string) (Client, error) {
			clientCalls++
			return client, nil
		}
		if code := Execute([]string{"attach", "--socket=" + value, "--agent", "quote", "--once"}, deps); code != 2 {
			t.Fatalf("invalid socket %q code=%d stderr=%s", value, code, stderr.String())
		}
		if clientCalls != 0 {
			t.Fatalf("invalid socket %q reached Console client", value)
		}
	}
}

func TestConsoleTTYCredentialAndHelpContracts(t *testing.T) {
	root := t.TempDir()
	t.Setenv(localprofile.EnvSocketPath, filepath.Join(root, "openagentx.sock"))
	t.Setenv(localprofile.EnvCredentialsPath, filepath.Join(root, "missing-credentials.json"))
	client := &testClient{}

	deps, _, stderr := consoleDeps(client, "", false)
	if code := Execute(nil, deps); code != 2 || !strings.Contains(stderr.String(), "interactive TTY") {
		t.Fatalf("non-interactive menu code/output mismatch: code=%d stderr=%s", code, stderr.String())
	}
	deps, _, stderr = consoleDeps(client, "", false)
	if code := Execute([]string{"login"}, deps); code != 2 || !strings.Contains(stderr.String(), "interactive TTY") {
		t.Fatalf("non-interactive login code/output mismatch: code=%d stderr=%s", code, stderr.String())
	}
	deps, _, stderr = consoleDeps(client, "", false)
	deps.NewCredentialStore = func(string) (CredentialStore, error) { return &testCredentialStore{missing: true}, nil }
	if code := Execute([]string{"logout"}, deps); code != 0 {
		t.Fatalf("missing credential code/output mismatch: code=%d stderr=%s", code, stderr.String())
	}

	secretPath := filepath.Join(root, "must-not-appear", "credentials.json")
	t.Setenv(localprofile.EnvCredentialsPath, secretPath)
	deps, out, _ := consoleDeps(client, "", false)
	if code := Execute([]string{"help"}, deps); code != 0 {
		t.Fatalf("Console help code=%d", code)
	}
	if strings.Contains(out.String(), secretPath) || strings.Contains(strings.ToLower(out.String()), "password=") {
		t.Fatalf("Console help leaked sensitive configuration: %s", out.String())
	}

	deps, _, stderr = consoleDeps(client, "", true)
	if code := Execute([]string{"login", "--content", "must-not-be-accepted"}, deps); code != 2 {
		t.Fatalf("login accepted unrelated legacy flag: code=%d stderr=%s", code, stderr.String())
	}
}

func TestLoginReadsPasswordOnlyFromTTYAndStoresInstallationCredential(t *testing.T) {
	root := t.TempDir()
	client := &testClient{installationID: "installation-login"}
	store := &testCredentialStore{missing: true}
	deps, out, stderr := consoleDeps(client, "", true)
	deps.ReadPassword = func(prompt string) (string, error) {
		if !strings.Contains(prompt, "password") {
			t.Fatalf("unexpected password prompt %q", prompt)
		}
		return "private-password", nil
	}
	deps.NewCredentialStore = func(string) (CredentialStore, error) { return store, nil }
	if code := Execute([]string{"login", "--socket", "/run/openagentx.sock", "--credentials", filepath.Join(root, "credentials.json")}, deps); code != 0 {
		t.Fatalf("login code=%d stderr=%s", code, stderr.String())
	}
	if !store.saved || store.credential.InstallationID != "installation-login" || store.credential.Token != "opaque-token" {
		t.Fatalf("stored credential=%+v saved=%v", store.credential, store.saved)
	}
	combined := out.String() + stderr.String()
	if strings.Contains(combined, "private-password") || strings.Contains(combined, "opaque-token") {
		t.Fatalf("login output leaked secret material: %s", combined)
	}
}

func TestAuthenticatedCommandLoadsCredentialWithoutPasswordPrompt(t *testing.T) {
	client := &testClient{attached: consoleapi.AttachResponse{WorkerStatus: domain.WorkerStatusOffline}}
	deps, _, stderr := consoleDeps(client, "", false)
	deps.ReadPassword = func(string) (string, error) { t.Fatal("authenticated command prompted for password"); return "", nil }
	if code := Execute([]string{"attach", "--socket", "/run/openagentx.sock", "--agent", "quote", "--once"}, deps); code != 0 {
		t.Fatalf("attach code=%d stderr=%s", code, stderr.String())
	}
	if !client.tokenSent || client.attachCount != 1 {
		t.Fatalf("credential/API path tokenSent=%v attach=%d", client.tokenSent, client.attachCount)
	}
}

func TestLogoutDeletesLocalCredentialWithoutSendingAcrossInstallationOrWhenOffline(t *testing.T) {
	for name, configure := range map[string]func(*Dependencies, *testClient, *testCredentialStore){
		"replacement": func(_ *Dependencies, client *testClient, _ *testCredentialStore) {
			client.installationID = "replacement-installation"
		},
		"offline": func(deps *Dependencies, _ *testClient, _ *testCredentialStore) {
			deps.NewClient = func(string) (Client, error) { return nil, fmt.Errorf("daemon unavailable") }
		},
	} {
		t.Run(name, func(t *testing.T) {
			client := &testClient{installationID: "installation-test"}
			store := &testCredentialStore{credential: credentialstore.Credential{SocketPath: "/run/openagentx.sock",
				InstallationID: "installation-test", Username: "owner", TokenID: "token-id", Token: "stored-secret", AbsoluteExpires: time.Now().Add(time.Hour)}}
			deps, _, stderr := consoleDeps(client, "", false)
			deps.NewCredentialStore = func(string) (CredentialStore, error) { return store, nil }
			configure(&deps, client, store)
			if code := Execute([]string{"logout", "--socket", "/run/openagentx.sock", "--credentials", filepath.Join(t.TempDir(), "credentials.json")}, deps); code != 0 {
				t.Fatalf("logout code=%d stderr=%s", code, stderr.String())
			}
			if !store.deleted || client.tokenSent || client.logoutCount != 0 {
				t.Fatalf("logout boundary deleted=%v tokenSent=%v remoteLogout=%d", store.deleted, client.tokenSent, client.logoutCount)
			}
			if !strings.Contains(stderr.String(), "local CLI credential deleted") {
				t.Fatalf("logout warning missing: %s", stderr.String())
			}
		})
	}
}
