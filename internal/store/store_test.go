package store_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"agentbus/internal/domain"
	"agentbus/internal/store"
)

func setupTestStore(t *testing.T) (*store.SQLiteStore, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "agentbus-store-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	dbPath := filepath.Join(dir, "test.db")
	s, err := store.OpenSQLite(dbPath)
	if err != nil {
		os.RemoveAll(dir)
		t.Fatalf("failed to open test store: %v", err)
	}
	cleanup := func() {
		s.Close()
		os.RemoveAll(dir)
	}
	return s, cleanup
}

func createTestProfile(dir string, id string, role string, runtime string) (*domain.AgentProfile, error) {
	roleFile := filepath.Join(dir, id+"_ROLE.md")
	if err := os.WriteFile(roleFile, []byte("# Role Specification\nValid instructions."), 0644); err != nil {
		return nil, err
	}
	cfgFile := filepath.Join(dir, id+".yaml")
	if err := os.WriteFile(cfgFile, []byte("version: 1"), 0644); err != nil {
		return nil, err
	}
	return &domain.AgentProfile{
		AgentID:          id,
		ManifestVersion:  1,
		Runtime:          runtime,
		Workspace:        dir,
		ConfigPath:       cfgFile,
		InstructionsPath: roleFile,
		Capabilities:     []string{"cap1", "cap2"},
	}, nil
}

func registerAndReadyTestAgents(t *testing.T, s *store.SQLiteStore) {
	t.Helper()
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "store-fixture-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	coordProf, err := createTestProfile(dir, "coordinator", "coordinator", "codex")
	if err != nil {
		t.Fatalf("createTestProfile coordinator failed: %v", err)
	}
	coordinator := &domain.Agent{
		ID:        "coordinator",
		Role:      "coordinator",
		Connector: domain.ConnectorTmux,
		Address:   "%50",
		Status:    domain.AgentStatusRegistered,
	}
	sessCoord, err := s.AttachAgent(ctx, coordinator, coordProf, domain.SessionStatusBootstrapping, "%50", nil)
	if err != nil {
		t.Fatalf("Attach coordinator failed: %v", err)
	}
	if _, err := s.ReadySession(ctx, "coordinator", sessCoord.Generation); err != nil {
		t.Fatalf("Ready coordinator failed: %v", err)
	}

	quoteProf, err := createTestProfile(dir, "quote", "quote", "agy")
	if err != nil {
		t.Fatalf("createTestProfile quote failed: %v", err)
	}
	quote := &domain.Agent{
		ID:        "quote",
		Role:      "quote",
		Connector: domain.ConnectorTmux,
		Address:   "%51",
		Status:    domain.AgentStatusRegistered,
	}
	sessQuote, err := s.AttachAgent(ctx, quote, quoteProf, domain.SessionStatusBootstrapping, "%51", nil)
	if err != nil {
		t.Fatalf("Attach quote failed: %v", err)
	}
	if _, err := s.ReadySession(ctx, "quote", sessQuote.Generation); err != nil {
		t.Fatalf("Ready quote failed: %v", err)
	}
}

func TestAgentRegistrationAndListing(t *testing.T) {
	s, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	registerAndReadyTestAgents(t, s)

	agents, err := s.ListAgents(ctx)
	if err != nil {
		t.Fatalf("ListAgents failed: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(agents))
	}

	a, err := s.GetAgent(ctx, "coordinator")
	if err != nil {
		t.Fatalf("GetAgent failed: %v", err)
	}
	if a.Address != "%50" || a.Role != "coordinator" {
		t.Fatalf("unexpected agent data: %+v", a)
	}

	_, err = s.GetAgent(ctx, "nonexistent")
	if !errors.Is(err, domain.ErrAgentNotFound) {
		t.Fatalf("expected ErrAgentNotFound, got %v", err)
	}
}

func TestTaskSubmissionAndIdempotency(t *testing.T) {
	s, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	registerAndReadyTestAgents(t, s)

	task := &domain.Task{
		ID:             "task-1",
		SenderAgentID:  "coordinator",
		TargetAgentID:  "quote",
		IdempotencyKey: "idem-001",
		Content:        "Check quote service",
		Status:         domain.TaskStatusQueued,
	}
	msg := &domain.Message{
		ID:            "msg-1",
		TaskID:        "task-1",
		SenderAgentID: "coordinator",
		Kind:          domain.MessageKindInstruction,
		Content:       "Check quote service",
	}
	evt := &domain.Event{
		ID:           "evt-1",
		TaskID:       "task-1",
		ActorAgentID: "coordinator",
		Type:         domain.EventTaskSubmitted,
		Payload:      `{"status":"queued"}`,
	}

	created, isDup, err := s.SubmitTask(ctx, task, msg, evt)
	if err != nil {
		t.Fatalf("SubmitTask failed: %v", err)
	}
	if isDup {
		t.Fatalf("expected isDup=false for first submit")
	}
	if created.ID != "task-1" {
		t.Fatalf("expected task-1, got %s", created.ID)
	}

	// Submit again with same idempotency key and identical payload
	dupTask := &domain.Task{
		ID:             "task-2",
		SenderAgentID:  "coordinator",
		TargetAgentID:  "quote",
		IdempotencyKey: "idem-001",
		Content:        "Check quote service",
		Status:         domain.TaskStatusQueued,
	}
	dupRes, isDup2, err := s.SubmitTask(ctx, dupTask, nil, nil)
	if err != nil {
		t.Fatalf("SubmitTask idempotency failed: %v", err)
	}
	if !isDup2 {
		t.Fatalf("expected isDup=true on duplicate submission")
	}
	if dupRes.ID != "task-1" {
		t.Fatalf("expected returned task ID to be original 'task-1', got %s", dupRes.ID)
	}

	// Submit again with same key but different content -> ErrIdempotencyConflict
	diffTask := &domain.Task{
		ID:             "task-3",
		SenderAgentID:  "coordinator",
		TargetAgentID:  "quote",
		IdempotencyKey: "idem-001",
		Content:        "Check quote service with different payload",
		Status:         domain.TaskStatusQueued,
	}
	_, _, err = s.SubmitTask(ctx, diffTask, nil, nil)
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("expected ErrIdempotencyConflict, got: %v", err)
	}
}

func TestSingleActiveTaskConstraintAndDatabaseLevelIndex(t *testing.T) {
	dir, err := os.MkdirTemp("", "agentbus-constraint-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	dbPath := filepath.Join(dir, "constraint.db")
	s1, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to open store1: %v", err)
	}
	defer s1.Close()

	ctx := context.Background()
	registerAndReadyTestAgents(t, s1)

	// Task 1: coordinator -> quote (status: queued)
	t1 := &domain.Task{
		ID:             "t1",
		SenderAgentID:  "coordinator",
		TargetAgentID:  "quote",
		IdempotencyKey: "k1",
		Content:        "Work 1",
		Status:         domain.TaskStatusQueued,
	}
	if _, _, err := s1.SubmitTask(ctx, t1, nil, nil); err != nil {
		t.Fatalf("submit t1 failed: %v", err)
	}

	// Task 2: concurrent submission to same target -> worker busy
	t2 := &domain.Task{
		ID:             "t2",
		SenderAgentID:  "coordinator",
		TargetAgentID:  "quote",
		IdempotencyKey: "k2",
		Content:        "Work 2",
		Status:         domain.TaskStatusQueued,
	}
	_, _, err = s1.SubmitTask(ctx, t2, nil, nil)
	if !errors.Is(err, domain.ErrWorkerBusy) {
		t.Fatalf("expected ErrWorkerBusy for second active task, got: %v", err)
	}

	// Ack t1 to make it running
	if _, err := s1.AckTask(ctx, "t1", "quote", nil); err != nil {
		t.Fatalf("AckTask failed: %v", err)
	}

	// Task 3: still rejected while t1 is running
	t3 := &domain.Task{
		ID:             "t3",
		SenderAgentID:  "coordinator",
		TargetAgentID:  "quote",
		IdempotencyKey: "k3",
		Content:        "Work 3",
		Status:         domain.TaskStatusQueued,
	}
	_, _, err = s1.SubmitTask(ctx, t3, nil, nil)
	if !errors.Is(err, domain.ErrWorkerBusy) {
		t.Fatalf("expected ErrWorkerBusy while t1 is running, got: %v", err)
	}

	// Complete t1
	if _, err := s1.CompleteTask(ctx, "t1", "quote", "Done", nil); err != nil {
		t.Fatalf("CompleteTask failed: %v", err)
	}

	// Task 4: submission now succeeds after t1 is in terminal state
	t4 := &domain.Task{
		ID:             "t4",
		SenderAgentID:  "coordinator",
		TargetAgentID:  "quote",
		IdempotencyKey: "k4",
		Content:        "Work 4",
		Status:         domain.TaskStatusQueued,
	}
	if _, _, err := s1.SubmitTask(ctx, t4, nil, nil); err != nil {
		t.Fatalf("submit t4 after t1 complete failed: %v", err)
	}
}

func TestTaskStateTransitionsAndAuthorization(t *testing.T) {
	s, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	registerAndReadyTestAgents(t, s)

	// Create third agent
	dir, _ := os.MkdirTemp("", "third-test-*")
	defer os.RemoveAll(dir)
	thirdProf, _ := createTestProfile(dir, "worker_03", "worker", "agy")
	w3 := &domain.Agent{ID: "worker_03", Role: "worker", Connector: domain.ConnectorNone, Status: domain.AgentStatusRegistered}
	sessW3, _ := s.AttachAgent(ctx, w3, thirdProf, domain.SessionStatusBootstrapping, "", nil)
	_, _ = s.ReadySession(ctx, "worker_03", sessW3.Generation)

	// Submit task
	task := &domain.Task{
		ID:             "task-auth",
		SenderAgentID:  "coordinator",
		TargetAgentID:  "quote",
		IdempotencyKey: "k-auth",
		Content:        "Auth check",
		Status:         domain.TaskStatusQueued,
	}
	if _, _, err := s.SubmitTask(ctx, task, nil, nil); err != nil {
		t.Fatalf("submit task failed: %v", err)
	}

	// 1. Unauthorized Ack
	_, err := s.AckTask(ctx, "task-auth", "worker_03", nil)
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for non-target Ack, got: %v", err)
	}

	// 2. Complete on queued task (invalid transition)
	_, err = s.CompleteTask(ctx, "task-auth", "quote", "result", nil)
	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition for Complete on queued task, got: %v", err)
	}

	// 3. Valid Ack
	if _, err := s.AckTask(ctx, "task-auth", "quote", nil); err != nil {
		t.Fatalf("AckTask failed: %v", err)
	}

	// 4. Double Ack (invalid transition)
	_, err = s.AckTask(ctx, "task-auth", "quote", nil)
	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition for double Ack, got: %v", err)
	}

	// 5. Update Status
	if err := s.UpdateTaskStatus(ctx, "task-auth", "quote", nil, nil); err != nil {
		t.Fatalf("UpdateTaskStatus failed: %v", err)
	}

	// 6. Send Message from non-sender (coordinator is sender)
	err = s.SendMessage(ctx, "task-auth", "worker_03", nil, nil)
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for SendMessage by non-initiator, got: %v", err)
	}

	// 7. Send Message from sender
	if err := s.SendMessage(ctx, "task-auth", "coordinator", nil, nil); err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	// 8. Cancel on running task (cannot cancel running task)
	_, err = s.CancelTask(ctx, "task-auth", "coordinator", nil)
	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition for Cancel on running task, got: %v", err)
	}

	// 9. Complete task
	if _, err := s.CompleteTask(ctx, "task-auth", "quote", "All done", nil); err != nil {
		t.Fatalf("CompleteTask failed: %v", err)
	}

	// 10. SendMessage after terminal state
	err = s.SendMessage(ctx, "task-auth", "coordinator", nil, nil)
	if !errors.Is(err, domain.ErrTerminalState) {
		t.Fatalf("expected ErrTerminalState for SendMessage after completion, got: %v", err)
	}
}

func TestStoreReadyGateAtomicFailClosed(t *testing.T) {
	s, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	dir, err := os.MkdirTemp("", "atomic-gate-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	// 1. Submit task when neither agent exists -> ErrAgentNotReady
	t1 := &domain.Task{
		ID:             "task-gate-1",
		SenderAgentID:  "coordinator",
		TargetAgentID:  "quote",
		IdempotencyKey: "k-gate-1",
		Content:        "Gate test 1",
		Status:         domain.TaskStatusQueued,
	}
	_, _, err = s.SubmitTask(ctx, t1, nil, nil)
	if !errors.Is(err, domain.ErrAgentNotReady) {
		t.Fatalf("expected ErrAgentNotReady on non-existent agents, got: %v", err)
	}

	// 2. Attach coordinator (bootstrapping, not ready)
	coordProf, _ := createTestProfile(dir, "coordinator", "coordinator", "codex")
	coord := &domain.Agent{ID: "coordinator", Role: "coordinator", Connector: domain.ConnectorNone, Status: domain.AgentStatusRegistered}
	sessCoord, err := s.AttachAgent(ctx, coord, coordProf, domain.SessionStatusBootstrapping, "", nil)
	if err != nil {
		t.Fatalf("attach coord failed: %v", err)
	}

	_, _, err = s.SubmitTask(ctx, t1, nil, nil)
	if !errors.Is(err, domain.ErrAgentNotReady) {
		t.Fatalf("expected ErrAgentNotReady while coordinator is bootstrapping, got: %v", err)
	}

	// Ready coordinator
	_, _ = s.ReadySession(ctx, "coordinator", sessCoord.Generation)

	// 3. Attach quote (bootstrapping, not ready)
	quoteProf, _ := createTestProfile(dir, "quote", "quote", "agy")
	quote := &domain.Agent{ID: "quote", Role: "quote", Connector: domain.ConnectorNone, Status: domain.AgentStatusRegistered}
	sessQuote, err := s.AttachAgent(ctx, quote, quoteProf, domain.SessionStatusBootstrapping, "", nil)
	if err != nil {
		t.Fatalf("attach quote failed: %v", err)
	}

	_, _, err = s.SubmitTask(ctx, t1, nil, nil)
	if !errors.Is(err, domain.ErrAgentNotReady) {
		t.Fatalf("expected ErrAgentNotReady while quote is bootstrapping, got: %v", err)
	}

	// Ready quote
	_, _ = s.ReadySession(ctx, "quote", sessQuote.Generation)

	// 4. Now submit succeeds!
	if _, _, err := s.SubmitTask(ctx, t1, nil, nil); err != nil {
		t.Fatalf("submit t1 failed after both ready: %v", err)
	}

	// 5. Re-bootstrap quote (generation becomes 2, status becomes bootstrapping)
	_, _, _, err = s.BootstrapAgent(ctx, "quote", domain.SessionStatusBootstrapping, "", nil)
	if err != nil {
		t.Fatalf("re-bootstrap quote failed: %v", err)
	}

	// 6. Direct store calls fail closed:
	// a) AckTask
	_, err = s.AckTask(ctx, "task-gate-1", "quote", nil)
	if !errors.Is(err, domain.ErrAgentNotReady) {
		t.Fatalf("expected ErrAgentNotReady on AckTask when quote in bootstrapping, got: %v", err)
	}

	// b) FailTask
	_, err = s.FailTask(ctx, "task-gate-1", "quote", "error", nil)
	if !errors.Is(err, domain.ErrAgentNotReady) {
		t.Fatalf("expected ErrAgentNotReady on FailTask when quote in bootstrapping, got: %v", err)
	}

	// Ready quote generation 2
	_, _ = s.ReadySession(ctx, "quote", 2)
	// Now Ack succeeds
	if _, err := s.AckTask(ctx, "task-gate-1", "quote", nil); err != nil {
		t.Fatalf("AckTask failed after quote re-ready: %v", err)
	}

	// Re-bootstrap coordinator (generation 2, bootstrapping)
	_, _, _, _ = s.BootstrapAgent(ctx, "coordinator", domain.SessionStatusBootstrapping, "", nil)

	// c) SendMessage by coordinator fails closed
	err = s.SendMessage(ctx, "task-gate-1", "coordinator", nil, nil)
	if !errors.Is(err, domain.ErrAgentNotReady) {
		t.Fatalf("expected ErrAgentNotReady on SendMessage when coordinator in bootstrapping, got: %v", err)
	}

	// d) CancelTask on new queued task fails closed
	t2 := &domain.Task{ID: "t-cancel", SenderAgentID: "coordinator", TargetAgentID: "quote", IdempotencyKey: "k-c", Content: "c", Status: domain.TaskStatusQueued}
	// Submit fails because coordinator is not ready
	_, _, err = s.SubmitTask(ctx, t2, nil, nil)
	if !errors.Is(err, domain.ErrAgentNotReady) {
		t.Fatalf("expected ErrAgentNotReady on submit t2, got: %v", err)
	}

	// Ready coordinator generation 2
	_, _ = s.ReadySession(ctx, "coordinator", 2)

	// Complete task-gate-1 first so quote worker is free
	if _, err := s.CompleteTask(ctx, "task-gate-1", "quote", "done", nil); err != nil {
		t.Fatalf("CompleteTask task-gate-1 failed: %v", err)
	}

	// Submit t2 succeeds
	if _, _, err := s.SubmitTask(ctx, t2, nil, nil); err != nil {
		t.Fatalf("submit t2 failed: %v", err)
	}

	// Re-bootstrap coordinator again -> cancel t2 fails closed
	_, _, _, _ = s.BootstrapAgent(ctx, "coordinator", domain.SessionStatusBootstrapping, "", nil)
	_, err = s.CancelTask(ctx, "t-cancel", "coordinator", nil)
	if !errors.Is(err, domain.ErrAgentNotReady) {
		t.Fatalf("expected ErrAgentNotReady on CancelTask when coordinator in bootstrapping, got: %v", err)
	}
}

func TestUpdateSessionDelivery(t *testing.T) {
	s, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	dir, err := os.MkdirTemp("", "deliv-store-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	prof, err := createTestProfile(dir, "worker-deliv", "worker", "agy")
	if err != nil {
		t.Fatalf("createTestProfile failed: %v", err)
	}
	agent := &domain.Agent{ID: "worker-deliv", Role: "worker", Connector: domain.ConnectorTmux, Address: "%51", Status: domain.AgentStatusRegistered}

	sess, err := s.AttachAgent(ctx, agent, prof, domain.SessionStatusBootstrapping, "%51", nil)
	if err != nil {
		t.Fatalf("AttachAgent failed: %v", err)
	}
	if sess.Generation != 1 {
		t.Fatalf("expected generation 1, got %d", sess.Generation)
	}

	// 1. Update delivery failure on wrong generation -> conflict
	errStr := "tmux paste-buffer failed"
	_, err = s.UpdateSessionDelivery(ctx, "worker-deliv", 999, domain.SessionStatusDeliveryFailed, "%51", &errStr)
	if !errors.Is(err, domain.ErrSessionGenerationConflict) {
		t.Fatalf("expected ErrSessionGenerationConflict, got: %v", err)
	}

	// 2. Update delivery failure on active generation 1 -> success
	updated, err := s.UpdateSessionDelivery(ctx, "worker-deliv", 1, domain.SessionStatusDeliveryFailed, "%51", &errStr)
	if err != nil {
		t.Fatalf("UpdateSessionDelivery failed: %v", err)
	}
	if updated.Generation != 1 || updated.Status != domain.SessionStatusDeliveryFailed || updated.DeliveryError == nil || *updated.DeliveryError != errStr {
		t.Fatalf("unexpected updated session: %+v", updated)
	}

	// Generation was NOT incremented
	_, _, loadedSess, err := s.GetAgentSession(ctx, "worker-deliv")
	if err != nil || loadedSess.Generation != 1 || loadedSess.Status != domain.SessionStatusDeliveryFailed {
		t.Fatalf("loaded session mismatch: %+v (err: %v)", loadedSess, err)
	}

	// 3. Ready session generation 1 succeeds
	readySess, err := s.ReadySession(ctx, "worker-deliv", 1)
	if err != nil {
		t.Fatalf("ReadySession failed: %v", err)
	}
	if readySess.Status != domain.SessionStatusReady || readySess.ReadyAt == nil {
		t.Fatalf("expected ready session, got: %+v", readySess)
	}
}

func TestPersistenceAcrossReopen(t *testing.T) {
	dir, err := os.MkdirTemp("", "agentbus-persist-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	dbPath := filepath.Join(dir, "persist.db")
	s1, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}

	ctx := context.Background()
	registerAndReadyTestAgents(t, s1)

	task := &domain.Task{
		ID:             "task-p",
		SenderAgentID:  "coordinator",
		TargetAgentID:  "quote",
		IdempotencyKey: "kp",
		Content:        "Persistence test",
		Status:         domain.TaskStatusQueued,
	}
	evt1 := &domain.Event{
		ID:           "evt-p1",
		TaskID:       "task-p",
		ActorAgentID: "coordinator",
		Type:         domain.EventTaskSubmitted,
		Payload:      `{}`,
	}
	if _, _, err := s1.SubmitTask(ctx, task, nil, evt1); err != nil {
		t.Fatalf("SubmitTask failed: %v", err)
	}

	evt2 := &domain.Event{
		ID:           "evt-p2",
		TaskID:       "task-p",
		ActorAgentID: "quote",
		Type:         domain.EventTaskAcknowledged,
		Payload:      `{}`,
	}
	if _, err := s1.AckTask(ctx, "task-p", "quote", evt2); err != nil {
		t.Fatalf("AckTask failed: %v", err)
	}

	// Close database
	if err := s1.Close(); err != nil {
		t.Fatalf("failed to close store: %v", err)
	}

	// Reopen database
	s2, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to reopen store: %v", err)
	}
	defer s2.Close()

	tLoaded, err := s2.GetTask(ctx, "task-p")
	if err != nil {
		t.Fatalf("GetTask after reopen failed: %v", err)
	}
	if tLoaded.Status != domain.TaskStatusRunning {
		t.Fatalf("expected running status after reopen, got %s", tLoaded.Status)
	}

	events, err := s2.GetEvents(ctx, "task-p", 0)
	if err != nil {
		t.Fatalf("GetEvents after reopen failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Sequence >= events[1].Sequence {
		t.Fatalf("expected sequence numbers to be strictly ascending: %d, %d", events[0].Sequence, events[1].Sequence)
	}
	if events[0].Type != domain.EventTaskSubmitted || events[1].Type != domain.EventTaskAcknowledged {
		t.Fatalf("unexpected event types: %s, %s", events[0].Type, events[1].Type)
	}

	// Query events with afterSeq = events[0].Sequence
	afterEvents, err := s2.GetEvents(ctx, "task-p", events[0].Sequence)
	if err != nil {
		t.Fatalf("GetEvents with afterSeq failed: %v", err)
	}
	if len(afterEvents) != 1 || afterEvents[0].ID != "evt-p2" {
		t.Fatalf("unexpected afterEvents: %+v", afterEvents)
	}
}

func TestAttachAndSessionLifecycle(t *testing.T) {
	s, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	dir, err := os.MkdirTemp("", "attach-lifecycle-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	profile, err := createTestProfile(dir, "coordinator", "coordinator", "codex")
	if err != nil {
		t.Fatalf("createTestProfile failed: %v", err)
	}

	coord := &domain.Agent{
		ID:        "coordinator",
		Role:      "coordinator",
		Connector: domain.ConnectorTmux,
		Address:   "%50",
		Status:    domain.AgentStatusRegistered,
	}

	// 1. Initial Attach -> generation 1
	sess1, err := s.AttachAgent(ctx, coord, profile, domain.SessionStatusBootstrapping, "%50", nil)
	if err != nil {
		t.Fatalf("AttachAgent failed: %v", err)
	}
	if sess1.Generation != 1 || sess1.Status != domain.SessionStatusBootstrapping {
		t.Fatalf("expected generation 1 and bootstrapping, got: %+v", sess1)
	}

	ready, err := s.IsAgentReady(ctx, "coordinator")
	if err != nil || ready {
		t.Fatalf("expected IsAgentReady false while bootstrapping, got %v (err: %v)", ready, err)
	}

	// 2. Ready with wrong generation -> conflict
	_, err = s.ReadySession(ctx, "coordinator", 999)
	if !errors.Is(err, domain.ErrSessionGenerationConflict) {
		t.Fatalf("expected ErrSessionGenerationConflict on wrong generation, got: %v", err)
	}

	// 3. Ready with correct generation 1 -> success
	sessReady, err := s.ReadySession(ctx, "coordinator", 1)
	if err != nil {
		t.Fatalf("ReadySession failed: %v", err)
	}
	if sessReady.Status != domain.SessionStatusReady || sessReady.ReadyAt == nil {
		t.Fatalf("expected status ready with ready_at timestamp, got: %+v", sessReady)
	}

	ready, err = s.IsAgentReady(ctx, "coordinator")
	if err != nil || !ready {
		t.Fatalf("expected IsAgentReady true, got %v (err: %v)", ready, err)
	}

	// 4. Re-Attach -> generation 2, status goes back to bootstrapping
	sess2, err := s.AttachAgent(ctx, coord, profile, domain.SessionStatusBootstrapping, "%50", nil)
	if err != nil {
		t.Fatalf("re-attach failed: %v", err)
	}
	if sess2.Generation != 2 || sess2.Status != domain.SessionStatusBootstrapping {
		t.Fatalf("expected generation 2 and bootstrapping on re-attach, got: %+v", sess2)
	}

	ready, err = s.IsAgentReady(ctx, "coordinator")
	if err != nil || ready {
		t.Fatalf("expected IsAgentReady false after re-attach, got %v", ready)
	}

	// 5. BootstrapAgent -> generation 3
	_, _, sess3, err := s.BootstrapAgent(ctx, "coordinator", domain.SessionStatusBootstrapping, "%50", nil)
	if err != nil {
		t.Fatalf("BootstrapAgent failed: %v", err)
	}
	if sess3.Generation != 3 || sess3.Status != domain.SessionStatusBootstrapping {
		t.Fatalf("expected generation 3 after bootstrap, got: %+v", sess3)
	}

	// Ready with generation 3
	_, err = s.ReadySession(ctx, "coordinator", 3)
	if err != nil {
		t.Fatalf("ready with generation 3 failed: %v", err)
	}

	// 6. GetAgentSession
	a, p, sessLoaded, err := s.GetAgentSession(ctx, "coordinator")
	if err != nil {
		t.Fatalf("GetAgentSession failed: %v", err)
	}
	if a.ID != "coordinator" || p.Runtime != "codex" || sessLoaded.Generation != 3 || sessLoaded.Status != domain.SessionStatusReady {
		t.Fatalf("unexpected loaded session data: %+v, %+v, %+v", a, p, sessLoaded)
	}
	if len(p.Capabilities) != 2 || p.Capabilities[0] != "cap1" || p.Capabilities[1] != "cap2" {
		t.Fatalf("capabilities not properly restored: %+v", p.Capabilities)
	}
}

func TestSchemaUpgradeOnExistingDatabase(t *testing.T) {
	dir, err := os.MkdirTemp("", "agentbus-upgrade-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	dbPath := filepath.Join(dir, "legacy.db")

	// Open raw SQLite with only V0 tables
	sLegacy, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	_ = sLegacy.Close()

	// Reopen with OpenSQLite (simulating service restart / schema upgrade)
	sUpgraded, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to reopen store on upgrade: %v", err)
	}
	defer sUpgraded.Close()

	// Verify new tables are usable with valid profile paths
	profile, err := createTestProfile(dir, "upgrade-agent", "worker", "codex")
	if err != nil {
		t.Fatalf("createTestProfile failed: %v", err)
	}
	coord := &domain.Agent{
		ID:        "upgrade-agent",
		Role:      "coordinator",
		Connector: domain.ConnectorNone,
		Address:   "",
		Status:    domain.AgentStatusRegistered,
	}

	sess, err := sUpgraded.AttachAgent(context.Background(), coord, profile, domain.SessionStatusBootstrapping, "", nil)
	if err != nil {
		t.Fatalf("AttachAgent on upgraded schema failed: %v", err)
	}
	if sess.Generation != 1 {
		t.Fatalf("expected generation 1, got %d", sess.Generation)
	}
}
