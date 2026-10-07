package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"openagentx/internal/api"
	"openagentx/internal/controlplane"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

type sessionFixture struct {
	r          *Repository
	f          repositoryFixture
	commands   *controlplane.CommandService
	worker     *domain.WorkerInstance
	guard      domain.WorkerWriteGuard
	descriptor openruntime.AdapterDescriptor
	source     string
	oldBinding domain.SessionBinding
}

func newSessionFixture(t *testing.T) sessionFixture {
	t.Helper()
	r, f, _ := externalFixture(t)
	d := messageDescriptor(openruntime.SteerNative)
	d.AdapterID = "codex-app-server"
	d.RuntimeIdentity.AdapterID = d.AdapterID
	d.SessionHandoff = true
	d.SessionModes = []domain.SessionMode{domain.SessionModeNew, domain.SessionModeResume}
	w, g := registerMessageWorker(t, r, f, d)
	x := sessionFixture{r: r, f: f, commands: nativeTaskCommand(t, r), worker: w, guard: g, descriptor: d}
	req := nativeTaskRequest(f, "old-context", "old-thread")
	req.RuntimeSession.BackendID = "local"
	source, e := x.commands.CreateTask(context.Background(), f.ownerPrincipal, req)
	if e != nil {
		t.Fatal(e)
	}
	x.source = source.TaskID
	run := x.begin(t, source.TaskID, "source")
	x.bound(t, run, "old-thread")
	if e = x.finish(run, openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, FinalReply: true, Result: "old reply", ProviderSessionID: "old-thread"}); e != nil {
		t.Fatal(e)
	}
	b, e := r.GetSessionBinding(context.Background(), x.source, f.agentID, "local")
	if e != nil {
		t.Fatal(e)
	}
	x.oldBinding = *b
	return x
}
func (x sessionFixture) request(key string) api.CreateTaskRequest {
	return api.CreateTaskRequest{Meta: api.CommandMeta{IdempotencyKey: key}, SenderPrincipalID: x.f.ownerPrincipal, TargetAgentID: x.f.agentID, OrganizationID: x.f.organizationID, DispatchMode: domain.DispatchModeDirect, Intent: domain.TaskIntentQuery, Content: "Context only: preserve role and workspace. Acknowledge HANDOFF-NONCE; do not repeat work.", NewSession: &domain.NewSessionRequest{BackendID: "local", ExpectedThreadID: "old-thread", ExpectedVersion: 0}}
}
func (x sessionFixture) begin(t *testing.T, taskID, suffix string) *domain.RunAttempt {
	t.Helper()
	ctx := context.Background()
	task, e := x.r.GetTask(ctx, taskID)
	if e != nil {
		t.Fatal(e)
	}
	backends, e := x.r.ListWorkerBackends(ctx, x.worker.ID)
	if e != nil {
		t.Fatal(e)
	}
	plan, e := (controlplane.M1TurnPlanner{Bindings: x.r}).Plan(ctx, *task, nil, backends)
	if e != nil {
		t.Fatal(e)
	}
	handoff, e := x.r.GetSessionHandoff(ctx, taskID)
	if e != nil {
		t.Fatal(e)
	}
	if handoff != nil && (!plan.Execution.Spec.Session.ForceNew || plan.SessionBinding != nil) {
		t.Fatal("handoff did not freeze a genuinely new session")
	}
	item, e := x.r.TryClaimMailbox(ctx, x.guard, 1, x.guard.CheckedAt.Add(time.Hour), journalEvent("claim-session-"+suffix, "mailbox.claimed", x.f.agentPrincipal, x.f.organizationID))
	if e != nil || item == nil || item.TaskID != taskID {
		t.Fatalf("claim=%+v err=%v", item, e)
	}
	resolved, _ := json.Marshal(plan.Execution)
	run := &domain.RunAttempt{ID: "run-session-" + suffix, TaskID: taskID, AgentID: x.f.agentID, Version: 1, Status: domain.RunAttemptStarting, WorkerInstanceID: x.worker.ID, FencingToken: x.worker.FencingToken, LeaseUntil: x.guard.CheckedAt.Add(time.Hour), ExecutionSpecVersion: 1, RequestedExecutionJSON: `{}`, ResolvedExecutionJSON: string(resolved), AdapterID: x.descriptor.AdapterID, BackendID: "local", Model: plan.Execution.Spec.Model, ReasoningMode: plan.Execution.Spec.Reasoning.Mode}
	if _, _, e = x.r.BeginClaimedRunAttempt(ctx, x.guard, item.ID, task.Version, run, "", nil, journalEvent("running-session-"+suffix, "task.running", x.f.agentPrincipal, x.f.organizationID), journalEvent("started-session-"+suffix, "run_attempt.started", x.f.agentPrincipal, x.f.organizationID), journalEvent("accepted-session-"+suffix, "mailbox.accepted", x.f.agentPrincipal, x.f.organizationID)); e != nil {
		t.Fatal(e)
	}
	return run
}
func (x sessionFixture) bound(t *testing.T, run *domain.RunAttempt, provider string) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"runtime_event_type": "session.bound", "payload": map[string]any{"provider_session_id": provider, "source": "new"}, "occurred_at": x.guard.CheckedAt})
	ev := journalEvent("bound-"+run.ID, "runtime.session.bound", x.f.agentPrincipal, x.f.organizationID)
	ev.Payload = payload
	ev.AggregateID = run.ID
	ev.AggregateType = "run_attempt"
	if e := x.r.AppendRunEvents(context.Background(), x.guard, run.ID, 1, []*domain.JournalEvent{ev}); e != nil {
		t.Fatal(e)
	}
}
func (x sessionFixture) finish(run *domain.RunAttempt, result openruntime.TurnResult) error {
	return x.r.FinishRun(context.Background(), x.guard, run.ID, 2, 1, result, nil, 0, nil, journalEvent("settled-"+run.ID, "task.settled", x.f.agentPrincipal, x.f.organizationID), journalEvent("finished-"+run.ID, "run_attempt.finished", x.f.agentPrincipal, x.f.organizationID))
}
func TestAgentSessionHandoffPublishesAtomicallyAndPreservesHistory(t *testing.T) {
	x := newSessionFixture(t)
	ctx := context.Background()
	service, _ := controlplane.NewExternalSessionService(x.r, nil, func() time.Time { return repositoryTestTime })
	binding, token, e := service.Bind(ctx, x.f.ownerPrincipal, domain.BindExternalSessionInput{AgentID: x.f.agentID, Mode: "managed", ManagedContextTaskID: x.source, ManagedBackendID: "local", AllowedPeerAgentIDs: []string{"pay"}})
	if e != nil {
		t.Fatal(e)
	}
	req := x.request("handoff-success")
	created, e := x.commands.CreateTask(ctx, x.f.ownerPrincipal, req)
	if e != nil {
		t.Fatal(e)
	}
	current, e := x.r.ReadAgentSession(ctx, x.f.agentID, "local")
	if e != nil || current.ThreadID != "old-thread" || current.PendingTaskID != created.TaskID || current.Version != 0 {
		t.Fatalf("pending=%+v %v", current, e)
	}
	if replay, e := x.commands.CreateTask(ctx, x.f.ownerPrincipal, req); e != nil || replay.TaskID != created.TaskID {
		t.Fatalf("pending replay=%+v %v", replay, e)
	}
	plain := req
	plain.NewSession = nil
	plain.Meta.IdempotencyKey = "during-pending"
	if _, e = x.commands.CreateTask(ctx, x.f.ownerPrincipal, plain); e == nil {
		t.Fatal("ordinary input bypassed pending gate")
	}
	if _, e = x.r.SetAgentModelSettings(ctx, x.f.agentID, domain.AgentModelSettingsUpdate{BackendID: "local", Model: x.descriptor.Models[0], ExpectedThreadID: "old-thread"}, x.f.ownerPrincipal); e == nil {
		t.Fatal("native model write bypassed pending gate")
	}
	run := x.begin(t, created.TaskID, "handoff")
	x.bound(t, run, "new-thread")
	current, _ = x.r.ReadAgentSession(ctx, x.f.agentID, "local")
	if current.ThreadID != "old-thread" {
		t.Fatal("candidate bound before successful handoff was published")
	}
	fault := errors.New("handoff settlement rollback")
	x.r.faultInjector = func(p FaultPoint) error {
		if p == FaultBeforeCommit {
			return fault
		}
		return nil
	}
	result := openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, FinalReply: true, Result: "ACK HANDOFF-NONCE", ProviderSessionID: "new-thread"}
	if e = x.finish(run, result); !errors.Is(e, fault) {
		t.Fatal("expected injected failure", e)
	}
	x.r.faultInjector = nil
	current, _ = x.r.ReadAgentSession(ctx, x.f.agentID, "local")
	if current.ThreadID != "old-thread" || current.PendingTaskID != created.TaskID {
		t.Fatal("rollback published session")
	}
	if e = x.finish(run, result); e != nil {
		t.Fatal(e)
	}
	current, e = x.r.ReadAgentSession(ctx, x.f.agentID, "local")
	if e != nil || current.ThreadID != "new-thread" || current.ContextTaskID != created.TaskID || current.Version != 1 || current.PendingTaskID != "" {
		t.Fatalf("published=%+v %v", current, e)
	}
	old, e := x.r.GetSessionBinding(ctx, x.source, x.f.agentID, "local")
	if e != nil || *old != x.oldBinding {
		t.Fatal("historical binding changed", e)
	}
	updated, e := service.Status(ctx, token)
	if e != nil || updated.ID != binding.ID || updated.Generation != binding.Generation+1 || updated.ThreadID != "new-thread" || updated.ManagedContextTaskID != created.TaskID || updated.AllowedPeerAgentIDs[0] != "pay" {
		t.Fatalf("managed=%+v err=%v", updated, e)
	}
	if _, _, e = service.Bind(ctx, x.f.ownerPrincipal, domain.BindExternalSessionInput{AgentID: x.f.agentID, Mode: "managed", ManagedContextTaskID: x.source, ManagedBackendID: "local", AllowedPeerAgentIDs: []string{"pay"}, ExpectedGeneration: updated.Generation}); e == nil {
		t.Fatal("managed context moved back to old thread")
	}
	if replay, e := x.commands.CreateTask(ctx, x.f.ownerPrincipal, req); e != nil || replay.TaskID != created.TaskID {
		t.Fatal("completed command replay not idempotent", e)
	}
	explicit := nativeTaskRequest(x.f, "retired-view", "old-thread")
	explicit.RuntimeSession.BackendID = "local"
	if _, e = x.commands.CreateTask(ctx, x.f.ownerPrincipal, explicit); e == nil {
		t.Fatal("old native view submitted work")
	}
	explicit.RuntimeSession.ProviderSessionID = ""
	explicit.RuntimeSession.SourceTaskID = x.source
	if _, e = x.commands.CreateTask(ctx, x.f.ownerPrincipal, explicit); e == nil {
		t.Fatal("retired source Task was continued")
	}
	if _, e = x.r.SetAgentModelSettings(ctx, x.f.agentID, domain.AgentModelSettingsUpdate{BackendID: "local", Model: x.descriptor.Models[0], ExpectedThreadID: "old-thread"}, x.f.ownerPrincipal); e == nil {
		t.Fatal("old native view changed model")
	}
	plain.Meta.IdempotencyKey = "after-publish"
	next, e := x.commands.CreateTask(ctx, x.f.ownerPrincipal, plain)
	if e != nil {
		t.Fatal(e)
	}
	nextBinding, e := x.r.GetSessionBinding(ctx, next.TaskID, x.f.agentID, "local")
	if e != nil || nextBinding.ProviderSessionID != "new-thread" {
		t.Fatalf("default=%+v %v", nextBinding, e)
	}
	var leaks int
	if e = x.r.db.QueryRow(`SELECT COUNT(*) FROM event_journal WHERE payload_json LIKE '%new-thread%'`).Scan(&leaks); e != nil || leaks != 0 {
		t.Fatalf("routing leaked into public journal: %d %v", leaks, e)
	}
}
func TestAgentSessionHandoffFailureCancellationAndNoReplay(t *testing.T) {
	for _, status := range []openruntime.TurnResultStatus{openruntime.TurnResultFailed, openruntime.TurnResultCanceled, openruntime.TurnResultUncertain, openruntime.TurnResultWaitingInput} {
		t.Run(string(status), func(t *testing.T) {
			x := newSessionFixture(t)
			ctx := context.Background()
			created, e := x.commands.CreateTask(ctx, x.f.ownerPrincipal, x.request("failed-handoff"))
			if e != nil {
				t.Fatal(e)
			}
			run := x.begin(t, created.TaskID, "failure")
			x.bound(t, run, "unpublished-candidate")
			if e = x.finish(run, openruntime.TurnResult{Status: status, Error: "not completed", ProviderSessionID: "unpublished-candidate"}); e != nil {
				t.Fatal(e)
			}
			current, e := x.r.ReadAgentSession(ctx, x.f.agentID, "local")
			if e != nil || current.ThreadID != "old-thread" || current.Version != 0 || current.PendingTaskID != "" {
				t.Fatalf("failed pointer=%+v %v", current, e)
			}
			candidate, e := x.r.GetSessionBinding(ctx, created.TaskID, x.f.agentID, "local")
			if e != nil || candidate.ProviderSessionID != "unpublished-candidate" {
				t.Fatal("candidate history lost", e)
			}
			task, e := x.r.GetTask(ctx, created.TaskID)
			if e != nil || !task.IsTerminal() {
				t.Fatal("handoff left automatically resumable", e)
			}
			replay, e := x.commands.CreateTask(ctx, x.f.ownerPrincipal, x.request("failed-handoff"))
			if e != nil || replay.TaskID != created.TaskID {
				t.Fatal("failure command replay created work", e)
			}
		})
	}
	t.Run("queued-cancel", func(t *testing.T) {
		x := newSessionFixture(t)
		ctx := context.Background()
		created, e := x.commands.CreateTask(ctx, x.f.ownerPrincipal, x.request("cancel-before-start"))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = x.commands.CancelTask(ctx, x.f.ownerPrincipal, created.TaskID, api.CancelTaskRequest{RequestedBy: x.f.ownerPrincipal, Meta: api.CommandMeta{IdempotencyKey: "cancel-handoff", ExpectedVersion: created.TaskVersion}}); e != nil {
			t.Fatal(e)
		}
		current, e := x.r.ReadAgentSession(ctx, x.f.agentID, "local")
		if e != nil || current.ThreadID != "old-thread" || current.PendingTaskID != "" {
			t.Fatal("queued cancel retained gate", e)
		}
	})
}
func TestAgentSessionHandoffAdmissionRollbackCASAndBusy(t *testing.T) {
	x := newSessionFixture(t)
	ctx := context.Background()
	before := nativeTaskCounts(t, x.r)
	for _, point := range []FaultPoint{FaultAfterStateWrite, FaultAfterDelivery, FaultBeforeCommit} {
		failure := errors.New("handoff request rollback")
		x.r.faultInjector = func(p FaultPoint) error {
			if p == point {
				return failure
			}
			return nil
		}
		if _, e := x.commands.CreateTask(ctx, x.f.ownerPrincipal, x.request("fault")); !errors.Is(e, failure) {
			t.Fatal(e)
		}
		x.r.faultInjector = nil
		nativeTaskUnchanged(t, x.r, before)
		if n := externalCount(t, x.r, "agent_sessions"); n != 0 {
			t.Fatal("pending row escaped rollback")
		}
	}
	stale := x.request("stale")
	stale.NewSession.ExpectedVersion = 42
	if _, e := x.commands.CreateTask(ctx, x.f.ownerPrincipal, stale); !errors.Is(e, domain.ErrStaleVersion) {
		t.Fatal("stale CAS", e)
	}
	busy := x.request("ordinary")
	busy.NewSession = nil
	if _, e := x.commands.CreateTask(ctx, x.f.ownerPrincipal, busy); e != nil {
		t.Fatal(e)
	}
	if _, e := x.commands.CreateTask(ctx, x.f.ownerPrincipal, x.request("busy")); e == nil {
		t.Fatal("queued work was bypassed")
	}
	// Fresh fixture isolates the compare-and-swap race from intentionally queued work.
	y := newSessionFixture(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for _, key := range []string{"race-a", "race-b"} {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			if _, e := y.commands.CreateTask(ctx, y.f.ownerPrincipal, y.request(key)); e == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}(key)
	}
	wg.Wait()
	if success != 1 {
		t.Fatalf("race admitted %d requests", success)
	}
}
func TestAgentSessionBootstrapDoesNotTrustUnexecutedNativeReference(t *testing.T) {
	r, f, _ := externalFixture(t)
	ctx := context.Background()
	empty, e := r.ReadAgentSession(ctx, f.agentID, "local")
	if e != nil || empty.ThreadID != "" || empty.Version != 0 {
		t.Fatal("empty initial session", e)
	}
	req := nativeTaskRequest(f, "unexecuted", "not-a-real-thread")
	req.RuntimeSession.BackendID = "local"
	if _, e = nativeTaskCommand(t, r).CreateTask(ctx, f.ownerPrincipal, req); e != nil {
		t.Fatal(e)
	}
	empty, e = r.ReadAgentSession(ctx, f.agentID, "local")
	if e != nil || empty.ThreadID != "" {
		t.Fatal("unexecuted claim became active", e)
	}
	if _, e = r.ReadAgentSession(ctx, "unknown", "local"); !errors.Is(e, domain.ErrAgentNotFound) {
		t.Fatal("unknown Agent accepted", e)
	}
}
func TestAgentSessionHandoffRecoveryAndPendingConsultation(t *testing.T) {
	t.Run("lease-expiry", func(t *testing.T) {
		x := newSessionFixture(t)
		ctx := context.Background()
		created, e := x.commands.CreateTask(ctx, x.f.ownerPrincipal, x.request("expired-handoff"))
		if e != nil {
			t.Fatal(e)
		}
		run := x.begin(t, created.TaskID, "expired")
		x.bound(t, run, "orphan-candidate")
		// Only the fixture clock changes; recovery uses its real transaction path.
		if e = x.r.CreatePrincipal(ctx, &domain.Principal{ID: "recovery-system", Kind: domain.PrincipalSystem, DisplayName: "Recovery", Status: domain.IdentityActive}, journalEvent("recovery-system-created", "principal.created", x.f.ownerPrincipal, "")); e != nil {
			t.Fatal(e)
		}
		x.r.now = func() time.Time { return repositoryTestTime.Add(2 * time.Hour) }
		if e = x.r.ReconcileExpired(ctx); e != nil {
			t.Fatal(e)
		}
		current, e := x.r.ReadAgentSession(ctx, x.f.agentID, "local")
		if e != nil || current.ThreadID != "old-thread" || current.PendingTaskID != "" {
			t.Fatalf("recovery=%+v %v", current, e)
		}
		task, _ := x.r.GetTask(ctx, created.TaskID)
		if task.Status != domain.TaskStatusUncertain {
			t.Fatal("expired handoff was not kept uncertain")
		}
	})
	t.Run("pending-consultation", func(t *testing.T) {
		x := newSessionFixture(t)
		ctx := context.Background()
		service, _ := controlplane.NewExternalSessionService(x.r, nil, func() time.Time { return repositoryTestTime })
		_, token, e := service.Bind(ctx, x.f.ownerPrincipal, domain.BindExternalSessionInput{AgentID: x.f.agentID, Mode: "managed", ManagedContextTaskID: x.source, ManagedBackendID: "local", AllowedPeerAgentIDs: []string{"pay"}})
		if e != nil {
			t.Fatal(e)
		}
		externalBind(t, service, x.f.ownerPrincipal, "pay", x.f.agentID)
		if _, e = service.Send(ctx, token, domain.SendExternalMessageInput{TargetAgentID: "pay", Kind: domain.ExternalMessageConsultation, Content: "read-only question", IdempotencyKey: "awaiting-reply"}); e != nil {
			t.Fatal(e)
		}
		if _, e = x.commands.CreateTask(ctx, x.f.ownerPrincipal, x.request("consultation-pending")); e == nil {
			t.Fatal("pending consultation migrated across generations")
		}
	})
}
func TestAgentRemovalIncludesActiveSessionInDigestAndClosure(t *testing.T) {
	r, _ := removalFixture(t)
	before := removalPlan(t, r)
	removalExec(t, r.db, `INSERT INTO agent_sessions VALUES('test','test','thread-test','task-test',1,NULL,'2026-01-01T00:00:00Z')`)
	after := removalPlan(t, r)
	if after.Counts["agent_sessions"] != 1 || after.Digest == before.Digest {
		t.Fatal("session excluded from maintenance snapshot")
	}
	if _, e := r.ApplyAgentRemoval(context.Background(), "owner", before.AgentIDs, before.Digest); e == nil {
		t.Fatal("stale removal snapshot accepted")
	}
	if _, e := r.ApplyAgentRemoval(context.Background(), "owner", after.AgentIDs, after.Digest); e != nil {
		t.Fatal(e)
	}
	if removalCount(t, r.db, "agent_sessions") != 0 {
		t.Fatal("removed Agent session retained")
	}
}
