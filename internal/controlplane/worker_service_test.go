package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agentbus/internal/api"
	"agentbus/internal/domain"
	openagentsqlite "agentbus/internal/persistence/sqlite"
	openruntime "agentbus/internal/runtime"
	"agentbus/internal/testkit"
)

type workerTestEnvironment struct {
	repository *openagentsqlite.Repository
	service    *WorkerService
	broker     *MemoryWakeupBroker
	clock      *testkit.FakeClock
	ownerID    string
	workerID   string
	agentID    string
	orgID      string
	backend    openruntime.BackendRegistration
}

func newWorkerTestEnvironment(t *testing.T, customize func(*WorkerServiceOptions)) *workerTestEnvironment {
	t.Helper()
	ctx := context.Background()
	clock := testkit.NewFakeClock(time.Date(2026, 8, 30, 15, 0, 0, 0, time.UTC))
	repository, err := openagentsqlite.Open(ctx, filepath.Join(t.TempDir(), "worker.db"), openagentsqlite.Options{Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	environment := &workerTestEnvironment{
		repository: repository, broker: NewMemoryWakeupBroker(), clock: clock,
		ownerID: "human-owner", workerID: "worker-principal", agentID: "quote", orgID: "org-main",
	}
	seedWorkerEnvironment(t, environment)
	descriptor := openruntime.AdapterDescriptor{
		AdapterID: "fake", BackendType: "fake", Version: "1", LaunchProtocol: "inproc",
		Models: []string{"model-1"}, ReasoningModes: []domain.ReasoningMode{domain.ReasoningBackendDefault},
		SessionModes: []domain.SessionMode{domain.SessionModeNew}, Steer: openruntime.SteerQueued,
		Approval: openruntime.ApprovalPreflight, Cancel: openruntime.CancelProcessSignal,
		BackendOptionsJSON: json.RawMessage(`{"type":"object"}`), MaxConcurrency: 1,
	}
	environment.backend = openruntime.BackendRegistration{BackendID: "local", Descriptor: descriptor, Health: openruntime.BackendHealthy}
	var idCounter atomic.Int64
	var tokenCounter atomic.Int64
	options := WorkerServiceOptions{
		Now:   clock.Now,
		NewID: func(prefix string) string { return fmt.Sprintf("%s-%04d", prefix, idCounter.Add(1)) },
		NewSessionToken: func() (string, error) {
			return fmt.Sprintf("worker-session-token-%040d", tokenCounter.Add(1)), nil
		},
		WorkerLease: time.Minute, TokenLifetime: 10 * time.Minute,
		MailboxLease: 10 * time.Second, RunLease: 2 * time.Minute,
	}
	if customize != nil {
		customize(&options)
	}
	service, err := NewWorkerService(repository, environment.broker, options)
	if err != nil {
		t.Fatal(err)
	}
	environment.service = service
	return environment
}

func seedWorkerEnvironment(t *testing.T, environment *workerTestEnvironment) {
	t.Helper()
	ctx := context.Background()
	createPrincipal := func(principal domain.Principal, eventID string, actor string) {
		if err := environment.repository.CreatePrincipal(ctx, &principal, &domain.JournalEvent{
			ID: eventID, EventType: "principal.created", ActorPrincipalID: actor,
			Payload: json.RawMessage(`{}`),
		}); err != nil {
			t.Fatal(err)
		}
	}
	createPrincipal(domain.Principal{ID: environment.ownerID, Kind: domain.PrincipalHuman, DisplayName: "Owner", Status: domain.IdentityActive}, "event-owner", environment.ownerID)
	createPrincipal(domain.Principal{ID: "agent-principal", Kind: domain.PrincipalAgent, DisplayName: "Quote Agent", Status: domain.IdentityActive}, "event-agent-principal", environment.ownerID)
	createPrincipal(domain.Principal{ID: environment.workerID, Kind: domain.PrincipalWorker, DisplayName: "Local Worker", Status: domain.IdentityActive}, "event-worker-principal", environment.ownerID)
	organization := &domain.Organization{ID: environment.orgID, Name: "Main", Status: domain.IdentityActive}
	if err := environment.repository.CreateOrganization(ctx, organization, &domain.JournalEvent{
		ID: "event-org", OrganizationID: environment.orgID, EventType: "organization.created",
		ActorPrincipalID: environment.ownerID, Payload: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	agent := &domain.AgentIdentity{
		ID: environment.agentID, PrincipalID: "agent-principal", OrganizationID: environment.orgID,
		DisplayName: "Quote", Status: domain.AgentIdentityActive, Version: 1,
	}
	profile := &domain.AgentProfileRecord{
		AgentID: environment.agentID, Version: 1, InstructionsPath: "/roles/quote.md",
		WorkspaceRoot: "/workspace/quote", Capabilities: []string{"coding"},
	}
	if err := environment.repository.CreateAgent(ctx, agent, profile, &domain.JournalEvent{
		ID: "event-agent", OrganizationID: environment.orgID, EventType: "agent.created",
		ActorPrincipalID: environment.ownerID, Payload: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
}

func (environment *workerTestEnvironment) register(t *testing.T, workerInstanceID string) *api.WorkerSession {
	t.Helper()
	session, err := environment.service.Register(context.Background(), environment.workerID, api.RegisterRequest{
		ContractVersion: api.ContractVersion, AgentID: environment.agentID, WorkerInstanceID: workerInstanceID,
		Transport: domain.WorkerTransportUnix, Capabilities: []string{"coding"},
		Backends: []openruntime.BackendRegistration{environment.backend},
	})
	if err != nil {
		t.Fatalf("register Worker: %v", err)
	}
	return session
}

func (environment *workerTestEnvironment) heartbeat(t *testing.T, session *api.WorkerSession) {
	t.Helper()
	err := environment.service.Heartbeat(context.Background(), environment.workerID, session.SessionToken, api.HeartbeatRequest{
		WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation,
		FencingToken: session.Worker.FencingToken, Status: domain.WorkerStatusOnline,
		BackendHealth: map[string]openruntime.BackendHealth{"local": openruntime.BackendHealthy},
	})
	if err != nil {
		t.Fatalf("heartbeat Worker: %v", err)
	}
}

func (environment *workerTestEnvironment) createTask(t *testing.T, suffix string) *domain.CreateTaskResult {
	t.Helper()
	task := &domain.Task{
		ID: "task-" + suffix, SenderPrincipalID: environment.ownerID, TargetAgentID: environment.agentID,
		OrganizationID: environment.orgID, DispatchMode: domain.DispatchModeDirect,
		IdempotencyKey: "idem-" + suffix, Content: "work " + suffix,
	}
	message := &domain.Message{ID: "message-" + suffix, TaskID: task.ID, Content: task.Content}
	mailbox := &domain.MailboxItem{ID: "mailbox-" + suffix}
	event := &domain.JournalEvent{
		ID: "event-task-" + suffix, OrganizationID: environment.orgID, EventType: "task.created",
		ActorPrincipalID: environment.ownerID, Payload: json.RawMessage(`{}`),
	}
	result, err := environment.repository.CreateTask(context.Background(), task, message, mailbox, event)
	if err != nil {
		t.Fatalf("create Task: %v", err)
	}
	return result
}

func claimRequest(session *api.WorkerSession, workCapacity int) api.ClaimRequest {
	return api.ClaimRequest{
		WorkerInstanceID: session.Worker.ID, AgentID: session.Worker.AgentID,
		Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken,
		WorkCapacity: workCapacity,
	}
}

func TestWorkerServiceRegisterClaimBeginEventsAndFinish(t *testing.T) {
	environment := newWorkerTestEnvironment(t, nil)
	session := environment.register(t, "worker-1")
	if session.Worker.Generation != 1 || session.Worker.FencingToken != 1 || session.Worker.Status != domain.WorkerStatusBootstrapping {
		t.Fatalf("unexpected Worker session: %+v", session)
	}
	environment.heartbeat(t, session)
	created := environment.createTask(t, "lifecycle")
	environment.broker.Publish(AgentMailboxTopic(environment.agentID))
	item, err := environment.service.ClaimMailbox(context.Background(), environment.workerID, session.SessionToken, claimRequest(session, 1))
	if err != nil || item == nil || item.ID != created.MailboxItem.ID || item.State != domain.MailboxStateClaimed {
		t.Fatalf("claimed item=%+v err=%v", item, err)
	}
	begin, err := environment.service.BeginAttempt(context.Background(), environment.workerID, session.SessionToken,
		item.ID, api.BeginAttemptRequest{
			WorkerInstanceID: session.Worker.ID, AgentID: environment.agentID,
			Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken,
			ExpectedItemState: domain.MailboxStateClaimed, ExpectedTaskVersion: 1,
		})
	if err != nil {
		t.Fatalf("begin attempt: %v", err)
	}
	if begin.MailboxItem.State != domain.MailboxStateAccepted || begin.Turn.Task.Status != domain.TaskStatusRunning ||
		begin.Turn.RunAttempt.BackendID != "local" || begin.Turn.Execution.Spec.Model != "model-1" {
		t.Fatalf("unexpected BeginAttempt response: %+v", begin)
	}
	runtimeEvent := openruntime.RuntimeEvent{
		Type: "turn.output", Payload: json.RawMessage(`{"text":"working"}`), OccurredAt: environment.clock.Now(),
	}
	if err := environment.service.AppendEvents(context.Background(), environment.workerID, session.SessionToken,
		begin.Turn.RunAttempt.ID, api.EventBatch{
			WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation,
			FencingToken: session.Worker.FencingToken, ExpectedRunVersion: 1,
			Events: []openruntime.RuntimeEvent{runtimeEvent},
		}); err != nil {
		t.Fatalf("append Runtime Event: %v", err)
	}
	if err := environment.service.Finish(context.Background(), environment.workerID, session.SessionToken,
		begin.Turn.RunAttempt.ID, api.FinishRunRequest{
			WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation,
			FencingToken: session.Worker.FencingToken, ExpectedTaskVersion: 2, ExpectedRunVersion: 1,
			Result: openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "done", SideEffectsKnown: true},
		}); err != nil {
		t.Fatalf("finish RunAttempt: %v", err)
	}
	settledTask, err := environment.repository.GetTask(context.Background(), created.Task.ID)
	if err != nil || settledTask.Status != domain.TaskStatusSucceeded || settledTask.Version != 3 || settledTask.Result == nil || *settledTask.Result != "done" {
		t.Fatalf("settled Task=%+v err=%v", settledTask, err)
	}
	settledRun, err := environment.repository.GetRunAttempt(context.Background(), begin.Turn.RunAttempt.ID)
	if err != nil || settledRun.Status != domain.RunAttemptSucceeded || settledRun.Version != 2 || settledRun.FinishedAt == nil {
		t.Fatalf("settled Run=%+v err=%v", settledRun, err)
	}
	// A lost successful response may be retried with the original CAS versions.
	// The same result is idempotent, while a different result remains a conflict.
	finishRequest := api.FinishRunRequest{
		WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation,
		FencingToken: session.Worker.FencingToken, ExpectedTaskVersion: 2, ExpectedRunVersion: 1,
		Result: openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "done", SideEffectsKnown: true},
	}
	if err := environment.service.Finish(context.Background(), environment.workerID, session.SessionToken,
		begin.Turn.RunAttempt.ID, finishRequest); err != nil {
		t.Fatalf("idempotent finish retry: %v", err)
	}
	finishRequest.Result.Result = "different"
	if err := environment.service.Finish(context.Background(), environment.workerID, session.SessionToken,
		begin.Turn.RunAttempt.ID, finishRequest); err == nil {
		t.Fatal("different terminal result must not be accepted as an idempotent retry")
	}
}

func TestHeartbeatSlidesTokenAndRenewsActiveRunLease(t *testing.T) {
	environment := newWorkerTestEnvironment(t, func(options *WorkerServiceOptions) {
		options.WorkerLease = 30 * time.Second
		options.TokenLifetime = time.Minute
		options.RunLease = 40 * time.Second
	})
	session := environment.register(t, "worker-renew")
	environment.heartbeat(t, session)
	created := environment.createTask(t, "renew")
	item, err := environment.service.ClaimMailbox(context.Background(), environment.workerID,
		session.SessionToken, claimRequest(session, 1))
	if err != nil {
		t.Fatal(err)
	}
	begin, err := environment.service.BeginAttempt(context.Background(), environment.workerID, session.SessionToken,
		item.ID, api.BeginAttemptRequest{
			WorkerInstanceID: session.Worker.ID, AgentID: environment.agentID,
			Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken,
			ExpectedItemState: domain.MailboxStateClaimed, ExpectedTaskVersion: created.Task.Version,
		})
	if err != nil {
		t.Fatal(err)
	}
	originalRunLease := begin.Turn.RunAttempt.LeaseUntil
	environment.clock.Advance(25 * time.Second)
	environment.heartbeat(t, session)
	credential, err := environment.repository.GetWorkerCredential(context.Background(), session.Worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !credential.TokenExpiresAt.Equal(environment.clock.Now().Add(time.Minute)) {
		t.Fatalf("token expiry was not renewed: %s", credential.TokenExpiresAt)
	}
	environment.clock.Advance(20 * time.Second)
	environment.heartbeat(t, session)
	run, err := environment.repository.GetRunAttempt(context.Background(), begin.Turn.RunAttempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !run.LeaseUntil.After(originalRunLease) {
		t.Fatalf("Active Run lease did not advance: original=%s renewed=%s", originalRunLease, run.LeaseUntil)
	}
	if err := environment.service.AppendEvents(context.Background(), environment.workerID, session.SessionToken,
		run.ID, api.EventBatch{
			WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation,
			FencingToken: session.Worker.FencingToken, ExpectedRunVersion: run.Version,
			Events: []openruntime.RuntimeEvent{{Type: "turn.heartbeat", Payload: json.RawMessage(`{}`), OccurredAt: environment.clock.Now()}},
		}); err != nil {
		t.Fatalf("renewed Worker must retain write authority: %v", err)
	}
}

func TestWorkerFinishWaitingInputKeepsTaskEligibleForQueuedFollowUp(t *testing.T) {
	environment := newWorkerTestEnvironment(t, nil)
	session := environment.register(t, "worker-waiting-input")
	environment.heartbeat(t, session)
	created := environment.createTask(t, "waiting-input")
	item, err := environment.service.ClaimMailbox(context.Background(), environment.workerID, session.SessionToken, claimRequest(session, 1))
	if err != nil {
		t.Fatal(err)
	}
	begin, err := environment.service.BeginAttempt(context.Background(), environment.workerID, session.SessionToken, item.ID, api.BeginAttemptRequest{
		WorkerInstanceID: session.Worker.ID, AgentID: environment.agentID, Generation: session.Worker.Generation,
		FencingToken: session.Worker.FencingToken, ExpectedItemState: domain.MailboxStateClaimed, ExpectedTaskVersion: created.Task.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := environment.service.Finish(context.Background(), environment.workerID, session.SessionToken, begin.Turn.RunAttempt.ID, api.FinishRunRequest{
		WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken,
		ExpectedTaskVersion: begin.Turn.Task.Version, ExpectedRunVersion: begin.Turn.RunAttempt.Version,
		Result: openruntime.TurnResult{Status: openruntime.TurnResultWaitingInput, Result: "need clarification", SideEffectsKnown: true},
	}); err != nil {
		t.Fatal(err)
	}
	task, err := environment.repository.GetTask(context.Background(), created.Task.ID)
	if err != nil || task.Status != domain.TaskStatusWaitingInput {
		t.Fatalf("waiting-input Task=%+v err=%v", task, err)
	}
	followup := &domain.Message{ID: "message-waiting-followup", TaskID: task.ID, SenderPrincipalID: environment.ownerID,
		Kind: domain.MessageKindSupplement, Content: "clarification"}
	followupMailbox := &domain.MailboxItem{ID: "mailbox-waiting-followup", Lane: domain.MailboxLaneWork}
	if _, err := environment.repository.CreateMessage(context.Background(), task.Version, followup, followupMailbox, &domain.JournalEvent{
		ID: "event-waiting-followup", OrganizationID: environment.orgID, EventType: "message.created", ActorPrincipalID: environment.ownerID, Payload: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	item, err = environment.service.ClaimMailbox(context.Background(), environment.workerID, session.SessionToken, claimRequest(session, 1))
	if err != nil || item == nil || item.ID != followupMailbox.ID {
		t.Fatalf("follow-up item=%+v err=%v", item, err)
	}
}

func TestWorkerAuthenticationLeaseGenerationFencingAndAgentBindingFailClosed(t *testing.T) {
	tests := []struct {
		name      string
		customize func(*WorkerServiceOptions)
		mutate    func(*workerTestEnvironment, *api.WorkerSession) (string, string, api.ClaimRequest)
		want      error
	}{
		{name: "wrong token", mutate: func(environment *workerTestEnvironment, session *api.WorkerSession) (string, string, api.ClaimRequest) {
			return environment.workerID, "wrong-token-that-is-long-enough-to-be-a-session-token", claimRequest(session, 1)
		}, want: domain.ErrUnauthorized},
		{name: "wrong principal", mutate: func(_ *workerTestEnvironment, session *api.WorkerSession) (string, string, api.ClaimRequest) {
			return "different-worker-principal", session.SessionToken, claimRequest(session, 1)
		}, want: domain.ErrUnauthorized},
		{name: "wrong Agent", mutate: func(environment *workerTestEnvironment, session *api.WorkerSession) (string, string, api.ClaimRequest) {
			request := claimRequest(session, 1)
			request.AgentID = "other-agent"
			return environment.workerID, session.SessionToken, request
		}, want: domain.ErrUnauthorized},
		{name: "stale generation", mutate: func(environment *workerTestEnvironment, session *api.WorkerSession) (string, string, api.ClaimRequest) {
			request := claimRequest(session, 1)
			request.Generation++
			return environment.workerID, session.SessionToken, request
		}, want: domain.ErrSessionGenerationConflict},
		{name: "stale fencing", mutate: func(environment *workerTestEnvironment, session *api.WorkerSession) (string, string, api.ClaimRequest) {
			request := claimRequest(session, 1)
			request.FencingToken++
			return environment.workerID, session.SessionToken, request
		}, want: domain.ErrFencingRejected},
		{name: "expired lease", mutate: func(environment *workerTestEnvironment, session *api.WorkerSession) (string, string, api.ClaimRequest) {
			environment.clock.Advance(61 * time.Second)
			return environment.workerID, session.SessionToken, claimRequest(session, 1)
		}, want: domain.ErrLeaseExpired},
		{name: "expired token", customize: func(options *WorkerServiceOptions) {
			options.TokenLifetime = 30 * time.Second
			options.WorkerLease = 2 * time.Minute
		}, mutate: func(environment *workerTestEnvironment, session *api.WorkerSession) (string, string, api.ClaimRequest) {
			environment.clock.Advance(31 * time.Second)
			return environment.workerID, session.SessionToken, claimRequest(session, 1)
		}, want: domain.ErrUnauthorized},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			environment := newWorkerTestEnvironment(t, test.customize)
			session := environment.register(t, "worker-auth")
			environment.heartbeat(t, session)
			principal, token, request := test.mutate(environment, session)
			_, err := environment.service.ClaimMailbox(context.Background(), principal, token, request)
			if test.name == "wrong Agent" {
				var domainError *domain.DomainError
				if !errors.As(err, &domainError) || domainError.Code != "FORBIDDEN" {
					t.Fatalf("wrong Agent error=%v", err)
				}
				return
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
}

func TestMailboxControlOrderingBackpressureAndAtLeastOnce(t *testing.T) {
	environment := newWorkerTestEnvironment(t, nil)
	session := environment.register(t, "worker-mailbox")
	environment.heartbeat(t, session)
	activeTask := environment.createTask(t, "active")
	activeItem, err := environment.service.ClaimMailbox(context.Background(), environment.workerID,
		session.SessionToken, claimRequest(session, 1))
	if err != nil {
		t.Fatal(err)
	}
	begin, err := environment.service.BeginAttempt(context.Background(), environment.workerID, session.SessionToken,
		activeItem.ID, api.BeginAttemptRequest{
			WorkerInstanceID: session.Worker.ID, AgentID: environment.agentID, Generation: session.Worker.Generation,
			FencingToken: session.Worker.FencingToken, ExpectedItemState: domain.MailboxStateClaimed,
			ExpectedTaskVersion: activeTask.Task.Version,
		})
	if err != nil {
		t.Fatal(err)
	}
	queuedTask := environment.createTask(t, "queued-behind-run")
	for index := 1; index <= 2; index++ {
		message := &domain.Message{
			ID: fmt.Sprintf("control-message-%d", index), TaskID: activeTask.Task.ID,
			SenderPrincipalID: environment.ownerID, Kind: domain.MessageKindSupplement,
			Content: fmt.Sprintf("control %d", index),
		}
		mailbox := &domain.MailboxItem{
			ID: fmt.Sprintf("control-mailbox-%d", index), Lane: domain.MailboxLaneControl,
			TargetRunID: begin.Turn.RunAttempt.ID, ExpectedRunVersion: begin.Turn.RunAttempt.Version,
		}
		event := &domain.JournalEvent{
			ID: fmt.Sprintf("control-event-%d", index), OrganizationID: environment.orgID,
			EventType: "message.created", ActorPrincipalID: environment.ownerID, Payload: json.RawMessage(`{}`),
		}
		if _, err := environment.repository.CreateMessage(context.Background(), int64(index+1), message, mailbox, event); err != nil {
			t.Fatalf("create control Message %d: %v", index, err)
		}
	}
	for index := 1; index <= 2; index++ {
		item, err := environment.service.ClaimMailbox(context.Background(), environment.workerID,
			session.SessionToken, claimRequest(session, 1))
		if err != nil || item == nil || item.ID != fmt.Sprintf("control-mailbox-%d", index) {
			t.Fatalf("control claim %d item=%+v err=%v", index, item, err)
		}
		if err := environment.service.AcceptMailbox(context.Background(), environment.workerID, session.SessionToken,
			item.ID, api.AcceptRequest{
				WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation,
				FencingToken: session.Worker.FencingToken, ExpectedItemState: domain.MailboxStateClaimed,
				Outcome: domain.MailboxStateAccepted,
			}); err != nil {
			t.Fatal(err)
		}
		if err := environment.service.AcceptMailbox(context.Background(), environment.workerID, session.SessionToken,
			item.ID, api.AcceptRequest{
				WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation,
				FencingToken: session.Worker.FencingToken, ExpectedItemState: domain.MailboxStateClaimed,
				Outcome: domain.MailboxStateAccepted,
			}); err != nil {
			t.Fatalf("idempotent mailbox accept %d: %v", index, err)
		}
	}
	item, err := environment.service.ClaimMailbox(context.Background(), environment.workerID,
		session.SessionToken, claimRequest(session, 1))
	if err != nil || item != nil {
		t.Fatalf("work item must remain backpressured during Active Run: item=%+v err=%v", item, err)
	}
	queuedMailbox, err := environment.repository.GetMailboxItem(context.Background(), queuedTask.MailboxItem.ID)
	if err != nil || queuedMailbox.State != domain.MailboxStatePending {
		t.Fatalf("queued mailbox=%+v err=%v", queuedMailbox, err)
	}

	// A claimed item is at-least-once: after its claim lease expires, the same
	// valid Worker can reclaim it and attempts increases.
	if err := environment.service.Finish(context.Background(), environment.workerID, session.SessionToken,
		begin.Turn.RunAttempt.ID, api.FinishRunRequest{
			WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation,
			FencingToken: session.Worker.FencingToken, ExpectedTaskVersion: 4, ExpectedRunVersion: 1,
			Result: openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, SideEffectsKnown: true},
		}); err != nil {
		t.Fatal(err)
	}
	claimed, err := environment.service.ClaimMailbox(context.Background(), environment.workerID,
		session.SessionToken, claimRequest(session, 1))
	if err != nil || claimed.ID != queuedTask.MailboxItem.ID || claimed.Attempts != 1 {
		t.Fatalf("first at-least-once claim=%+v err=%v", claimed, err)
	}
	environment.clock.Advance(11 * time.Second)
	reclaimed, err := environment.service.ClaimMailbox(context.Background(), environment.workerID,
		session.SessionToken, claimRequest(session, 1))
	if err != nil || reclaimed.ID != claimed.ID || reclaimed.Attempts != 2 {
		t.Fatalf("reclaimed item=%+v err=%v", reclaimed, err)
	}
}

type droppingBroker struct{}

func (droppingBroker) Subscribe(string) (<-chan struct{}, func()) {
	return make(chan struct{}), func() {}
}
func (droppingBroker) Publish(string) {}

func TestLongPollRechecksDatabaseWhenBrokerWakeupIsLost(t *testing.T) {
	environment := newWorkerTestEnvironment(t, nil)
	service, err := NewWorkerService(environment.repository, droppingBroker{}, WorkerServiceOptions{
		Now:             environment.clock.Now,
		NewID:           func(prefix string) string { return prefix + "-lost-wakeup" },
		NewSessionToken: func() (string, error) { return "worker-session-token-lost-wakeup-000000000000000000", nil },
		WorkerLease:     2 * time.Minute, TokenLifetime: 10 * time.Minute,
		MailboxLease: 10 * time.Second, RunLease: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	environment.service = service
	session := environment.register(t, "worker-lost-wakeup")
	environment.heartbeat(t, session)
	request := claimRequest(session, 1)
	request.WaitSeconds = 1
	result := make(chan *domain.MailboxItem, 1)
	errorsChannel := make(chan error, 1)
	go func() {
		item, err := environment.service.ClaimMailbox(context.Background(), environment.workerID, session.SessionToken, request)
		result <- item
		errorsChannel <- err
	}()
	time.Sleep(100 * time.Millisecond)
	created := environment.createTask(t, "lost-wakeup")
	select {
	case item := <-result:
		if err := <-errorsChannel; err != nil || item == nil || item.ID != created.MailboxItem.ID {
			t.Fatalf("long poll item=%+v err=%v", item, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("long poll did not recheck persistent Mailbox after lost wakeup")
	}
}

func TestExpiredWorkerCannotRaceReplacementWorkerForMailbox(t *testing.T) {
	environment := newWorkerTestEnvironment(t, nil)
	oldSession := environment.register(t, "worker-old")
	environment.heartbeat(t, oldSession)
	environment.clock.Advance(61 * time.Second)
	newSession := environment.register(t, "worker-new")
	environment.heartbeat(t, newSession)
	created := environment.createTask(t, "replacement-race")
	start := make(chan struct{})
	type outcome struct {
		item *domain.MailboxItem
		err  error
	}
	outcomes := make(chan outcome, 2)
	var group sync.WaitGroup
	for _, session := range []*api.WorkerSession{oldSession, newSession} {
		session := session
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			item, err := environment.service.ClaimMailbox(context.Background(), environment.workerID,
				session.SessionToken, claimRequest(session, 1))
			outcomes <- outcome{item: item, err: err}
		}()
	}
	close(start)
	group.Wait()
	close(outcomes)
	successes, rejections := 0, 0
	for outcome := range outcomes {
		if outcome.err == nil && outcome.item != nil && outcome.item.ID == created.MailboxItem.ID {
			successes++
		} else if errors.Is(outcome.err, domain.ErrFencingRejected) || errors.Is(outcome.err, domain.ErrLeaseExpired) {
			rejections++
		} else {
			t.Fatalf("unexpected replacement race outcome: %+v", outcome)
		}
	}
	if successes != 1 || rejections != 1 {
		t.Fatalf("replacement race success=%d rejection=%d", successes, rejections)
	}
}

var _ WorkerState = (*openagentsqlite.Repository)(nil)
