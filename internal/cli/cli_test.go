package cli_test

import (
	"context"
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

func TestCLIFullWorkflow(t *testing.T) {
	socketPath, cleanup := setupDaemon(t)
	defer cleanup()

	// 1. Register coordinator
	code := cli.Execute([]string{
		"agent", "register",
		"--id", "coordinator",
		"--role", "coordinator",
		"--connector", "tmux",
		"--address", "%50",
		"--socket", socketPath,
	})
	if code != 0 {
		t.Fatalf("register coordinator failed with code %d", code)
	}

	// 2. Register quote
	code = cli.Execute([]string{
		"agent", "register",
		"--id", "quote",
		"--role", "quote",
		"--connector", "tmux",
		"--address", "%51",
		"--socket", socketPath,
	})
	if code != 0 {
		t.Fatalf("register quote failed with code %d", code)
	}

	// Register third-party agent for auth test
	code = cli.Execute([]string{
		"agent", "register",
		"--id", "other_agent",
		"--role", "worker",
		"--connector", "none",
		"--socket", socketPath,
	})
	if code != 0 {
		t.Fatalf("register other_agent failed with code %d", code)
	}

	// 3. List agents
	code = cli.Execute([]string{
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

	_ = cli.Execute([]string{"agent", "register", "--id", "c", "--role", "coordinator", "--connector", "none", "--socket", socketPath})
	_ = cli.Execute([]string{"agent", "register", "--id", "q", "--role", "quote", "--connector", "none", "--socket", socketPath})

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
