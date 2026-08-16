package cli_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agentbus/internal/cli"
	"agentbus/internal/client"
	"agentbus/internal/connector"
	"agentbus/internal/domain"
	"agentbus/internal/server"
	"agentbus/internal/service"
	"agentbus/internal/store"
)

type mockRunner struct{}

func (m *mockRunner) Run(ctx context.Context, stdin string, name string, args ...string) (string, error) {
	return "%51 0\n", nil
}

func setupDaemon(t *testing.T) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "agentbus-cli-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(dir, "cli.db")
	socketPath := filepath.Join(dir, "cli.sock")

	st, err := store.OpenSQLite(dbPath)
	if err != nil {
		os.RemoveAll(dir)
		t.Fatalf("failed to open store: %v", err)
	}

	conn := connector.NewTmuxConnector(&mockRunner{})
	svc := service.NewService(st, conn, nil)
	srv := server.NewServer(svc, socketPath, nil)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	for i := 0; i < 50; i++ {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	cleanup := func() {
		cancel()
		<-errCh
		st.Close()
		os.RemoveAll(dir)
	}

	return socketPath, cleanup
}

func TestCLIBasicHelpAndVersion(t *testing.T) {
	if code := cli.Execute([]string{"help"}); code != 0 {
		t.Fatalf("expected 0 for help, got %d", code)
	}
	if code := cli.Execute([]string{"version"}); code != 0 {
		t.Fatalf("expected 0 for version, got %d", code)
	}
	if code := cli.Execute([]string{"nonexistent"}); code != 1 {
		t.Fatalf("expected 1 for nonexistent command, got %d", code)
	}
}

func attachAndReadyCLIAgent(t *testing.T, socketPath string, dir string, id string, role string, conn string, address string) {
	t.Helper()
	roleFile := filepath.Join(dir, id+"_ROLE.md")
	_ = os.WriteFile(roleFile, []byte("# Role\nInstructions"), 0644)

	yamlContent := fmt.Sprintf(`version: 1
id: %s
role: %s
runtime: agy
connector: %s
address: "%s"
workspace: "."
instructions: "%s_ROLE.md"
capabilities:
  - %s
`, id, role, conn, address, id, role)
	cfgFile := filepath.Join(dir, id+".yaml")
	_ = os.WriteFile(cfgFile, []byte(yamlContent), 0644)

	code := cli.Execute([]string{"agent", "attach", "--config", cfgFile, "--no-notify", "--socket", socketPath})
	if code != 0 {
		t.Fatalf("agent attach failed for %s", id)
	}

	code = cli.Execute([]string{"session", "ready", "--agent", id, "--generation", "1", "--socket", socketPath})
	if code != 0 {
		t.Fatalf("session ready failed for %s", id)
	}
}

func TestCLIFullWorkflow(t *testing.T) {
	socketPath, cleanup := setupDaemon(t)
	defer cleanup()

	dir, err := os.MkdirTemp("", "cli-wf-manifests-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	// 1. Attach and ready orchestrator
	attachAndReadyCLIAgent(t, socketPath, dir, "orchestrator", "orchestrator", "tmux", "%50")

	// 2. Attach and ready quote
	attachAndReadyCLIAgent(t, socketPath, dir, "quote", "quote", "tmux", "%51")

	// Attach and ready third-party agent for auth test
	attachAndReadyCLIAgent(t, socketPath, dir, "other_agent", "worker", "none", "")

	// 3. List agents
	code := cli.Execute([]string{
		"agent", "list",
		"--socket", socketPath,
	})
	if code != 0 {
		t.Fatalf("agent list failed with code %d", code)
	}

	// 4. Get agent
	code = cli.Execute([]string{
		"agent", "get", "orchestrator",
		"--socket", socketPath,
	})
	if code != 0 {
		t.Fatalf("agent get failed with code %d", code)
	}

	// 5. Submit task
	code = cli.Execute([]string{
		"task", "submit",
		"--from", "orchestrator",
		"--to", "quote",
		"--idempotency-key", "cli-workflow-1",
		"--content", "Check Quote Service CLI",
		"--socket", socketPath,
	})
	if code != 0 {
		t.Fatalf("task submit failed with code %d", code)
	}

	// Query task ID via client or list
	c := client.NewClient(socketPath)
	tasks, err := c.ListTasks(context.Background(), "quote", "")
	if err != nil || len(tasks) == 0 {
		t.Fatalf("failed to list tasks via client: %v", err)
	}
	taskID := tasks[0].ID

	// 6. Get task without --agent -> should fail
	code = cli.Execute([]string{
		"task", "get", taskID,
		"--socket", socketPath,
	})
	if code == 0 {
		t.Fatalf("task get without --agent should fail")
	}

	// Get task with unauthorized agent -> should fail
	code = cli.Execute([]string{
		"task", "get", taskID,
		"--agent", "other_agent",
		"--socket", socketPath,
	})
	if code == 0 {
		t.Fatalf("task get with unauthorized agent should fail")
	}

	// Get task with authorized agent quote -> should succeed
	code = cli.Execute([]string{
		"task", "get", taskID,
		"--agent", "quote",
		"--socket", socketPath,
	})
	if code != 0 {
		t.Fatalf("task get failed with code %d", code)
	}

	// 7. Ack task
	code = cli.Execute([]string{
		"task", "ack", taskID,
		"--agent", "quote",
		"--socket", socketPath,
	})
	if code != 0 {
		t.Fatalf("task ack failed with code %d", code)
	}

	// 8. Update task status
	code = cli.Execute([]string{
		"task", "status", taskID,
		"--agent", "quote",
		"--message", "Checking Quote Service APIs",
		"--socket", socketPath,
	})
	if code != 0 {
		t.Fatalf("task status update failed with code %d", code)
	}

	// 9. Send supplemental message
	code = cli.Execute([]string{
		"task", "send", taskID,
		"--from", "orchestrator",
		"--content", "Also verify /portfolio endpoint",
		"--socket", socketPath,
	})
	if code != 0 {
		t.Fatalf("task send failed with code %d", code)
	}

	// Verify supplemental message can be retrieved via task get
	detail, err := c.GetTask(context.Background(), taskID, "quote")
	if err != nil {
		t.Fatalf("failed to get task after send: %v", err)
	}
	if len(detail.Messages) < 2 {
		t.Fatalf("expected at least 2 messages (instruction + supplement), got %d", len(detail.Messages))
	}
	var foundSupplement bool
	for _, m := range detail.Messages {
		if m.Kind == domain.MessageKindSupplement && m.Content == "Also verify /portfolio endpoint" {
			foundSupplement = true
			break
		}
	}
	if !foundSupplement {
		t.Fatalf("supplemental message not found in task detail messages: %+v", detail.Messages)
	}

	// 10. Complete task
	code = cli.Execute([]string{
		"task", "complete", taskID,
		"--agent", "quote",
		"--result", "All Quote APIs verified successfully",
		"--socket", socketPath,
	})
	if code != 0 {
		t.Fatalf("task complete failed with code %d", code)
	}

	// 11. Watch completed task (with --agent orchestrator)
	code = cli.Execute([]string{
		"task", "watch", taskID,
		"--agent", "orchestrator",
		"--after", "0",
		"--timeout", "1s",
		"--socket", socketPath,
	})
	if code != 0 {
		t.Fatalf("task watch failed with code %d", code)
	}
}

func TestCLIFailAndCancelWorkflow(t *testing.T) {
	socketPath, cleanup := setupDaemon(t)
	defer cleanup()

	dir, err := os.MkdirTemp("", "cli-fail-manifests-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	attachAndReadyCLIAgent(t, socketPath, dir, "c", "orchestrator", "none", "")
	attachAndReadyCLIAgent(t, socketPath, dir, "q", "quote", "none", "")

	// Test Cancel on Queued task
	_ = cli.Execute([]string{
		"task", "submit",
		"--from", "c",
		"--to", "q",
		"--idempotency-key", "cancel-demo",
		"--content", "To cancel",
		"--socket", socketPath,
	})

	c := client.NewClient(socketPath)
	tasks, _ := c.ListTasks(context.Background(), "q", "")
	taskID := tasks[0].ID

	// Cancel task
	code := cli.Execute([]string{
		"task", "cancel", taskID,
		"--agent", "c",
		"--socket", socketPath,
	})
	if code != 0 {
		t.Fatalf("task cancel failed with code %d", code)
	}

	// Submit another task for Fail testing
	_ = cli.Execute([]string{
		"task", "submit",
		"--from", "c",
		"--to", "q",
		"--idempotency-key", "fail-demo",
		"--content", "To fail",
		"--socket", socketPath,
	})

	tasks2, _ := c.ListTasks(context.Background(), "q", "queued")
	taskID2 := tasks2[0].ID

	// Ack task2
	_ = cli.Execute([]string{"task", "ack", taskID2, "--agent", "q", "--socket", socketPath})

	// Fail task2
	code = cli.Execute([]string{
		"task", "fail", taskID2,
		"--agent", "q",
		"--error", "Quote service timed out",
		"--socket", socketPath,
	})
	if code != 0 {
		t.Fatalf("task fail failed with code %d", code)
	}
}

func TestCLIBootstrapAndSessionWorkflow(t *testing.T) {
	socketPath, cleanup := setupDaemon(t)
	defer cleanup()

	dir, err := os.MkdirTemp("", "agent-manifest-cli-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	rolePath := filepath.Join(dir, "ROLE.md")
	_ = os.WriteFile(rolePath, []byte("# Worker Role\nDo work"), 0644)

	yamlContent := `version: 1
id: cli-worker
role: worker
runtime: agy
connector: none
workspace: "."
instructions: "ROLE.md"
capabilities:
  - test
`
	configPath := filepath.Join(dir, "agent.yaml")
	_ = os.WriteFile(configPath, []byte(yamlContent), 0644)

	// 1. Test whoami
	code := cli.Execute([]string{"agent", "whoami", "--config", configPath})
	if code != 0 {
		t.Fatalf("agent whoami failed with code %d", code)
	}

	// 2. Test attach
	code = cli.Execute([]string{"agent", "attach", "--config", configPath, "--no-notify", "--socket", socketPath})
	if code != 0 {
		t.Fatalf("agent attach failed with code %d", code)
	}

	// 3. Test session show
	code = cli.Execute([]string{"session", "show", "--agent", "cli-worker", "--socket", socketPath})
	if code != 0 {
		t.Fatalf("session show failed with code %d", code)
	}

	// 4. Test session ready
	code = cli.Execute([]string{"session", "ready", "--agent", "cli-worker", "--generation", "1", "--socket", socketPath})
	if code != 0 {
		t.Fatalf("session ready failed with code %d", code)
	}

	// 5. Test bootstrap
	code = cli.Execute([]string{"agent", "bootstrap", "--id", "cli-worker", "--socket", socketPath})
	if code != 0 {
		t.Fatalf("agent bootstrap failed with code %d", code)
	}
}

func TestCLILaunchStrictBehavior(t *testing.T) {
	socketPath, cleanup := setupDaemon(t)
	defer cleanup()

	dir, err := os.MkdirTemp("", "agent-launch-cli-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	rolePath := filepath.Join(dir, "ROLE.md")
	_ = os.WriteFile(rolePath, []byte("# Launch Worker\nInstructions"), 0644)

	yamlContent := `version: 1
id: launch-worker
role: worker
runtime: agy
connector: tmux
address: "%51"
workspace: "."
instructions: "ROLE.md"
capabilities:
  - launch
`
	configPath := filepath.Join(dir, "agent.yaml")
	_ = os.WriteFile(configPath, []byte(yamlContent), 0644)

	// Save existing TMUX_PANE
	origPane := os.Getenv("TMUX_PANE")
	defer func() {
		if origPane != "" {
			_ = os.Setenv("TMUX_PANE", origPane)
		} else {
			_ = os.Unsetenv("TMUX_PANE")
		}
	}()

	// 1. Missing TMUX_PANE -> must fail before launching
	_ = os.Unsetenv("TMUX_PANE")
	code := cli.Execute([]string{
		"agent", "launch",
		"--config", configPath,
		"--socket", socketPath,
		"--", "sleep", "1",
	})
	if code == 0 {
		t.Fatalf("expected non-zero exit code when $TMUX_PANE is missing")
	}

	// 2. Address conflict: TMUX_PANE=%51 vs --address %99 -> must fail before launching
	_ = os.Setenv("TMUX_PANE", "%51")
	code = cli.Execute([]string{
		"agent", "launch",
		"--config", configPath,
		"--address", "%99",
		"--socket", socketPath,
		"--", "sleep", "1",
	})
	if code == 0 {
		t.Fatalf("expected non-zero exit code for pane address conflict")
	}

	// 3. Child process exits prematurely before bootstrap delay -> must fail
	_ = os.Setenv("TMUX_PANE", "%51")
	code = cli.Execute([]string{
		"agent", "launch",
		"--config", configPath,
		"--bootstrap-delay", "500ms",
		"--socket", socketPath,
		"--", "sh", "-c", "exit 0",
	})
	if code == 0 {
		t.Fatalf("expected non-zero exit code when child exits before attach delay")
	}

	// 4. Successful attach and child exit code propagation (exit code 42)
	_ = os.Setenv("TMUX_PANE", "%51")
	code = cli.Execute([]string{
		"agent", "launch",
		"--config", configPath,
		"--bootstrap-delay", "50ms",
		"--socket", socketPath,
		"--", "sh", "-c", "sleep 0.2; exit 42",
	})
	if code != 42 {
		t.Fatalf("expected exit code 42 from child process, got %d", code)
	}

	// Verify session was registered in daemon
	c := client.NewClient(socketPath)
	sessResp, err := c.GetSession(context.Background(), "launch-worker")
	if err != nil {
		t.Fatalf("GetSession for launched worker failed: %v", err)
	}
	if sessResp.Session.Generation != 1 || sessResp.Agent.ID != "launch-worker" {
		t.Fatalf("unexpected launched session: %+v", sessResp)
	}
}

type slowRunner struct {
	delay time.Duration
}

func (s *slowRunner) Run(ctx context.Context, stdin string, name string, args ...string) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(s.delay):
	}
	return "%51 0\n", nil
}

func setupCustomDaemon(t *testing.T, runner connector.Runner) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "agentbus-cli-custom-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(dir, "cli.db")
	socketPath := filepath.Join(dir, "cli.sock")

	st, err := store.OpenSQLite(dbPath)
	if err != nil {
		os.RemoveAll(dir)
		t.Fatalf("failed to open store: %v", err)
	}

	var conn *connector.TmuxConnector
	if runner != nil {
		conn = connector.NewTmuxConnector(runner)
	}
	svc := service.NewService(st, conn, nil)
	srv := server.NewServer(svc, socketPath, nil)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	for i := 0; i < 50; i++ {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	cleanup := func() {
		cancel()
		<-errCh
		st.Close()
		os.RemoveAll(dir)
	}

	return socketPath, cleanup
}

func createLaunchTestConfigFile(t *testing.T, id string, pane string) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "agent-launch-cfg-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	rolePath := filepath.Join(dir, "ROLE.md")
	_ = os.WriteFile(rolePath, []byte("# Role Spec\nDo work"), 0644)
	yamlContent := fmt.Sprintf(`version: 1
id: %s
role: worker
runtime: agy
connector: tmux
address: "%s"
workspace: "."
instructions: "ROLE.md"
capabilities:
  - test
`, id, pane)
	configPath := filepath.Join(dir, "agent.yaml")
	_ = os.WriteFile(configPath, []byte(yamlContent), 0644)
	return configPath, func() { os.RemoveAll(dir) }
}

func TestCLILaunchChildExitsDuringDelayedAttach(t *testing.T) {
	// Daemon with 300ms delayed attach
	socketPath, cleanup := setupCustomDaemon(t, &slowRunner{delay: 300 * time.Millisecond})
	defer cleanup()

	configPath, cleanCfg := createLaunchTestConfigFile(t, "slow-agent", "%51")
	defer cleanCfg()

	origPane := os.Getenv("TMUX_PANE")
	defer func() {
		if origPane != "" {
			_ = os.Setenv("TMUX_PANE", origPane)
		} else {
			_ = os.Unsetenv("TMUX_PANE")
		}
	}()
	_ = os.Setenv("TMUX_PANE", "%51")

	// Child exits immediately (0s), while attach takes 300ms
	code := cli.Execute([]string{
		"agent", "launch",
		"--config", configPath,
		"--bootstrap-delay", "0s",
		"--socket", socketPath,
		"--", "sh", "-c", "exit 0",
	})
	if code == 0 {
		t.Fatalf("expected non-zero exit code when child exits before delayed attach finishes")
	}
}

func TestCLILaunchDaemonReturnsSkippedDispositionFailsClosed(t *testing.T) {
	// Daemon without connector (runner == nil) -> attach returns disposition: skipped
	socketPath, cleanup := setupCustomDaemon(t, nil)
	defer cleanup()

	configPath, cleanCfg := createLaunchTestConfigFile(t, "skipped-agent", "%51")
	defer cleanCfg()

	origPane := os.Getenv("TMUX_PANE")
	defer func() {
		if origPane != "" {
			_ = os.Setenv("TMUX_PANE", origPane)
		} else {
			_ = os.Unsetenv("TMUX_PANE")
		}
	}()
	_ = os.Setenv("TMUX_PANE", "%51")

	// Child is running (sleeps 5s)
	code := cli.Execute([]string{
		"agent", "launch",
		"--config", configPath,
		"--bootstrap-delay", "10ms",
		"--socket", socketPath,
		"--", "sh", "-c", "sleep 5; exit 0",
	})
	if code == 0 {
		t.Fatalf("expected non-zero exit code when attach returns disposition: skipped")
	}
}

func TestCLILaunchAttachAPIFailureFailsClosed(t *testing.T) {
	configPath, cleanCfg := createLaunchTestConfigFile(t, "fail-api-agent", "%51")
	defer cleanCfg()

	origPane := os.Getenv("TMUX_PANE")
	defer func() {
		if origPane != "" {
			_ = os.Setenv("TMUX_PANE", origPane)
		} else {
			_ = os.Unsetenv("TMUX_PANE")
		}
	}()
	_ = os.Setenv("TMUX_PANE", "%51")

	// Invalid socket path that cannot be connected
	deadSocket := filepath.Join(os.TempDir(), "nonexistent-dead.sock")

	// Child is running (sleeps 5s)
	code := cli.Execute([]string{
		"agent", "launch",
		"--config", configPath,
		"--bootstrap-delay", "10ms",
		"--socket", deadSocket,
		"--", "sh", "-c", "sleep 5; exit 0",
	})
	if code == 0 {
		t.Fatalf("expected non-zero exit code when attach API fails")
	}
}

func runWithPipes(stdinContent string, args []string) (int, string, string) {
	oldStdin := os.Stdin
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	defer func() {
		os.Stdin = oldStdin
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	// stdin pipe
	inR, inW, _ := os.Pipe()
	go func() {
		_, _ = inW.Write([]byte(stdinContent))
		_ = inW.Close()
	}()
	os.Stdin = inR

	// stdout pipe
	outR, outW, _ := os.Pipe()
	os.Stdout = outW

	// stderr pipe
	errR, errW, _ := os.Pipe()
	os.Stderr = errW

	code := cli.Execute(args)

	_ = outW.Close()
	_ = errW.Close()

	outBytes, _ := io.ReadAll(outR)
	errBytes, _ := io.ReadAll(errR)

	return code, strings.TrimSpace(string(outBytes)), strings.TrimSpace(string(errBytes))
}

func TestCLIRuntimeAGYHookValidation(t *testing.T) {
	// 1. Missing --event
	code, _, errOut := runWithPipes("{}", []string{"runtime", "agy-hook"})
	if code == 0 || !strings.Contains(errOut, "flag --event is required") {
		t.Fatalf("expected non-zero exit and error message for missing --event, got code=%d, err=%s", code, errOut)
	}

	// 2. Invalid --event
	code, _, errOut = runWithPipes("{}", []string{"runtime", "agy-hook", "--event", "InvalidEvent"})
	if code == 0 || !strings.Contains(errOut, "invalid --event") {
		t.Fatalf("expected non-zero exit for invalid --event, got code=%d, err=%s", code, errOut)
	}

	// 3. Stdin empty
	code, _, errOut = runWithPipes("", []string{"runtime", "agy-hook", "--event", "PreToolUse"})
	if code == 0 || !strings.Contains(errOut, "stdin is empty") {
		t.Fatalf("expected non-zero exit for empty stdin, got code=%d, err=%s", code, errOut)
	}

	// 4. Stdin invalid JSON
	code, _, errOut = runWithPipes("not-valid-json", []string{"runtime", "agy-hook", "--event", "PreToolUse"})
	if code == 0 || !strings.Contains(errOut, "JSON") {
		t.Fatalf("expected non-zero exit for invalid JSON, got code=%d, err=%s", code, errOut)
	}

	// 5. Stdin null object
	code, _, errOut = runWithPipes("null", []string{"runtime", "agy-hook", "--event", "PreToolUse"})
	if code == 0 || !strings.Contains(errOut, "JSON") {
		t.Fatalf("expected non-zero exit for null JSON, got code=%d, err=%s", code, errOut)
	}

	// 6. Stdin oversized (> 1 MiB)
	oversized := "{\"data\":\"" + strings.Repeat("x", 1024*1024+10) + "\"}"
	code, _, errOut = runWithPipes(oversized, []string{"runtime", "agy-hook", "--event", "PreToolUse"})
	if code == 0 || !strings.Contains(errOut, "exceeds maximum allowed size") {
		t.Fatalf("expected non-zero exit for oversized stdin, got code=%d, err=%s", code, errOut)
	}

	// 7. Stdin trailing tokens and non-object values
	invalidInputs := []string{
		`{"a":1} {"b":2}`,
		`{"a":1} trailing garbage`,
		`[1, 2, 3]`,
		`"primitive-string"`,
		`12345`,
		`true`,
	}
	for _, inp := range invalidInputs {
		code, _, errOut = runWithPipes(inp, []string{"runtime", "agy-hook", "--event", "PreToolUse"})
		if code == 0 || errOut == "" {
			t.Fatalf("expected non-zero exit for invalid stdin '%s', got code=%d, err=%s", inp, code, errOut)
		}
	}
}

func TestCLIRuntimeAGYHookUnmanagedAgentDegradation(t *testing.T) {
	// Clean environment
	origAgentID := os.Getenv("AGENTBUS_AGENT_ID")
	origPane := os.Getenv("TMUX_PANE")
	defer func() {
		if origAgentID != "" {
			_ = os.Setenv("AGENTBUS_AGENT_ID", origAgentID)
		} else {
			_ = os.Unsetenv("AGENTBUS_AGENT_ID")
		}
		if origPane != "" {
			_ = os.Setenv("TMUX_PANE", origPane)
		} else {
			_ = os.Unsetenv("TMUX_PANE")
		}
	}()
	_ = os.Unsetenv("AGENTBUS_AGENT_ID")
	_ = os.Unsetenv("TMUX_PANE")

	// 1. Unmanaged AGY outside tmux, no env -> neutral {} and exit 0
	code, stdout, errOut := runWithPipes(`{"conversationId":"conv-unmanaged-1"}`, []string{
		"runtime", "agy-hook",
		"--event", "Stop",
	})
	if code != 0 || stdout != "{}" {
		t.Fatalf("expected neutral {} for unmanaged agent outside tmux, got code=%d, stdout=%s, err=%s", code, stdout, errOut)
	}

	// 2. Unmanaged AGY with dead socket -> neutral {} and exit 0
	deadSocket := filepath.Join(os.TempDir(), "nonexistent-unmanaged.sock")
	code, stdout, errOut = runWithPipes(`{"conversationId":"conv-unmanaged-2"}`, []string{
		"runtime", "agy-hook",
		"--event", "PostInvocation",
		"--socket", deadSocket,
	})
	if code != 0 || stdout != "{}" {
		t.Fatalf("expected neutral {} for unmanaged agent with dead socket, got code=%d, stdout=%s, err=%s", code, stdout, errOut)
	}

	// 3. Unmanaged AGY in tmux pane not registered in daemon -> neutral {} and exit 0
	socketPath, cleanup := setupDaemon(t)
	defer cleanup()

	_ = os.Setenv("TMUX_PANE", "%99")
	code, stdout, errOut = runWithPipes(`{"conversationId":"conv-unmanaged-3"}`, []string{
		"runtime", "agy-hook",
		"--event", "Stop",
		"--socket", socketPath,
	})
	if code != 0 || stdout != "{}" {
		t.Fatalf("expected neutral {} for unregistered TMUX_PANE in auto mode, got code=%d, stdout=%s, err=%s", code, stdout, errOut)
	}
}

func TestCLIRuntimeAGYHookAgentAndTaskResolution(t *testing.T) {
	socketPath, cleanup := setupDaemon(t)
	defer cleanup()

	c := client.NewClient(socketPath)
	ctx := context.Background()

	dir, err := os.MkdirTemp("", "agy-hook-agent-res-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	roleOrch := filepath.Join(dir, "orch_ROLE.md")
	_ = os.WriteFile(roleOrch, []byte("# Orch\nRole"), 0644)
	roleQuote := filepath.Join(dir, "quote_ROLE.md")
	_ = os.WriteFile(roleQuote, []byte("# Quote\nRole"), 0644)

	// 1. Attach & ready orchestrator (codex, %50)
	orchAgent := &domain.Agent{ID: "orchestrator", Role: "orchestrator", Connector: domain.ConnectorTmux, Address: "%50"}
	orchProf := &domain.AgentProfile{AgentID: "orchestrator", ManifestVersion: 1, Runtime: "codex", Workspace: dir, ConfigPath: filepath.Join(dir, "c.yaml"), InstructionsPath: roleOrch, Capabilities: []string{"c"}}
	sessOrch, _ := c.AttachAgent(ctx, service.AttachAgentRequest{Agent: orchAgent, Profile: orchProf, NoNotify: true})
	_, _ = c.ReadySession(ctx, "orchestrator", sessOrch.Session.Generation)

	// 2. Attach & ready quote-service (agy, %51)
	quoteAgent := &domain.Agent{ID: "quote-service", Role: "quote", Connector: domain.ConnectorTmux, Address: "%51"}
	quoteProf := &domain.AgentProfile{AgentID: "quote-service", ManifestVersion: 1, Runtime: "agy", Workspace: dir, ConfigPath: filepath.Join(dir, "q.yaml"), InstructionsPath: roleQuote, Capabilities: []string{"q"}}
	sessQuote, _ := c.AttachAgent(ctx, service.AttachAgentRequest{Agent: quoteAgent, Profile: quoteProf, NoNotify: true})
	_, _ = c.ReadySession(ctx, "quote-service", sessQuote.Session.Generation)

	// 3. Submit task from orchestrator to quote-service
	submitResp, err := c.SubmitTask(ctx, service.SubmitTaskRequest{
		SenderAgentID:  "orchestrator",
		TargetAgentID:  "quote-service",
		IdempotencyKey: "agy-hook-res-1",
		Content:        "Work for quote",
	})
	if err != nil {
		t.Fatalf("SubmitTask failed: %v", err)
	}
	taskID := submitResp.Task.ID

	// Clean env on exit
	origAgentID := os.Getenv("AGENTBUS_AGENT_ID")
	origPane := os.Getenv("TMUX_PANE")
	defer func() {
		if origAgentID != "" {
			_ = os.Setenv("AGENTBUS_AGENT_ID", origAgentID)
		} else {
			_ = os.Unsetenv("AGENTBUS_AGENT_ID")
		}
		if origPane != "" {
			_ = os.Setenv("TMUX_PANE", origPane)
		} else {
			_ = os.Unsetenv("TMUX_PANE")
		}
	}()

	// Case A: Explicit --agent and --task
	code, stdout, errOut := runWithPipes(`{"tool_name":"read_file"}`, []string{
		"runtime", "agy-hook",
		"--event", "PreToolUse",
		"--agent", "quote-service",
		"--task", taskID,
		"--socket", socketPath,
	})
	if code != 0 || stdout != "{}" {
		t.Fatalf("Case A failed: code=%d, stdout=%s, stderr=%s", code, stdout, errOut)
	}

	// Case B: Explicit --task target mismatch for non-Stop -> returns 1
	code, _, errOut = runWithPipes(`{}`, []string{
		"runtime", "agy-hook",
		"--event", "PreToolUse",
		"--agent", "orchestrator",
		"--task", taskID,
		"--socket", socketPath,
	})
	if code == 0 || !strings.Contains(errOut, "does not match resolved agent") {
		t.Fatalf("Case B expected mismatch error, got code=%d, err=%s", code, errOut)
	}

	// Case B2: Explicit --task target mismatch for Stop -> fail closed continue
	code, stdout, errOut = runWithPipes(`{}`, []string{
		"runtime", "agy-hook",
		"--event", "Stop",
		"--agent", "orchestrator",
		"--task", taskID,
		"--socket", socketPath,
	})
	if code != 0 || !strings.Contains(stdout, `"decision": "continue"`) {
		t.Fatalf("Case B2 expected continue on target mismatch during Stop, got code=%d, stdout=%s, err=%s", code, stdout, errOut)
	}

	// Case C: AGENTBUS_AGENT_ID auto-resolution and auto task resolution
	_ = os.Setenv("AGENTBUS_AGENT_ID", "quote-service")
	_ = os.Unsetenv("TMUX_PANE")
	code, stdout, errOut = runWithPipes(`{"tool_name":"write_file"}`, []string{
		"runtime", "agy-hook",
		"--event", "PostToolUse",
		"--socket", socketPath,
	})
	if code != 0 || stdout != "{}" {
		t.Fatalf("Case C failed: code=%d, stdout=%s, stderr=%s", code, stdout, errOut)
	}

	// Case D: TMUX_PANE auto-resolution (%51 -> quote-service)
	_ = os.Unsetenv("AGENTBUS_AGENT_ID")
	_ = os.Setenv("TMUX_PANE", "%51")
	code, stdout, errOut = runWithPipes(`{"invocation_id":"inv-1"}`, []string{
		"runtime", "agy-hook",
		"--event", "PreInvocation",
		"--socket", socketPath,
	})
	if code != 0 || stdout != "{}" {
		t.Fatalf("Case D failed: code=%d, stdout=%s, stderr=%s", code, stdout, errOut)
	}

	// Case E: Active task selection ignores sender-only tasks
	_ = os.Setenv("AGENTBUS_AGENT_ID", "orchestrator")
	_ = os.Unsetenv("TMUX_PANE")
	// orchestrator has 0 active tasks as target (it only sent taskID)
	code, stdout, errOut = runWithPipes(`{}`, []string{
		"runtime", "agy-hook",
		"--event", "PreInvocation",
		"--socket", socketPath,
	})
	if code != 0 || stdout != "{}" {
		t.Fatalf("Case E failed: code=%d, stdout=%s, stderr=%s", code, stdout, errOut)
	}
}

func TestCLIRuntimeAGYHookStopGate(t *testing.T) {
	socketPath, cleanup := setupDaemon(t)
	defer cleanup()

	c := client.NewClient(socketPath)
	ctx := context.Background()

	dir, err := os.MkdirTemp("", "agy-hook-stop-gate-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	roleOrch := filepath.Join(dir, "orch_ROLE.md")
	_ = os.WriteFile(roleOrch, []byte("# Orch\nRole"), 0644)
	roleQuote := filepath.Join(dir, "quote_ROLE.md")
	_ = os.WriteFile(roleQuote, []byte("# Quote\nRole"), 0644)

	orchAgent := &domain.Agent{ID: "orchestrator", Role: "orchestrator", Connector: domain.ConnectorNone}
	orchProf := &domain.AgentProfile{AgentID: "orchestrator", ManifestVersion: 1, Runtime: "codex", Workspace: dir, ConfigPath: filepath.Join(dir, "c.yaml"), InstructionsPath: roleOrch, Capabilities: []string{"c"}}
	sessOrch, _ := c.AttachAgent(ctx, service.AttachAgentRequest{Agent: orchAgent, Profile: orchProf, NoNotify: true})
	_, _ = c.ReadySession(ctx, "orchestrator", sessOrch.Session.Generation)

	quoteAgent := &domain.Agent{ID: "quote-service", Role: "quote", Connector: domain.ConnectorNone}
	quoteProf := &domain.AgentProfile{AgentID: "quote-service", ManifestVersion: 1, Runtime: "agy", Workspace: dir, ConfigPath: filepath.Join(dir, "q.yaml"), InstructionsPath: roleQuote, Capabilities: []string{"q"}}
	sessQuote, _ := c.AttachAgent(ctx, service.AttachAgentRequest{Agent: quoteAgent, Profile: quoteProf, NoNotify: true})
	_, _ = c.ReadySession(ctx, "quote-service", sessQuote.Session.Generation)

	// 1. Submit task (status: queued)
	submitResp, err := c.SubmitTask(ctx, service.SubmitTaskRequest{
		SenderAgentID:  "orchestrator",
		TargetAgentID:  "quote-service",
		IdempotencyKey: "stop-gate-task-1",
		Content:        "Stop gate test task",
	})
	if err != nil {
		t.Fatalf("SubmitTask failed: %v", err)
	}
	taskID := submitResp.Task.ID

	// Case A: Stop on active task -> must return decision: continue and exit 0
	code, stdout, errOut := runWithPipes(`{"conversationId":"conv-stop-1","reason":"assistant_idle"}`, []string{
		"runtime", "agy-hook",
		"--event", "Stop",
		"--agent", "quote-service",
		"--task", taskID,
		"--socket", socketPath,
	})
	if code != 0 {
		t.Fatalf("Case A expected exit code 0, got %d (err: %s)", code, errOut)
	}
	if !strings.Contains(stdout, `"decision": "continue"`) || !strings.Contains(stdout, "still active") {
		t.Fatalf("Case A expected decision: continue in stdout, got: %s", stdout)
	}

	// Validate parseable single JSON object
	var respA map[string]any
	if err := json.Unmarshal([]byte(stdout), &respA); err != nil {
		t.Fatalf("Case A stdout is not valid JSON: %v", err)
	}

	// Ack task to make it running
	_, _ = c.AckTask(ctx, taskID, "quote-service")

	// Case A2: Stop on running task -> still returns decision: continue
	code, stdout, _ = runWithPipes(`{"conversationId":"conv-stop-1"}`, []string{
		"runtime", "agy-hook",
		"--event", "Stop",
		"--agent", "quote-service",
		"--task", taskID,
		"--socket", socketPath,
	})
	if code != 0 || !strings.Contains(stdout, `"decision": "continue"`) {
		t.Fatalf("Case A2 expected decision: continue on running task, got code=%d, stdout=%s", code, stdout)
	}

	// Case B: Complete task -> Stop on completed task returns {}
	_, err = c.CompleteTask(ctx, taskID, "quote-service", "Done")
	if err != nil {
		t.Fatalf("CompleteTask failed: %v", err)
	}

	code, stdout, errOut = runWithPipes(`{"conversationId":"conv-stop-1"}`, []string{
		"runtime", "agy-hook",
		"--event", "Stop",
		"--agent", "quote-service",
		"--task", taskID,
		"--socket", socketPath,
	})
	if code != 0 || stdout != "{}" {
		t.Fatalf("Case B expected {} on completed task, got code=%d, stdout=%s, err=%s", code, stdout, errOut)
	}

	// Case C: Stop when no active task -> returns {}
	code, stdout, errOut = runWithPipes(`{}`, []string{
		"runtime", "agy-hook",
		"--event", "Stop",
		"--agent", "quote-service",
		"--socket", socketPath,
	})
	if code != 0 || stdout != "{}" {
		t.Fatalf("Case C expected {} when no active task, got code=%d, stdout=%s, err=%s", code, stdout, errOut)
	}

	// Case D: Stop API failure on managed agent -> fails closed with decision: continue and valid JSON
	deadSocket := filepath.Join(os.TempDir(), "nonexistent-stop.sock")
	code, stdout, errOut = runWithPipes(`{}`, []string{
		"runtime", "agy-hook",
		"--event", "Stop",
		"--agent", "quote-service",
		"--task", taskID,
		"--socket", deadSocket,
	})
	if code != 0 || !strings.Contains(stdout, `"decision": "continue"`) || !strings.Contains(stdout, "failed for task") {
		t.Fatalf("Case D expected fail-closed continue decision on API failure, got code=%d, stdout=%s, stderr=%s", code, stdout, errOut)
	}
	if !strings.Contains(errOut, "failed to get task") {
		t.Fatalf("Case D expected diagnostic in stderr, got: %s", errOut)
	}

	// Case E: Daemon list tasks failure on managed agent during Stop -> fails closed with decision: continue
	code, stdout, errOut = runWithPipes(`{}`, []string{
		"runtime", "agy-hook",
		"--event", "Stop",
		"--agent", "quote-service",
		"--socket", deadSocket,
	})
	if code != 0 || !strings.Contains(stdout, `"decision": "continue"`) {
		t.Fatalf("Case E expected fail-closed continue on daemon list failure, got code=%d, stdout=%s, stderr=%s", code, stdout, errOut)
	}
}

func TestCLIRuntimeAGYHookControlTimeout(t *testing.T) {
	dir, err := os.MkdirTemp("", "agentbus-slow-sock-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	slowSocket := filepath.Join(dir, "slow.sock")
	l, err := net.Listen("unix", slowSocket)
	if err != nil {
		t.Fatalf("failed to listen on slow socket: %v", err)
	}
	defer l.Close()

	// Slow server accepts connections and delays response
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				time.Sleep(300 * time.Millisecond)
			}(conn)
		}
	}()

	// 1. Managed agent Stop event times out -> must fail closed with decision: continue and exit 0
	code, stdout, errOut := runWithPipes(`{"conversationId":"conv-timeout-1"}`, []string{
		"runtime", "agy-hook",
		"--event", "Stop",
		"--agent", "managed-quote",
		"--control-timeout", "50ms",
		"--socket", slowSocket,
	})
	if code != 0 || !strings.Contains(stdout, `"decision": "continue"`) {
		t.Fatalf("expected fail closed continue decision on Stop timeout, got code=%d, stdout=%s, stderr=%s", code, stdout, errOut)
	}
	if !strings.Contains(errOut, "timed out") && !strings.Contains(errOut, "context deadline exceeded") && !strings.Contains(errOut, "failed") {
		t.Fatalf("expected diagnostic in stderr on timeout, got: %s", errOut)
	}

	// 2. Managed agent non-Stop event times out -> must exit 1 with stderr diagnostic
	code, stdout, errOut = runWithPipes(`{"conversationId":"conv-timeout-2"}`, []string{
		"runtime", "agy-hook",
		"--event", "PostInvocation",
		"--agent", "managed-quote",
		"--control-timeout", "50ms",
		"--socket", slowSocket,
	})
	if code == 0 {
		t.Fatalf("expected non-zero exit for non-Stop event on timeout, got code=%d, stdout=%s, stderr=%s", code, stdout, errOut)
	}
	if !strings.Contains(errOut, "timed out") && !strings.Contains(errOut, "context deadline exceeded") && !strings.Contains(errOut, "failed") {
		t.Fatalf("expected diagnostic in stderr on non-Stop timeout, got: %s", errOut)
	}
}

func TestCLITaskWait(t *testing.T) {
	socketPath, cleanup := setupDaemon(t)
	defer cleanup()

	c := client.NewClient(socketPath)
	ctx := context.Background()

	dir, err := os.MkdirTemp("", "agentbus-task-wait-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	roleOrch := filepath.Join(dir, "orch_ROLE.md")
	_ = os.WriteFile(roleOrch, []byte("# Orch\nRole"), 0644)
	roleWorker := filepath.Join(dir, "worker_ROLE.md")
	_ = os.WriteFile(roleWorker, []byte("# Worker\nRole"), 0644)

	// Attach & ready orchestrator
	orchAgent := &domain.Agent{ID: "orchestrator", Role: "orchestrator", Connector: domain.ConnectorNone}
	orchProf := &domain.AgentProfile{AgentID: "orchestrator", ManifestVersion: 1, Runtime: "codex", Workspace: dir, ConfigPath: filepath.Join(dir, "c.yaml"), InstructionsPath: roleOrch, Capabilities: []string{"c"}}
	sessOrch, _ := c.AttachAgent(ctx, service.AttachAgentRequest{Agent: orchAgent, Profile: orchProf, NoNotify: true})
	_, _ = c.ReadySession(ctx, "orchestrator", sessOrch.Session.Generation)

	// Attach & ready worker
	workerAgent := &domain.Agent{ID: "worker", Role: "worker", Connector: domain.ConnectorNone}
	workerProf := &domain.AgentProfile{AgentID: "worker", ManifestVersion: 1, Runtime: "agy", Workspace: dir, ConfigPath: filepath.Join(dir, "w.yaml"), InstructionsPath: roleWorker, Capabilities: []string{"w"}}
	sessWorker, _ := c.AttachAgent(ctx, service.AttachAgentRequest{Agent: workerAgent, Profile: workerProf, NoNotify: true})
	_, _ = c.ReadySession(ctx, "worker", sessWorker.Session.Generation)

	// 1. Validation tests
	code, _, errOut := runWithPipes("", []string{"task", "wait"})
	if code != 1 || !strings.Contains(errOut, "task ID is required") {
		t.Fatalf("expected code 1 for missing task ID, got %d: %s", code, errOut)
	}

	code, _, errOut = runWithPipes("", []string{"task", "wait", "task-123"})
	if code != 1 || !strings.Contains(errOut, "flag --agent is required") {
		t.Fatalf("expected code 1 for missing --agent, got %d: %s", code, errOut)
	}

	code, _, errOut = runWithPipes("", []string{"task", "wait", "task-123", "--agent", "orchestrator", "--timeout", "invalid"})
	if code != 1 || !strings.Contains(errOut, "invalid --timeout") {
		t.Fatalf("expected code 1 for invalid --timeout, got %d: %s", code, errOut)
	}

	code, _, errOut = runWithPipes("", []string{"task", "wait", "task-123", "--agent", "orchestrator", "--timeout", "0s"})
	if code != 1 || !strings.Contains(errOut, "must be greater than 0") {
		t.Fatalf("expected code 1 for 0s timeout, got %d: %s", code, errOut)
	}

	code, _, errOut = runWithPipes("", []string{"task", "wait", "task-123", "--agent", "orchestrator", "--timeout", "-5s"})
	if code != 1 || !strings.Contains(errOut, "must be greater than 0") {
		t.Fatalf("expected code 1 for negative timeout, got %d: %s", code, errOut)
	}

	// 2. Immediate return on already terminal task
	submitResp1, err := c.SubmitTask(ctx, service.SubmitTaskRequest{
		SenderAgentID:  "orchestrator",
		TargetAgentID:  "worker",
		IdempotencyKey: "wait-task-succeeded-1",
		Content:        "Task to succeed",
	})
	if err != nil {
		t.Fatalf("SubmitTask 1 failed: %v", err)
	}
	taskID1 := submitResp1.Task.ID
	_, _ = c.AckTask(ctx, taskID1, "worker")
	_, _ = c.CompleteTask(ctx, taskID1, "worker", "Done 1")

	code, stdout, errOut := runWithPipes("", []string{"task", "wait", taskID1, "--agent", "orchestrator", "--socket", socketPath})
	if code != 0 {
		t.Fatalf("expected code 0 for succeeded task, got %d (err: %s)", code, errOut)
	}
	var res1 struct {
		Task     *domain.Task      `json:"task"`
		Messages []*domain.Message `json:"messages"`
	}
	if err := json.Unmarshal([]byte(stdout), &res1); err != nil || res1.Task == nil || res1.Task.Status != domain.TaskStatusSucceeded {
		t.Fatalf("failed to decode valid succeeded TaskDetail json: %v, stdout: %s", err, stdout)
	}

	// 3. Failed task exit code 2
	submitResp2, err := c.SubmitTask(ctx, service.SubmitTaskRequest{
		SenderAgentID:  "orchestrator",
		TargetAgentID:  "worker",
		IdempotencyKey: "wait-task-failed-2",
		Content:        "Task to fail",
	})
	if err != nil {
		t.Fatalf("SubmitTask 2 failed: %v", err)
	}
	taskID2 := submitResp2.Task.ID
	_, _ = c.AckTask(ctx, taskID2, "worker")
	_, _ = c.FailTask(ctx, taskID2, "worker", "Failed reason")

	code, stdout, errOut = runWithPipes("", []string{"task", "wait", taskID2, "--agent", "orchestrator", "--socket", socketPath})
	if code != 2 {
		t.Fatalf("expected code 2 for failed task, got %d (err: %s)", code, errOut)
	}
	var res2 struct {
		Task *domain.Task `json:"task"`
	}
	if err := json.Unmarshal([]byte(stdout), &res2); err != nil || res2.Task == nil || res2.Task.Status != domain.TaskStatusFailed {
		t.Fatalf("failed to decode valid failed TaskDetail json: %v, stdout: %s", err, stdout)
	}

	// 4. Canceled task exit code 3
	submitResp3, err := c.SubmitTask(ctx, service.SubmitTaskRequest{
		SenderAgentID:  "orchestrator",
		TargetAgentID:  "worker",
		IdempotencyKey: "wait-task-canceled-3",
		Content:        "Task to cancel",
	})
	if err != nil {
		t.Fatalf("SubmitTask 3 failed: %v", err)
	}
	taskID3 := submitResp3.Task.ID
	_, _ = c.CancelTask(ctx, taskID3, "orchestrator")

	code, stdout, errOut = runWithPipes("", []string{"task", "wait", taskID3, "--agent", "orchestrator", "--socket", socketPath})
	if code != 3 {
		t.Fatalf("expected code 3 for canceled task, got %d (err: %s)", code, errOut)
	}
	var res3 struct {
		Task *domain.Task `json:"task"`
	}
	if err := json.Unmarshal([]byte(stdout), &res3); err != nil || res3.Task == nil || res3.Task.Status != domain.TaskStatusCanceled {
		t.Fatalf("failed to decode valid canceled TaskDetail json: %v, stdout: %s", err, stdout)
	}

	// 5. Blocking wait awakened by event and completion
	submitResp4, err := c.SubmitTask(ctx, service.SubmitTaskRequest{
		SenderAgentID:  "orchestrator",
		TargetAgentID:  "worker",
		IdempotencyKey: "wait-task-active-4",
		Content:        "Task to run and complete",
	})
	if err != nil {
		t.Fatalf("SubmitTask 4 failed: %v", err)
	}
	taskID4 := submitResp4.Task.ID

	go func() {
		time.Sleep(100 * time.Millisecond)
		_, _ = c.AckTask(context.Background(), taskID4, "worker")
		time.Sleep(50 * time.Millisecond)
		_, _ = c.RecordRuntimeEvent(context.Background(), taskID4, service.RecordRuntimeEventRequest{
			Agent:   "worker",
			Runtime: "agy",
			Event:   domain.AGYEventPostInvocation,
			Payload: []byte(`{"conversationId":"conv-wait-test"}`),
		})
		time.Sleep(50 * time.Millisecond)
		_, _ = c.CompleteTask(context.Background(), taskID4, "worker", "Task 4 finished")
	}()

	waitStart := time.Now()
	code, stdout, errOut = runWithPipes("", []string{
		"task", "wait", taskID4,
		"--agent", "orchestrator",
		"--timeout", "5s",
		"--socket", socketPath,
	})
	waitElapsed := time.Since(waitStart)

	if code != 0 {
		t.Fatalf("expected code 0 for awakened completed task, got %d (err: %s)", code, errOut)
	}
	if waitElapsed > 2*time.Second {
		t.Fatalf("wait took too long (%v), event awakening may have failed", waitElapsed)
	}
	var res4 struct {
		Task     *domain.Task      `json:"task"`
		Messages []*domain.Message `json:"messages"`
	}
	if err := json.Unmarshal([]byte(stdout), &res4); err != nil || res4.Task == nil || res4.Task.Status != domain.TaskStatusSucceeded {
		t.Fatalf("failed to decode valid awakened TaskDetail json: %v, stdout: %s", err, stdout)
	}
	// Verify stdout contains NO event stream or raw string noise
	if strings.Contains(stdout, "runtime.event_observed") || strings.Contains(stdout, "sequence") {
		t.Fatalf("stdout must only contain TaskDetail and no event stream: %s", stdout)
	}

	// 6. Overall timeout exit code 4 (stdout must be strictly empty)
	submitResp5, err := c.SubmitTask(ctx, service.SubmitTaskRequest{
		SenderAgentID:  "orchestrator",
		TargetAgentID:  "worker",
		IdempotencyKey: "wait-task-timeout-5",
		Content:        "Task to time out",
	})
	if err != nil {
		t.Fatalf("SubmitTask 5 failed: %v", err)
	}
	taskID5 := submitResp5.Task.ID

	timeoutStart := time.Now()
	code, stdout, errOut = runWithPipes("", []string{
		"task", "wait", taskID5,
		"--agent", "orchestrator",
		"--timeout", "100ms",
		"--socket", socketPath,
	})
	timeoutElapsed := time.Since(timeoutStart)

	if code != 4 {
		t.Fatalf("expected code 4 on wait timeout, got %d (err: %s)", code, errOut)
	}
	if stdout != "" {
		t.Fatalf("expected strictly empty stdout on timeout, got: %s", stdout)
	}
	if !strings.Contains(errOut, "timed out waiting for task") || !strings.Contains(errOut, taskID5) {
		t.Fatalf("expected timeout error in stderr, got: %s", errOut)
	}
	if timeoutElapsed > 1*time.Second {
		t.Fatalf("timeout took unexpectedly long (%v), expected ~100ms", timeoutElapsed)
	}

	// Clean up taskID5 to free up worker
	_, _ = c.CancelTask(ctx, taskID5, "orchestrator")

	// 7. Attached but not ready caller -> code 1
	unreadyAgent := &domain.Agent{ID: "unready-agent", Role: "worker", Connector: domain.ConnectorNone}
	unreadyProf := &domain.AgentProfile{AgentID: "unready-agent", ManifestVersion: 1, Runtime: "agy", Workspace: dir, ConfigPath: filepath.Join(dir, "u.yaml"), InstructionsPath: roleWorker, Capabilities: []string{"u"}}
	_, err = c.AttachAgent(ctx, service.AttachAgentRequest{Agent: unreadyAgent, Profile: unreadyProf, NoNotify: true})
	if err != nil {
		t.Fatalf("Attach unready agent failed: %v", err)
	}

	code, _, errOut = runWithPipes("", []string{
		"task", "wait", taskID1,
		"--agent", "unready-agent",
		"--socket", socketPath,
	})
	if code != 1 || (!strings.Contains(errOut, "AGENT_NOT_READY") && !strings.Contains(errOut, "not ready")) {
		t.Fatalf("expected code 1 with not ready error for unready caller, got %d: %s", code, errOut)
	}

	// 8. Session loses ready state during wait -> quickly returns code 1 (does not wait for full timeout)
	submitResp6, err := c.SubmitTask(ctx, service.SubmitTaskRequest{
		SenderAgentID:  "orchestrator",
		TargetAgentID:  "worker",
		IdempotencyKey: "wait-task-lost-ready-6",
		Content:        "Task where session loses ready",
	})
	if err != nil {
		t.Fatalf("SubmitTask 6 failed: %v", err)
	}
	taskID6 := submitResp6.Task.ID

	go func() {
		time.Sleep(100 * time.Millisecond)
		// Bootstrap orchestrator again -> puts session into bootstrapping (not ready)
		_, _ = c.BootstrapAgent(context.Background(), "orchestrator")
		// Worker records an event to wake up the waiting broker
		_, _ = c.RecordRuntimeEvent(context.Background(), taskID6, service.RecordRuntimeEventRequest{
			Agent:   "worker",
			Runtime: "agy",
			Event:   domain.AGYEventPostInvocation,
			Payload: []byte(`{"event":"PostInvocation"}`),
		})
	}()

	lostReadyStart := time.Now()
	code, stdout, errOut = runWithPipes("", []string{
		"task", "wait", taskID6,
		"--agent", "orchestrator",
		"--timeout", "10s",
		"--socket", socketPath,
	})
	lostReadyElapsed := time.Since(lostReadyStart)

	if code != 1 {
		t.Fatalf("expected code 1 when caller loses ready during wait, got %d (stdout: %s, err: %s)", code, stdout, errOut)
	}
	if lostReadyElapsed > 3*time.Second {
		t.Fatalf("wait took too long (%v) when ready was lost; expected quick return (<3s << 10s)", lostReadyElapsed)
	}

	// 9. Non-existent socket error -> code 1
	deadSocket := filepath.Join(os.TempDir(), "nonexistent-wait-dead.sock")
	code, _, errOut = runWithPipes("", []string{
		"task", "wait", taskID1,
		"--agent", "orchestrator",
		"--socket", deadSocket,
	})
	if code != 1 || errOut == "" {
		t.Fatalf("expected code 1 for dead socket, got %d: %s", code, errOut)
	}

	// 10. Top-level help includes wait
	code, stdout, errOut = runWithPipes("", []string{"help"})
	if code != 0 {
		t.Fatalf("expected code 0 for help, got %d", code)
	}
	if !strings.Contains(errOut, "wait") && !strings.Contains(stdout, "wait") {
		t.Fatalf("expected help to mention wait subcommand")
	}
}
