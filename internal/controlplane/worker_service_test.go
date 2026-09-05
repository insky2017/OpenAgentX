package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"openagentx/internal/api"
	"openagentx/internal/domain"
	openagentsqlite "openagentx/internal/persistence/sqlite"
	openruntime "openagentx/internal/runtime"
	"openagentx/internal/testkit"
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

type preflightTurnPlanner struct {
	scopeDigest string
}

func (p preflightTurnPlanner) Plan(ctx context.Context, task domain.Task, messages []domain.Message, backends []openruntime.BackendRegistration) (TurnPlan, error) {
	plan, err := (M1TurnPlanner{}).Plan(ctx, task, messages, backends)
	if err != nil {
		return TurnPlan{}, err
	}
	plan.PreflightScopeDigest = p.scopeDigest
	return plan, nil
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

func TestRegisterWorkerRejectsUnknownLogicalAgent(t *testing.T) {
	environment := newWorkerTestEnvironment(t, nil)
	_, err := environment.service.Register(context.Background(), environment.workerID, api.RegisterRequest{
		ContractVersion: api.ContractVersion, AgentID: "missing-agent", WorkerInstanceID: "worker-missing-agent",
		Transport: domain.WorkerTransportUnix, Capabilities: []string{"coding"},
		Backends: []openruntime.BackendRegistration{environment.backend},
	})
	if !errors.Is(err, domain.ErrAgentNotFound) {
		t.Fatalf("register unknown Agent error=%v", err)
	}
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

func TestHeartbeatSecurityErrorsPrecedeAckOwnershipAndRollbackUnknownBackend(t *testing.T) {
	environment := newWorkerTestEnvironment(t, nil)
	session := environment.register(t, "worker-heartbeat-priority")
	request := api.HeartbeatRequest{
		WorkerInstanceID: session.Worker.ID, Generation: 0, FencingToken: 0,
		Status:        domain.WorkerStatusOnline,
		BackendHealth: map[string]openruntime.BackendHealth{"local": openruntime.BackendHealthy},
		NetworkBindings: map[string]api.NetworkBindingAck{"other": {
			BackendID: "other", ProfileID: "proxy", ProfileVersion: 1, BindingRevision: 1, State: "applied",
		}},
	}
	if err := environment.service.Heartbeat(context.Background(), "wrong-principal", "wrong-token", request); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("wrong identity did not win combined error priority: %v", err)
	}
	if err := environment.service.Heartbeat(context.Background(), environment.workerID, session.SessionToken, request); err == nil {
		t.Fatal("invalid generation unexpectedly accepted")
	}
	request.Generation = session.Worker.Generation
	request.FencingToken = session.Worker.FencingToken + 1
	if err := environment.service.Heartbeat(context.Background(), environment.workerID, session.SessionToken, request); !errors.Is(err, domain.ErrFencingRejected) {
		t.Fatalf("fencing error did not precede Backend ownership: %v", err)
	}
	request.FencingToken = session.Worker.FencingToken
	if err := environment.service.Heartbeat(context.Background(), environment.workerID, session.SessionToken, request); err == nil {
		t.Fatal("unknown network Backend acknowledgement unexpectedly accepted")
	}
	credential, err := environment.repository.GetWorkerCredential(context.Background(), session.Worker.ID)
	if err != nil || credential.Worker.Status != domain.WorkerStatusBootstrapping {
		t.Fatalf("unknown Backend acknowledgement committed heartbeat: Worker=%+v err=%v", credential, err)
	}
}

func TestRegisterAndPullIgnoreBindingForBackendRemovedFromWorkerYAML(t *testing.T) {
	environment := newWorkerTestEnvironment(t, nil)
	environment.backend.Descriptor.NetworkModes = []string{"inherit", "named_profile"}
	now := environment.clock.Now()
	profile := &domain.ProxyProfile{
		ID: "proxy-removed-backend", Version: 1, Status: domain.NetworkProfileDraft,
		Mode: "only_socks5", Host: "proxy.internal", Port: 28080,
		ConfigFile: "/etc/openagentx/proxy.conf", CreatedBy: environment.ownerID,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := environment.repository.CreateProxyProfile(context.Background(), profile); err != nil {
		t.Fatal(err)
	}
	published, err := environment.repository.PublishProxyProfile(context.Background(), profile.ID, 1, environment.ownerID, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := environment.repository.BindNetworkProfile(context.Background(), &domain.NetworkBinding{
		AgentID: environment.agentID, BackendID: "removed", ProfileID: published.ID,
		ProfileVersion: published.Version, Version: 1, DesiredStatus: "pending", UpdatedAt: now.Add(2 * time.Second),
	}, 0); err != nil {
		t.Fatal(err)
	}
	session := environment.register(t, "worker-without-removed-backend")
	if len(session.NetworkBindings) != 0 {
		t.Fatalf("registration returned removed Backend binding: %+v", session.NetworkBindings)
	}
	bindings, err := environment.service.PullNetworkBindings(context.Background(), environment.workerID, session.SessionToken,
		api.NetworkBindingPullRequest{WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken})
	if err != nil || len(bindings) != 0 {
		t.Fatalf("pull returned removed Backend binding=%+v err=%v", bindings, err)
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

func controlClaimRequest(session *api.WorkerSession, waitSeconds int) api.ControlClaimRequest {
	return api.ControlClaimRequest{
		WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation,
		FencingToken: session.Worker.FencingToken, WaitSeconds: waitSeconds,
	}
}

func newWorkerAdminTestService(t *testing.T, environment *workerTestEnvironment, broker WakeupBroker) *WorkerAdminService {
	t.Helper()
	service, err := NewWorkerAdminService(environment.repository, broker, environment.clock.Now,
		func(prefix string) string { return prefix + "-control-test" })
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func createWorkerCommand(t *testing.T, service *WorkerAdminService, environment *workerTestEnvironment, session *api.WorkerSession, suffix string) *domain.WorkerCommand {
	t.Helper()
	command, err := service.Command(context.Background(), environment.ownerID, session.Worker.ID,
		domain.WorkerCommandHealthCheck, api.WorkerAdminRequest{
			Meta:        api.CommandMeta{IdempotencyKey: "control-" + suffix, ExpectedVersion: 1},
			RequestedBy: environment.ownerID, ExpectedGeneration: session.Worker.Generation,
		})
	if err != nil {
		t.Fatal(err)
	}
	return command
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
			ExpectedItemState: domain.MailboxStateClaimed,
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

func TestWorkerAppendApprovalRequestedAtomicIdempotentAndApply(t *testing.T) {
	environment := newWorkerTestEnvironment(t, nil)
	session := environment.register(t, "worker-approval-requested")
	environment.heartbeat(t, session)
	created := environment.createTask(t, "approval-requested")
	item, err := environment.service.ClaimMailbox(context.Background(), environment.workerID, session.SessionToken, claimRequest(session, 1))
	if err != nil || item == nil {
		t.Fatalf("claim item=%+v err=%v", item, err)
	}
	begin, err := environment.service.BeginAttempt(context.Background(), environment.workerID, session.SessionToken, item.ID, api.BeginAttemptRequest{
		WorkerInstanceID: session.Worker.ID, AgentID: environment.agentID, Generation: session.Worker.Generation,
		FencingToken: session.Worker.FencingToken, ExpectedItemState: domain.MailboxStateClaimed,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"approval_request_id":"approval-runtime-1","scope_digest":"scope-runtime-1","expires_at":"2026-08-30T16:00:00Z"}`)
	batch := api.EventBatch{WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken, ExpectedRunVersion: begin.Turn.RunAttempt.Version,
		Events: []openruntime.RuntimeEvent{{Type: "approval.requested", Payload: payload, OccurredAt: environment.clock.Now()}}}
	if err := environment.service.AppendEvents(context.Background(), environment.workerID, session.SessionToken, begin.Turn.RunAttempt.ID, batch); err != nil {
		t.Fatalf("append approval.requested: %v", err)
	}
	task, _ := environment.repository.GetTask(context.Background(), created.Task.ID)
	run, _ := environment.repository.GetRunAttempt(context.Background(), begin.Turn.RunAttempt.ID)
	if task.Status != domain.TaskStatusWaitingApproval || task.Version != begin.Turn.Task.Version+1 || run.Version != begin.Turn.RunAttempt.Version {
		t.Fatalf("approval state task=%+v run=%+v", task, run)
	}
	if err := environment.service.AppendEvents(context.Background(), environment.workerID, session.SessionToken, begin.Turn.RunAttempt.ID, batch); err != nil {
		t.Fatalf("idempotent approval retry: %v", err)
	}
	taskAfterRetry, _ := environment.repository.GetTask(context.Background(), created.Task.ID)
	if taskAfterRetry.Version != task.Version {
		t.Fatalf("retry advanced task version: before=%d after=%d", task.Version, taskAfterRetry.Version)
	}
	conflict := batch
	conflict.Events = []openruntime.RuntimeEvent{{Type: "approval.requested", Payload: json.RawMessage(`{"approval_request_id":"approval-runtime-1","scope_digest":"different-scope","expires_at":"2026-08-30T16:00:00Z"}`), OccurredAt: environment.clock.Now()}}
	if err := environment.service.AppendEvents(context.Background(), environment.workerID, session.SessionToken, begin.Turn.RunAttempt.ID, conflict); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("conflicting approval retry error=%v", err)
	}
	bad := batch
	bad.Events = []openruntime.RuntimeEvent{{Type: "approval.requested", Payload: json.RawMessage(`{"approval_request_id":"approval-runtime-2","scope_digest":"scope-runtime-2","expires_at":"2026-08-30T16:00:00Z","extra":true}`), OccurredAt: environment.clock.Now()}}
	if err := environment.service.AppendEvents(context.Background(), environment.workerID, session.SessionToken, begin.Turn.RunAttempt.ID, bad); err == nil {
		t.Fatal("malformed approval payload accepted")
	}
	commands, err := NewCommandService(environment.repository, environment.broker, environment.clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	decisionResponse, err := commands.DecideApproval(context.Background(), environment.ownerID, "approval-runtime-1", api.DecideApprovalRequest{Meta: api.CommandMeta{IdempotencyKey: "approval-runtime-decision", ExpectedVersion: task.Version}, DecidedBy: environment.ownerID, Decision: domain.ApprovalDecisionApprove})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := environment.service.ClaimMailbox(context.Background(), environment.workerID, session.SessionToken, claimRequest(session, 0))
	if err != nil || claimed == nil || claimed.Kind != domain.MailboxKindApproval {
		t.Fatalf("claim approval=%+v err=%v", claimed, err)
	}
	if err := environment.service.AcceptMailbox(context.Background(), environment.workerID, session.SessionToken, claimed.ID, api.AcceptRequest{WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken, ExpectedItemState: domain.MailboxStateClaimed, Outcome: domain.MailboxStateAccepted}); err != nil {
		t.Fatal(err)
	}
	task, _ = environment.repository.GetTask(context.Background(), created.Task.ID)
	decision, _ := environment.repository.GetApprovalDecision(context.Background(), decisionResponse.Decision.ID)
	if task.Status != domain.TaskStatusRunning || decision == nil || decision.State != domain.ApprovalDecisionApplied {
		t.Fatalf("applied approval task=%+v decision=%+v", task, decision)
	}
	if err := environment.service.Finish(context.Background(), environment.workerID, session.SessionToken, begin.Turn.RunAttempt.ID, api.FinishRunRequest{WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken, ExpectedTaskVersion: begin.Turn.Task.Version, ExpectedRunVersion: begin.Turn.RunAttempt.Version, Result: openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "approved", SideEffectsKnown: true}}); err != nil {
		t.Fatalf("finish after approval version delta: %v", err)
	}
}

func TestWorkerFinishPersistsRuntimeDiagnosticToRunTaskAndEvents(t *testing.T) {
	environment := newWorkerTestEnvironment(t, nil)
	session := environment.register(t, "worker-error-persistence")
	environment.heartbeat(t, session)
	created := environment.createTask(t, "runtime-error")
	item, err := environment.service.ClaimMailbox(context.Background(), environment.workerID, session.SessionToken, claimRequest(session, 1))
	if err != nil || item == nil {
		t.Fatalf("claim item=%+v err=%v", item, err)
	}
	begin, err := environment.service.BeginAttempt(context.Background(), environment.workerID, session.SessionToken,
		item.ID, api.BeginAttemptRequest{
			WorkerInstanceID: session.Worker.ID, AgentID: environment.agentID,
			Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken,
			ExpectedItemState: domain.MailboxStateClaimed,
		})
	if err != nil {
		t.Fatal(err)
	}
	diagnostic := "AGY stream-json ended without a terminal event; AGY stderr: provider unavailable"
	result := openruntime.TurnResult{Status: openruntime.TurnResultUncertain, Error: diagnostic, SideEffectsKnown: false}
	if err := environment.service.Finish(context.Background(), environment.workerID, session.SessionToken,
		begin.Turn.RunAttempt.ID, api.FinishRunRequest{
			WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation,
			FencingToken: session.Worker.FencingToken, ExpectedTaskVersion: begin.Turn.Task.Version,
			ExpectedRunVersion: begin.Turn.RunAttempt.Version, Result: result,
		}); err != nil {
		t.Fatal(err)
	}
	settledRun, err := environment.repository.GetRunAttempt(context.Background(), begin.Turn.RunAttempt.ID)
	if err != nil || !strings.Contains(settledRun.ResultJSON, diagnostic) {
		t.Fatalf("run=%+v err=%v", settledRun, err)
	}
	settledTask, err := environment.repository.GetTask(context.Background(), created.Task.ID)
	if err != nil || settledTask.Error == nil || *settledTask.Error != diagnostic {
		t.Fatalf("task=%+v err=%v", settledTask, err)
	}
	events, err := environment.repository.ListJournal(context.Background(), 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	matched := map[string]bool{"run_attempt.finished": false, "task.settled": false}
	for _, event := range events {
		if _, ok := matched[event.EventType]; ok && strings.Contains(string(event.Payload), diagnostic) {
			matched[event.EventType] = true
		}
	}
	if !matched["run_attempt.finished"] || !matched["task.settled"] {
		t.Fatalf("terminal error events=%v", matched)
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
	environment.createTask(t, "renew")
	item, err := environment.service.ClaimMailbox(context.Background(), environment.workerID,
		session.SessionToken, claimRequest(session, 1))
	if err != nil {
		t.Fatal(err)
	}
	begin, err := environment.service.BeginAttempt(context.Background(), environment.workerID, session.SessionToken,
		item.ID, api.BeginAttemptRequest{
			WorkerInstanceID: session.Worker.ID, AgentID: environment.agentID,
			Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken,
			ExpectedItemState: domain.MailboxStateClaimed,
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
		FencingToken: session.Worker.FencingToken, ExpectedItemState: domain.MailboxStateClaimed,
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

func TestBeginAttemptAtomicallyConsumesMatchingPreflightApprovalOnce(t *testing.T) {
	const scopeDigest = "scope-preflight-begin"
	environment := newWorkerTestEnvironment(t, func(options *WorkerServiceOptions) {
		options.Planner = preflightTurnPlanner{scopeDigest: scopeDigest}
	})
	session := environment.register(t, "worker-preflight-begin")
	environment.heartbeat(t, session)
	created := environment.createTask(t, "preflight-begin")
	item, err := environment.service.ClaimMailbox(context.Background(), environment.workerID, session.SessionToken, claimRequest(session, 1))
	if err != nil || item == nil {
		t.Fatalf("claim preflight work=%+v err=%v", item, err)
	}
	beginRequest := api.BeginAttemptRequest{
		WorkerInstanceID: session.Worker.ID, AgentID: environment.agentID, Generation: session.Worker.Generation,
		FencingToken: session.Worker.FencingToken, ExpectedItemState: domain.MailboxStateClaimed,
	}
	if _, err := environment.service.BeginAttempt(context.Background(), environment.workerID, session.SessionToken, item.ID, beginRequest); !errors.Is(err, domain.ErrApprovalStale) {
		t.Fatalf("BeginAttempt without preflight Approval error=%v", err)
	}
	unchangedTask, err := environment.repository.GetTask(context.Background(), created.Task.ID)
	if err != nil || unchangedTask.Status != domain.TaskStatusQueued || unchangedTask.Version != created.Task.Version {
		t.Fatalf("failed preflight BeginAttempt changed Task=%+v err=%v", unchangedTask, err)
	}
	unchangedItem, err := environment.repository.GetMailboxItem(context.Background(), item.ID)
	if err != nil || unchangedItem.State != domain.MailboxStateClaimed {
		t.Fatalf("failed preflight BeginAttempt changed mailbox=%+v err=%v", unchangedItem, err)
	}

	approval := &domain.ApprovalRequest{
		ID: "approval-preflight-begin", TaskID: created.Task.ID, Mode: domain.ApprovalModePreflight,
		ScopeDigest: scopeDigest, State: domain.ApprovalRequestPending, ExpiresAt: environment.clock.Now().Add(time.Hour),
	}
	if err := environment.repository.CreateApprovalRequest(context.Background(), approval, &domain.JournalEvent{
		ID: "event-approval-preflight-begin", OrganizationID: environment.orgID, EventType: "approval.created",
		ActorPrincipalID: environment.ownerID, Payload: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := environment.repository.DecideApproval(context.Background(), approval.ID, &domain.ApprovalDecision{
		ID: "decision-preflight-begin", ApprovalRequestID: approval.ID, DecidedBy: environment.ownerID,
		Decision: domain.ApprovalDecisionApprove, State: domain.ApprovalDecisionPersisted, IdempotencyKey: "preflight-begin-idem",
	}, nil, &domain.JournalEvent{
		ID: "event-decision-preflight-begin", OrganizationID: environment.orgID, EventType: "approval.decided",
		ActorPrincipalID: environment.ownerID, Payload: json.RawMessage(`{}`),
	}, nil); err != nil {
		t.Fatal(err)
	}
	begin, err := environment.service.BeginAttempt(context.Background(), environment.workerID, session.SessionToken, item.ID, beginRequest)
	if err != nil {
		t.Fatalf("BeginAttempt with matching preflight Approval: %v", err)
	}
	persistedApproval, err := environment.repository.GetApprovalRequest(context.Background(), approval.ID)
	if err != nil || persistedApproval.State != domain.ApprovalRequestConsumed {
		t.Fatalf("consumed preflight Approval=%+v err=%v", persistedApproval, err)
	}
	if err := environment.service.Finish(context.Background(), environment.workerID, session.SessionToken, begin.Turn.RunAttempt.ID, api.FinishRunRequest{
		WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken,
		ExpectedTaskVersion: begin.Turn.Task.Version, ExpectedRunVersion: begin.Turn.RunAttempt.Version,
		Result: openruntime.TurnResult{Status: openruntime.TurnResultWaitingInput, Result: "need another turn", SideEffectsKnown: true},
	}); err != nil {
		t.Fatal(err)
	}
	waitingTask, err := environment.repository.GetTask(context.Background(), created.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	followup := &domain.Message{ID: "message-preflight-second-turn", TaskID: created.Task.ID,
		SenderPrincipalID: environment.ownerID, Kind: domain.MessageKindSupplement, Content: "continue"}
	followupItem := &domain.MailboxItem{ID: "mailbox-preflight-second-turn", Lane: domain.MailboxLaneWork}
	if _, err := environment.repository.CreateMessage(context.Background(), waitingTask.Version, followup, followupItem, &domain.JournalEvent{
		ID: "event-preflight-second-turn", OrganizationID: environment.orgID, EventType: "message.created",
		ActorPrincipalID: environment.ownerID, Payload: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	item, err = environment.service.ClaimMailbox(context.Background(), environment.workerID, session.SessionToken, claimRequest(session, 1))
	if err != nil || item == nil || item.ID != followupItem.ID {
		t.Fatalf("claim second preflight turn=%+v err=%v", item, err)
	}
	if _, err := environment.service.BeginAttempt(context.Background(), environment.workerID, session.SessionToken, item.ID, beginRequest); !errors.Is(err, domain.ErrApprovalStale) {
		t.Fatalf("consumed preflight Approval was reused: %v", err)
	}
}

func TestWorkerSessionBindingCreateResumeAndUpdateAcrossTurns(t *testing.T) {
	environment := newWorkerTestEnvironment(t, nil)
	environment.backend.Descriptor.SessionModes = []domain.SessionMode{domain.SessionModeNew, domain.SessionModeResume}
	session := environment.register(t, "worker-session-binding")
	environment.heartbeat(t, session)
	created := environment.createTask(t, "session-binding")

	item, err := environment.service.ClaimMailbox(context.Background(), environment.workerID, session.SessionToken, claimRequest(session, 1))
	if err != nil || item == nil {
		t.Fatalf("claim first turn item=%+v err=%v", item, err)
	}
	first, err := environment.service.BeginAttempt(context.Background(), environment.workerID, session.SessionToken, item.ID, api.BeginAttemptRequest{
		WorkerInstanceID: session.Worker.ID, AgentID: environment.agentID, Generation: session.Worker.Generation,
		FencingToken: session.Worker.FencingToken, ExpectedItemState: domain.MailboxStateClaimed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Turn.Execution.Spec.Session.Mode != domain.SessionModeNew || first.Turn.SessionBinding != nil {
		t.Fatalf("first turn must start a new session: %+v", first.Turn)
	}
	if err := environment.service.Finish(context.Background(), environment.workerID, session.SessionToken, first.Turn.RunAttempt.ID, api.FinishRunRequest{
		WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken,
		ExpectedTaskVersion: first.Turn.Task.Version, ExpectedRunVersion: first.Turn.RunAttempt.Version,
		Result: openruntime.TurnResult{Status: openruntime.TurnResultWaitingInput, Result: "need follow-up", ProviderSessionID: "provider-session-1", SideEffectsKnown: true},
	}); err != nil {
		t.Fatal(err)
	}
	binding, err := environment.repository.GetSessionBinding(context.Background(), created.Task.ID, environment.agentID, environment.backend.BackendID)
	if err != nil || binding.ProviderSessionID != "provider-session-1" || binding.Version != 1 {
		t.Fatalf("created binding=%+v err=%v", binding, err)
	}

	waitingTask, err := environment.repository.GetTask(context.Background(), created.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	followup := &domain.Message{ID: "message-session-binding-followup", TaskID: created.Task.ID,
		SenderPrincipalID: environment.ownerID, Kind: domain.MessageKindSupplement, Content: "continue"}
	followupItem := &domain.MailboxItem{ID: "mailbox-session-binding-followup", Lane: domain.MailboxLaneWork}
	if _, err := environment.repository.CreateMessage(context.Background(), waitingTask.Version, followup, followupItem, &domain.JournalEvent{
		ID: "event-session-binding-followup", OrganizationID: environment.orgID, EventType: "message.created",
		ActorPrincipalID: environment.ownerID, Payload: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	item, err = environment.service.ClaimMailbox(context.Background(), environment.workerID, session.SessionToken, claimRequest(session, 1))
	if err != nil || item == nil {
		t.Fatalf("claim second turn item=%+v err=%v", item, err)
	}
	second, err := environment.service.BeginAttempt(context.Background(), environment.workerID, session.SessionToken, item.ID, api.BeginAttemptRequest{
		WorkerInstanceID: session.Worker.ID, AgentID: environment.agentID, Generation: session.Worker.Generation,
		FencingToken: session.Worker.FencingToken, ExpectedItemState: domain.MailboxStateClaimed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Turn.Execution.Spec.Session.Mode != domain.SessionModeResume || second.Turn.SessionBinding == nil ||
		second.Turn.SessionBinding.ProviderSessionID != "provider-session-1" {
		t.Fatalf("second turn did not resume binding: %+v", second.Turn)
	}
	if err := environment.service.Finish(context.Background(), environment.workerID, session.SessionToken, second.Turn.RunAttempt.ID, api.FinishRunRequest{
		WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken,
		ExpectedTaskVersion: second.Turn.Task.Version, ExpectedRunVersion: second.Turn.RunAttempt.Version,
		Result: openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "done", ProviderSessionID: "provider-session-2", SideEffectsKnown: true},
	}); err != nil {
		t.Fatal(err)
	}
	binding, err = environment.repository.GetSessionBinding(context.Background(), created.Task.ID, environment.agentID, environment.backend.BackendID)
	if err != nil || binding.ProviderSessionID != "provider-session-2" || binding.Version != 2 {
		t.Fatalf("updated binding=%+v err=%v", binding, err)
	}
	settled, err := environment.repository.GetTask(context.Background(), created.Task.ID)
	if err != nil || settled.Status != domain.TaskStatusSucceeded {
		t.Fatalf("second turn Task=%+v err=%v", settled, err)
	}
}

type staticBindingReader struct {
	binding *domain.SessionBinding
}

func (r staticBindingReader) GetSessionBinding(_ context.Context, contextID, agentID, backendID string) (*domain.SessionBinding, error) {
	if r.binding != nil && r.binding.ContextID == contextID && r.binding.AgentID == agentID && r.binding.BackendID == backendID {
		copy := *r.binding
		return &copy, nil
	}
	return nil, domain.ErrNotFound
}

func TestM1TurnPlannerStartsNewSessionAfterBackendSwitch(t *testing.T) {
	binding := &domain.SessionBinding{ID: "binding-old", ContextID: "task-switch", AgentID: "quote",
		BackendID: "backend-old", ProviderSessionID: "provider-old", State: domain.SessionBindingActive, Version: 1}
	descriptor := openruntime.AdapterDescriptor{AdapterID: "fake", Models: []string{"model-1"},
		ReasoningModes: []domain.ReasoningMode{domain.ReasoningBackendDefault},
		SessionModes:   []domain.SessionMode{domain.SessionModeNew, domain.SessionModeResume}}
	plan, err := (M1TurnPlanner{Bindings: staticBindingReader{binding: binding}}).Plan(context.Background(), domain.Task{
		ID: "task-switch", TargetAgentID: "quote",
	}, nil, []openruntime.BackendRegistration{{BackendID: "backend-new", Descriptor: descriptor, Health: openruntime.BackendHealthy}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Execution.Spec.Session.Mode != domain.SessionModeNew || plan.SessionBinding != nil || plan.Execution.Spec.BackendID != "backend-new" {
		t.Fatalf("backend switch plan=%+v", plan)
	}
}

func TestM1TurnPlannerPrefersResumableBindingOverEarlierNewBackend(t *testing.T) {
	binding := &domain.SessionBinding{ID: "binding-resume", ContextID: "task-resume", AgentID: "quote",
		BackendID: "zzz-resume", ProviderSessionID: "provider-resume", State: domain.SessionBindingActive, Version: 1}
	descriptor := openruntime.AdapterDescriptor{AdapterID: "fake", Models: []string{"model-1"},
		ReasoningModes: []domain.ReasoningMode{domain.ReasoningBackendDefault},
		SessionModes:   []domain.SessionMode{domain.SessionModeNew, domain.SessionModeResume}}
	plan, err := (M1TurnPlanner{Bindings: staticBindingReader{binding: binding}}).Plan(context.Background(), domain.Task{
		ID: "task-resume", TargetAgentID: "quote",
	}, nil, []openruntime.BackendRegistration{
		{BackendID: "aaa-new", Descriptor: descriptor, Health: openruntime.BackendHealthy},
		{BackendID: "zzz-resume", Descriptor: descriptor, Health: openruntime.BackendHealthy},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Execution.Spec.BackendID != "zzz-resume" || plan.Execution.Spec.Session.Mode != domain.SessionModeResume ||
		plan.SessionBinding == nil || plan.SessionBinding.ProviderSessionID != "provider-resume" {
		t.Fatalf("planner discarded resumable binding: %+v", plan)
	}
}

type bindingRaceState struct {
	*openagentsqlite.Repository
	raced bool
}

func (s *bindingRaceState) FinishRun(ctx context.Context, guard domain.WorkerWriteGuard, runID string,
	expectedTaskVersion, expectedRunVersion int64, result openruntime.TurnResult, binding *domain.SessionBinding,
	expectedBindingVersion int64, bindingEvent, taskEvent, runEvent *domain.JournalEvent,
) error {
	if binding != nil && expectedBindingVersion > 0 && !s.raced {
		s.raced = true
		concurrent := *binding
		concurrent.ProviderSessionID = "provider-concurrent"
		concurrent.Version = expectedBindingVersion + 1
		if err := s.Repository.SaveSessionBinding(ctx, &concurrent, expectedBindingVersion, &domain.JournalEvent{
			ID: "event-binding-concurrent", EventType: "session_binding.updated",
			ActorPrincipalID: guard.PrincipalID, Payload: json.RawMessage(`{}`),
		}); err != nil {
			return err
		}
	}
	return s.Repository.FinishRun(ctx, guard, runID, expectedTaskVersion, expectedRunVersion,
		result, binding, expectedBindingVersion, bindingEvent, taskEvent, runEvent)
}

func TestWorkerFinishBindingCASFailureDoesNotSettleRunOrTask(t *testing.T) {
	environment := newWorkerTestEnvironment(t, nil)
	environment.backend.Descriptor.SessionModes = []domain.SessionMode{domain.SessionModeNew, domain.SessionModeResume}
	racingState := &bindingRaceState{Repository: environment.repository}
	var idCounter atomic.Int64
	service, err := NewWorkerService(racingState, environment.broker, WorkerServiceOptions{
		Now: environment.clock.Now, NewID: func(prefix string) string { return fmt.Sprintf("%s-race-%d", prefix, idCounter.Add(1)) },
		NewSessionToken: func() (string, error) { return "worker-session-token-binding-race-00000000000000000001", nil },
		WorkerLease:     time.Minute, TokenLifetime: 10 * time.Minute, MailboxLease: 10 * time.Second, RunLease: 2 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	environment.service = service
	session := environment.register(t, "worker-binding-race")
	environment.heartbeat(t, session)
	created := environment.createTask(t, "binding-race")
	if err := environment.repository.SaveSessionBinding(context.Background(), &domain.SessionBinding{
		ID: "binding-race", ContextID: created.Task.ID, AgentID: environment.agentID, BackendID: environment.backend.BackendID,
		ProviderSessionID: "provider-original", State: domain.SessionBindingActive, Version: 1,
	}, 0, &domain.JournalEvent{ID: "event-binding-race", EventType: "session_binding.created", ActorPrincipalID: environment.ownerID, Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	item, err := environment.service.ClaimMailbox(context.Background(), environment.workerID, session.SessionToken, claimRequest(session, 1))
	if err != nil {
		t.Fatal(err)
	}
	begin, err := environment.service.BeginAttempt(context.Background(), environment.workerID, session.SessionToken, item.ID, api.BeginAttemptRequest{
		WorkerInstanceID: session.Worker.ID, AgentID: environment.agentID, Generation: session.Worker.Generation,
		FencingToken: session.Worker.FencingToken, ExpectedItemState: domain.MailboxStateClaimed,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = environment.service.Finish(context.Background(), environment.workerID, session.SessionToken, begin.Turn.RunAttempt.ID, api.FinishRunRequest{
		WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken,
		ExpectedTaskVersion: begin.Turn.Task.Version, ExpectedRunVersion: begin.Turn.RunAttempt.Version,
		Result: openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, ProviderSessionID: "provider-finish", SideEffectsKnown: true},
	})
	if !errors.Is(err, domain.ErrStaleVersion) {
		t.Fatalf("finish binding CAS error=%v", err)
	}
	run, err := environment.repository.GetRunAttempt(context.Background(), begin.Turn.RunAttempt.ID)
	if err != nil || !run.Status.Active() || run.Version != begin.Turn.RunAttempt.Version {
		t.Fatalf("Run must remain active after binding CAS failure: run=%+v err=%v", run, err)
	}
	task, err := environment.repository.GetTask(context.Background(), created.Task.ID)
	if err != nil || task.Status != domain.TaskStatusRunning || task.Version != begin.Turn.Task.Version {
		t.Fatalf("Task must remain running after binding CAS failure: task=%+v err=%v", task, err)
	}
}

func TestWorkerFinishQueuedWorkMessageWaitsButAcceptedNativeMessageSettles(t *testing.T) {
	for _, test := range []struct {
		name       string
		lane       domain.MailboxLane
		accept     bool
		wantStatus domain.TaskStatus
	}{
		{name: "queued work", lane: domain.MailboxLaneWork, wantStatus: domain.TaskStatusWaitingInput},
		{name: "pending native control", lane: domain.MailboxLaneControl, wantStatus: domain.TaskStatusWaitingInput},
		{name: "accepted native control", lane: domain.MailboxLaneControl, accept: true, wantStatus: domain.TaskStatusSucceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			environment := newWorkerTestEnvironment(t, nil)
			session := environment.register(t, "worker-finish-message-"+strings.ReplaceAll(test.name, " ", "-"))
			environment.heartbeat(t, session)
			created := environment.createTask(t, "finish-message-"+strings.ReplaceAll(test.name, " ", "-"))
			item, err := environment.service.ClaimMailbox(context.Background(), environment.workerID, session.SessionToken, claimRequest(session, 1))
			if err != nil {
				t.Fatal(err)
			}
			begin, err := environment.service.BeginAttempt(context.Background(), environment.workerID, session.SessionToken, item.ID, api.BeginAttemptRequest{
				WorkerInstanceID: session.Worker.ID, AgentID: environment.agentID, Generation: session.Worker.Generation,
				FencingToken: session.Worker.FencingToken, ExpectedItemState: domain.MailboxStateClaimed,
			})
			if err != nil {
				t.Fatal(err)
			}
			message := &domain.Message{ID: "message-race-" + strings.ReplaceAll(test.name, " ", "-"), TaskID: created.Task.ID,
				SenderPrincipalID: environment.ownerID, Kind: domain.MessageKindSupplement, Content: "follow-up"}
			messageItem := &domain.MailboxItem{ID: "mailbox-race-" + strings.ReplaceAll(test.name, " ", "-"), Lane: test.lane,
				TargetRunID: begin.Turn.RunAttempt.ID, ExpectedRunVersion: begin.Turn.RunAttempt.Version}
			if _, err := environment.repository.CreateMessage(context.Background(), begin.Turn.Task.Version, message, messageItem, &domain.JournalEvent{
				ID: "event-race-" + strings.ReplaceAll(test.name, " ", "-"), OrganizationID: environment.orgID,
				EventType: "message.created", ActorPrincipalID: environment.ownerID, Payload: json.RawMessage(`{}`),
			}); err != nil {
				t.Fatal(err)
			}
			if test.accept {
				claimed, err := environment.service.ClaimMailbox(context.Background(), environment.workerID, session.SessionToken, claimRequest(session, 1))
				if err != nil || claimed == nil || claimed.ID != messageItem.ID {
					t.Fatalf("claim native control=%+v err=%v", claimed, err)
				}
				if err := environment.service.AcceptMailbox(context.Background(), environment.workerID, session.SessionToken, claimed.ID, api.AcceptRequest{
					WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken,
					ExpectedItemState: domain.MailboxStateClaimed, Outcome: domain.MailboxStateAccepted,
				}); err != nil {
					t.Fatal(err)
				}
			}
			if err := environment.service.Finish(context.Background(), environment.workerID, session.SessionToken, begin.Turn.RunAttempt.ID, api.FinishRunRequest{
				WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken,
				ExpectedTaskVersion: begin.Turn.Task.Version, ExpectedRunVersion: begin.Turn.RunAttempt.Version,
				Result: openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, SideEffectsKnown: true},
			}); err != nil {
				t.Fatal(err)
			}
			settled, err := environment.repository.GetTask(context.Background(), created.Task.ID)
			if err != nil || settled.Status != test.wantStatus {
				t.Fatalf("Task status=%+v err=%v want=%s", settled, err, test.wantStatus)
			}
		})
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

type observedBroker struct {
	inner      *MemoryWakeupBroker
	subscribed chan string
}

func newObservedBroker() *observedBroker {
	return &observedBroker{inner: NewMemoryWakeupBroker(), subscribed: make(chan string, 1)}
}

func (b *observedBroker) Subscribe(topic string) (<-chan struct{}, func()) {
	wakeup, unsubscribe := b.inner.Subscribe(topic)
	select {
	case b.subscribed <- topic:
	default:
	}
	return wakeup, unsubscribe
}

func (b *observedBroker) Publish(topic string) { b.inner.Publish(topic) }

type subscribeHookBroker struct {
	once sync.Once
	hook func()
}

func (b *subscribeHookBroker) Subscribe(string) (<-chan struct{}, func()) {
	b.once.Do(b.hook)
	return make(chan struct{}), func() {}
}

func (*subscribeHookBroker) Publish(string) {}

func TestWorkerControlLongPollWaitsAndTimesOut(t *testing.T) {
	environment := newWorkerTestEnvironment(t, nil)
	session := environment.register(t, "worker-control-timeout")
	environment.heartbeat(t, session)
	started := time.Now()
	command, err := environment.service.ClaimWorkerCommand(context.Background(), environment.workerID,
		session.SessionToken, controlClaimRequest(session, 1))
	elapsed := time.Since(started)
	if err != nil || command != nil {
		t.Fatalf("control timeout command=%+v err=%v", command, err)
	}
	if elapsed < 900*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("control long poll elapsed=%s want approximately 1s", elapsed)
	}
}

func TestWorkerControlLongPollWakesWhenAdminCommandCommits(t *testing.T) {
	broker := newObservedBroker()
	environment := newWorkerTestEnvironment(t, nil)
	environment.broker = broker.inner
	service, err := NewWorkerService(environment.repository, broker, WorkerServiceOptions{
		Now: environment.clock.Now, NewID: func(prefix string) string { return prefix + "-wakeup" },
		NewSessionToken: func() (string, error) { return "worker-control-wakeup-token-000000000000000000", nil },
		WorkerLease:     time.Minute, TokenLifetime: 10 * time.Minute, MailboxLease: 10 * time.Second, RunLease: 2 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	environment.service = service
	session := environment.register(t, "worker-control-wakeup")
	environment.heartbeat(t, session)
	result := make(chan *domain.WorkerCommand, 1)
	errorsChannel := make(chan error, 1)
	go func() {
		command, claimErr := service.ClaimWorkerCommand(context.Background(), environment.workerID,
			session.SessionToken, controlClaimRequest(session, 5))
		result <- command
		errorsChannel <- claimErr
	}()
	select {
	case topic := <-broker.subscribed:
		if topic != WorkerControlTopic(session.Worker.ID) {
			t.Fatalf("subscribed topic=%q", topic)
		}
	case <-time.After(time.Second):
		t.Fatal("control claim did not subscribe")
	}
	created := createWorkerCommand(t, newWorkerAdminTestService(t, environment, broker), environment, session, "wakeup")
	select {
	case command := <-result:
		if err := <-errorsChannel; err != nil || command == nil || command.ID != created.ID {
			t.Fatalf("woken command=%+v err=%v", command, err)
		}
	case <-time.After(time.Second):
		t.Fatal("committed Worker command did not wake control long poll")
	}
}

func TestWorkerControlLongPollRechecksAfterSubscribeRace(t *testing.T) {
	environment := newWorkerTestEnvironment(t, nil)
	broker := &subscribeHookBroker{}
	service, err := NewWorkerService(environment.repository, broker, WorkerServiceOptions{
		Now: environment.clock.Now, NewID: func(prefix string) string { return prefix + "-subscribe-race" },
		NewSessionToken: func() (string, error) { return "worker-control-subscribe-race-token-00000000000000", nil },
		WorkerLease:     time.Minute, TokenLifetime: 10 * time.Minute, MailboxLease: 10 * time.Second, RunLease: 2 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	environment.service = service
	session := environment.register(t, "worker-control-subscribe-race")
	environment.heartbeat(t, session)
	admin := newWorkerAdminTestService(t, environment, droppingBroker{})
	var created *domain.WorkerCommand
	broker.hook = func() { created = createWorkerCommand(t, admin, environment, session, "subscribe-race") }
	started := time.Now()
	command, err := service.ClaimWorkerCommand(context.Background(), environment.workerID,
		session.SessionToken, controlClaimRequest(session, 5))
	if err != nil || command == nil || created == nil || command.ID != created.ID {
		t.Fatalf("subscribe-race command=%+v created=%+v err=%v", command, created, err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("subscribe recheck relied on timeout: %s", elapsed)
	}
}

func TestWorkerControlLongPollRechecksDatabaseWhenWakeupIsLost(t *testing.T) {
	environment := newWorkerTestEnvironment(t, nil)
	service, err := NewWorkerService(environment.repository, droppingBroker{}, WorkerServiceOptions{
		Now: environment.clock.Now, NewID: func(prefix string) string { return prefix + "-control-lost-wakeup" },
		NewSessionToken: func() (string, error) { return "worker-control-lost-wakeup-token-000000000000000", nil },
		WorkerLease:     2 * time.Minute, TokenLifetime: 10 * time.Minute, MailboxLease: 10 * time.Second, RunLease: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	environment.service = service
	session := environment.register(t, "worker-control-lost-wakeup")
	environment.heartbeat(t, session)
	result := make(chan *domain.WorkerCommand, 1)
	errorsChannel := make(chan error, 1)
	go func() {
		command, claimErr := service.ClaimWorkerCommand(context.Background(), environment.workerID,
			session.SessionToken, controlClaimRequest(session, 1))
		result <- command
		errorsChannel <- claimErr
	}()
	time.Sleep(100 * time.Millisecond)
	created := createWorkerCommand(t, newWorkerAdminTestService(t, environment, droppingBroker{}), environment, session, "lost-wakeup")
	select {
	case command := <-result:
		if err := <-errorsChannel; err != nil || command == nil || command.ID != created.ID {
			t.Fatalf("lost-wakeup command=%+v err=%v", command, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("control long poll did not recheck after lost wakeup")
	}
}

func TestWorkerControlLongPollHonorsContextCancellation(t *testing.T) {
	environment := newWorkerTestEnvironment(t, nil)
	session := environment.register(t, "worker-control-cancel")
	environment.heartbeat(t, session)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := environment.service.ClaimWorkerCommand(ctx, environment.workerID,
			session.SessionToken, controlClaimRequest(session, 5))
		result <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("control long poll ignored context cancellation")
	}
}

func TestWorkerControlClaimAuthorityFailsClosed(t *testing.T) {
	tests := []struct {
		name      string
		customize func(*WorkerServiceOptions)
		mutate    func(*workerTestEnvironment, *api.WorkerSession) (string, api.ControlClaimRequest)
		want      error
	}{
		{name: "wrong token", mutate: func(_ *workerTestEnvironment, session *api.WorkerSession) (string, api.ControlClaimRequest) {
			return "wrong-worker-control-session-token", controlClaimRequest(session, 0)
		}, want: domain.ErrUnauthorized},
		{name: "stale generation", mutate: func(_ *workerTestEnvironment, session *api.WorkerSession) (string, api.ControlClaimRequest) {
			request := controlClaimRequest(session, 0)
			request.Generation++
			return session.SessionToken, request
		}, want: domain.ErrSessionGenerationConflict},
		{name: "stale fencing", mutate: func(_ *workerTestEnvironment, session *api.WorkerSession) (string, api.ControlClaimRequest) {
			request := controlClaimRequest(session, 0)
			request.FencingToken++
			return session.SessionToken, request
		}, want: domain.ErrFencingRejected},
		{name: "expired token", customize: func(options *WorkerServiceOptions) {
			options.TokenLifetime = 30 * time.Second
			options.WorkerLease = 2 * time.Minute
		}, mutate: func(environment *workerTestEnvironment, session *api.WorkerSession) (string, api.ControlClaimRequest) {
			environment.clock.Advance(31 * time.Second)
			return session.SessionToken, controlClaimRequest(session, 0)
		}, want: domain.ErrUnauthorized},
		{name: "expired lease", mutate: func(environment *workerTestEnvironment, session *api.WorkerSession) (string, api.ControlClaimRequest) {
			environment.clock.Advance(61 * time.Second)
			return session.SessionToken, controlClaimRequest(session, 0)
		}, want: domain.ErrLeaseExpired},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			environment := newWorkerTestEnvironment(t, test.customize)
			session := environment.register(t, "worker-control-authority")
			environment.heartbeat(t, session)
			token, request := test.mutate(environment, session)
			_, err := environment.service.ClaimWorkerCommand(context.Background(), environment.workerID, token, request)
			if !errors.Is(err, test.want) {
				t.Fatalf("control authority error=%v want=%v", err, test.want)
			}
		})
	}
}

func TestWorkerControlClaimFailsClosedAfterLeaseRevoke(t *testing.T) {
	broker := newObservedBroker()
	environment := newWorkerTestEnvironment(t, nil)
	service, err := NewWorkerService(environment.repository, broker, WorkerServiceOptions{
		Now: environment.clock.Now, NewID: func(prefix string) string { return prefix + "-revoke" },
		NewSessionToken: func() (string, error) { return "worker-control-revoke-token-00000000000000000000", nil },
		WorkerLease:     time.Minute, TokenLifetime: 10 * time.Minute, MailboxLease: 10 * time.Second, RunLease: 2 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	environment.service = service
	session := environment.register(t, "worker-control-revoke")
	environment.heartbeat(t, session)
	result := make(chan error, 1)
	go func() {
		_, claimErr := service.ClaimWorkerCommand(context.Background(), environment.workerID,
			session.SessionToken, controlClaimRequest(session, 5))
		result <- claimErr
	}()
	select {
	case <-broker.subscribed:
	case <-time.After(time.Second):
		t.Fatal("revocation test control claim did not subscribe")
	}
	admin := newWorkerAdminTestService(t, environment, broker)
	if _, err := admin.Revoke(context.Background(), environment.ownerID, session.Worker.ID, session.Worker.Generation); err != nil {
		t.Fatal(err)
	}
	select {
	case claimErr := <-result:
		if !errors.Is(claimErr, domain.ErrFencingRejected) && !errors.Is(claimErr, domain.ErrLeaseExpired) {
			t.Fatalf("revoked control claim error=%v", claimErr)
		}
	case <-time.After(time.Second):
		t.Fatal("lease revoke did not wake and reject control claim")
	}
}

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
