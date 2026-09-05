package workercli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"openagentx/internal/api"
	"openagentx/internal/api/workerapi"
	"openagentx/internal/controlplane"
	"openagentx/internal/domain"
	"openagentx/internal/network/secretstore"
	openagentsqlite "openagentx/internal/persistence/sqlite"
	openruntime "openagentx/internal/runtime"
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
	workflow := newWorkerProcessNetworkWorkflow(t, repository, broker)
	service, err := controlplane.NewWorkerService(repository, broker, controlplane.WorkerServiceOptions{NetworkWorkflow: workflow})
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
	bootstrapWorkerProcessInherit(t, repository, workflow, workerResult, "consecutive")

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

func TestRunWorkerProcessCompletesMultiTurnTaskWithSessionResume(t *testing.T) {
	ctx := context.Background()
	repository, err := openagentsqlite.Open(ctx, filepath.Join(t.TempDir(), "worker-process-multiturn.db"), openagentsqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	seedWorkerProcessIdentity(t, repository)
	broker := controlplane.NewMemoryWakeupBroker()
	workflow := newWorkerProcessNetworkWorkflow(t, repository, broker)
	service, err := controlplane.NewWorkerService(repository, broker, controlplane.WorkerServiceOptions{NetworkWorkflow: workflow})
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
      model: fake-multiturn-model
      result: completed-after-follow-up
      result_status_sequence:
        - waiting_input
        - succeeded
      provider_session_id: provider-session-multiturn-1
`, socketPath)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	workerContext, cancelWorker := context.WithCancel(ctx)
	workerResult := make(chan error, 1)
	go func() { workerResult <- RunWorkerProcess(workerContext, configPath) }()
	t.Cleanup(func() {
		cancelWorker()
		select {
		case <-workerResult:
		case <-time.After(3 * time.Second):
		}
		cancelServer()
		select {
		case <-serverResult:
		case <-time.After(3 * time.Second):
		}
	})
	bootstrapWorkerProcessInherit(t, repository, workflow, workerResult, "multiturn")

	created := createWorkerProcessTask(t, repository, "multiturn")
	broker.Publish(controlplane.AgentMailboxTopic("quote"))
	waitForTaskStatus(t, repository, created.Task.ID, domain.TaskStatusWaitingInput, workerResult)
	firstSettled, err := repository.GetTask(ctx, created.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if firstSettled.Result == nil || *firstSettled.Result != "completed-after-follow-up" {
		t.Fatalf("first Task result=%+v", firstSettled)
	}
	binding, err := repository.GetSessionBinding(ctx, created.Task.ID, "quote", "local")
	if err != nil || binding.ProviderSessionID != "provider-session-multiturn-1" || binding.Version != 1 {
		t.Fatalf("first SessionBinding=%+v err=%v", binding, err)
	}

	followup := &domain.Message{
		ID: "message-process-multiturn-followup", TaskID: created.Task.ID,
		SenderPrincipalID: "human-owner", Kind: domain.MessageKindSupplement,
		Content: "continue and complete the task",
	}
	followupItem := &domain.MailboxItem{ID: "mailbox-process-multiturn-followup", Lane: domain.MailboxLaneWork}
	if _, err := repository.CreateMessage(ctx, firstSettled.Version, followup, followupItem, &domain.JournalEvent{
		ID: "event-process-multiturn-followup", OrganizationID: "org-main", EventType: "message.created",
		ActorPrincipalID: "human-owner", Payload: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	broker.Publish(controlplane.AgentMailboxTopic("quote"))
	waitForTaskStatus(t, repository, created.Task.ID, domain.TaskStatusSucceeded, workerResult)

	settled, err := repository.GetTask(ctx, created.Task.ID)
	if err != nil || settled.Result == nil || *settled.Result != "completed-after-follow-up" {
		t.Fatalf("final Task=%+v err=%v", settled, err)
	}
	binding, err = repository.GetSessionBinding(ctx, created.Task.ID, "quote", "local")
	if err != nil || binding.ProviderSessionID != "provider-session-multiturn-1" || binding.Version != 2 {
		t.Fatalf("final SessionBinding=%+v err=%v", binding, err)
	}
	messages, err := repository.ListMessages(ctx, created.Task.ID)
	if err != nil || len(messages) != 2 || messages[1].Content != followup.Content || messages[1].Kind != domain.MessageKindSupplement {
		t.Fatalf("messages=%+v err=%v", messages, err)
	}
	assertMultiTurnWorkerProcessAudit(t, repository, created.Task.ID)
}

func assertMultiTurnWorkerProcessAudit(t *testing.T, repository *openagentsqlite.Repository, taskID string) {
	t.Helper()
	events, err := repository.ListJournal(context.Background(), 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	runIDs := make([]string, 0, 2)
	eventCounts := make(map[string]int)
	var previousSequence int64
	for _, event := range events {
		if event.Sequence <= previousSequence {
			t.Fatalf("Journal sequence not strictly increasing: previous=%d event=%+v", previousSequence, event)
		}
		previousSequence = event.Sequence
		eventCounts[event.EventType]++
		if event.EventType == "run_attempt.started" {
			run, getErr := repository.GetRunAttempt(context.Background(), event.AggregateID)
			if getErr == nil && run.TaskID == taskID {
				runIDs = append(runIDs, run.ID)
			}
		}
	}
	if len(runIDs) != 2 {
		t.Fatalf("run_attempt.started events for Task %s=%v; all event counts=%v", taskID, runIDs, eventCounts)
	}
	if eventCounts["run_attempt.finished"] != 2 || eventCounts["task.settled"] != 2 ||
		eventCounts["session_binding.saved"] != 2 || eventCounts["message.created"] != 1 {
		t.Fatalf("incomplete multi-turn Event Journal audit: %v", eventCounts)
	}
	first, err := repository.GetRunAttempt(context.Background(), runIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	second, err := repository.GetRunAttempt(context.Background(), runIDs[1])
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != domain.RunAttemptSucceeded || second.Status != domain.RunAttemptSucceeded || first.WorkerInstanceID != second.WorkerInstanceID {
		t.Fatalf("RunAttempts first=%+v second=%+v", first, second)
	}
	var firstSpec, secondSpec domain.ResolvedExecutionSpec
	if err := json.Unmarshal([]byte(first.ResolvedExecutionJSON), &firstSpec); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(second.ResolvedExecutionJSON), &secondSpec); err != nil {
		t.Fatal(err)
	}
	if firstSpec.Spec.Session.Mode != domain.SessionModeNew || secondSpec.Spec.Session.Mode != domain.SessionModeResume ||
		firstSpec.Spec.Session.ContextID != taskID || secondSpec.Spec.Session.ContextID != taskID {
		t.Fatalf("resolved sessions first=%+v second=%+v", firstSpec.Spec.Session, secondSpec.Spec.Session)
	}
	for _, run := range []*domain.RunAttempt{first, second} {
		var result openruntime.TurnResult
		if err := json.Unmarshal([]byte(run.ResultJSON), &result); err != nil {
			t.Fatal(err)
		}
		if result.ProviderSessionID != "provider-session-multiturn-1" {
			t.Fatalf("RunAttempt %s provider session=%q", run.ID, result.ProviderSessionID)
		}
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

func newWorkerProcessNetworkWorkflow(t *testing.T, repository *openagentsqlite.Repository, broker *controlplane.MemoryWakeupBroker) *controlplane.NetworkWorkflowService {
	t.Helper()
	secrets, err := secretstore.Open(filepath.Join(t.TempDir(), "network-secrets"))
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := controlplane.NewNetworkWorkflowService(repository, secrets, broker, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	return workflow
}

func bootstrapWorkerProcessInherit(t *testing.T, repository *openagentsqlite.Repository, workflow *controlplane.NetworkWorkflowService, workerResult <-chan error, suffix string) {
	t.Helper()
	var worker domain.WorkerInstance
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-workerResult:
			t.Fatalf("Worker exited before inherit bootstrap: %v", err)
		default:
		}
		workers, err := repository.ListWorkers(context.Background(), 10)
		if err == nil && len(workers) == 1 && workers[0].Status != domain.WorkerStatusOffline {
			worker = workers[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if worker.ID == "" {
		t.Fatal("Worker did not register before inherit bootstrap")
	}
	result, err := workflow.StartModeTest(context.Background(), "human-owner", api.TestNetworkModeRequest{
		Meta:    api.CommandMeta{IdempotencyKey: "process-inherit-test-" + suffix, ExpectedVersion: 0},
		AgentID: "quote", BackendID: "local", Mode: domain.NetworkInherit,
		WorkerInstanceID: worker.ID, Generation: worker.Generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		TestID string `json:"test_id"`
	}
	if err := json.Unmarshal(result, &receipt); err != nil || receipt.TestID == "" {
		t.Fatalf("inherit test receipt=%s err=%v", result, err)
	}
	waitForNetworkModeTestState(t, repository, receipt.TestID, "succeeded", workerResult)
	if _, err := workflow.PublishMode(context.Background(), "human-owner", api.PublishNetworkModeRequest{
		Meta:   api.CommandMeta{IdempotencyKey: "process-inherit-publish-" + suffix, ExpectedVersion: 0},
		TestID: receipt.TestID, WorkerInstanceID: worker.ID, Generation: worker.Generation,
	}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-workerResult:
			t.Fatalf("Worker exited during inherit apply: %v", err)
		default:
		}
		binding, err := repository.GetNetworkBinding(context.Background(), "quote", "local")
		if err == nil && binding.DesiredStatus == "applied" && binding.AppliedBindingRevision == binding.Version {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("inherit binding was not applied")
}

func waitForNetworkModeTestState(t *testing.T, repository *openagentsqlite.Repository, testID, state string, workerResult <-chan error) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-workerResult:
			t.Fatalf("Worker exited during network mode test: %v", err)
		default:
		}
		result, err := repository.GetNetworkModeTest(context.Background(), testID)
		if err == nil && result.State == state {
			return
		}
		if err == nil && result.State == "failed" {
			t.Fatalf("network mode test %s failed: diagnostic=%s probes=%+v", testID, result.DiagnosticCode, result.ProbeResults)
		}
		time.Sleep(10 * time.Millisecond)
	}
	result, err := repository.GetNetworkModeTest(context.Background(), testID)
	t.Fatalf("network mode test %s did not reach %s: result=%+v err=%v", testID, state, result, err)
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
