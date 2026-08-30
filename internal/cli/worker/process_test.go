package workercli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"openagentx/internal/api/workerapi"
	"openagentx/internal/controlplane"
	"openagentx/internal/domain"
	openagentsqlite "openagentx/internal/persistence/sqlite"
	"openagentx/internal/transport/unixhttp"
)

func TestRunWorkerProcessCompletesConsecutiveTasksWithoutTerminalInput(t *testing.T) {
	ctx := context.Background()
	repository, err := openagentsqlite.Open(ctx, filepath.Join(t.TempDir(), "worker-process.db"), openagentsqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	seedWorkerProcessIdentity(t, repository)
	broker := controlplane.NewMemoryWakeupBroker()
	service, err := controlplane.NewWorkerService(repository, broker, controlplane.WorkerServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := workerapi.NewHandler(service, workerapi.StaticPrincipal("worker-principal"))
	if err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(t.TempDir(), "run", "openagentx.sock")
	server, err := unixhttp.NewServer(socketPath, handler, nil)
	if err != nil {
		t.Fatal(err)
	}
	serverContext, cancelServer := context.WithCancel(ctx)
	serverResult := make(chan error, 1)
	go func() { serverResult <- server.Start(serverContext) }()
	waitForPath(t, socketPath)

	configPath := filepath.Join(t.TempDir(), "agent.yaml")
	config := fmt.Sprintf(`version: 1
agent_id: quote
transport: unix
unix_socket: %s
capabilities: [coding]
heartbeat_interval: 20ms
mailbox_wait: 1s
control_wait: 1s
shutdown_timeout: 1s
enable_worker_control: false
runtime_backends:
  - backend_id: local
    adapter_id: fake
    options:
      model: fake-model
      result: completed-by-resident-worker
`, socketPath)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	workerContext, cancelWorker := context.WithCancel(ctx)
	workerResult := make(chan error, 1)
	go func() { workerResult <- RunWorkerProcess(workerContext, configPath) }()

	for _, suffix := range []string{"a", "b"} {
		created := createWorkerProcessTask(t, repository, suffix)
		broker.Publish(controlplane.AgentMailboxTopic("quote"))
		waitForTaskStatus(t, repository, created.Task.ID, domain.TaskStatusSucceeded, workerResult)
		settled, err := repository.GetTask(ctx, created.Task.ID)
		if err != nil || settled.Result == nil || *settled.Result != "completed-by-resident-worker" {
			t.Fatalf("settled Task=%+v err=%v", settled, err)
		}
	}

	cancelWorker()
	select {
	case err := <-workerResult:
		if err != nil {
			t.Fatalf("stop Resident Worker: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Resident Worker did not stop")
	}
	// The process owns a long-polling HTTP client; close idle UDS connections
	// before asking the listener to perform graceful shutdown.
	cancelServer()
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
}

func seedWorkerProcessIdentity(t *testing.T, repository *openagentsqlite.Repository) {
	t.Helper()
	ctx := context.Background()
	principals := []domain.Principal{
		{ID: "human-owner", Kind: domain.PrincipalHuman, DisplayName: "Owner", Status: domain.IdentityActive},
		{ID: "agent-principal", Kind: domain.PrincipalAgent, DisplayName: "Quote", Status: domain.IdentityActive},
		{ID: "worker-principal", Kind: domain.PrincipalWorker, DisplayName: "Worker", Status: domain.IdentityActive},
	}
	for index := range principals {
		if err := repository.CreatePrincipal(ctx, &principals[index], &domain.JournalEvent{
			ID: fmt.Sprintf("event-principal-%d", index), EventType: "principal.created",
			ActorPrincipalID: "human-owner", Payload: json.RawMessage(`{}`),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.CreateOrganization(ctx, &domain.Organization{
		ID: "org-main", Name: "Main", Status: domain.IdentityActive,
	}, &domain.JournalEvent{
		ID: "event-org", OrganizationID: "org-main", EventType: "organization.created",
		ActorPrincipalID: "human-owner", Payload: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateAgent(ctx, &domain.AgentIdentity{
		ID: "quote", PrincipalID: "agent-principal", OrganizationID: "org-main",
		DisplayName: "Quote", Status: domain.AgentIdentityActive, Version: 1,
	}, &domain.AgentProfileRecord{
		AgentID: "quote", Version: 1, InstructionsPath: "/roles/quote.md",
		WorkspaceRoot: "/workspace/quote", Capabilities: []string{"coding"},
	}, &domain.JournalEvent{
		ID: "event-agent", OrganizationID: "org-main", EventType: "agent.created",
		ActorPrincipalID: "human-owner", Payload: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
}

func createWorkerProcessTask(t *testing.T, repository *openagentsqlite.Repository, suffix string) *domain.CreateTaskResult {
	t.Helper()
	task := &domain.Task{
		ID: "task-process-" + suffix, SenderPrincipalID: "human-owner", TargetAgentID: "quote",
		OrganizationID: "org-main", DispatchMode: domain.DispatchModeDirect,
		IdempotencyKey: "process-" + suffix, Content: "work " + suffix,
	}
	result, err := repository.CreateTask(context.Background(), task, &domain.Message{
		ID: "message-process-" + suffix, TaskID: task.ID, Content: task.Content,
	}, &domain.MailboxItem{ID: "mailbox-process-" + suffix}, &domain.JournalEvent{
		ID: "event-task-process-" + suffix, OrganizationID: "org-main", EventType: "task.created",
		ActorPrincipalID: "human-owner", Payload: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func waitForPath(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("path was not created: %s", path)
}

func waitForTaskStatus(t *testing.T, repository *openagentsqlite.Repository, taskID string, status domain.TaskStatus, workerResult <-chan error) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-workerResult:
			t.Fatalf("Worker exited while waiting for Task %s: %v", taskID, err)
		default:
		}
		task, err := repository.GetTask(context.Background(), taskID)
		if err == nil && task.Status == status {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	task, err := repository.GetTask(context.Background(), taskID)
	t.Fatalf("Task %s status=%v err=%v want=%s", taskID, task.Status, err, status)
}
