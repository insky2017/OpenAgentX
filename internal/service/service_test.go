package service_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agentbus/internal/connector"
	"agentbus/internal/domain"
	"agentbus/internal/service"
	"agentbus/internal/store"
)

type mockRunner struct {
	probeOutput string
	probeErr    error
	loadErr     error
	pasteErr    error
	sendErr     error
}

func (m *mockRunner) Run(ctx context.Context, stdin string, name string, args ...string) (string, error) {
	if len(args) > 0 {
		switch args[0] {
		case "display-message":
			return m.probeOutput, m.probeErr
		case "load-buffer":
			return "", m.loadErr
		case "paste-buffer":
			return "", m.pasteErr
		case "send-keys":
			return "", m.sendErr
		}
	}
	return "", nil
}

func setupTestService(t *testing.T, runner connector.Runner) (*service.Service, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "agentbus-svc-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	dbPath := filepath.Join(dir, "test.db")
	s, err := store.OpenSQLite(dbPath)
	if err != nil {
		os.RemoveAll(dir)
		t.Fatalf("failed to open store: %v", err)
	}

	conn := connector.NewTmuxConnector(runner)
	svc := service.NewService(s, conn, nil)

	cleanup := func() {
		s.Close()
		os.RemoveAll(dir)
	}
	return svc, cleanup
}

func TestServiceSubmitAndConnectorNotification(t *testing.T) {
	runner := &mockRunner{probeOutput: "%51 0\n"}
	svc, cleanup := setupTestService(t, runner)
	defer cleanup()

	ctx := context.Background()

	// Register agents
	if err := svc.RegisterAgent(ctx, &domain.Agent{
		ID:        "coordinator",
		Role:      "coordinator",
		Connector: domain.ConnectorTmux,
		Address:   "%50",
	}); err != nil {
		t.Fatalf("RegisterAgent coordinator failed: %v", err)
	}
	if err := svc.RegisterAgent(ctx, &domain.Agent{
		ID:        "quote",
		Role:      "quote",
		Connector: domain.ConnectorTmux,
		Address:   "%51",
	}); err != nil {
		t.Fatalf("RegisterAgent quote failed: %v", err)
	}

	// Submit task
	resp, err := svc.SubmitTask(ctx, service.SubmitTaskRequest{
		SenderAgentID:  "coordinator",
		TargetAgentID:  "quote",
		IdempotencyKey: "svc-idem-1",
		Content:        "Run quote check",
	})
	if err != nil {
		t.Fatalf("SubmitTask failed: %v", err)
	}
	if resp.IsDuplicate {
		t.Fatalf("expected non-duplicate")
	}

	// Check events for task.notified
	events, err := svc.GetEvents(ctx, resp.Task.ID, "coordinator", 0, 0)
	if err != nil {
		t.Fatalf("GetEvents failed: %v", err)
	}
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events (submitted + notified), got %d", len(events))
	}
	if events[0].Type != domain.EventTaskSubmitted || events[1].Type != domain.EventTaskNotified {
		t.Fatalf("unexpected event types: %s, %s", events[0].Type, events[1].Type)
	}
}

func TestServiceCallerAuthorization(t *testing.T) {
	runner := &mockRunner{probeOutput: "%51 0\n"}
	svc, cleanup := setupTestService(t, runner)
	defer cleanup()

	ctx := context.Background()

	_ = svc.RegisterAgent(ctx, &domain.Agent{ID: "coordinator", Role: "coordinator", Connector: domain.ConnectorNone})
	_ = svc.RegisterAgent(ctx, &domain.Agent{ID: "quote", Role: "quote", Connector: domain.ConnectorNone})
	_ = svc.RegisterAgent(ctx, &domain.Agent{ID: "third_party", Role: "worker", Connector: domain.ConnectorNone})

	resp, err := svc.SubmitTask(ctx, service.SubmitTaskRequest{
		SenderAgentID:  "coordinator",
		TargetAgentID:  "quote",
		IdempotencyKey: "auth-idem-1",
		Content:        "Confidential task content",
	})
	if err != nil {
		t.Fatalf("SubmitTask failed: %v", err)
	}
	taskID := resp.Task.ID

	// 1. Empty caller agent on GetTask -> reject
	_, err = svc.GetTask(ctx, taskID, "")
	if err == nil {
		t.Fatalf("expected error for empty caller agent on GetTask")
	}

	// 2. Third-party registered agent on GetTask -> ErrUnauthorized
	_, err = svc.GetTask(ctx, taskID, "third_party")
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for third_party agent on GetTask, got: %v", err)
	}

	// 3. Sender and Target on GetTask -> success
	if _, err := svc.GetTask(ctx, taskID, "coordinator"); err != nil {
		t.Fatalf("coordinator GetTask failed: %v", err)
	}
	if _, err := svc.GetTask(ctx, taskID, "quote"); err != nil {
		t.Fatalf("quote GetTask failed: %v", err)
	}

	// 4. Third-party on GetEvents -> ErrUnauthorized
	_, err = svc.GetEvents(ctx, taskID, "third_party", 0, 0)
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for third_party on GetEvents, got: %v", err)
	}

	// 5. Third-party on GetTaskMessages -> ErrUnauthorized
	_, err = svc.GetTaskMessages(ctx, taskID, "third_party")
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for third_party on GetTaskMessages, got: %v", err)
	}

	// 6. ListTasks without agent -> rejected
	_, err = svc.ListTasks(ctx, "", "")
	if err == nil {
		t.Fatalf("expected error when listing tasks without agent filter")
	}

	// 7. ListTasks for third_party -> returns 0 tasks
	tasks, err := svc.ListTasks(ctx, "third_party", "")
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks for third_party, got %d", len(tasks))
	}
}

func TestServiceWatchBlocking(t *testing.T) {
	runner := &mockRunner{probeOutput: "%51 0\n"}
	svc, cleanup := setupTestService(t, runner)
	defer cleanup()

	ctx := context.Background()

	_ = svc.RegisterAgent(ctx, &domain.Agent{ID: "coordinator", Role: "coordinator", Connector: domain.ConnectorNone})
	_ = svc.RegisterAgent(ctx, &domain.Agent{ID: "quote", Role: "quote", Connector: domain.ConnectorNone})

	resp, err := svc.SubmitTask(ctx, service.SubmitTaskRequest{
		SenderAgentID:  "coordinator",
		TargetAgentID:  "quote",
		IdempotencyKey: "watch-idem-1",
		Content:        "Watch test",
	})
	if err != nil {
		t.Fatalf("SubmitTask failed: %v", err)
	}

	events1, err := svc.GetEvents(ctx, resp.Task.ID, "coordinator", 0, 0)
	if err != nil || len(events1) == 0 {
		t.Fatalf("failed to get initial events: %v", err)
	}
	lastSeq := events1[len(events1)-1].Sequence

	// Launch a goroutine that ACKs the task after 50ms
	go func() {
		time.Sleep(50 * time.Millisecond)
		_, _ = svc.AckTask(context.Background(), resp.Task.ID, "quote")
	}()

	// Blocking watch with 1s timeout
	watchStart := time.Now()
	newEvents, err := svc.GetEvents(ctx, resp.Task.ID, "coordinator", lastSeq, 1*time.Second)
	watchElapsed := time.Since(watchStart)

	if err != nil {
		t.Fatalf("GetEvents with watch failed: %v", err)
	}
	if len(newEvents) != 1 {
		t.Fatalf("expected 1 new event, got %d", len(newEvents))
	}
	if newEvents[0].Type != domain.EventTaskAcknowledged {
		t.Fatalf("expected EventTaskAcknowledged, got %s", newEvents[0].Type)
	}
	if watchElapsed > 500*time.Millisecond {
		t.Fatalf("watch took unexpectedly long (%v), expected ~50ms", watchElapsed)
	}
}
