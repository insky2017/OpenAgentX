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

func registerTestAgents(t *testing.T, s *store.SQLiteStore) {
	ctx := context.Background()
	coordinator := &domain.Agent{
		ID:        "coordinator",
		Role:      "coordinator",
		Connector: domain.ConnectorTmux,
		Address:   "%50",
		Status:    domain.AgentStatusRegistered,
	}
	if err := s.RegisterAgent(ctx, coordinator); err != nil {
		t.Fatalf("failed to register coordinator: %v", err)
	}

	quote := &domain.Agent{
		ID:        "quote",
		Role:      "quote",
		Connector: domain.ConnectorTmux,
		Address:   "%51",
		Status:    domain.AgentStatusRegistered,
	}
	if err := s.RegisterAgent(ctx, quote); err != nil {
		t.Fatalf("failed to register quote agent: %v", err)
	}
}

func TestAgentRegistrationAndListing(t *testing.T) {
	s, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	registerTestAgents(t, s)

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
	registerTestAgents(t, s)

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

	// Submit again with same idempotency key
	dupTask := &domain.Task{
		ID:             "task-2",
		SenderAgentID:  "coordinator",
		TargetAgentID:  "quote",
		IdempotencyKey: "idem-001",
		Content:        "Check quote service again",
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

	registerTestAgents(t, s1)
	ctx := context.Background()

	task1 := &domain.Task{
		ID:             "task-1",
		SenderAgentID:  "coordinator",
		TargetAgentID:  "quote",
		IdempotencyKey: "idem-001",
		Content:        "First task",
		Status:         domain.TaskStatusQueued,
	}
	if _, _, err := s1.SubmitTask(ctx, task1, nil, nil); err != nil {
		t.Fatalf("task1 submit failed: %v", err)
	}

	// Second task on store1
	task2 := &domain.Task{
		ID:             "task-2",
		SenderAgentID:  "coordinator",
		TargetAgentID:  "quote",
		IdempotencyKey: "idem-002",
		Content:        "Second task",
		Status:         domain.TaskStatusQueued,
	}
	_, _, err = s1.SubmitTask(ctx, task2, nil, nil)
	if !errors.Is(err, domain.ErrWorkerBusy) {
		t.Fatalf("expected ErrWorkerBusy for second active task, got: %v", err)
	}

	// Open a SECOND store connection directly to test DB-level partial unique index enforcement
	s2, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to open store2: %v", err)
	}
	defer s2.Close()

	task3 := &domain.Task{
		ID:             "task-3",
		SenderAgentID:  "coordinator",
		TargetAgentID:  "quote",
		IdempotencyKey: "idem-003",
		Content:        "Third task via store2",
		Status:         domain.TaskStatusQueued,
	}
	_, _, err = s2.SubmitTask(ctx, task3, nil, nil)
	if !errors.Is(err, domain.ErrWorkerBusy) {
		t.Fatalf("expected ErrWorkerBusy from store2 due to partial unique index, got: %v", err)
	}

	// Ack first task -> running
	ackEvt := &domain.Event{
		ID:           "evt-ack",
		TaskID:       "task-1",
		ActorAgentID: "quote",
		Type:         domain.EventTaskAcknowledged,
		Payload:      `{}`,
	}
	if _, err := s1.AckTask(ctx, "task-1", "quote", ackEvt); err != nil {
		t.Fatalf("AckTask failed: %v", err)
	}

	// Still busy when running
	_, _, err = s2.SubmitTask(ctx, task3, nil, nil)
	if !errors.Is(err, domain.ErrWorkerBusy) {
		t.Fatalf("expected ErrWorkerBusy when first task is running, got: %v", err)
	}

	// Complete first task -> succeeded
	compEvt := &domain.Event{
		ID:           "evt-comp",
		TaskID:       "task-1",
		ActorAgentID: "quote",
		Type:         domain.EventTaskSucceeded,
		Payload:      `{}`,
	}
	if _, err := s1.CompleteTask(ctx, "task-1", "quote", "all good", compEvt); err != nil {
		t.Fatalf("CompleteTask failed: %v", err)
	}

	// Now task3 via store2 must succeed!
	created3, isDup, err := s2.SubmitTask(ctx, task3, nil, nil)
	if err != nil {
		t.Fatalf("task3 submission after task1 completion failed: %v", err)
	}
	if isDup || created3.ID != "task-3" {
		t.Fatalf("unexpected task3 result: %+v, isDup=%v", created3, isDup)
	}
}

func TestStateTransitionsAndPermissions(t *testing.T) {
	s, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	registerTestAgents(t, s)

	task := &domain.Task{
		ID:             "task-state",
		SenderAgentID:  "coordinator",
		TargetAgentID:  "quote",
		IdempotencyKey: "idem-state",
		Content:        "Task state testing",
		Status:         domain.TaskStatusQueued,
	}
	if _, _, err := s.SubmitTask(ctx, task, nil, nil); err != nil {
		t.Fatalf("SubmitTask failed: %v", err)
	}

	// 1. Unauthorized ACK (coordinator trying to ACK)
	_, err := s.AckTask(ctx, "task-state", "coordinator", nil)
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized when coordinator acks, got: %v", err)
	}

	// 2. Complete before ACK (queued -> complete is invalid)
	_, err = s.CompleteTask(ctx, "task-state", "quote", "result", nil)
	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition when completing queued task, got: %v", err)
	}

	// 3. Proper ACK by target
	ackEvt := &domain.Event{
		ID:           "evt-ack",
		TaskID:       "task-state",
		ActorAgentID: "quote",
		Type:         domain.EventTaskAcknowledged,
		Payload:      `{}`,
	}
	tAcked, err := s.AckTask(ctx, "task-state", "quote", ackEvt)
	if err != nil {
		t.Fatalf("AckTask failed: %v", err)
	}
	if tAcked.Status != domain.TaskStatusRunning {
		t.Fatalf("expected status running, got %s", tAcked.Status)
	}

	// 4. Status update by quote
	statusMsg := &domain.Message{
		ID:            "msg-stat",
		TaskID:        "task-state",
		SenderAgentID: "quote",
		Kind:          domain.MessageKindStatusUpdate,
		Content:       "50% done",
	}
	statusEvt := &domain.Event{
		ID:           "evt-stat",
		TaskID:       "task-state",
		ActorAgentID: "quote",
		Type:         domain.EventTaskStatusUpdated,
		Payload:      `{"message":"50% done"}`,
	}
	if err := s.UpdateTaskStatus(ctx, "task-state", "quote", statusMsg, statusEvt); err != nil {
		t.Fatalf("UpdateTaskStatus failed: %v", err)
	}

	// Status update by unauthorized actor (coordinator)
	if err := s.UpdateTaskStatus(ctx, "task-state", "coordinator", statusMsg, statusEvt); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized when coordinator updates worker status, got: %v", err)
	}

	// 5. Send supplemental message by coordinator
	sendMsg := &domain.Message{
		ID:            "msg-supp",
		TaskID:        "task-state",
		SenderAgentID: "coordinator",
		Kind:          domain.MessageKindSupplement,
		Content:       "Also check cache",
	}
	sendEvt := &domain.Event{
		ID:           "evt-send",
		TaskID:       "task-state",
		ActorAgentID: "coordinator",
		Type:         domain.EventTaskMessageSent,
		Payload:      `{"content":"Also check cache"}`,
	}
	if err := s.SendMessage(ctx, "task-state", "coordinator", sendMsg, sendEvt); err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	// Send message by unauthorized actor (quote)
	if err := s.SendMessage(ctx, "task-state", "quote", sendMsg, sendEvt); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized when target sends coordinator supplemental msg, got: %v", err)
	}

	// 6. Cancel running task (must fail in V0)
	_, err = s.CancelTask(ctx, "task-state", "coordinator", nil)
	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition when cancelling running task, got: %v", err)
	}

	// 7. Complete task
	compEvt := &domain.Event{
		ID:           "evt-comp",
		TaskID:       "task-state",
		ActorAgentID: "quote",
		Type:         domain.EventTaskSucceeded,
		Payload:      `{"result":"finished successfully"}`,
	}
	tCompleted, err := s.CompleteTask(ctx, "task-state", "quote", "finished successfully", compEvt)
	if err != nil {
		t.Fatalf("CompleteTask failed: %v", err)
	}
	if tCompleted.Status != domain.TaskStatusSucceeded || *tCompleted.Result != "finished successfully" {
		t.Fatalf("unexpected completed task state: %+v", tCompleted)
	}

	// 8. Subsequent state transition on terminal state must fail
	_, err = s.FailTask(ctx, "task-state", "quote", "fail after success", nil)
	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition on terminal state, got: %v", err)
	}
}

func TestCancelQueuedTask(t *testing.T) {
	s, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	registerTestAgents(t, s)

	task := &domain.Task{
		ID:             "task-cancel",
		SenderAgentID:  "coordinator",
		TargetAgentID:  "quote",
		IdempotencyKey: "idem-cancel",
		Content:        "To be canceled",
		Status:         domain.TaskStatusQueued,
	}
	if _, _, err := s.SubmitTask(ctx, task, nil, nil); err != nil {
		t.Fatalf("SubmitTask failed: %v", err)
	}

	// Cancel by wrong actor
	_, err := s.CancelTask(ctx, "task-cancel", "quote", nil)
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized when target tries to cancel, got: %v", err)
	}

	// Cancel by coordinator
	cancelEvt := &domain.Event{
		ID:           "evt-cancel",
		TaskID:       "task-cancel",
		ActorAgentID: "coordinator",
		Type:         domain.EventTaskCanceled,
		Payload:      `{}`,
	}
	tCanceled, err := s.CancelTask(ctx, "task-cancel", "coordinator", cancelEvt)
	if err != nil {
		t.Fatalf("CancelTask failed: %v", err)
	}
	if tCanceled.Status != domain.TaskStatusCanceled {
		t.Fatalf("expected status canceled, got %s", tCanceled.Status)
	}
}

func TestEventOrderingAndPersistenceAcrossReopen(t *testing.T) {
	dir, err := os.MkdirTemp("", "agentbus-reopen-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	dbPath := filepath.Join(dir, "persist.db")
	s, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}

	ctx := context.Background()
	registerTestAgents(t, s)

	task := &domain.Task{
		ID:             "task-p",
		SenderAgentID:  "coordinator",
		TargetAgentID:  "quote",
		IdempotencyKey: "idem-p",
		Content:        "Persist test",
		Status:         domain.TaskStatusQueued,
	}
	evt1 := &domain.Event{
		ID:           "evt-p1",
		TaskID:       "task-p",
		ActorAgentID: "coordinator",
		Type:         domain.EventTaskSubmitted,
		Payload:      `{}`,
	}
	if _, _, err := s.SubmitTask(ctx, task, nil, evt1); err != nil {
		t.Fatalf("SubmitTask failed: %v", err)
	}

	evt2 := &domain.Event{
		ID:           "evt-p2",
		TaskID:       "task-p",
		ActorAgentID: "quote",
		Type:         domain.EventTaskAcknowledged,
		Payload:      `{}`,
	}
	if _, err := s.AckTask(ctx, "task-p", "quote", evt2); err != nil {
		t.Fatalf("AckTask failed: %v", err)
	}

	// Close database
	if err := s.Close(); err != nil {
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
