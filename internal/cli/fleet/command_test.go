package fleet

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	consoleclient "openagentx/internal/client/console"
	"openagentx/internal/credentialstore"
	"openagentx/internal/domain"
	fleetmodel "openagentx/internal/fleet"
)

type fleetTestWindow struct {
	name    string
	panes   []int
	managed bool
	agentID string
	dead    bool
}

type testTmux struct {
	session bool
	windows map[string]*fleetTestWindow
	calls   []string
	nextID  int
}

func (t *testTmux) Run(_ context.Context, args ...string) (string, error) {
	t.calls = append(t.calls, strings.Join(args, " "))
	if args[0] == "has-session" && !t.session {
		return "", fmt.Errorf("missing")
	}
	switch args[0] {
	case "display-message":
		target := strings.TrimSuffix(fleetArgAfter(args, "-t"), ".0")
		window := t.windows[target]
		if window == nil {
			return "", fmt.Errorf("window missing")
		}
		switch fleetArgAfter(args, "-F") {
		case "#{window_name}":
			return window.name + "\n", nil
		case "#{pane_dead}":
			if window.dead {
				return "1\n", nil
			}
			return "0\n", nil
		default:
			return "", fmt.Errorf("unexpected display format")
		}
	case "list-windows":
		ids := make([]string, 0, len(t.windows))
		for id := range t.windows {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		return strings.Join(ids, "\n") + "\n", nil
	case "list-panes":
		window := t.windows[fleetArgAfter(args, "-t")]
		var output strings.Builder
		for _, pane := range window.panes {
			fmt.Fprintf(&output, "%d\n", pane)
		}
		return output.String(), nil
	case "show-options":
		window := t.windows[fleetArgAfter(args, "-t")]
		var output strings.Builder
		if window.managed {
			fmt.Fprintln(&output, "@openagentx_managed 1")
		}
		if window.agentID != "" {
			fmt.Fprintf(&output, "@openagentx_agent_id %s\n", window.agentID)
		}
		return output.String(), nil
	case "new-session", "new-window":
		if t.windows == nil {
			t.windows = make(map[string]*fleetTestWindow)
		}
		t.session = true
		t.nextID++
		id := "@" + strconv.Itoa(t.nextID)
		t.windows[id] = &fleetTestWindow{name: fleetArgAfter(args, "-n"), panes: []int{0}}
		return id + "\n", nil
	case "set-option":
		window := t.windows[fleetArgAfter(args, "-t")]
		for index, arg := range args {
			switch arg {
			case "@openagentx_managed":
				window.managed = !fleetHasArg(args, "-u") && index+1 < len(args) && args[index+1] == "1"
			case "@openagentx_agent_id":
				window.agentID = ""
				if !fleetHasArg(args, "-u") && index+1 < len(args) {
					window.agentID = args[index+1]
				}
			}
		}
	case "respawn-pane":
		window := t.windows[strings.TrimSuffix(fleetArgAfter(args, "-t"), ".0")]
		if !fleetHasArg(args, "-k") && !window.dead {
			return "", fmt.Errorf("live pane")
		}
		window.dead = false
	}
	return "", nil
}

func fleetArgAfter(args []string, flag string) string {
	for index := range args {
		if args[index] == flag && index+1 < len(args) {
			return args[index+1]
		}
	}
	return ""
}

func fleetHasArg(args []string, expected string) bool {
	for _, arg := range args {
		if arg == expected {
			return true
		}
	}
	return false
}

type testConsole struct {
	probe       openapi.CLIInstallationResponse
	usedInstall string
	usedToken   string
	session     openapi.CLISessionResponse
	sessionErr  error
	options     []domain.ConsoleAgentOption
	attached    map[string]consoleapi.AttachResponse
	sequences   map[string][]consoleapi.AttachResponse
	attachCalls map[string]int
	commands    []domain.WorkerCommandKind
	operations  []string
}

func (c *testConsole) ProbeInstallation(context.Context) (openapi.CLIInstallationResponse, error) {
	return c.probe, nil
}
func (c *testConsole) UseCredential(_ context.Context, install, token string) error {
	c.usedInstall, c.usedToken = install, token
	return nil
}
func (c *testConsole) Session(context.Context) (openapi.CLISessionResponse, error) {
	return c.session, c.sessionErr
}
func (c *testConsole) ListAgentOptions(context.Context) ([]domain.ConsoleAgentOption, error) {
	return append([]domain.ConsoleAgentOption(nil), c.options...), nil
}
func (c *testConsole) Attach(_ context.Context, agentID, _ string) (consoleapi.AttachResponse, error) {
	if c.attachCalls == nil {
		c.attachCalls = make(map[string]int)
	}
	c.operations = append(c.operations, "attach:"+agentID)
	index := c.attachCalls[agentID]
	c.attachCalls[agentID]++
	if sequence := c.sequences[agentID]; len(sequence) > 0 {
		if index >= len(sequence) {
			index = len(sequence) - 1
		}
		response := sequence[index]
		if response.AgentID == "" {
			response.AgentID = agentID
		}
		return response, nil
	}
	response := c.attached[agentID]
	if response.AgentID == "" {
		response.AgentID = agentID
	}
	return response, nil
}
func (c *testConsole) WorkerCommand(_ context.Context, workerID string, _ int64, kind domain.WorkerCommandKind, _ string, _ bool) (openapi.WorkerCommandResponse, error) {
	c.commands = append(c.commands, kind)
	c.operations = append(c.operations, "command:"+workerID)
	return openapi.WorkerCommandResponse{Command: domain.WorkerCommand{State: domain.WorkerCommandPending}}, nil
}

type testCredentialStore struct {
	credential credentialstore.Credential
	deleted    []credentialstore.Credential
}

func (s *testCredentialStore) Load(socket, installation, _ string) (credentialstore.Credential, error) {
	if s.credential.SocketPath != socket || s.credential.InstallationID != installation {
		return credentialstore.Credential{}, credentialstore.ErrNotFound
	}
	return s.credential, nil
}
func (s *testCredentialStore) Delete(credential credentialstore.Credential) (bool, error) {
	s.deleted = append(s.deleted, credential)
	return true, nil
}

type fleetFixture struct {
	home, manifest, database, socket, workerDir, credentials, binary string
}

func newFleetFixture(t *testing.T) fleetFixture {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(home, ".openagentx")
	fixture := fleetFixture{home: home, manifest: filepath.Join(profile, "fleet.yaml"), database: filepath.Join(profile, "data", "openagentx.db"),
		socket: filepath.Join(profile, "run", "openagentx.sock"), workerDir: filepath.Join(profile, "workers"),
		credentials: filepath.Join(profile, "credentials.json"), binary: filepath.Join(home, ".local", "bin", "openagentx")}
	if err := os.MkdirAll(filepath.Dir(fixture.binary), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.binary, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (f fleetFixture) args(command string) []string {
	return []string{command, "--file", f.manifest, "--db", f.database, "--socket", f.socket, "--worker-dir", f.workerDir, "--credentials", f.credentials}
}

func ownerConsole(now time.Time) *testConsole {
	options := []domain.ConsoleAgentOption{
		{AgentID: "quote", OrganizationID: "default", DisplayName: "Quote", WorkerStatus: domain.WorkerStatusOffline},
		{AgentID: "risk", OrganizationID: "default", DisplayName: "Risk", WorkerStatus: domain.WorkerStatusOffline},
	}
	return &testConsole{probe: openapi.CLIInstallationResponse{InstallationID: "install-1"}, options: options,
		session: openapi.CLISessionResponse{InstallationID: "install-1", AbsoluteExpiresAt: now.Add(time.Hour),
			Principal: openapi.CLIPrincipal{TokenID: "token-1", Username: "owner", Roles: []string{"owner"}, Scopes: []string{"console.read", "fleet.lifecycle"}}}, attached: make(map[string]consoleapi.AttachResponse)}
}

func fixtureDeps(f fleetFixture, now time.Time, console *testConsole) (Dependencies, *testCredentialStore, *bytes.Buffer, *bytes.Buffer) {
	store := &testCredentialStore{credential: credentialstore.Credential{SocketPath: f.socket, InstallationID: "install-1", Username: "owner", TokenID: "token-1", Token: "opaque-test-token", AbsoluteExpires: now.Add(time.Hour)}}
	out, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	deps := Dependencies{Out: out, Err: stderr, In: strings.NewReader(""), IsInteractive: func() bool { return false }, Now: func() time.Time { return now },
		NewConsole: func(string) (consoleClient, error) { return console, nil }, NewCredentialStore: func(string) (credentialStore, error) { return store, nil },
		Tmux: &testTmux{}, UserHomeDir: func() (string, error) { return f.home, nil },
		RunSystemctl: func(context.Context, ...string) (string, error) { return "inactive", nil }, RunLoginctl: func(context.Context, ...string) (string, error) { return "yes", nil }}
	return deps, store, out, stderr
}

func writeWorker(t *testing.T, path, agentID, socket string) []byte {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	content := []byte(fmt.Sprintf("version: 1\nagent_id: %s\ntransport: unix\nunix_socket: %s\nruntime_backends:\n  - backend_id: local\n    adapter_id: fake\n", agentID, socket))
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return content
}

func writeManifest(t *testing.T, f fleetFixture, agents ...string) {
	t.Helper()
	manifest := fleetmodel.Manifest{Version: 1, Session: fleetmodel.SessionName}
	for _, agentID := range agents {
		path := filepath.Join(f.workerDir, agentID+".yaml")
		writeWorker(t, path, agentID, f.socket)
		manifest.Agents = append(manifest.Agents, fleetmodel.Agent{AgentID: agentID, WorkerConfig: path, Enabled: true})
	}
	encoded, err := fleetmodel.Encode(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(f.manifest), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.manifest, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFleetInitMissingManifestRequiresExplicitNonTTYSelectionAndImportsCanonicalConfig(t *testing.T) {
	now := time.Date(2026, 9, 15, 1, 0, 0, 0, time.UTC)
	f := newFleetFixture(t)
	console := ownerConsole(now)
	deps, _, out, stderr := fixtureDeps(f, now, console)
	if code := Execute(f.args("init"), deps); code != 1 || !strings.Contains(stderr.String(), "requires at least one explicit --agent") {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	source := filepath.Join(t.TempDir(), "quote.yaml")
	content := writeWorker(t, source, "quote", f.socket)
	args := append(f.args("init"), "--agent", "quote", "--worker-config", "quote="+source)
	if code := Execute(args, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	canonical, err := os.ReadFile(filepath.Join(f.workerDir, "quote.yaml"))
	if err != nil || !bytes.Equal(canonical, content) {
		t.Fatalf("canonical=%q err=%v", canonical, err)
	}
	manifest, err := fleetmodel.LoadFile(f.manifest)
	if err != nil || manifest.Agents[0].WorkerConfig != filepath.Join(f.workerDir, "quote.yaml") {
		t.Fatalf("manifest=%+v err=%v", manifest, err)
	}
	if strings.Contains(out.String(), "opaque-test-token") || strings.Contains(strings.Join(deps.Tmux.(*testTmux).calls, " "), "opaque-test-token") {
		t.Fatal("credential leaked")
	}
}

func TestFleetInitTTYUsesExplicitMultiSelectAndExistingCanonicalConfigs(t *testing.T) {
	now := time.Now().UTC()
	f := newFleetFixture(t)
	console := ownerConsole(now)
	console.options = make([]domain.ConsoleAgentOption, 125)
	for index := range console.options {
		console.options[index] = domain.ConsoleAgentOption{AgentID: fmt.Sprintf("agent-%03d", index), OrganizationID: "default", DisplayName: fmt.Sprintf("Agent %03d", index), WorkerStatus: domain.WorkerStatusOffline}
	}
	writeWorker(t, filepath.Join(f.workerDir, "agent-000.yaml"), "agent-000", f.socket)
	writeWorker(t, filepath.Join(f.workerDir, "agent-124.yaml"), "agent-124", f.socket)
	deps, _, _, stderr := fixtureDeps(f, now, console)
	deps.IsInteractive = func() bool { return true }
	selectorCalls := 0
	deps.SelectAgents = func(options []domain.ConsoleAgentOption) ([]string, error) {
		selectorCalls++
		if len(options) != 125 {
			t.Fatalf("options=%v", options)
		}
		return []string{"agent-124", "agent-000"}, nil
	}
	if code := Execute(f.args("init"), deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	manifest, err := fleetmodel.LoadFile(f.manifest)
	if err != nil || selectorCalls != 1 || manifest.Agents[0].AgentID != "agent-000" || manifest.Agents[1].AgentID != "agent-124" {
		t.Fatalf("manifest=%+v calls=%d err=%v", manifest, selectorCalls, err)
	}
}

func TestFleetInstallationMismatchNeverSendsStoredToken(t *testing.T) {
	now := time.Now().UTC()
	f := newFleetFixture(t)
	console := ownerConsole(now)
	console.probe.InstallationID = "replacement-installation"
	deps, _, _, _ := fixtureDeps(f, now, console)
	if _, _, err := authenticateFleetClient(context.Background(), f.paths(), domain.WebRoleViewer, domain.CLIScopeConsoleRead, deps); err == nil {
		t.Fatal("installation mismatch was accepted")
	}
	if console.usedToken != "" {
		t.Fatal("stored token was sent to a replacement installation")
	}
}

func TestFleetUsesResolvedProfilePathsWithoutExplicitFlags(t *testing.T) {
	now := time.Now().UTC()
	f := newFleetFixture(t)
	writeManifest(t, f, "quote")
	console := ownerConsole(now)
	console.session.Principal.Roles = []string{"viewer"}
	console.session.Principal.Scopes = []string{"console.read"}
	deps, _, _, stderr := fixtureDeps(f, now, console)
	profile := filepath.Dir(f.manifest)
	t.Setenv("OPENAGENTX_HOME", profile)
	for _, name := range []string{"OPENAGENTX_FLEET_MANIFEST", "OPENAGENTX_DATABASE_PATH", "OPENAGENTX_SOCKET_PATH", "OPENAGENTX_WORKER_CONFIG_DIR", "OPENAGENTX_CREDENTIALS_PATH"} {
		old, present := os.LookupEnv(name)
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if present {
				_ = os.Setenv(name, old)
			} else {
				_ = os.Unsetenv(name)
			}
		})
	}
	if code := Execute([]string{"workspace"}, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if console.usedInstall != "install-1" {
		t.Fatal("default profile did not authenticate through the configured socket")
	}
}

func TestFleetInitConflictAndAtomicFailureAreNonDestructive(t *testing.T) {
	now := time.Now().UTC()
	f := newFleetFixture(t)
	console := ownerConsole(now)
	existing := writeWorker(t, filepath.Join(f.workerDir, "quote.yaml"), "quote", f.socket)
	source := filepath.Join(t.TempDir(), "quote.yaml")
	writeWorker(t, source, "quote", f.socket)
	if err := os.WriteFile(source, append([]byte(nil), bytes.ReplaceAll(existing, []byte("adapter_id: fake"), []byte("adapter_id: other"))...), 0o600); err != nil {
		t.Fatal(err)
	}
	deps, _, _, stderr := fixtureDeps(f, now, console)
	args := append(f.args("init"), "--agent", "quote", "--worker-config", "quote="+source)
	if code := Execute(args, deps); code != 1 {
		t.Fatalf("conflict code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "existing sha256") || !strings.Contains(stderr.String(), "requested sha256") {
		t.Fatalf("missing safe conflict summary: %s", stderr.String())
	}
	current, _ := os.ReadFile(filepath.Join(f.workerDir, "quote.yaml"))
	if !bytes.Equal(current, existing) {
		t.Fatal("conflict changed canonical config")
	}
	if _, err := os.Stat(f.manifest); !os.IsNotExist(err) {
		t.Fatalf("conflict installed manifest: %v", err)
	}

	f2 := newFleetFixture(t)
	source2 := filepath.Join(t.TempDir(), "quote.yaml")
	writeWorker(t, source2, "quote", f2.socket)
	deps2, _, _, stderr2 := fixtureDeps(f2, now, ownerConsole(now))
	canonical := filepath.Join(f2.workerDir, "quote.yaml")
	deps2.BeforeAtomicRename = func(path string) error {
		if path == canonical {
			return errors.New("injected crash")
		}
		return nil
	}
	args2 := append(f2.args("init"), "--agent", "quote", "--worker-config", "quote="+source2)
	if code := Execute(args2, deps2); code != 1 {
		t.Fatalf("atomic code=%d stderr=%s", code, stderr2.String())
	}
	if _, err := os.Stat(canonical); !os.IsNotExist(err) {
		t.Fatalf("failed import replaced config: %v", err)
	}
}

func TestFleetInitValidatesAndInstallsSameCapturedWorkerBytes(t *testing.T) {
	now := time.Now().UTC()
	f := newFleetFixture(t)
	source := filepath.Join(t.TempDir(), "quote.yaml")
	original := writeWorker(t, source, "quote", f.socket)
	if err := os.Chmod(source, 0o644); err != nil {
		t.Fatal(err)
	}
	deps, _, _, stderr := fixtureDeps(f, now, ownerConsole(now))
	canonical := filepath.Join(f.workerDir, "quote.yaml")
	deps.BeforeAtomicRename = func(path string) error {
		if path != canonical {
			return nil
		}
		return os.WriteFile(source, []byte("version: invalid\n"), 0o600)
	}
	args := append(f.args("init"), "--agent", "quote", "--worker-config", "quote="+source)
	if code := Execute(args, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	installed, err := os.ReadFile(canonical)
	if err != nil || !bytes.Equal(installed, original) {
		t.Fatalf("installed bytes differ from validated capture: content=%q err=%v", installed, err)
	}
}

func TestFleetInitRejectsWorkerSourceSymlinkAndCanonicalSwap(t *testing.T) {
	now := time.Now().UTC()
	t.Run("source symlink", func(t *testing.T) {
		f := newFleetFixture(t)
		target := filepath.Join(t.TempDir(), "target.yaml")
		writeWorker(t, target, "quote", f.socket)
		source := filepath.Join(t.TempDir(), "source.yaml")
		if err := os.Symlink(target, source); err != nil {
			t.Fatal(err)
		}
		deps, _, _, stderr := fixtureDeps(f, now, ownerConsole(now))
		args := append(f.args("init"), "--agent", "quote", "--worker-config", "quote="+source)
		if code := Execute(args, deps); code != 1 {
			t.Fatalf("code=%d stderr=%s", code, stderr.String())
		}
		if _, err := os.Stat(f.manifest); !os.IsNotExist(err) {
			t.Fatalf("unsafe source installed manifest: %v", err)
		}
	})

	t.Run("canonical replaced before rename", func(t *testing.T) {
		f := newFleetFixture(t)
		source := filepath.Join(t.TempDir(), "source.yaml")
		writeWorker(t, source, "quote", f.socket)
		canonical := filepath.Join(f.workerDir, "quote.yaml")
		target := filepath.Join(t.TempDir(), "target.yaml")
		if err := os.WriteFile(target, []byte("do not replace\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		deps, _, _, stderr := fixtureDeps(f, now, ownerConsole(now))
		deps.BeforeAtomicRename = func(path string) error {
			if path != canonical {
				return nil
			}
			return os.Symlink(target, canonical)
		}
		args := append(f.args("init"), "--agent", "quote", "--worker-config", "quote="+source)
		if code := Execute(args, deps); code != 1 {
			t.Fatalf("code=%d stderr=%s", code, stderr.String())
		}
		content, err := os.ReadFile(target)
		if err != nil || string(content) != "do not replace\n" {
			t.Fatalf("swap target changed: content=%q err=%v", content, err)
		}
		if _, err := os.Stat(f.manifest); !os.IsNotExist(err) {
			t.Fatalf("canonical swap installed manifest: %v", err)
		}
	})
}

func TestFleetPrepareRejectsUnsafeManifestFile(t *testing.T) {
	now := time.Now().UTC()
	for name, install := range map[string]func(t *testing.T, f fleetFixture){
		"broad permissions": func(t *testing.T, f fleetFixture) {
			writeManifest(t, f, "quote")
			if err := os.Chmod(f.manifest, 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"symlink": func(t *testing.T, f fleetFixture) {
			writeManifest(t, f, "quote")
			target := filepath.Join(t.TempDir(), "fleet.yaml")
			content, err := os.ReadFile(f.manifest)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, content, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(f.manifest); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, f.manifest); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFleetFixture(t)
			install(t, f)
			deps, _, _, stderr := fixtureDeps(f, now, ownerConsole(now))
			if code := Execute(f.args("status"), deps); code != 1 {
				t.Fatalf("code=%d stderr=%s", code, stderr.String())
			}
		})
	}
}

func TestFleetInitRejectsRelativeImportedWorkerPathSemantics(t *testing.T) {
	now := time.Now().UTC()
	f := newFleetFixture(t)
	source := filepath.Join(t.TempDir(), "quote.yaml")
	content := fmt.Sprintf("version: 1\nagent_id: quote\ntransport: unix\nunix_socket: %s\nnetwork_materialization_dir: relative/network\nruntime_backends:\n  - backend_id: local\n    adapter_id: fake\n", f.socket)
	if err := os.WriteFile(source, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	deps, _, _, stderr := fixtureDeps(f, now, ownerConsole(now))
	args := append(f.args("init"), "--agent", "quote", "--worker-config", "quote="+source)
	if code := Execute(args, deps); code != 1 || !strings.Contains(stderr.String(), "must be absolute before importing") {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}

func TestFleetRepeatedExplicitInitIsIdempotentAndDifferentManifestConflicts(t *testing.T) {
	now := time.Now().UTC()
	f := newFleetFixture(t)
	writeManifest(t, f, "quote")
	deps, _, _, stderr := fixtureDeps(f, now, ownerConsole(now))
	if code := Execute(append(f.args("init"), "--agent", "quote"), deps); code != 0 {
		t.Fatalf("same init code=%d stderr=%s", code, stderr.String())
	}
	before, _ := os.ReadFile(f.manifest)
	writeWorker(t, filepath.Join(f.workerDir, "risk.yaml"), "risk", f.socket)
	if code := Execute(append(f.args("init"), "--agent", "risk"), deps); code != 1 {
		t.Fatalf("different init code=%d stderr=%s", code, stderr.String())
	}
	after, _ := os.ReadFile(f.manifest)
	if !bytes.Equal(before, after) || !strings.Contains(stderr.String(), "existing sha256") {
		t.Fatalf("manifest conflict was destructive or unclear: %s", stderr.String())
	}
}

func TestFleetInitRecoversFromCanonicalConfigInstalledBeforeManifestFailure(t *testing.T) {
	now := time.Now().UTC()
	f := newFleetFixture(t)
	source := filepath.Join(t.TempDir(), "quote.yaml")
	writeWorker(t, source, "quote", f.socket)
	deps, _, _, stderr := fixtureDeps(f, now, ownerConsole(now))
	deps.BeforeAtomicRename = func(path string) error {
		if path == f.manifest {
			return errors.New("injected manifest crash")
		}
		return nil
	}
	args := append(f.args("init"), "--agent", "quote", "--worker-config", "quote="+source)
	if code := Execute(args, deps); code != 1 {
		t.Fatalf("first code=%d stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(f.workerDir, "quote.yaml")); err != nil {
		t.Fatalf("canonical config was not durably installed: %v", err)
	}
	if _, err := os.Stat(f.manifest); !os.IsNotExist(err) {
		t.Fatalf("failed manifest write installed target: %v", err)
	}
	deps.BeforeAtomicRename = nil
	if code := Execute(append(f.args("init"), "--agent", "quote"), deps); code != 0 {
		t.Fatalf("retry code=%d stderr=%s", code, stderr.String())
	}
}

func TestFleetUpPreflightsActualUserUnitBeforeTmuxAndStartsWithUserManager(t *testing.T) {
	now := time.Now().UTC()
	f := newFleetFixture(t)
	writeManifest(t, f, "quote")
	console := ownerConsole(now)
	tmux := &testTmux{}
	deps, _, _, stderr := fixtureDeps(f, now, console)
	deps.Tmux = tmux
	var systemctlCalls []string
	deps.RunSystemctl = func(_ context.Context, args ...string) (string, error) {
		call := strings.Join(args, " ")
		systemctlCalls = append(systemctlCalls, call)
		if strings.Contains(call, "LoadState") {
			return "loaded", nil
		}
		if strings.Contains(call, "ExecStart") {
			return fmt.Sprintf("{ path=%s ; argv[]=%s worker run --config %s ; ignore_errors=no ; }", f.binary, f.binary, filepath.Join(f.workerDir, "quote.yaml")), nil
		}
		if strings.Contains(call, "WorkingDirectory") {
			return f.home, nil
		}
		if strings.Contains(call, "EnvironmentFiles") {
			return filepath.Join(f.workerDir, "quote.env") + " (ignore_errors=yes)", nil
		}
		return "", nil
	}
	if code := Execute(f.args("up"), deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if systemctlCalls[len(systemctlCalls)-1] != "--user start openagentx-worker@quote.service" {
		t.Fatalf("calls=%v", systemctlCalls)
	}
	joined := strings.Join(tmux.calls, "\n")
	if !strings.Contains(joined, f.binary+" console attach --socket "+f.socket+" --credentials "+f.credentials+" --agent quote") || strings.Contains(joined, "username") {
		t.Fatalf("Console argv=%s", joined)
	}
}

func TestFleetUpRejectsUnitMismatchBeforeAnyMutation(t *testing.T) {
	now := time.Now().UTC()
	f := newFleetFixture(t)
	writeManifest(t, f, "quote")
	console := ownerConsole(now)
	tmux := &testTmux{}
	deps, _, _, stderr := fixtureDeps(f, now, console)
	deps.Tmux = tmux
	deps.RunSystemctl = func(_ context.Context, args ...string) (string, error) {
		if strings.Contains(strings.Join(args, " "), "LoadState") {
			return "loaded", nil
		}
		return "{ argv[]=/tmp/openagentx worker run --config /tmp/other.yaml ; }", nil
	}
	if code := Execute(f.args("up"), deps); code != 1 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	for _, call := range tmux.calls {
		if strings.HasPrefix(call, "new-") || strings.HasPrefix(call, "set-option") {
			t.Fatalf("unit mismatch mutated tmux: %v", tmux.calls)
		}
	}
}

func TestFleetWorkspaceAndUpRejectMissingCanonicalBinaryBeforeHostMutation(t *testing.T) {
	now := time.Now().UTC()
	for _, command := range []string{"workspace", "up"} {
		t.Run(command, func(t *testing.T) {
			f := newFleetFixture(t)
			writeManifest(t, f, "quote")
			if err := os.Remove(f.binary); err != nil {
				t.Fatal(err)
			}
			tmux := &testTmux{}
			deps, _, _, stderr := fixtureDeps(f, now, ownerConsole(now))
			deps.Tmux = tmux
			systemctlCalls := 0
			deps.RunSystemctl = func(context.Context, ...string) (string, error) {
				systemctlCalls++
				return "", nil
			}
			if code := Execute(f.args(command), deps); code != 1 {
				t.Fatalf("code=%d stderr=%s", code, stderr.String())
			}
			if len(tmux.calls) != 0 || systemctlCalls != 0 {
				t.Fatalf("missing canonical binary mutated host: tmux=%v systemctl_calls=%d", tmux.calls, systemctlCalls)
			}
		})
	}
}

func TestFleetInitRejectsInvalidCanonicalBinaryBeforeFileOrTmuxMutation(t *testing.T) {
	now := time.Now().UTC()
	for name, invalidate := range map[string]func(t *testing.T, path string){
		"missing": func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		},
		"non executable": func(t *testing.T, path string) {
			if err := os.Chmod(path, 0o600); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFleetFixture(t)
			source := filepath.Join(t.TempDir(), "quote.yaml")
			writeWorker(t, source, "quote", f.socket)
			invalidate(t, f.binary)
			tmux := &testTmux{}
			deps, _, _, stderr := fixtureDeps(f, now, ownerConsole(now))
			deps.Tmux = tmux
			args := append(f.args("init"), "--agent", "quote", "--worker-config", "quote="+source)
			if code := Execute(args, deps); code != 1 {
				t.Fatalf("code=%d stderr=%s", code, stderr.String())
			}
			canonical := filepath.Join(f.workerDir, "quote.yaml")
			if _, err := os.Stat(canonical); !os.IsNotExist(err) {
				t.Fatalf("invalid binary created canonical config: %v", err)
			}
			if _, err := os.Stat(f.manifest); !os.IsNotExist(err) {
				t.Fatalf("invalid binary created manifest: %v", err)
			}
			if len(tmux.calls) != 0 {
				t.Fatalf("invalid binary mutated tmux: %v", tmux.calls)
			}
		})
	}
}

func TestFleetStatusAndDownDoNotRequireCanonicalBinary(t *testing.T) {
	now := time.Now().UTC()
	f := newFleetFixture(t)
	writeManifest(t, f, "quote")
	if err := os.Remove(f.binary); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"status", "down"} {
		t.Run(command, func(t *testing.T) {
			deps, _, _, stderr := fixtureDeps(f, now, ownerConsole(now))
			if code := Execute(f.args(command), deps); code != 0 {
				t.Fatalf("code=%d stderr=%s", code, stderr.String())
			}
		})
	}
}

func TestFleetPreflightRejectsUnsafeWorkerEnvironmentFile(t *testing.T) {
	now := time.Now().UTC()
	f := newFleetFixture(t)
	writeManifest(t, f, "quote")
	environment := filepath.Join(f.workerDir, "quote.env")
	if err := os.WriteFile(environment, []byte("RUNTIME_OPTION=value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps, _, _, stderr := fixtureDeps(f, now, ownerConsole(now))
	if code := Execute(f.args("status"), deps); code != 1 || !strings.Contains(stderr.String(), "permissions no wider than 0600") {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}

func TestFleetWorkspaceFlagRespawnsOnlyManagedDeadPaneZero(t *testing.T) {
	now := time.Now().UTC()
	f := newFleetFixture(t)
	writeManifest(t, f, "quote")
	console := ownerConsole(now)
	console.session.Principal.Roles = []string{"viewer"}
	console.session.Principal.Scopes = []string{"console.read"}
	tmux := &testTmux{session: true, nextID: 3, windows: map[string]*fleetTestWindow{
		"@1": {name: "overview", panes: []int{0}, managed: true},
		"@2": {name: "quote", panes: []int{0, 1, 2}, managed: true, agentID: "quote", dead: true},
		"@3": {name: "scratch", panes: []int{0, 1}, dead: true},
	}}
	deps, _, out, stderr := fixtureDeps(f, now, console)
	deps.Tmux = tmux
	if code := Execute(append(f.args("workspace"), "--respawn-dead"), deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	calls := strings.Join(tmux.calls, "\n")
	if !strings.Contains(calls, "respawn-pane -t @2.0 -- "+f.binary+" console attach") || strings.Contains(calls, "respawn-pane -k -t @2.0") || strings.Contains(calls, "@3.0 --") {
		t.Fatalf("unsafe respawn calls=%s", calls)
	}
	if !strings.Contains(out.String(), `"Respawned": [`) || !reflect.DeepEqual(tmux.windows["@2"].panes, []int{0, 1, 2}) {
		t.Fatalf("out=%s panes=%v", out.String(), tmux.windows["@2"].panes)
	}
}

func TestFleetStatusUsesReadScopeAndAllSystemctlCallsUseUser(t *testing.T) {
	now := time.Now().UTC()
	f := newFleetFixture(t)
	writeManifest(t, f, "quote")
	console := ownerConsole(now)
	console.session.Principal.Roles = []string{"viewer"}
	console.session.Principal.Scopes = []string{"console.read"}
	deps, _, out, stderr := fixtureDeps(f, now, console)
	var calls []string
	deps.RunSystemctl = func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "inactive", nil
	}
	if code := Execute(f.args("status"), deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !reflect.DeepEqual(calls, []string{"--user is-active openagentx-worker@quote.service"}) || !strings.Contains(out.String(), `"agent_id": "quote"`) {
		t.Fatalf("calls=%v out=%s", calls, out.String())
	}
}

func TestFleetAuthenticationChecksRoleScopeExpiryAndDeletesOnlyUnauthenticated(t *testing.T) {
	now := time.Now().UTC()
	f := newFleetFixture(t)
	for name, mutate := range map[string]func(*testConsole, *testCredentialStore){
		"wrong role":    func(c *testConsole, _ *testCredentialStore) { c.session.Principal.Roles = []string{"viewer"} },
		"wrong scope":   func(c *testConsole, _ *testCredentialStore) { c.session.Principal.Scopes = []string{"console.read"} },
		"expired local": func(_ *testConsole, s *testCredentialStore) { s.credential.AbsoluteExpires = now.Add(-time.Second) },
		"revoked": func(c *testConsole, _ *testCredentialStore) {
			c.sessionErr = &consoleclient.APIError{StatusCode: 401, Code: openapi.ErrorCLIUnauthenticated, Message: "invalid CLI credentials"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			console := ownerConsole(now)
			deps, store, _, _ := fixtureDeps(f, now, console)
			mutate(console, store)
			_, _, err := authenticateFleetClient(context.Background(), f.paths(), domain.WebRoleOwner, domain.CLIScopeFleetLifecycle, deps)
			if err == nil {
				t.Fatal("invalid session accepted")
			}
			shouldDelete := name == "expired local" || name == "revoked"
			if (len(store.deleted) != 0) != shouldDelete {
				t.Fatalf("deleted=%d", len(store.deleted))
			}
		})
	}
}

func (f fleetFixture) paths() fleetPaths {
	return fleetPaths{manifest: f.manifest, database: f.database, socket: f.socket, workerDir: f.workerDir, credentials: f.credentials}
}

func TestFleetDownQueuesAllThenObservesBusyIdleAndMultipleAgentsUntilOffline(t *testing.T) {
	now := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)
	start := now
	f := newFleetFixture(t)
	writeManifest(t, f, "quote", "risk")
	console := ownerConsole(now)
	console.options[0].WorkerStatus, console.options[0].Generation = domain.WorkerStatusOnline, 7
	console.options[1].WorkerStatus, console.options[1].Generation = domain.WorkerStatusOnline, 4
	console.sequences = map[string][]consoleapi.AttachResponse{
		"quote": {{WorkerInstanceID: "worker-quote", Generation: 7, WorkerStatus: domain.WorkerStatusOnline, ActiveRun: &consoleapi.RunSnapshot{RunID: "run-busy", TaskID: "task-busy", Status: domain.RunAttemptRunning, UpdatedAt: start}}, {WorkerInstanceID: "worker-quote", Generation: 7, WorkerStatus: domain.WorkerStatusDraining, ActiveRun: &consoleapi.RunSnapshot{RunID: "run-busy", Status: domain.RunAttemptRunning}}, {WorkerStatus: domain.WorkerStatusOffline}},
		"risk":  {{WorkerInstanceID: "worker-risk", Generation: 4, WorkerStatus: domain.WorkerStatusOnline}, {WorkerStatus: domain.WorkerStatusOffline}},
	}
	deps, _, out, stderr := fixtureDeps(f, now, console)
	deps.Now = func() time.Time { return now }
	deps.Wait = func(context.Context, time.Duration) error { now = now.Add(time.Second); return nil }
	if code := Execute(f.args("down"), deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !reflect.DeepEqual(console.commands, []domain.WorkerCommandKind{domain.WorkerCommandStop, domain.WorkerCommandStop}) || !reflect.DeepEqual(console.operations[:4], []string{"attach:quote", "attach:risk", "command:worker-quote", "command:worker-risk"}) {
		t.Fatalf("operations=%v", console.operations)
	}
	for _, expected := range []string{`"run_id": "run-busy"`, `"worker_status": "draining"`, `"no_new_tasks": true`, "offline; graceful stop completed"} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("missing %q: %s", expected, out.String())
		}
	}
}

func TestFleetDownInterruptionKeepsIntentAndForceRequiresTwoConfirmations(t *testing.T) {
	now := time.Now().UTC()
	f := newFleetFixture(t)
	writeManifest(t, f, "quote")
	console := ownerConsole(now)
	console.options[0].WorkerStatus, console.options[0].Generation = domain.WorkerStatusOnline, 7
	console.sequences = map[string][]consoleapi.AttachResponse{"quote": {{WorkerInstanceID: "worker-quote", Generation: 7, WorkerStatus: domain.WorkerStatusOnline}, {WorkerInstanceID: "worker-quote", Generation: 7, WorkerStatus: domain.WorkerStatusDraining}}}
	ctx, cancel := context.WithCancel(context.Background())
	deps, _, out, stderr := fixtureDeps(f, now, console)
	deps.Context = ctx
	deps.Wait = func(waitContext context.Context, _ time.Duration) error {
		cancel()
		<-waitContext.Done()
		return waitContext.Err()
	}
	if code := Execute(f.args("down"), deps); code != 0 || !strings.Contains(out.String(), "persisted graceful-stop intents remain active") {
		t.Fatalf("code=%d out=%s stderr=%s", code, out.String(), stderr.String())
	}

	forceConsole := ownerConsole(now)
	forceConsole.attached["quote"] = consoleapi.AttachResponse{WorkerInstanceID: "worker-quote", Generation: 7, WorkerStatus: domain.WorkerStatusOnline}
	forceDeps, _, forceOut, forceErr := fixtureDeps(f, now, forceConsole)
	if code := Execute(append(f.args("force-stop"), "--confirm-force-stop"), forceDeps); code != 2 || len(forceConsole.commands) != 0 {
		t.Fatalf("single confirm code=%d commands=%v", code, forceConsole.commands)
	}
	args := append(f.args("force-stop"), "--confirm-force-stop", "--confirm-active-run-uncertain")
	if code := Execute(args, forceDeps); code != 0 || !reflect.DeepEqual(forceConsole.commands, []domain.WorkerCommandKind{domain.WorkerCommandForceStop}) {
		t.Fatalf("code=%d commands=%v err=%s", code, forceConsole.commands, forceErr.String())
	}
	if strings.Contains(forceOut.String(), "graceful") || strings.Contains(forceOut.String(), "draining") {
		t.Fatalf("force disguised as graceful: %s", forceOut.String())
	}
}

func TestFleetHelpHasNoPasswordOrUsernameAndInvalidOverridesFailClosed(t *testing.T) {
	var out bytes.Buffer
	if code := Execute([]string{"help"}, Dependencies{Out: &out, Err: &out}); code != 0 {
		t.Fatal(code)
	}
	if strings.Contains(out.String(), "owner-username") || strings.Contains(strings.ToLower(out.String()), "password input") {
		t.Fatalf("legacy auth help remains: %s", out.String())
	}
	for _, args := range [][]string{{"status", "--file="}, {"status", "--worker-dir", "relative"}, {"status", "--credentials="}} {
		var stderr bytes.Buffer
		if code := Execute(args, Dependencies{Out: &bytes.Buffer{}, Err: &stderr}); code != 2 {
			t.Fatalf("args=%v code=%d stderr=%s", args, code, stderr.String())
		}
	}
}
