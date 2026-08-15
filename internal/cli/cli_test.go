package cli_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

	// 1. Attach and ready coordinator
	attachAndReadyCLIAgent(t, socketPath, dir, "coordinator", "coordinator", "tmux", "%50")

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
		"agent", "get", "coordinator",
		"--socket", socketPath,
	})
	if code != 0 {
		t.Fatalf("agent get failed with code %d", code)
	}

	// 5. Submit task
	code = cli.Execute([]string{
		"task", "submit",
		"--from", "coordinator",
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
		"--from", "coordinator",
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

	// 11. Watch completed task (with --agent coordinator)
	code = cli.Execute([]string{
		"task", "watch", taskID,
		"--agent", "coordinator",
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

	attachAndReadyCLIAgent(t, socketPath, dir, "c", "coordinator", "none", "")
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
