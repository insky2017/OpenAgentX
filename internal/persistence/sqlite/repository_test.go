package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"openagentx/internal/domain"
)

var repositoryTestTime = time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)

type repositoryFixture struct {
	ownerPrincipal string
	agentPrincipal string
	organizationID string
	agentID        string
}

func openTestRepository(t *testing.T, fault func(FaultPoint) error) (*Repository, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "openagentx.db")
	repository, err := Open(context.Background(), path, Options{
		Now: func() time.Time { return repositoryTestTime }, FaultInjector: fault,
	})
	if err != nil {
		t.Fatalf("open target repository: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	return repository, path
}

func journalEvent(id string, eventType string, actor string, organization string) *domain.JournalEvent {
	return &domain.JournalEvent{
		ID: id, OrganizationID: organization, EventType: eventType,
		ActorPrincipalID: actor, Payload: json.RawMessage(`{}`),
	}
}

func seedRepository(t *testing.T, repository *Repository) repositoryFixture {
	t.Helper()
	ctx := context.Background()
	fixture := repositoryFixture{
		ownerPrincipal: "human-owner", agentPrincipal: "agent-quote-principal",
		organizationID: "org-main", agentID: "quote",
	}
	owner := &domain.Principal{
		ID: fixture.ownerPrincipal, Kind: domain.PrincipalHuman,
		DisplayName: "Owner", Status: domain.IdentityActive,
	}
	if err := repository.CreatePrincipal(ctx, owner, journalEvent("event-principal-owner", "principal.created", fixture.ownerPrincipal, "")); err != nil {
		t.Fatalf("create owner principal: %v", err)
	}
	agentPrincipal := &domain.Principal{
		ID: fixture.agentPrincipal, Kind: domain.PrincipalAgent,
		DisplayName: "Quote Agent", Status: domain.IdentityActive,
	}
	if err := repository.CreatePrincipal(ctx, agentPrincipal, journalEvent("event-principal-agent", "principal.created", fixture.ownerPrincipal, "")); err != nil {
		t.Fatalf("create Agent principal: %v", err)
	}
	organization := &domain.Organization{ID: fixture.organizationID, Name: "Main", Status: domain.IdentityActive}
	if err := repository.CreateOrganization(ctx, organization, journalEvent("event-org", "organization.created", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatalf("create organization: %v", err)
	}
	agent := &domain.AgentIdentity{
		ID: fixture.agentID, PrincipalID: fixture.agentPrincipal, OrganizationID: fixture.organizationID,
		DisplayName: "Quote Service", Status: domain.AgentIdentityActive, Version: 1,
	}
	profile := &domain.AgentProfileRecord{
		AgentID: fixture.agentID, Version: 1, InstructionsPath: "/srv/openagentx/quote/ROLE.md",
		WorkspaceRoot: "/srv/openagentx/quote", Capabilities: []string{"quotes", "coding"},
	}
	if err := repository.CreateAgent(ctx, agent, profile, journalEvent("event-agent", "agent.created", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatalf("create logical Agent: %v", err)
	}
	return fixture
}

func newTaskDelivery(fixture repositoryFixture, suffix string) (*domain.Task, *domain.Message, *domain.MailboxItem, *domain.JournalEvent) {
	taskID := "task-" + suffix
	task := &domain.Task{
		ID: taskID, SenderPrincipalID: fixture.ownerPrincipal, TargetAgentID: fixture.agentID,
		OrganizationID: fixture.organizationID, DispatchMode: domain.DispatchModeDirect,
		IdempotencyKey: "idem-" + suffix, Content: "work " + suffix,
	}
	message := &domain.Message{ID: "message-" + suffix, TaskID: taskID, Content: "work " + suffix}
	mailbox := &domain.MailboxItem{ID: "mailbox-" + suffix}
	event := journalEvent("event-task-"+suffix, "task.created", fixture.ownerPrincipal, fixture.organizationID)
	return task, message, mailbox, event
}

func createTask(t *testing.T, repository *Repository, fixture repositoryFixture, suffix string) *CreateTaskResult {
	t.Helper()
	task, message, mailbox, event := newTaskDelivery(fixture, suffix)
	result, err := repository.CreateTask(context.Background(), task, message, mailbox, event)
	if err != nil {
		t.Fatalf("create Task %s: %v", suffix, err)
	}
	return result
}

func TestOpenConfiguresTargetSQLiteAndReopensCurrentState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reopen.db")
	ctx := context.Background()
	repository, err := Open(ctx, path, Options{Now: func() time.Time { return repositoryTestTime }})
	if err != nil {
		t.Fatal(err)
	}
	fixture := seedRepository(t, repository)
	created := createTask(t, repository, fixture, "reopen")

	var journalMode string
	var foreignKeys, busyTimeout int
	if err := repository.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if err := repository.db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if err := repository.db.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if journalMode != "wal" || foreignKeys != 1 || busyTimeout < 5000 {
		t.Fatalf("sqlite pragmas: journal=%s foreign_keys=%d busy_timeout=%d", journalMode, foreignKeys, busyTimeout)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, path, Options{Now: func() time.Time { return repositoryTestTime.Add(time.Minute) }})
	if err != nil {
		t.Fatalf("reopen repository: %v", err)
	}
	defer reopened.Close()
	got, err := reopened.GetTask(ctx, created.Task.ID)
	if err != nil || got.Content != created.Task.Content {
		t.Fatalf("reopened Task = %+v, err=%v", got, err)
	}
	agent, profile, err := reopened.GetAgent(ctx, fixture.agentID)
	if err != nil || agent.ID != fixture.agentID || len(profile.Capabilities) != 2 {
		t.Fatalf("reopened Agent/Profile = %+v / %+v, err=%v", agent, profile, err)
	}
	version, err := reopened.SchemaVersion(ctx)
	if err != nil || version != 1 {
		t.Fatalf("schema version=%d err=%v", version, err)
	}
}

func TestCreateTaskCommitsStateMessageMailboxAndJournalAtomically(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	result := createTask(t, repository, fixture, "atomic")
	if result.Replay || result.Task.Version != 1 || result.MailboxItem.Sequence <= 0 || result.Event.Sequence <= 0 {
		t.Fatalf("unexpected create result: %+v", result)
	}
	messages, err := repository.ListMessages(context.Background(), result.Task.ID)
	if err != nil || len(messages) != 1 || messages[0].Sequence != 1 {
		t.Fatalf("messages=%+v err=%v", messages, err)
	}
	mailbox, err := repository.GetMailboxItem(context.Background(), result.MailboxItem.ID)
	if err != nil || mailbox.TaskID != result.Task.ID || mailbox.Lane != domain.MailboxLaneWork {
		t.Fatalf("mailbox=%+v err=%v", mailbox, err)
	}
	events, err := repository.ListJournal(context.Background(), result.Event.Sequence-1, 10)
	if err != nil || len(events) == 0 || events[0].ID != result.Event.ID {
		t.Fatalf("journal=%+v err=%v", events, err)
	}
}

func TestCreateTaskRollbackAndIdempotency(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	injected := errors.New("stop before journal")
	failing := newRepository(repository.db, Options{
		Now: func() time.Time { return repositoryTestTime },
		FaultInjector: func(point FaultPoint) error {
			if point == FaultAfterDelivery {
				return injected
			}
			return nil
		},
	})
	task, message, mailbox, event := newTaskDelivery(fixture, "rollback")
	if _, err := failing.CreateTask(context.Background(), task, message, mailbox, event); !errors.Is(err, injected) {
		t.Fatalf("fault result=%v", err)
	}
	for table, selector := range map[string][2]string{
		"tasks": {"task_id", task.ID}, "messages": {"message_id", message.ID},
		"mailbox_items": {"mailbox_item_id", mailbox.ID}, "event_journal": {"event_id", event.ID},
	} {
		var count int
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s = ?", table, selector[0])
		if err := repository.db.QueryRow(query, selector[1]).Scan(&count); err != nil || count != 0 {
			t.Fatalf("rollback left %s row count=%d err=%v", table, count, err)
		}
	}

	original := createTask(t, repository, fixture, "idem")
	replayTask, replayMessage, replayMailbox, replayEvent := newTaskDelivery(fixture, "idem")
	replayTask.ID = "task-different-client-id"
	replayMessage.TaskID = replayTask.ID
	replayMessage.ID = "message-different-client-id"
	replayMailbox.ID = "mailbox-different-client-id"
	replayEvent.ID = "event-different-client-id"
	replay, err := repository.CreateTask(context.Background(), replayTask, replayMessage, replayMailbox, replayEvent)
	if err != nil || !replay.Replay || replay.Task.ID != original.Task.ID || replay.MailboxItem.ID != original.MailboxItem.ID {
		t.Fatalf("idempotent replay=%+v err=%v", replay, err)
	}
	replayTask.Content = "different work"
	if _, err := repository.CreateTask(context.Background(), replayTask, replayMessage, replayMailbox, replayEvent); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("idempotency conflict=%v", err)
	}
}

func TestConcurrentTaskIdempotencyReturnsOneCreationAndOneReplay(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	start := make(chan struct{})
	type outcome struct {
		result *CreateTaskResult
		err    error
	}
	outcomes := make(chan outcome, 2)
	var group sync.WaitGroup
	for index := 0; index < 2; index++ {
		index := index
		group.Add(1)
		go func() {
			defer group.Done()
			task, message, mailbox, event := newTaskDelivery(fixture, "concurrent-idem")
			task.ID = fmt.Sprintf("task-concurrent-%d", index)
			message.ID = fmt.Sprintf("message-concurrent-%d", index)
			message.TaskID = task.ID
			mailbox.ID = fmt.Sprintf("mailbox-concurrent-%d", index)
			event.ID = fmt.Sprintf("event-concurrent-%d", index)
			<-start
			result, err := repository.CreateTask(context.Background(), task, message, mailbox, event)
			outcomes <- outcome{result: result, err: err}
		}()
	}
	close(start)
	group.Wait()
	close(outcomes)
	created, replayed := 0, 0
	var persistedTaskID string
	for outcome := range outcomes {
		if outcome.err != nil {
			t.Fatalf("concurrent idempotent create: %v", outcome.err)
		}
		if persistedTaskID == "" {
			persistedTaskID = outcome.result.Task.ID
		} else if persistedTaskID != outcome.result.Task.ID {
			t.Fatalf("idempotent results disagree: %s != %s", persistedTaskID, outcome.result.Task.ID)
		}
		if outcome.result.Replay {
			replayed++
		} else {
			created++
		}
	}
	if created != 1 || replayed != 1 {
		t.Fatalf("idempotency outcomes: created=%d replayed=%d", created, replayed)
	}
	for table, where := range map[string]string{
		"tasks": "idempotency_key='idem-concurrent-idem'", "messages": "task_id='" + persistedTaskID + "'",
		"mailbox_items": "task_id='" + persistedTaskID + "'", "event_journal": "event_type='task.created' AND aggregate_id='" + persistedTaskID + "'",
	} {
		var count int
		if err := repository.db.QueryRow("SELECT COUNT(*) FROM " + table + " WHERE " + where).Scan(&count); err != nil || count != 1 {
			t.Fatalf("idempotent %s count=%d err=%v", table, count, err)
		}
	}
}

func TestTaskCASMessageAndForeignKeyEnforcement(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	created := createTask(t, repository, fixture, "cas")
	transitionEvent := journalEvent("event-task-dispatching", "task.dispatching", fixture.ownerPrincipal, fixture.organizationID)
	transitioned, err := repository.TransitionTask(context.Background(), TaskTransition{
		TaskID: created.Task.ID, ExpectedVersion: 1, AllowedFrom: []domain.TaskStatus{domain.TaskStatusQueued},
		To: domain.TaskStatusDispatching, Event: transitionEvent,
	})
	if err != nil || transitioned.Version != 2 {
		t.Fatalf("transitioned=%+v err=%v", transitioned, err)
	}
	if _, err := repository.TransitionTask(context.Background(), TaskTransition{
		TaskID: created.Task.ID, ExpectedVersion: 1, AllowedFrom: []domain.TaskStatus{domain.TaskStatusQueued},
		To: domain.TaskStatusRunning, Event: journalEvent("event-stale", "task.running", fixture.ownerPrincipal, fixture.organizationID),
	}); !errors.Is(err, domain.ErrStaleVersion) {
		t.Fatalf("stale CAS error=%v", err)
	}

	message := &domain.Message{
		ID: "message-followup", TaskID: created.Task.ID, SenderPrincipalID: fixture.ownerPrincipal,
		Kind: domain.MessageKindSupplement, Content: "more detail",
	}
	mailbox := &domain.MailboxItem{ID: "mailbox-followup", Lane: domain.MailboxLaneWork}
	messageResult, err := repository.CreateMessage(context.Background(), 2, message, mailbox,
		journalEvent("event-message", "message.created", fixture.ownerPrincipal, fixture.organizationID))
	if err != nil || messageResult.Task.Version != 3 || messageResult.Message.Sequence != 2 {
		t.Fatalf("message result=%+v err=%v", messageResult, err)
	}

	missingTask, missingMessage, missingMailbox, missingEvent := newTaskDelivery(fixture, "missing-agent")
	missingTask.TargetAgentID = "does-not-exist"
	if _, err := repository.CreateTask(context.Background(), missingTask, missingMessage, missingMailbox, missingEvent); err == nil {
		t.Fatal("foreign key violation must reject unknown target Agent")
	}
	var foreignKeyViolations int
	if err := repository.db.QueryRow("SELECT COUNT(*) FROM pragma_foreign_key_check").Scan(&foreignKeyViolations); err != nil || foreignKeyViolations != 0 {
		t.Fatalf("foreign key check=%d err=%v", foreignKeyViolations, err)
	}
}

func TestConcurrentBeginRunAllowsOneActiveRunAndRollsBackLoserTask(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	first := createTask(t, repository, fixture, "run-a")
	second := createTask(t, repository, fixture, "run-b")
	worker := &domain.WorkerInstance{
		ID: "worker-1", AgentID: fixture.agentID, Generation: 1, Transport: domain.WorkerTransportUnix,
		AuthenticatedPrincipal: "uds:1000", Status: domain.WorkerStatusOnline,
		Capabilities: []string{"fake"}, LeaseUntil: repositoryTestTime.Add(time.Hour), FencingToken: 1,
	}
	if err := repository.CreateWorkerInstance(context.Background(), worker,
		journalEvent("event-worker", "worker.registered", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	var group sync.WaitGroup
	results := make(chan error, 2)
	tasks := []domain.Task{first.Task, second.Task}
	for index := range tasks {
		index := index
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			runID := fmt.Sprintf("run-%d", index+1)
			run := &domain.RunAttempt{
				ID: runID, TaskID: tasks[index].ID, AgentID: fixture.agentID, Version: 1,
				Status: domain.RunAttemptStarting, WorkerInstanceID: worker.ID, FencingToken: 1,
				LeaseUntil: repositoryTestTime.Add(time.Hour), ExecutionSpecVersion: 1,
				RequestedExecutionJSON: `{}`, ResolvedExecutionJSON: `{}`, AdapterID: "fake",
				BackendID: "local", Model: "model-1", ReasoningMode: "effort", ReasoningValue: "high",
			}
			_, err := repository.BeginRunAttempt(context.Background(), tasks[index].Version, run,
				journalEvent(fmt.Sprintf("event-task-running-%d", index), "task.running", fixture.ownerPrincipal, fixture.organizationID),
				journalEvent(fmt.Sprintf("event-run-%d", index), "run_attempt.started", fixture.ownerPrincipal, fixture.organizationID))
			results <- err
		}()
	}
	close(start)
	group.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		var domainError *domain.DomainError
		if errors.As(err, &domainError) && domainError.Code == "CONFLICT" {
			conflicts++
			continue
		}
		t.Fatalf("unexpected concurrent BeginRun error: %v", err)
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("BeginRun outcomes: success=%d conflict=%d", successes, conflicts)
	}
	listed, err := repository.ListTasks(context.Background(), fixture.agentID, 10)
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[domain.TaskStatus]int{}
	for _, task := range listed {
		if task.ID == first.Task.ID || task.ID == second.Task.ID {
			statuses[task.Status]++
		}
	}
	if statuses[domain.TaskStatusRunning] != 1 || statuses[domain.TaskStatusQueued] != 1 {
		t.Fatalf("loser Task was not rolled back: statuses=%v", statuses)
	}
	var runCount, runEventCount int
	if err := repository.db.QueryRow("SELECT COUNT(*) FROM run_attempts").Scan(&runCount); err != nil {
		t.Fatal(err)
	}
	if err := repository.db.QueryRow("SELECT COUNT(*) FROM event_journal WHERE event_type='run_attempt.started'").Scan(&runEventCount); err != nil {
		t.Fatal(err)
	}
	if runCount != 1 || runEventCount != 1 {
		t.Fatalf("run count=%d event count=%d", runCount, runEventCount)
	}
}

func TestWorkerRunSessionAndJournalAreReadableAndJournalIsImmutable(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	created := createTask(t, repository, fixture, "entities")
	worker := &domain.WorkerInstance{
		ID: "worker-entity", AgentID: fixture.agentID, Generation: 1, Transport: domain.WorkerTransportUnix,
		AuthenticatedPrincipal: "uds:1000", Capabilities: []string{"fake"}, Status: domain.WorkerStatusOnline,
		LeaseUntil: repositoryTestTime.Add(time.Hour), FencingToken: 1,
	}
	if err := repository.CreateWorkerInstance(context.Background(), worker,
		journalEvent("event-worker-entity", "worker.registered", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	gotWorker, err := repository.GetWorkerInstance(context.Background(), worker.ID)
	if err != nil || gotWorker.Generation != 1 || len(gotWorker.Capabilities) != 1 {
		t.Fatalf("Worker=%+v err=%v", gotWorker, err)
	}
	run := &domain.RunAttempt{
		ID: "run-entity", TaskID: created.Task.ID, AgentID: fixture.agentID, Version: 1,
		Status: domain.RunAttemptRunning, WorkerInstanceID: worker.ID, FencingToken: 1,
		LeaseUntil: repositoryTestTime.Add(time.Hour), ExecutionSpecVersion: 1,
		RequestedExecutionJSON: `{}`, ResolvedExecutionJSON: `{}`, AdapterID: "fake", BackendID: "local",
		Model: "model-1", ReasoningMode: "effort", ReasoningValue: "high",
	}
	if _, err := repository.BeginRunAttempt(context.Background(), 1, run,
		journalEvent("event-task-running-entity", "task.running", fixture.ownerPrincipal, fixture.organizationID),
		journalEvent("event-run-entity", "run_attempt.started", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	gotRun, err := repository.GetRunAttempt(context.Background(), run.ID)
	if err != nil || gotRun.TaskID != created.Task.ID || gotRun.StartedAt.IsZero() {
		t.Fatalf("RunAttempt=%+v err=%v", gotRun, err)
	}
	binding := &domain.SessionBinding{
		ID: "binding-1", ContextID: "context-1", AgentID: fixture.agentID, BackendID: "local",
		ProviderSessionID: "provider-session-1", State: domain.SessionBindingActive, Version: 1,
	}
	if err := repository.CreateSessionBinding(context.Background(), binding,
		journalEvent("event-binding", "session_binding.created", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	gotBinding, err := repository.GetSessionBinding(context.Background(), binding.ContextID, fixture.agentID, binding.BackendID)
	if err != nil || gotBinding.ProviderSessionID != binding.ProviderSessionID {
		t.Fatalf("SessionBinding=%+v err=%v", gotBinding, err)
	}

	events, err := repository.ListJournal(context.Background(), 0, 100)
	if err != nil || len(events) < 9 {
		t.Fatalf("journal count=%d err=%v", len(events), err)
	}
	for index := 1; index < len(events); index++ {
		if events[index].Sequence <= events[index-1].Sequence {
			t.Fatalf("journal sequence is not monotonic at %d", index)
		}
	}
	firstSequence := events[0].Sequence
	page, err := repository.ListJournal(context.Background(), firstSequence, 2)
	if err != nil || len(page) != 2 || page[0].Sequence <= firstSequence {
		t.Fatalf("journal page=%+v err=%v", page, err)
	}
	if _, err := repository.db.Exec("UPDATE event_journal SET event_type='tampered' WHERE sequence=?", firstSequence); err == nil {
		t.Fatal("Event Journal update must be rejected")
	}
	if _, err := repository.db.Exec("DELETE FROM event_journal WHERE sequence=?", firstSequence); err == nil {
		t.Fatal("Event Journal delete must be rejected")
	}
}
