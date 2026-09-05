package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"openagentx/internal/api"
	"openagentx/internal/controlplane"
	"openagentx/internal/domain"
	"openagentx/internal/network/secretstore"
	openruntime "openagentx/internal/runtime"
	"openagentx/internal/runtime/descriptors"
)

func messageDescriptor(steer openruntime.SteerMode) openruntime.AdapterDescriptor {
	descriptor := descriptors.CodexACP()
	descriptor.Steer = steer
	descriptor.RuntimeIdentity = domain.RuntimeIdentity{AdapterID: descriptor.AdapterID, AdapterVersion: descriptor.Version}
	return descriptor
}

func trustedMessageNetwork(descriptor openruntime.AdapterDescriptor) domain.NetworkPolicy {
	mode := domain.NetworkModePolicy{AgentID: "quote", BackendID: "local", PolicyVersion: 1, Mode: domain.NetworkInherit}
	return domain.NetworkPolicy{
		Mode: domain.NetworkInherit, PolicyVersion: 1, BindingRevision: 1,
		ManifestDigest: mode.ComputeManifestDigest(), RuntimeIdentity: descriptor.RuntimeIdentity,
	}
}

func registerMessageWorker(t *testing.T, repository *Repository, fixture repositoryFixture, descriptor openruntime.AdapterDescriptor) (*domain.WorkerInstance, domain.WorkerWriteGuard) {
	t.Helper()
	ctx := context.Background()
	registration := domain.WorkerRegistration{
		WorkerInstanceID: "worker-message-route", AgentID: fixture.agentID, Transport: domain.WorkerTransportUnix,
		PrincipalID: fixture.agentPrincipal, Capabilities: []string{"coding"}, SessionTokenDigest: "message-route-token-digest",
		TokenExpiresAt: repositoryTestTime.Add(time.Hour), LeaseUntil: repositoryTestTime.Add(time.Hour),
	}
	worker, _, err := repository.RegisterWorker(ctx, registration, []openruntime.BackendRegistration{{
		BackendID: "local", Descriptor: descriptor, Health: openruntime.BackendHealthy, Network: domain.NetworkPolicy{Mode: domain.NetworkInherit},
	}}, journalEvent("event-worker-message-route", "worker.registered", fixture.ownerPrincipal, fixture.organizationID))
	if err != nil {
		t.Fatalf("register Message worker: %v", err)
	}
	guard := domain.WorkerWriteGuard{
		WorkerInstanceID: worker.ID, AgentID: fixture.agentID, PrincipalID: fixture.agentPrincipal,
		SessionTokenDigest: registration.SessionTokenDigest, Generation: worker.Generation, FencingToken: worker.FencingToken,
		CheckedAt: repositoryTestTime,
	}
	if _, err := repository.HeartbeatWorker(ctx, guard, domain.WorkerStatusOnline, nil,
		repositoryTestTime.Add(time.Hour), repositoryTestTime.Add(time.Hour),
		nil,
		journalEvent("event-worker-message-route-online", "worker.heartbeat", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatalf("activate Message worker: %v", err)
	}
	secrets, err := secretstore.Open(filepath.Join(t.TempDir(), "network-secrets"))
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := controlplane.NewNetworkWorkflowService(repository, secrets, nil, func() time.Time { return repositoryTestTime })
	if err != nil {
		t.Fatal(err)
	}
	testResult, err := workflow.StartModeTest(ctx, fixture.ownerPrincipal, api.TestNetworkModeRequest{
		Meta:    api.CommandMeta{IdempotencyKey: "message-bootstrap-inherit", ExpectedVersion: 0},
		AgentID: fixture.agentID, BackendID: "local", Mode: domain.NetworkInherit,
		WorkerInstanceID: worker.ID, Generation: worker.Generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		TestID string `json:"test_id"`
	}
	if err := json.Unmarshal(testResult, &receipt); err != nil || receipt.TestID == "" {
		t.Fatalf("decode inherit mode test receipt=%s err=%v", testResult, err)
	}
	testWork := pullNetworkWork(t, workflow, guard)
	ackNetworkWork(t, workflow, guard, testWork.Work.ID, api.NetworkWorkAckRequest{State: "succeeded", ProbeResults: successfulInheritProbeResults()})
	if _, err := workflow.PublishMode(ctx, fixture.ownerPrincipal, api.PublishNetworkModeRequest{
		Meta: api.CommandMeta{IdempotencyKey: "message-publish-inherit", ExpectedVersion: 0}, TestID: receipt.TestID,
		WorkerInstanceID: worker.ID, Generation: worker.Generation,
	}); err != nil {
		t.Fatal(err)
	}
	applyWork := pullNetworkWork(t, workflow, guard)
	policy := policyForWork(applyWork.Work)
	ackNetworkWork(t, workflow, guard, applyWork.Work.ID, api.NetworkWorkAckRequest{State: "succeeded", Policy: &policy})
	return worker, guard
}

func successfulInheritProbeResults() []domain.NetworkProbeResult {
	results := successfulProbeResults()
	for index := range results {
		if results[index].Layer == domain.NetworkProbeSecret || results[index].Layer == domain.NetworkProbeEndpoint {
			results[index].State = domain.NetworkProbeNotVerified
			results[index].DiagnosticCode = domain.NetworkDiagnosticInheritUnknown
		}
	}
	return results
}

func beginMessageRun(t *testing.T, repository *Repository, fixture repositoryFixture, worker *domain.WorkerInstance, descriptor openruntime.AdapterDescriptor, suffix string) (*CreateTaskResult, *domain.RunAttempt) {
	t.Helper()
	created := createTask(t, repository, fixture, suffix)
	run := &domain.RunAttempt{
		ID: "run-" + suffix, TaskID: created.Task.ID, AgentID: fixture.agentID, Version: 1,
		Status: domain.RunAttemptRunning, WorkerInstanceID: worker.ID, FencingToken: worker.FencingToken,
		LeaseUntil: repositoryTestTime.Add(time.Hour), ExecutionSpecVersion: 1, RequestedExecutionJSON: `{}`,
		ResolvedExecutionJSON: `{}`, AdapterID: descriptor.AdapterID, BackendID: "local", Model: descriptor.Models[0],
		ReasoningMode: domain.ReasoningBackendDefault,
	}
	if _, err := repository.BeginRunAttempt(context.Background(), created.Task.Version, run,
		journalEvent("event-task-running-"+suffix, "task.running", fixture.ownerPrincipal, fixture.organizationID),
		journalEvent("event-run-started-"+suffix, "run_attempt.started", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatalf("begin Message run: %v", err)
	}
	return created, run
}

func createRoutedMessage(t *testing.T, repository *Repository, fixture repositoryFixture, taskID string, version int64, suffix string) (*CreateMessageResult, error) {
	t.Helper()
	return repository.CreateMessage(context.Background(), version,
		&domain.Message{ID: "message-followup-" + suffix, TaskID: taskID, SenderPrincipalID: fixture.ownerPrincipal,
			Kind: domain.MessageKindSupplement, Content: "follow up " + suffix},
		&domain.MailboxItem{ID: "mailbox-followup-" + suffix, State: domain.MailboxStatePending},
		journalEvent("event-message-"+suffix, "task.message_created", fixture.ownerPrincipal, fixture.organizationID))
}

func TestCreateMessageRoutesByActiveBackendCapability(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		steer openruntime.SteerMode
		lane  domain.MailboxLane
	}{
		{name: "native is active Run control", steer: openruntime.SteerNative, lane: domain.MailboxLaneControl},
		{name: "queued is deferred work", steer: openruntime.SteerQueued, lane: domain.MailboxLaneWork},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repository, _ := openTestRepository(t, nil)
			fixture := seedRepository(t, repository)
			descriptor := messageDescriptor(testCase.steer)
			worker, _ := registerMessageWorker(t, repository, fixture, descriptor)
			created, run := beginMessageRun(t, repository, fixture, worker, descriptor, "route-"+string(testCase.steer))
			result, err := createRoutedMessage(t, repository, fixture, created.Task.ID, 2, "route-"+string(testCase.steer))
			if err != nil {
				t.Fatal(err)
			}
			if result.MailboxItem.Lane != testCase.lane || result.MailboxItem.Sequence <= 0 {
				t.Fatalf("mailbox=%+v", result.MailboxItem)
			}
			if testCase.lane == domain.MailboxLaneControl && (result.MailboxItem.TargetRunID != run.ID || result.MailboxItem.ExpectedRunVersion != run.Version) {
				t.Fatalf("native Message must target active Run: %+v", result.MailboxItem)
			}
			if testCase.lane == domain.MailboxLaneWork && (result.MailboxItem.TargetRunID != "" || result.MailboxItem.ExpectedRunVersion != 0) {
				t.Fatalf("queued Message must not target active Run: %+v", result.MailboxItem)
			}
		})
	}
}

func TestCreateMessageDoesNotRouteToAnotherTasksActiveRun(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	descriptor := messageDescriptor(openruntime.SteerNative)
	worker, _ := registerMessageWorker(t, repository, fixture, descriptor)
	_, foreignRun := beginMessageRun(t, repository, fixture, worker, descriptor, "foreign-active-run")
	target := createTask(t, repository, fixture, "message-own-task")

	result, err := createRoutedMessage(t, repository, fixture, target.Task.ID, target.Task.Version, "message-own-task")
	if err != nil {
		t.Fatal(err)
	}
	if result.MailboxItem.Lane != domain.MailboxLaneWork || result.MailboxItem.TargetRunID != "" || result.MailboxItem.ExpectedRunVersion != 0 {
		t.Fatalf("Message must not target another Task's active Run: mailbox=%+v foreignRun=%+v", result.MailboxItem, foreignRun)
	}
}

func TestCreateMessageRejectsUnsupportedAndRollsBack(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	descriptor := messageDescriptor(openruntime.SteerUnsupported)
	worker, _ := registerMessageWorker(t, repository, fixture, descriptor)
	created, _ := beginMessageRun(t, repository, fixture, worker, descriptor, "unsupported-message")
	_, err := createRoutedMessage(t, repository, fixture, created.Task.ID, 2, "unsupported-message")
	if !errors.Is(err, domain.ErrUnsupportedCapability) {
		t.Fatalf("error=%v", err)
	}
	task, err := repository.GetTask(context.Background(), created.Task.ID)
	if err != nil || task.Version != 2 {
		t.Fatalf("Task after rejected Message=%+v err=%v", task, err)
	}
	messages, err := repository.ListMessages(context.Background(), created.Task.ID)
	if err != nil || len(messages) != 1 {
		t.Fatalf("messages=%+v err=%v", messages, err)
	}
	var count int
	if err := repository.db.QueryRow(`SELECT COUNT(*) FROM mailbox_items WHERE task_id=?`, created.Task.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("mailbox count=%d err=%v", count, err)
	}
}

func TestCreateMessageRejectsInvalidPersistedSteerCapability(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	descriptor := messageDescriptor(openruntime.SteerNative)
	worker, _ := registerMessageWorker(t, repository, fixture, descriptor)
	created, _ := beginMessageRun(t, repository, fixture, worker, descriptor, "invalid-persisted-steer")
	descriptor.Steer = openruntime.SteerMode("invalid")
	raw, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.db.Exec(`UPDATE runtime_backend_registrations SET descriptor_json=?
		WHERE worker_instance_id=? AND backend_id='local'`, string(raw), worker.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := createRoutedMessage(t, repository, fixture, created.Task.ID, 2, "invalid-persisted-steer"); !errors.Is(err, domain.ErrUnsupportedCapability) {
		t.Fatalf("invalid persisted capability error=%v", err)
	}
	task, err := repository.GetTask(context.Background(), created.Task.ID)
	if err != nil || task.Version != 2 {
		t.Fatalf("Task after invalid capability=%+v err=%v", task, err)
	}
}

func TestCreateMessageRejectsInvalidEffectiveBackendDescriptorAndRollsBack(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	descriptor := messageDescriptor(openruntime.SteerQueued)
	worker, _ := registerMessageWorker(t, repository, fixture, descriptor)
	created := createTask(t, repository, fixture, "invalid-effective-descriptor")

	if _, err := repository.db.Exec(`UPDATE runtime_backend_registrations SET descriptor_json=?
		WHERE worker_instance_id=? AND backend_id='local'`, `{"adapter_id":`, worker.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := createRoutedMessage(t, repository, fixture, created.Task.ID, created.Task.Version, "invalid-effective-descriptor"); err == nil {
		t.Fatal("invalid effective descriptor must fail closed")
	}
	task, err := repository.GetTask(context.Background(), created.Task.ID)
	if err != nil || task.Version != created.Task.Version {
		t.Fatalf("Task after invalid effective descriptor=%+v err=%v", task, err)
	}
	messages, err := repository.ListMessages(context.Background(), created.Task.ID)
	if err != nil || len(messages) != 1 {
		t.Fatalf("messages after invalid effective descriptor=%+v err=%v", messages, err)
	}
}

func TestCreateMessageUsesEffectiveBackendWhenTaskWaitingInput(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	registerMessageWorker(t, repository, fixture, messageDescriptor(openruntime.SteerQueued))
	created := createTask(t, repository, fixture, "waiting-input-route")
	if _, err := repository.TransitionTask(context.Background(), domain.TaskTransition{
		TaskID: created.Task.ID, ExpectedVersion: created.Task.Version, AllowedFrom: []domain.TaskStatus{domain.TaskStatusQueued},
		To: domain.TaskStatusWaitingInput, Event: journalEvent("event-waiting-input-route", "task.waiting_input", fixture.ownerPrincipal, fixture.organizationID),
	}); err != nil {
		t.Fatal(err)
	}
	result, err := createRoutedMessage(t, repository, fixture, created.Task.ID, 2, "waiting-input-route")
	if err != nil || result.MailboxItem.Lane != domain.MailboxLaneWork || result.MailboxItem.TargetRunID != "" {
		t.Fatalf("waiting-input Message=%+v err=%v", result, err)
	}
}

func TestCreateMessageUsesResumeOnlyBackendWhenTaskHasActiveBinding(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	descriptor := messageDescriptor(openruntime.SteerQueued)
	descriptor.SessionModes = []domain.SessionMode{domain.SessionModeResume}
	registerMessageWorker(t, repository, fixture, descriptor)
	created := createTask(t, repository, fixture, "resume-only-route")
	binding := &domain.SessionBinding{
		ID: "binding-resume-only-route", ContextID: created.Task.ID, AgentID: fixture.agentID,
		BackendID: "local", ProviderSessionID: "provider-resume-only", State: domain.SessionBindingActive, Version: 1,
	}
	if err := repository.SaveSessionBinding(context.Background(), binding, 0,
		journalEvent("event-binding-resume-only-route", "session_binding.created", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}

	result, err := createRoutedMessage(t, repository, fixture, created.Task.ID, created.Task.Version, "resume-only-route")
	if err != nil || result.MailboxItem.Lane != domain.MailboxLaneWork || result.MailboxItem.TargetRunID != "" {
		t.Fatalf("resumable effective backend Message=%+v err=%v", result, err)
	}
}

func TestCreateMessageRejectsWhenNoActiveRunAndNoDeferredBackend(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	registerMessageWorker(t, repository, fixture, messageDescriptor(openruntime.SteerUnsupported))
	created := createTask(t, repository, fixture, "waiting-input-unsupported")
	if _, err := repository.TransitionTask(context.Background(), domain.TaskTransition{
		TaskID: created.Task.ID, ExpectedVersion: created.Task.Version, AllowedFrom: []domain.TaskStatus{domain.TaskStatusQueued},
		To: domain.TaskStatusWaitingInput, Event: journalEvent("event-waiting-input-unsupported", "task.waiting_input", fixture.ownerPrincipal, fixture.organizationID),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := createRoutedMessage(t, repository, fixture, created.Task.ID, 2, "waiting-input-unsupported"); !errors.Is(err, domain.ErrUnsupportedCapability) {
		t.Fatalf("error=%v", err)
	}
	messages, err := repository.ListMessages(context.Background(), created.Task.ID)
	if err != nil || len(messages) != 1 {
		t.Fatalf("messages=%+v err=%v", messages, err)
	}
}

func TestCreateMessageCommandReplayIsIdempotent(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	registerMessageWorker(t, repository, fixture, messageDescriptor(openruntime.SteerQueued))
	created := createTask(t, repository, fixture, "message-idempotent")
	service, err := controlplane.NewCommandService(repository, nil, func() time.Time { return repositoryTestTime })
	if err != nil {
		t.Fatal(err)
	}
	req := api.CreateMessageRequest{Meta: api.CommandMeta{IdempotencyKey: "message-idempotency-key", ExpectedVersion: 1}, SenderPrincipalID: fixture.ownerPrincipal, Content: "same message"}
	first, err := service.CreateMessage(context.Background(), fixture.ownerPrincipal, created.Task.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateMessage(context.Background(), fixture.ownerPrincipal, created.Task.ID, req)
	if err != nil || second.MessageID != first.MessageID || second.Sequence != first.Sequence {
		t.Fatalf("Message replay first=%+v second=%+v err=%v", first, second, err)
	}
	messages, err := repository.ListMessages(context.Background(), created.Task.ID)
	if err != nil || len(messages) != 2 {
		t.Fatalf("messages=%+v err=%v", messages, err)
	}
	conflicting := req
	conflicting.Content = "different message"
	if _, err := service.CreateMessage(context.Background(), fixture.ownerPrincipal, created.Task.ID, conflicting); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("conflicting replay error=%v", err)
	}
}

func TestCreateMessageConcurrentReplayCreatesOneDelivery(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	registerMessageWorker(t, repository, fixture, messageDescriptor(openruntime.SteerQueued))
	created := createTask(t, repository, fixture, "concurrent-message-idempotency")

	type outcome struct {
		result *CreateMessageResult
		err    error
	}
	start := make(chan struct{})
	results := make(chan outcome, 2)
	var group sync.WaitGroup
	for index := 0; index < 2; index++ {
		index := index
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			result, err := repository.CreateMessage(context.Background(), created.Task.Version,
				&domain.Message{ID: "message-concurrent-replay", TaskID: created.Task.ID, SenderPrincipalID: fixture.ownerPrincipal,
					Kind: domain.MessageKindSupplement, Content: "same concurrent message"},
				&domain.MailboxItem{ID: "mailbox-concurrent-replay-" + string(rune('a'+index)), State: domain.MailboxStatePending},
				journalEvent("event-message-concurrent-replay-"+string(rune('a'+index)), "task.message_created", fixture.ownerPrincipal, fixture.organizationID))
			results <- outcome{result: result, err: err}
		}()
	}
	close(start)
	group.Wait()
	close(results)

	var messageID string
	for outcome := range results {
		if outcome.err != nil {
			t.Fatalf("concurrent CreateMessage error=%v", outcome.err)
		}
		if outcome.result == nil {
			t.Fatal("concurrent CreateMessage returned nil result")
		}
		if messageID == "" {
			messageID = outcome.result.Message.ID
		} else if messageID != outcome.result.Message.ID {
			t.Fatalf("concurrent replay results disagree: %q != %q", messageID, outcome.result.Message.ID)
		}
	}
	messages, err := repository.ListMessages(context.Background(), created.Task.ID)
	if err != nil || len(messages) != 2 || messages[1].ID != "message-concurrent-replay" {
		t.Fatalf("persisted concurrent Message=%+v err=%v", messages, err)
	}
	var mailboxCount, eventCount int
	if err := repository.db.QueryRow(`SELECT COUNT(*) FROM mailbox_items WHERE message_id=?`, messageID).Scan(&mailboxCount); err != nil || mailboxCount != 1 {
		t.Fatalf("concurrent Message mailbox count=%d err=%v", mailboxCount, err)
	}
	if err := repository.db.QueryRow(`SELECT COUNT(*) FROM event_journal WHERE aggregate_type='task' AND aggregate_id=? AND event_type='task.message_created'`, created.Task.ID).Scan(&eventCount); err != nil || eventCount != 1 {
		t.Fatalf("concurrent Message event count=%d err=%v", eventCount, err)
	}
	task, err := repository.GetTask(context.Background(), created.Task.ID)
	if err != nil || task.Version != created.Task.Version+1 {
		t.Fatalf("concurrent Message Task=%+v err=%v", task, err)
	}
}

func TestSupersededControlMessageDefersSameMailboxItem(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	descriptor := messageDescriptor(openruntime.SteerNative)
	worker, guard := registerMessageWorker(t, repository, fixture, descriptor)
	created, run := beginMessageRun(t, repository, fixture, worker, descriptor, "defer-message")
	// BeginRunAttempt is an unguarded test setup helper and does not consume the
	// original Task mailbox item. Mark that setup item consumed so the assertion
	// below can prove the deferred Message is the next claimable work item.
	if _, err := repository.db.Exec(`UPDATE mailbox_items SET state='accepted' WHERE mailbox_item_id=?`, created.MailboxItem.ID); err != nil {
		t.Fatal(err)
	}
	message, err := createRoutedMessage(t, repository, fixture, created.Task.ID, 2, "defer-message")
	if err != nil || message.MailboxItem.Lane != domain.MailboxLaneControl {
		t.Fatalf("create control Message=%+v err=%v", message, err)
	}
	claimUntil := repositoryTestTime.Add(10 * time.Minute)
	claimed, err := repository.TryClaimMailbox(context.Background(), guard, 1, claimUntil,
		journalEvent("event-claim-defer-message", "mailbox.claimed", fixture.ownerPrincipal, fixture.organizationID))
	if err != nil || claimed == nil || claimed.ID != message.MailboxItem.ID {
		t.Fatalf("claim control Message=%+v err=%v", claimed, err)
	}
	if err := repository.FinishRun(context.Background(), guard, run.ID, 2, run.Version,
		openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "turn completed", SideEffectsKnown: true}, nil, 0, nil,
		journalEvent("event-task-finish-defer-message", "task.waiting_input", fixture.ownerPrincipal, fixture.organizationID),
		journalEvent("event-run-finish-defer-message", "run_attempt.finished", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	task, err := repository.GetTask(context.Background(), created.Task.ID)
	if err != nil || task.Status != domain.TaskStatusWaitingInput {
		t.Fatalf("pending native Message did not keep Task open: task=%+v err=%v", task, err)
	}
	if err := repository.AcceptMailboxItem(context.Background(), guard, claimed.ID, domain.MailboxStateClaimed, domain.MailboxStateSuperseded,
		journalEvent("event-defer-message", "mailbox.superseded", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	if err := repository.AcceptMailboxItem(context.Background(), guard, claimed.ID, domain.MailboxStateClaimed, domain.MailboxStateSuperseded,
		journalEvent("event-defer-message-retry", "mailbox.superseded", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatalf("idempotent Message defer retry: %v", err)
	}
	deferred, err := repository.GetMailboxItem(context.Background(), claimed.ID)
	if err != nil || deferred.Lane != domain.MailboxLaneWork || deferred.State != domain.MailboxStatePending || deferred.TargetRunID != "" || deferred.ExpectedRunVersion != 0 || deferred.WorkerInstanceID != "" || deferred.LeaseUntil != nil {
		t.Fatalf("deferred Message=%+v err=%v", deferred, err)
	}
	next, err := repository.TryClaimMailbox(context.Background(), guard, 1, repositoryTestTime.Add(20*time.Minute),
		journalEvent("event-reclaim-defer-message", "mailbox.claimed", fixture.ownerPrincipal, fixture.organizationID))
	if err != nil || next == nil || next.ID != claimed.ID || next.Sequence != claimed.Sequence || next.Attempts != 2 {
		t.Fatalf("reclaimed deferred Message=%+v err=%v", next, err)
	}
	if err := repository.AcceptMailboxItem(context.Background(), guard, claimed.ID, domain.MailboxStateClaimed, domain.MailboxStateSuperseded,
		journalEvent("event-defer-message-after-reclaim-retry", "mailbox.superseded", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatalf("old Message defer acknowledgement after reclaim: %v", err)
	}
	afterOldRetry, err := repository.GetMailboxItem(context.Background(), claimed.ID)
	if err != nil || afterOldRetry.State != domain.MailboxStateClaimed || afterOldRetry.Lane != domain.MailboxLaneWork ||
		afterOldRetry.WorkerInstanceID != guard.WorkerInstanceID || afterOldRetry.Attempts != 2 {
		t.Fatalf("old defer acknowledgement changed reclaimed Message=%+v err=%v", afterOldRetry, err)
	}
	var eventType string
	if err := repository.db.QueryRow(`SELECT event_type FROM event_journal WHERE event_id=?`, "event-defer-message").Scan(&eventType); err != nil || eventType != "mailbox.message_deferred" {
		t.Fatalf("defer Event type=%q err=%v", eventType, err)
	}
	var retryEvents int
	if err := repository.db.QueryRow(`SELECT COUNT(*) FROM event_journal WHERE event_id='event-defer-message-retry'`).Scan(&retryEvents); err != nil || retryEvents != 0 {
		t.Fatalf("idempotent defer retry appended event: count=%d err=%v", retryEvents, err)
	}
	if err := repository.db.QueryRow(`SELECT COUNT(*) FROM event_journal WHERE event_id='event-defer-message-after-reclaim-retry'`).Scan(&retryEvents); err != nil || retryEvents != 0 {
		t.Fatalf("old defer acknowledgement after reclaim appended event: count=%d err=%v", retryEvents, err)
	}
}
