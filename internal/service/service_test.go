package service_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func setupTestService(t *testing.T, runner connector.Runner) (*service.Service, string, func()) {
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
	return svc, dir, cleanup
}

func createServiceTestProfile(dir string, id string, runtime string) *domain.AgentProfile {
	roleFile := filepath.Join(dir, id+"_ROLE.md")
	_ = os.WriteFile(roleFile, []byte("# Role\nValid instructions."), 0644)
	cfgFile := filepath.Join(dir, id+".yaml")
	_ = os.WriteFile(cfgFile, []byte("version: 1"), 0644)
	return &domain.AgentProfile{
		AgentID:          id,
		ManifestVersion:  1,
		Runtime:          runtime,
		Workspace:        dir,
		ConfigPath:       cfgFile,
		InstructionsPath: roleFile,
		Capabilities:     []string{"cap"},
	}
}

func attachAndReadyAgent(t *testing.T, svc *service.Service, dir string, id string, role string, connectorType string, address string) {
	t.Helper()
	ctx := context.Background()
	agent := &domain.Agent{
		ID:        id,
		Role:      role,
		Connector: connectorType,
		Address:   address,
		Status:    domain.AgentStatusRegistered,
	}
	profile := createServiceTestProfile(dir, id, "test")

	resp, err := svc.AttachAgent(ctx, service.AttachAgentRequest{
		Agent:    agent,
		Profile:  profile,
		NoNotify: true,
	})
	if err != nil {
		t.Fatalf("AttachAgent %s failed: %v", id, err)
	}

	_, err = svc.ReadySession(ctx, id, resp.Session.Generation)
	if err != nil {
		t.Fatalf("ReadySession %s failed: %v", id, err)
	}
}

func TestServiceSubmitAndConnectorNotification(t *testing.T) {
	runner := &mockRunner{probeOutput: "%51 0\n"}
	svc, dir, cleanup := setupTestService(t, runner)
	defer cleanup()

	ctx := context.Background()

	// Attach and ready agents
	attachAndReadyAgent(t, svc, dir, "coordinator", "coordinator", domain.ConnectorTmux, "%50")
	attachAndReadyAgent(t, svc, dir, "quote", "quote", domain.ConnectorTmux, "%51")

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

func TestTaskReadyGate(t *testing.T) {
	runner := &mockRunner{probeOutput: "%51 0\n"}
	svc, dir, cleanup := setupTestService(t, runner)
	defer cleanup()

	ctx := context.Background()

	// 1. Submit when neither agent is attached/ready
	_, err := svc.SubmitTask(ctx, service.SubmitTaskRequest{
		SenderAgentID:  "coordinator",
		TargetAgentID:  "quote",
		IdempotencyKey: "gate-1",
		Content:        "Test gate",
	})
	if !errors.Is(err, domain.ErrAgentNotReady) {
		t.Fatalf("expected ErrAgentNotReady when agents not ready, got: %v", err)
	}

	// 2. Attach coordinator (bootstrapping, not yet ready)
	coord := &domain.Agent{ID: "coordinator", Role: "coordinator", Connector: domain.ConnectorNone}
	coordProf := createServiceTestProfile(dir, "coordinator", "codex")
	sessCoord, err := svc.AttachAgent(ctx, service.AttachAgentRequest{Agent: coord, Profile: coordProf, NoNotify: true})
	if err != nil {
		t.Fatalf("Attach coord failed: %v", err)
	}

	_, err = svc.SubmitTask(ctx, service.SubmitTaskRequest{
		SenderAgentID:  "coordinator",
		TargetAgentID:  "quote",
		IdempotencyKey: "gate-2",
		Content:        "Test gate",
	})
	if !errors.Is(err, domain.ErrAgentNotReady) {
		t.Fatalf("expected ErrAgentNotReady while coordinator is bootstrapping, got: %v", err)
	}

	// Ready coordinator
	_, _ = svc.ReadySession(ctx, "coordinator", sessCoord.Session.Generation)

	// Quote is still not ready
	_, err = svc.SubmitTask(ctx, service.SubmitTaskRequest{
		SenderAgentID:  "coordinator",
		TargetAgentID:  "quote",
		IdempotencyKey: "gate-3",
		Content:        "Test gate",
	})
	if !errors.Is(err, domain.ErrAgentNotReady) {
		t.Fatalf("expected ErrAgentNotReady while target is not ready, got: %v", err)
	}

	// Attach and ready quote
	quote := &domain.Agent{ID: "quote", Role: "quote", Connector: domain.ConnectorNone}
	quoteProf := createServiceTestProfile(dir, "quote", "agy")
	sessQuote, _ := svc.AttachAgent(ctx, service.AttachAgentRequest{Agent: quote, Profile: quoteProf, NoNotify: true})
	_, _ = svc.ReadySession(ctx, "quote", sessQuote.Session.Generation)

	// Now both are ready -> submit succeeds!
	submitResp, err := svc.SubmitTask(ctx, service.SubmitTaskRequest{
		SenderAgentID:  "coordinator",
		TargetAgentID:  "quote",
		IdempotencyKey: "gate-4",
		Content:        "Test gate success",
	})
	if err != nil {
		t.Fatalf("expected submit success after both ready, got: %v", err)
	}

	// 3. Re-bootstrap quote -> session goes back to bootstrapping
	_, err = svc.BootstrapAgent(ctx, "quote")
	if err != nil {
		t.Fatalf("BootstrapAgent failed: %v", err)
	}

	// Attempting to ack task while quote is bootstrapping -> blocked by ready gate!
	_, err = svc.AckTask(ctx, submitResp.Task.ID, "quote")
	if !errors.Is(err, domain.ErrAgentNotReady) {
		t.Fatalf("expected ErrAgentNotReady on AckTask when quote re-bootstrapping, got: %v", err)
	}

	// Ready quote with generation 2
	sessLoaded, _ := svc.GetSession(ctx, "quote")
	_, err = svc.ReadySession(ctx, "quote", sessLoaded.Session.Generation)
	if err != nil {
		t.Fatalf("ReadySession generation 2 failed: %v", err)
	}

	// Now Ack succeeds!
	_, err = svc.AckTask(ctx, submitResp.Task.ID, "quote")
	if err != nil {
		t.Fatalf("AckTask failed after re-ready: %v", err)
	}
}

func TestDeliveryFailureNoDoubleIncrement(t *testing.T) {
	// Probe succeeds, but paste-buffer fails
	runner := &mockRunner{
		probeOutput: "%51 0\n",
		pasteErr:    errors.New("tmux paste failed"),
	}
	svc, dir, cleanup := setupTestService(t, runner)
	defer cleanup()

	ctx := context.Background()

	quote := &domain.Agent{ID: "quote", Role: "quote", Connector: domain.ConnectorTmux, Address: "%51"}
	quoteProf := createServiceTestProfile(dir, "quote", "agy")

	// 1. Attach with delivery failure
	attachResp, err := svc.AttachAgent(ctx, service.AttachAgentRequest{
		Agent:    quote,
		Profile:  quoteProf,
		NoNotify: false,
	})
	if err != nil {
		t.Fatalf("AttachAgent returned unexpected error: %v", err)
	}

	// Assert generation incremented only once (is 1)
	if attachResp.Session.Generation != 1 {
		t.Fatalf("expected generation 1, got %d", attachResp.Session.Generation)
	}
	if attachResp.Session.Status != domain.SessionStatusDeliveryFailed {
		t.Fatalf("expected status 'delivery_failed', got %s", attachResp.Session.Status)
	}
	if attachResp.Disposition != connector.DispositionDeliveryFailed {
		t.Fatalf("expected disposition 'delivery_failed', got %s", attachResp.Disposition)
	}
	if attachResp.DeliveryError == "" {
		t.Fatalf("expected non-empty DeliveryError on delivery failure")
	}

	// Assert response matches GetSession
	sessGet, err := svc.GetSession(ctx, "quote")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if sessGet.Session.Generation != 1 || sessGet.Session.Status != domain.SessionStatusDeliveryFailed {
		t.Fatalf("session from GetSession mismatch: %+v", sessGet.Session)
	}

	// Assert this generation can be readied
	readySess, err := svc.ReadySession(ctx, "quote", 1)
	if err != nil {
		t.Fatalf("ReadySession on delivery_failed session failed: %v", err)
	}
	if readySess.Status != domain.SessionStatusReady || readySess.ReadyAt == nil {
		t.Fatalf("expected ready session after ReadySession, got %+v", readySess)
	}

	// 2. BootstrapAgent with delivery failure -> generation becomes 2 (incremented only once!)
	bootResp, err := svc.BootstrapAgent(ctx, "quote")
	if err != nil {
		t.Fatalf("BootstrapAgent failed: %v", err)
	}
	if bootResp.Session.Generation != 2 {
		t.Fatalf("expected generation 2 on bootstrap, got %d", bootResp.Session.Generation)
	}
	if bootResp.Session.Status != domain.SessionStatusDeliveryFailed {
		t.Fatalf("expected status 'delivery_failed' on bootstrap failure, got %s", bootResp.Session.Status)
	}
	if bootResp.Disposition != connector.DispositionDeliveryFailed {
		t.Fatalf("expected disposition 'delivery_failed', got %s", bootResp.Disposition)
	}

	sessGet2, _ := svc.GetSession(ctx, "quote")
	if sessGet2.Session.Generation != 2 || sessGet2.Session.Status != domain.SessionStatusDeliveryFailed {
		t.Fatalf("session from GetSession after bootstrap mismatch: %+v", sessGet2.Session)
	}

	// Ready generation 2 succeeds
	_, err = svc.ReadySession(ctx, "quote", 2)
	if err != nil {
		t.Fatalf("ReadySession on generation 2 failed: %v", err)
	}
}

func TestDispositionHonesty(t *testing.T) {
	runner := &mockRunner{probeOutput: "%51 0\n"}
	svc, dir, cleanup := setupTestService(t, runner)
	defer cleanup()

	ctx := context.Background()

	// 1. --no-notify -> disposition must be 'skipped'
	coord := &domain.Agent{ID: "coordinator", Role: "coordinator", Connector: domain.ConnectorTmux, Address: "%50"}
	coordProf := createServiceTestProfile(dir, "coordinator", "codex")
	resp, err := svc.AttachAgent(ctx, service.AttachAgentRequest{Agent: coord, Profile: coordProf, NoNotify: true})
	if err != nil {
		t.Fatalf("AttachAgent failed: %v", err)
	}
	if resp.Disposition != connector.DispositionSkipped {
		t.Fatalf("expected disposition 'skipped' for no-notify attach, got '%s'", resp.Disposition)
	}

	// 2. connector: none -> disposition must be 'skipped'
	worker := &domain.Agent{ID: "none-worker", Role: "worker", Connector: domain.ConnectorNone, Address: ""}
	workerProf := createServiceTestProfile(dir, "none-worker", "agy")
	resp, err = svc.AttachAgent(ctx, service.AttachAgentRequest{Agent: worker, Profile: workerProf, NoNotify: false})
	if err != nil {
		t.Fatalf("AttachAgent failed: %v", err)
	}
	if resp.Disposition != connector.DispositionSkipped {
		t.Fatalf("expected disposition 'skipped' for connector: none attach, got '%s'", resp.Disposition)
	}

	// 3. Bootstrap on connector: none -> disposition must be 'skipped'
	bootResp, err := svc.BootstrapAgent(ctx, "none-worker")
	if err != nil {
		t.Fatalf("BootstrapAgent failed: %v", err)
	}
	if bootResp.Disposition != connector.DispositionSkipped {
		t.Fatalf("expected disposition 'skipped' for connector: none bootstrap, got '%s'", bootResp.Disposition)
	}
}

func TestServiceCallerAuthorization(t *testing.T) {
	runner := &mockRunner{probeOutput: "%51 0\n"}
	svc, dir, cleanup := setupTestService(t, runner)
	defer cleanup()

	ctx := context.Background()

	attachAndReadyAgent(t, svc, dir, "coordinator", "coordinator", domain.ConnectorNone, "")
	attachAndReadyAgent(t, svc, dir, "quote", "quote", domain.ConnectorNone, "")
	attachAndReadyAgent(t, svc, dir, "third_party", "worker", domain.ConnectorNone, "")

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
	svc, dir, cleanup := setupTestService(t, runner)
	defer cleanup()

	ctx := context.Background()

	attachAndReadyAgent(t, svc, dir, "coordinator", "coordinator", domain.ConnectorNone, "")
	attachAndReadyAgent(t, svc, dir, "quote", "quote", domain.ConnectorNone, "")

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

type mockFailDeliveryStore struct {
	store.Store
	failDeliveryErr error
}

func (m *mockFailDeliveryStore) UpdateSessionDelivery(ctx context.Context, agentID string, generation int64, status string, resolvedPane string, delivErr *string) (*domain.AgentSession, error) {
	if m.failDeliveryErr != nil {
		return nil, m.failDeliveryErr
	}
	return m.Store.UpdateSessionDelivery(ctx, agentID, generation, status, resolvedPane, delivErr)
}

func TestServiceUpdateSessionDeliveryCASFailure(t *testing.T) {
	runner := &mockRunner{
		probeOutput: "%51 0\n",
		pasteErr:    errors.New("tmux paste failed"),
	}
	dir, err := os.MkdirTemp("", "agentbus-cas-fail-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	dbPath := filepath.Join(dir, "test.db")
	realStore, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer realStore.Close()

	mockStore := &mockFailDeliveryStore{
		Store:           realStore,
		failDeliveryErr: errors.New("simulated CAS error on update delivery"),
	}

	conn := connector.NewTmuxConnector(runner)
	svc := service.NewService(mockStore, conn, nil)

	ctx := context.Background()
	quote := &domain.Agent{ID: "quote", Role: "quote", Connector: domain.ConnectorTmux, Address: "%51"}
	quoteProf := createServiceTestProfile(dir, "quote", "agy")

	// 1. AttachAgent: notification delivery fails AND store.UpdateSessionDelivery fails
	_, err = svc.AttachAgent(ctx, service.AttachAgentRequest{
		Agent:    quote,
		Profile:  quoteProf,
		NoNotify: false,
	})
	if err == nil {
		t.Fatalf("expected error from AttachAgent when UpdateSessionDelivery fails, got nil")
	}
	if !strings.Contains(err.Error(), "simulated CAS error") || !strings.Contains(err.Error(), "tmux paste failed") {
		t.Fatalf("expected combined error context, got: %v", err)
	}

	// 2. Attach successfully first with no-notify
	mockStore.failDeliveryErr = nil
	attachResp, err := svc.AttachAgent(ctx, service.AttachAgentRequest{
		Agent:    quote,
		Profile:  quoteProf,
		NoNotify: true,
	})
	if err != nil {
		t.Fatalf("AttachAgent with no-notify failed: %v", err)
	}
	if attachResp.Session.Generation != 2 {
		t.Fatalf("expected generation 2, got %d", attachResp.Session.Generation)
	}

	// 3. BootstrapAgent: notification delivery fails AND store.UpdateSessionDelivery fails
	mockStore.failDeliveryErr = errors.New("simulated CAS error on bootstrap delivery update")
	_, err = svc.BootstrapAgent(ctx, "quote")
	if err == nil {
		t.Fatalf("expected error from BootstrapAgent when UpdateSessionDelivery fails, got nil")
	}
	if !strings.Contains(err.Error(), "simulated CAS error") || !strings.Contains(err.Error(), "tmux paste failed") {
		t.Fatalf("expected combined error context, got: %v", err)
	}
}
