package fleet

import (
	"bytes"
	"context"
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
	"openagentx/internal/domain"
	"openagentx/internal/localprofile"
)

type testTmux struct {
	session bool
	windows map[string]*fleetTestWindow
	calls   []string
	nextID  int
}

type fleetTestWindow struct {
	name    string
	panes   []int
	managed bool
	agentID string
}

func (t *testTmux) Run(_ context.Context, args ...string) (string, error) {
	t.calls = append(t.calls, strings.Join(args, " "))
	if args[0] == "has-session" && !t.session {
		return "", fmt.Errorf("missing")
	}
	switch args[0] {
	case "list-windows":
		ids := make([]string, 0, len(t.windows))
		for id := range t.windows {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		var output strings.Builder
		for _, id := range ids {
			fmt.Fprintf(&output, "%s\t%s\n", id, t.windows[id].name)
		}
		return output.String(), nil
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
	loginCount  int
	attached    map[string]consoleapi.AttachResponse
	sequences   map[string][]consoleapi.AttachResponse
	attachCalls map[string]int
	commands    []domain.WorkerCommandKind
	operations  []string
}

func (c *testConsole) Login(context.Context, string, string) error {
	c.loginCount++
	return nil
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

func writeFleetFixture(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "ROLE.md"), []byte("# Fleet test role\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, agentID := range []string{"quote", "risk"} {
		identity := fmt.Sprintf(`version: 1
agent_id: %s
principal_id: agent-%s
organization_id: default
display_name: %s
profile:
  instructions_path: ROLE.md
  workspace_root: .
  capabilities: [coding]
`, agentID, agentID, agentID)
		worker := fmt.Sprintf(`version: 1
agent_id: %s
transport: unix
unix_socket: /run/openagentx/openagentx.sock
runtime_backends:
  - backend_id: local
    adapter_id: fake
`, agentID)
		if err := os.WriteFile(filepath.Join(directory, agentID+".identity.yaml"), []byte(identity), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, agentID+".worker.yaml"), []byte(worker), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := `version: 1
agents:
  - agent_id: quote
    identity_file: quote.identity.yaml
    worker_config: quote.worker.yaml
    enabled: true
  - agent_id: risk
    identity_file: risk.identity.yaml
    worker_config: risk.worker.yaml
    enabled: false
`
	path := filepath.Join(directory, "fleet.yaml")
	if err := os.WriteFile(path, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFleetUpPreflightsWorkspaceStartsConsoleAndEnabledSystemdUnits(t *testing.T) {
	manifestPath := writeFleetFixture(t)
	validatedConfig, err := os.ReadFile(filepath.Join(filepath.Dir(manifestPath), "quote.worker.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	canonicalConfig := filepath.Join(t.TempDir(), "quote.yaml")
	if err := os.WriteFile(canonicalConfig, validatedConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	tmux := &testTmux{}
	var units []string
	deps := Dependencies{
		Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, Tmux: tmux,
		SystemdConfigPath: func(string) string { return canonicalConfig },
		RunSystemctl: func(_ context.Context, args ...string) (string, error) {
			units = append(units, strings.Join(args, " "))
			return "", nil
		},
	}
	if code := Execute([]string{"up", "--file", manifestPath, "--socket", "/run/openagentx/openagentx.sock"}, deps); code != 0 {
		t.Fatalf("Fleet up code=%d stderr=%s", code, deps.Err)
	}
	if !reflect.DeepEqual(units, []string{"start openagentx-worker@quote.service"}) {
		t.Fatalf("systemd starts=%v", units)
	}
	joined := strings.Join(tmux.calls, "\n")
	if !strings.Contains(joined, "console attach --socket /run/openagentx/openagentx.sock --username owner --agent quote") {
		t.Fatalf("workspace did not start formal Console: %v", tmux.calls)
	}
}

func TestFleetUsesProfileDefaultsAndRejectsInvalidOverrides(t *testing.T) {
	manifestPath := writeFleetFixture(t)
	profileHome := filepath.Dir(manifestPath)
	for _, name := range []string{localprofile.EnvFleetManifest, localprofile.EnvSocketPath, localprofile.EnvDatabasePath} {
		value, present := os.LookupEnv(name)
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if present {
				_ = os.Setenv(name, value)
			} else {
				_ = os.Unsetenv(name)
			}
		})
	}
	t.Setenv(localprofile.EnvHome, profileHome)
	validatedConfig, err := os.ReadFile(filepath.Join(profileHome, "quote.worker.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	canonicalConfig := filepath.Join(t.TempDir(), "quote.yaml")
	if err := os.WriteFile(canonicalConfig, validatedConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	tmux := &testTmux{}
	deps := Dependencies{
		Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, Tmux: tmux,
		SystemdConfigPath: func(string) string { return canonicalConfig },
		RunSystemctl:      func(context.Context, ...string) (string, error) { return "", nil },
	}
	if code := Execute([]string{"up"}, deps); code != 0 {
		t.Fatalf("default Fleet up code=%d stderr=%s", code, deps.Err)
	}
	defaultSocket := filepath.Join(profileHome, "run", "openagentx.sock")
	if calls := strings.Join(tmux.calls, "\n"); !strings.Contains(calls, "console attach --socket "+defaultSocket) {
		t.Fatalf("Fleet did not use profile socket %q: %s", defaultSocket, calls)
	}

	tests := [][]string{
		{"status", "--file="},
		{"status", "--file", "relative/fleet.yaml"},
		{"up", "--file", manifestPath, "--socket="},
		{"init", "--file", manifestPath, "--db", "relative/openagentx.db"},
	}
	for _, args := range tests {
		var stderr bytes.Buffer
		invalidDeps := Dependencies{Out: &bytes.Buffer{}, Err: &stderr, Tmux: &testTmux{}}
		if code := Execute(args, invalidDeps); code != 2 {
			t.Fatalf("invalid override %v code=%d stderr=%s", args, code, stderr.String())
		}
	}
}

func TestFleetUpRejectsMismatchedCanonicalConfigBeforeAnyMutation(t *testing.T) {
	manifestPath := writeFleetFixture(t)
	canonicalDirectory := t.TempDir()
	canonical := filepath.Join(canonicalDirectory, "quote.yaml")
	if err := os.WriteFile(canonical, []byte("version: 1\nagent_id: another\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tmux := &testTmux{}
	systemctlCalls := 0
	var stderr bytes.Buffer
	deps := Dependencies{
		Out: &bytes.Buffer{}, Err: &stderr, Tmux: tmux,
		SystemdConfigPath: func(string) string { return canonical },
		RunSystemctl: func(context.Context, ...string) (string, error) {
			systemctlCalls++
			return "", nil
		},
	}
	if code := Execute([]string{"up", "--file", manifestPath}, deps); code != 1 {
		t.Fatalf("mismatched canonical config code=%d stderr=%s", code, stderr.String())
	}
	if systemctlCalls != 0 {
		t.Fatalf("mismatched config started systemd %d times", systemctlCalls)
	}
	for _, call := range tmux.calls {
		if strings.Contains(call, "new-session") || strings.Contains(call, "new-window") || strings.Contains(call, "set-option") {
			t.Fatalf("mismatched config mutated tmux: %v", tmux.calls)
		}
	}
	if !strings.Contains(stderr.String(), "does not match canonical systemd config") {
		t.Fatalf("missing fail-closed config diagnostic: %s", stderr.String())
	}
}

func TestFleetUpConflictFailsBeforeTmuxOrSystemdMutation(t *testing.T) {
	manifestPath := writeFleetFixture(t)
	tmux := &testTmux{session: true, nextID: 2, windows: map[string]*fleetTestWindow{
		"@1": {name: "overview", panes: []int{0}, managed: true},
		"@2": {name: "quote", panes: []int{0}},
	}}
	systemctlCalls := 0
	deps := Dependencies{
		Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, Tmux: tmux,
		RunSystemctl: func(context.Context, ...string) (string, error) {
			systemctlCalls++
			return "", nil
		},
	}
	if code := Execute([]string{"up", "--file", manifestPath}, deps); code == 0 {
		t.Fatal("Fleet up accepted a conflicting tmux window")
	}
	if systemctlCalls != 0 {
		t.Fatalf("conflict started systemd units: %d", systemctlCalls)
	}
	for _, call := range tmux.calls {
		if strings.Contains(call, "new-window") || strings.Contains(call, "set-option") {
			t.Fatalf("conflict mutated tmux: %v", tmux.calls)
		}
	}
}

func TestFleetDownQueuesAllThenObservesBusyIdleAndMultipleAgentsUntilOffline(t *testing.T) {
	manifestPath := writeFleetFixture(t)
	start := time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC)
	now := start
	console := &testConsole{sequences: map[string][]consoleapi.AttachResponse{
		"quote": {
			{WorkerInstanceID: "worker-quote", Generation: 7, WorkerStatus: domain.WorkerStatusOnline, LeaseUntil: start.Add(time.Minute), ActiveRun: &consoleapi.RunSnapshot{RunID: "run-busy", TaskID: "task-busy", Status: domain.RunAttemptRunning, UpdatedAt: start}},
			{WorkerInstanceID: "worker-quote", Generation: 7, WorkerStatus: domain.WorkerStatusDraining, LeaseUntil: start.Add(time.Minute), ActiveRun: &consoleapi.RunSnapshot{RunID: "run-busy", TaskID: "task-busy", Status: domain.RunAttemptRunning, UpdatedAt: start.Add(time.Second)}},
			{WorkerInstanceID: "worker-quote", Generation: 7, WorkerStatus: domain.WorkerStatusDraining, LeaseUntil: start.Add(time.Minute)},
			{WorkerStatus: domain.WorkerStatusOffline},
		},
		"risk": {
			{WorkerInstanceID: "worker-risk", Generation: 4, WorkerStatus: domain.WorkerStatusOnline, LeaseUntil: start.Add(time.Minute)},
			{WorkerStatus: domain.WorkerStatusOffline},
		},
	}}
	var out, stderr bytes.Buffer
	deps := Dependencies{
		Out: &out, Err: &stderr, ReadPassword: func(string) (string, error) { return "password", nil },
		NewConsole: func(string) (consoleClient, error) { return console, nil },
		Now:        func() time.Time { return now },
		Wait: func(context.Context, time.Duration) error {
			now = now.Add(time.Second)
			return nil
		},
	}
	if code := Execute([]string{"down", "--file", manifestPath, "--socket", "/run/openagentx.sock"}, deps); code != 0 {
		t.Fatalf("Fleet down code=%d stderr=%s", code, stderr.String())
	}
	if !reflect.DeepEqual(console.commands, []domain.WorkerCommandKind{domain.WorkerCommandStop, domain.WorkerCommandStop}) || console.loginCount != 1 {
		t.Fatalf("graceful commands=%v logins=%d", console.commands, console.loginCount)
	}
	if len(console.operations) < 5 || !reflect.DeepEqual(console.operations[:4], []string{"attach:quote", "attach:risk", "command:worker-quote", "command:worker-risk"}) {
		t.Fatalf("Fleet did not queue every target before observing: %v", console.operations)
	}
	for _, required := range []string{`"run_id": "run-busy"`, `"worker_status": "draining"`, `"no_new_tasks": true`, `"elapsed": "`, "offline; graceful stop completed"} {
		if !strings.Contains(out.String(), required) {
			t.Fatalf("graceful observation missing %q: %s", required, out.String())
		}
	}
}

func TestFleetDownObservationCanBeInterruptedWithoutRevokingIntent(t *testing.T) {
	manifestPath := writeFleetFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	console := &testConsole{sequences: map[string][]consoleapi.AttachResponse{
		"quote": {{WorkerInstanceID: "worker-quote", Generation: 7, WorkerStatus: domain.WorkerStatusOnline}, {WorkerInstanceID: "worker-quote", Generation: 7, WorkerStatus: domain.WorkerStatusDraining}},
		"risk":  {{WorkerStatus: domain.WorkerStatusOffline}},
	}}
	var out, stderr bytes.Buffer
	deps := Dependencies{
		Out: &out, Err: &stderr, Context: ctx, ReadPassword: func(string) (string, error) { return "password", nil },
		NewConsole: func(string) (consoleClient, error) { return console, nil },
		Wait: func(waitContext context.Context, _ time.Duration) error {
			cancel()
			<-waitContext.Done()
			return waitContext.Err()
		},
	}
	if code := Execute([]string{"down", "--file", manifestPath, "--socket", "/run/openagentx.sock"}, deps); code != 0 {
		t.Fatalf("interrupted down code=%d stderr=%s", code, stderr.String())
	}
	if !reflect.DeepEqual(console.commands, []domain.WorkerCommandKind{domain.WorkerCommandStop}) || !strings.Contains(out.String(), "persisted graceful-stop intents remain active") {
		t.Fatalf("interruption revoked or obscured intent: commands=%v output=%s", console.commands, out.String())
	}
}

func TestFleetForceStopRemainsSeparateAndDoesNotUseGracefulObservation(t *testing.T) {
	manifestPath := writeFleetFixture(t)
	console := &testConsole{attached: map[string]consoleapi.AttachResponse{
		"quote": {WorkerInstanceID: "worker-quote", Generation: 7, WorkerStatus: domain.WorkerStatusOnline},
		"risk":  {WorkerStatus: domain.WorkerStatusOffline},
	}}
	var out, stderr bytes.Buffer
	deps := Dependencies{
		Out: &out, Err: &stderr, ReadPassword: func(string) (string, error) { return "password", nil },
		NewConsole: func(string) (consoleClient, error) { return console, nil },
	}
	if code := Execute([]string{"force-stop", "--file", manifestPath, "--socket", "/run/openagentx.sock"}, deps); code != 2 {
		t.Fatalf("unconfirmed force-stop code=%d", code)
	}
	if len(console.commands) != 0 {
		t.Fatalf("unconfirmed force-stop reached API: %v", console.commands)
	}
	if code := Execute([]string{"force-stop", "--file", manifestPath, "--socket", "/run/openagentx.sock", "--confirm-force-stop"}, deps); code != 0 {
		t.Fatalf("confirmed force-stop code=%d stderr=%s", code, stderr.String())
	}
	if !reflect.DeepEqual(console.commands, []domain.WorkerCommandKind{domain.WorkerCommandForceStop}) || console.loginCount != 1 {
		t.Fatalf("separate stop commands=%v logins=%d", console.commands, console.loginCount)
	}
	if strings.Contains(out.String(), "draining") || strings.Contains(out.String(), "graceful stop") {
		t.Fatalf("force-stop was presented as graceful: %s", out.String())
	}
}
