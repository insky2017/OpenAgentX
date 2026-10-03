package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"openagentx/internal/controlplane"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

type managedFixture struct {
	r        *Repository
	f        repositoryFixture
	s        *controlplane.ExternalSessionService
	tokens   map[string]string
	bindings map[string]*domain.ExternalSessionBinding
}

func newManagedFixture(t *testing.T) managedFixture {
	t.Helper()
	r, f, s := externalFixture(t)
	x := managedFixture{r: r, f: f, s: s, tokens: map[string]string{}, bindings: map[string]*domain.ExternalSessionBinding{}}
	for _, agent := range []string{f.agentID, "pay"} {
		af := f
		af.agentID = agent
		if agent == "pay" {
			af.agentPrincipal = "principal-pay"
		}
		task, msg, mail, event := newTaskDelivery(af, "managed-context-"+agent)
		binding := &domain.SessionBinding{ID: "native-" + agent, ContextID: task.ID, AgentID: agent, BackendID: "local", ProviderSessionID: "existing-real-thread-" + agent, State: domain.SessionBindingActive, Version: 1, CreatedAt: repositoryTestTime, UpdatedAt: repositoryTestTime}
		if _, err := r.CreateTaskWithSession(context.Background(), task, msg, mail, event, binding); err != nil {
			t.Fatal(err)
		}
		peer := "pay"
		if agent == "pay" {
			peer = f.agentID
		}
		b, token, err := s.Bind(context.Background(), f.ownerPrincipal, domain.BindExternalSessionInput{AgentID: agent, Mode: "managed", ManagedContextTaskID: task.ID, ManagedBackendID: "local", AllowedPeerAgentIDs: []string{peer}})
		if err != nil {
			t.Fatal(err)
		}
		if b.ThreadID != binding.ProviderSessionID || b.HostID != "managed-worker" {
			t.Fatalf("wrong native binding %+v", b)
		}
		x.bindings[agent] = b
		x.tokens[agent] = token
	}
	if _, err := r.ApplyExternalRoles(context.Background(), f.ownerPrincipal, domain.ApplyExternalRolesInput{OrganizationID: f.organizationID, Rules: []domain.ExternalRoleRule{{Scope: "pay.contract", OwnerAgentID: "pay", Description: "Read-only payment contract"}}}); err != nil {
		t.Fatal(err)
	}
	return x
}
func (x managedFixture) send(t *testing.T, key string) *domain.ExternalMessage {
	t.Helper()
	m, err := x.s.Send(context.Background(), x.tokens[x.f.agentID], domain.SendExternalMessageInput{TargetAgentID: "pay", Scope: "pay.contract", Kind: domain.ExternalMessageConsultation, Content: "Read the contract and answer only; do not mutate.", IdempotencyKey: key})
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func (x managedFixture) begin(t *testing.T, taskID, agent string) (*domain.RunAttempt, domain.WorkerWriteGuard) {
	t.Helper()
	ctx := context.Background()
	principal := x.f.agentPrincipal
	if agent == "pay" {
		principal = "principal-pay"
	}
	tokenDigest := sha256.Sum256([]byte("managed-worker-token-" + agent))
	reg := domain.WorkerRegistration{WorkerInstanceID: "managed-worker-" + agent, AgentID: agent, Transport: domain.WorkerTransportUnix, PrincipalID: principal, Capabilities: []string{"coding"}, SessionTokenDigest: hex.EncodeToString(tokenDigest[:]), TokenExpiresAt: repositoryTestTime.Add(time.Hour), LeaseUntil: repositoryTestTime.Add(time.Hour)}
	d := messageDescriptor(openruntime.SteerQueued)
	worker, _, err := x.r.RegisterWorker(ctx, reg, []openruntime.BackendRegistration{{BackendID: "local", Descriptor: d, Health: openruntime.BackendHealthy, Network: domain.NetworkPolicy{Mode: domain.NetworkInherit}}}, journalEvent("managed-worker-register-"+agent, "worker.registered", principal, x.f.organizationID))
	if err != nil {
		t.Fatal(err)
	}
	guard := domain.WorkerWriteGuard{WorkerInstanceID: worker.ID, AgentID: agent, PrincipalID: principal, SessionTokenDigest: reg.SessionTokenDigest, Generation: worker.Generation, FencingToken: worker.FencingToken, CheckedAt: repositoryTestTime}
	if _, err = x.r.HeartbeatWorker(ctx, guard, domain.WorkerStatusOnline, nil, repositoryTestTime.Add(time.Hour), repositoryTestTime.Add(time.Hour), nil, journalEvent("managed-worker-online-"+agent, "worker.heartbeat", principal, x.f.organizationID)); err != nil {
		t.Fatal(err)
	}
	resolved, _ := json.Marshal(domain.ResolvedExecutionSpec{Version: 1, Spec: domain.ExecutionSpec{BackendID: "local", Session: domain.SessionSpec{Mode: domain.SessionModeResume, ContextID: taskID}}})
	run := &domain.RunAttempt{ID: "managed-run-" + agent, TaskID: taskID, AgentID: agent, Version: 1, Status: domain.RunAttemptRunning, WorkerInstanceID: worker.ID, FencingToken: worker.FencingToken, LeaseUntil: repositoryTestTime.Add(time.Hour), ExecutionSpecVersion: 1, RequestedExecutionJSON: `{}`, ResolvedExecutionJSON: string(resolved), AdapterID: d.AdapterID, BackendID: "local", Model: d.Models[0], ReasoningMode: domain.ReasoningBackendDefault}
	if _, err = x.r.BeginRunAttempt(ctx, 1, run, journalEvent("managed-running-"+agent, "task.running", principal, x.f.organizationID), journalEvent("managed-started-"+agent, "run_attempt.started", principal, x.f.organizationID)); err != nil {
		t.Fatal(err)
	}
	return run, guard
}
func (x managedFixture) finish(run *domain.RunAttempt, guard domain.WorkerWriteGuard, result openruntime.TurnResult) error {
	return x.r.FinishRun(context.Background(), guard, run.ID, 2, 1, result, nil, 0, nil, journalEvent("managed-settled-"+run.AgentID, "task.settled", guard.PrincipalID, x.f.organizationID), journalEvent("managed-finished-"+run.AgentID, "run_attempt.finished", guard.PrincipalID, x.f.organizationID))
}
func TestManagedConsultationAtomicDispatchFinishAndNoReplyLoop(t *testing.T) {
	x := newManagedFixture(t)
	ctx := context.Background()
	m := x.send(t, "managed-ask")
	if _, err := x.s.Receipt(ctx, x.tokens["pay"], m.ID, domain.ExternalReceiptInput{State: "out_of_scope", Note: "conflicting manual receipt"}); err == nil {
		t.Fatal("manual receipt detached managed Task state")
	}
	if m.TaskID == "" || m.ProcessingState != "accepted" {
		t.Fatalf("missing acceptance %+v", m)
	}
	task, err := x.r.GetTask(ctx, m.TaskID)
	if err != nil || task.Intent != domain.TaskIntentQuery {
		t.Fatalf("wrong task %+v %v", task, err)
	}
	native, err := x.r.GetSessionBinding(ctx, task.ID, "pay", "local")
	if err != nil || native.ProviderSessionID != x.bindings["pay"].ThreadID {
		t.Fatalf("forked native thread %+v %v", native, err)
	}
	replay := x.send(t, "managed-ask")
	if replay.ID != m.ID || replay.TaskID != m.TaskID || externalCount(t, x.r, "managed_message_tasks") != 1 {
		t.Fatal("send replay duplicated work")
	}
	run, guard := x.begin(t, task.ID, "pay")
	result := openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, FinalReply: true, Result: "Contract verified by reading only."}
	before := externalCount(t, x.r, "tasks")
	x.r.faultInjector = func(p FaultPoint) error {
		if p == FaultBeforeCommit {
			return errors.New("injected finish failure")
		}
		return nil
	}
	if err = x.finish(run, guard, result); err == nil {
		t.Fatal("finish fault committed")
	}
	if externalCount(t, x.r, "tasks") != before || externalCount(t, x.r, "external_messages") != 1 {
		t.Fatal("partial result/continuation committed")
	}
	current, _ := x.r.GetTask(ctx, task.ID)
	if current.Status != domain.TaskStatusRunning {
		t.Fatal("partial task settlement")
	}
	x.r.faultInjector = nil
	if err = x.finish(run, guard, result); err != nil {
		t.Fatal(err)
	}
	if err = x.finish(run, guard, result); err != nil {
		t.Fatal(err)
	}
	if externalCount(t, x.r, "external_messages") != 2 || externalCount(t, x.r, "managed_message_tasks") != 2 {
		t.Fatal("missing or duplicate result/continuation")
	}
	inbox, err := x.s.Inbox(ctx, x.tokens[x.f.agentID], 0, 100)
	if err != nil || len(inbox) != 1 || inbox[0].ReplyToMessageID != m.ID || inbox[0].TaskID == "" {
		t.Fatalf("bad correlated result %v %v", inbox, err)
	}
	targets, err := x.r.ManagedCollaborationWakeTargets(ctx, run.ID)
	if err != nil || len(targets) != 1 || targets[0] != x.f.agentID {
		t.Fatalf("wake targets %v %v", targets, err)
	}
	continuation, err := x.r.GetTask(ctx, inbox[0].TaskID)
	if err != nil || continuation.ParentTaskID == nil || *continuation.ParentTaskID != x.bindings[x.f.agentID].ManagedContextTaskID {
		t.Fatal("lost origin context")
	}
	if !strings.Contains(continuation.Content, "within your own Agent responsibilities") || !strings.Contains(continuation.Content, "Original consultation scope: pay.contract") {
		t.Fatal("result consumption confuses original scope with requester responsibility")
	}
	continuationRun, continuationGuard := x.begin(t, continuation.ID, x.f.agentID)
	if err = x.finish(continuationRun, continuationGuard, openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, FinalReply: true, Result: "Original question answered using the consultation."}); err != nil {
		t.Fatal(err)
	}
	if externalCount(t, x.r, "external_messages") != 2 || externalCount(t, x.r, "managed_message_tasks") != 2 {
		t.Fatal("result caused an infinite reply chain")
	}
	var acknowledged int
	if err = x.r.db.QueryRow(`SELECT COUNT(*) FROM external_messages WHERE delivery_state='acknowledged' AND acknowledged_at IS NOT NULL`).Scan(&acknowledged); err != nil || acknowledged != 2 {
		t.Fatal("finished managed messages remain unread", err)
	}
}
func TestManagedSendRollbackRequestRejectionAndRestart(t *testing.T) {
	x := newManagedFixture(t)
	ctx := context.Background()
	in := domain.SendExternalMessageInput{TargetAgentID: "pay", Scope: "pay.contract", Kind: domain.ExternalMessageRequest, Content: "change production", IdempotencyKey: "mutation"}
	if _, err := x.s.Send(ctx, x.tokens[x.f.agentID], in); err == nil {
		t.Fatal("managed mutation admitted")
	}
	in.Kind = domain.ExternalMessageConsultation
	in.Content = "read only"
	in.IdempotencyKey = "rollback"
	before := externalCount(t, x.r, "tasks")
	for _, point := range []FaultPoint{FaultAfterStateWrite, FaultAfterDelivery, FaultBeforeCommit} {
		x.r.faultInjector = func(p FaultPoint) error {
			if p == point {
				return errors.New("send fail")
			}
			return nil
		}
		if _, err := x.s.Send(ctx, x.tokens[x.f.agentID], in); err == nil {
			t.Fatal("send fault committed")
		}
		if externalCount(t, x.r, "tasks") != before || externalCount(t, x.r, "external_messages") != 0 || externalCount(t, x.r, "managed_message_tasks") != 0 {
			t.Fatal("partial managed dispatch")
		}
	}
	x.r.faultInjector = nil
	m := x.send(t, "restart")
	var seq int
	var name, path string
	if err := x.r.db.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	if err := x.r.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path, Options{Now: func() time.Time { return repositoryTestTime }})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	s, err := controlplane.NewExternalSessionService(reopened, nil, func() time.Time { return repositoryTestTime })
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, x.tokens["pay"], m.ID)
	if err != nil || got.TaskID != m.TaskID || got.ManagedState != "queued" {
		t.Fatalf("restart lost association %+v %v", got, err)
	}
	var n int
	if err = reopened.db.QueryRow(`SELECT COUNT(*) FROM mailbox_items WHERE task_id=? AND state='pending'`, m.TaskID).Scan(&n); err != nil || n != 1 {
		t.Fatal("restart lost durable work")
	}
}
func TestManagedFailedUncertainAndRevokedRecipientPauseContinuation(t *testing.T) {
	for _, scenario := range []string{"failed", "uncertain", "revoked", "role_changed"} {
		t.Run(scenario, func(t *testing.T) {
			x := newManagedFixture(t)
			m := x.send(t, "error-result")
			run, guard := x.begin(t, m.TaskID, "pay")
			result := openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, FinalReply: true, Result: "answer"}
			switch scenario {
			case "failed":
				result = openruntime.TurnResult{Status: openruntime.TurnResultFailed, Error: "provider failed"}
			case "uncertain":
				result = openruntime.TurnResult{Status: openruntime.TurnResultUncertain, Error: "outcome unknown"}
			case "revoked":
				if err := x.s.Revoke(context.Background(), x.f.ownerPrincipal, x.f.agentID, 1); err != nil {
					t.Fatal(err)
				}
			case "role_changed":
				if _, err := x.r.ApplyExternalRoles(context.Background(), x.f.ownerPrincipal, domain.ApplyExternalRolesInput{OrganizationID: x.f.organizationID, ExpectedVersion: 1, Rules: []domain.ExternalRoleRule{{Scope: "pay.contract", OwnerAgentID: "other", Description: "Ownership moved"}}}); err != nil {
					t.Fatal(err)
				}
			}
			if err := x.finish(run, guard, result); err != nil {
				t.Fatal("completed Run could not settle:", err)
			}
			if externalCount(t, x.r, "external_messages") != 2 || externalCount(t, x.r, "managed_message_tasks") != 1 {
				t.Fatal("failure was lost or automatically continued")
			}
			var state string
			if err := x.r.db.QueryRow(`SELECT state FROM managed_message_tasks WHERE task_id=?`, m.TaskID).Scan(&state); err != nil || state != "needs_review" {
				t.Fatalf("state %s %v", state, err)
			}
		})
	}
}
func TestManagedAdmissionRechecksScopeAndBinding(t *testing.T) {
	for _, scenario := range []string{"role_changed", "binding_revoked"} {
		t.Run(scenario, func(t *testing.T) {
			x := newManagedFixture(t)
			ctx := context.Background()
			m := x.send(t, "admission")
			if scenario == "role_changed" {
				if _, err := x.r.ApplyExternalRoles(ctx, x.f.ownerPrincipal, domain.ApplyExternalRolesInput{OrganizationID: x.f.organizationID, ExpectedVersion: 1, Rules: []domain.ExternalRoleRule{{Scope: "pay.contract", OwnerAgentID: "other", Description: "Moved"}}}); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := x.s.Revoke(ctx, x.f.ownerPrincipal, "pay", 1); err != nil {
					t.Fatal(err)
				}
			}
			task, err := x.r.GetTask(ctx, m.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			tx, err := x.r.begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if err = validateManagedTaskAdmissionTx(ctx, tx, task, nil, repositoryTestTime); err == nil {
				t.Fatal("changed role/binding admitted")
			}
		})
	}
}

func TestManagedClaimedAdmissionPauseIsAtomicAndDoesNotBlockNextTask(t *testing.T) {
	for _, scenario := range []string{"role_changed", "planner_new_session"} {
		t.Run(scenario, func(t *testing.T) {
			x := newManagedFixture(t)
			ctx := context.Background()
			m := x.send(t, "paused-claim")
			// These rows represent already established native context fixtures, not pending real work.
			if _, err := x.r.db.Exec(`UPDATE mailbox_items SET state='accepted',accepted_at=? WHERE task_id IN (?,?)`, formatTime(repositoryTestTime), x.bindings["pay"].ManagedContextTaskID, x.bindings[x.f.agentID].ManagedContextTaskID); err != nil {
				t.Fatal(err)
			}
			af := x.f
			af.agentID = "pay"
			af.agentPrincipal = "principal-pay"
			d := messageDescriptor(openruntime.SteerQueued)
			worker, guard := registerMessageWorker(t, x.r, af, d)
			next := createTask(t, x.r, af, "after-paused-consultation")
			claimed, err := x.r.TryClaimMailbox(ctx, guard, 1, guard.CheckedAt.Add(time.Hour), journalEvent("managed-admission-claim", "mailbox.claimed", af.agentPrincipal, af.organizationID))
			if err != nil || claimed == nil || claimed.TaskID != m.TaskID {
				t.Fatalf("claim %+v %v", claimed, err)
			}
			if scenario == "role_changed" {
				if _, err = x.r.ApplyExternalRoles(ctx, x.f.ownerPrincipal, domain.ApplyExternalRolesInput{OrganizationID: x.f.organizationID, ExpectedVersion: 1, Rules: []domain.ExternalRoleRule{{Scope: "pay.contract", OwnerAgentID: "other", Description: "Owner changed after claim"}}}); err != nil {
					t.Fatal(err)
				}
			}
			backends, err := x.r.ListWorkerBackends(ctx, worker.ID)
			if err != nil || len(backends) != 1 {
				t.Fatal(err)
			}
			resolved, _ := json.Marshal(domain.ResolvedExecutionSpec{Version: 1, Spec: domain.ExecutionSpec{AdapterID: d.AdapterID, BackendID: "local", Model: d.Models[0], Reasoning: domain.ReasoningSpec{Mode: domain.ReasoningBackendDefault}, Session: domain.SessionSpec{Mode: domain.SessionModeResume, ContextID: m.TaskID}, Timeout: time.Minute, Network: backends[0].Network}})
			if scenario == "planner_new_session" {
				var spec domain.ResolvedExecutionSpec
				if err := json.Unmarshal(resolved, &spec); err != nil {
					t.Fatal(err)
				}
				spec.Spec.Session.Mode = domain.SessionModeNew
				resolved, _ = json.Marshal(spec)
			}
			run := &domain.RunAttempt{ID: "never-started-managed", TaskID: m.TaskID, AgentID: af.agentID, Version: 1, Status: domain.RunAttemptStarting, WorkerInstanceID: worker.ID, FencingToken: worker.FencingToken, LeaseUntil: guard.CheckedAt.Add(time.Hour), ExecutionSpecVersion: 1, RequestedExecutionJSON: `{}`, ResolvedExecutionJSON: string(resolved), AdapterID: d.AdapterID, BackendID: "local", Model: d.Models[0], ReasoningMode: domain.ReasoningBackendDefault}
			begin := func() error {
				_, _, e := x.r.BeginClaimedRunAttempt(ctx, guard, claimed.ID, 1, run, "", nil, journalEvent("managed-no-run-task", "task.running", af.agentPrincipal, af.organizationID), journalEvent("managed-no-run-run", "run_attempt.started", af.agentPrincipal, af.organizationID), journalEvent("managed-no-run-mailbox", "mailbox.accepted", af.agentPrincipal, af.organizationID))
				return e
			}
			x.r.faultInjector = func(p FaultPoint) error {
				if p == FaultBeforeCommit {
					return errors.New("pause failure")
				}
				return nil
			}
			if err = begin(); err == nil || errors.Is(err, domain.ErrManagedCollaborationPaused) {
				t.Fatal("faulted pause consumed claim")
			}
			persisted, _ := x.r.GetTask(ctx, m.TaskID)
			mail, _ := x.r.GetMailboxItem(ctx, claimed.ID)
			if persisted.Status != domain.TaskStatusQueued || mail.State != domain.MailboxStateClaimed {
				t.Fatal("pause partially committed")
			}
			x.r.faultInjector = nil
			if err = begin(); !errors.Is(err, domain.ErrManagedCollaborationPaused) {
				t.Fatalf("pause result %v", err)
			}
			persisted, _ = x.r.GetTask(ctx, m.TaskID)
			mail, _ = x.r.GetMailboxItem(ctx, claimed.ID)
			if persisted.Status != domain.TaskStatusFailed || persisted.Error == nil || *persisted.Error != "managed_collaboration_needs_review" || mail.State != domain.MailboxStateAccepted {
				t.Fatal("missing visible review state")
			}
			if externalCount(t, x.r, "run_attempts") != 0 || externalCount(t, x.r, "external_messages") != 1 {
				t.Fatal("paused admission fabricated runtime or reply")
			}
			following, err := x.r.TryClaimMailbox(ctx, guard, 1, guard.CheckedAt.Add(time.Hour), journalEvent("managed-following-claim", "mailbox.claimed", af.agentPrincipal, af.organizationID))
			if err != nil || following == nil || following.TaskID != next.Task.ID {
				t.Fatalf("paused claim blocked next task %+v %v", following, err)
			}
		})
	}
}

func TestManagedBindingCannotTakeOverExternalOrForeignContext(t *testing.T) {
	x := newManagedFixture(t)
	ctx := context.Background()
	in := domain.BindExternalSessionInput{AgentID: "other", Mode: "managed", ManagedContextTaskID: x.bindings["pay"].ManagedContextTaskID, ManagedBackendID: "local", AllowedPeerAgentIDs: []string{"pay"}}
	if _, _, err := x.s.Bind(ctx, x.f.ownerPrincipal, in); err == nil {
		t.Fatal("foreign Agent native context stolen")
	}
	old, _ := externalBind(t, x.s, x.f.ownerPrincipal, "other", "pay")
	if err := x.s.Revoke(ctx, x.f.ownerPrincipal, "other", old.Generation); err != nil {
		t.Fatal(err)
	}
	in.ExpectedGeneration = old.Generation + 1
	if _, _, err := x.s.Bind(ctx, x.f.ownerPrincipal, in); err == nil {
		t.Fatal("external binding overwritten by managed mode")
	}
	preserved, err := x.s.Binding(ctx, x.f.ownerPrincipal, "other")
	if err != nil || preserved.Mode != "external" || preserved.ThreadID != old.ThreadID || preserved.HostID != old.HostID {
		t.Fatal("original Desktop data modified")
	}
	if _, err = x.s.Send(ctx, x.tokens[x.f.agentID], domain.SendExternalMessageInput{TargetAgentID: "pay", Kind: domain.ExternalMessageConsultation, Content: "read this", IdempotencyKey: "missing-scope"}); err == nil {
		t.Fatal("managed query accepted without scope")
	}
}

func TestRevokedExternalMigrationPreservesIdentityThreadAndAtomicity(t *testing.T) {
	for _, scenario := range []string{"active", "stale", "unfinished_task", "pending_request", "unread_result", "thread_mismatch", "rollback", "success"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			r, f, s := externalFixture(t)
			old, oldToken := externalBind(t, s, f.ownerPrincipal, f.agentID, "pay")
			in := domain.BindExternalSessionInput{AgentID: f.agentID, Mode: "managed", ManagedContextTaskID: "imported-context", ManagedBackendID: "local", AllowedPeerAgentIDs: []string{"pay"}, ExpectedGeneration: old.Generation}
			if scenario == "pending_request" || scenario == "unread_result" {
				_, payToken := externalBind(t, s, f.ownerPrincipal, "pay", f.agentID)
				m, err := s.Send(ctx, oldToken, domain.SendExternalMessageInput{TargetAgentID: "pay", Kind: domain.ExternalMessageConsultation, Content: "Read-only contract question", IdempotencyKey: "before-migration"})
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "unread_result" {
					if _, err = s.Send(ctx, payToken, domain.SendExternalMessageInput{Kind: domain.ExternalMessageResult, ReplyToMessageID: m.ID, Content: "Contract answer", IdempotencyKey: "before-migration-result"}); err != nil {
						t.Fatal(err)
					}
				}
			}
			if scenario != "active" {
				if err := s.Revoke(ctx, f.ownerPrincipal, f.agentID, old.Generation); err != nil {
					t.Fatal(err)
				}
				in.ExpectedGeneration++
				if _, err := s.Status(ctx, oldToken); !errors.Is(err, domain.ErrUnauthorized) {
					t.Fatalf("revocation retained old token: %v", err)
				}
			}
			if scenario == "stale" {
				in.ExpectedGeneration = old.Generation
			}
			if scenario != "active" && scenario != "stale" {
				task, msg, mail, event := newTaskDelivery(f, in.ManagedContextTaskID)
				in.ManagedContextTaskID = task.ID
				thread := old.ThreadID
				if scenario == "thread_mismatch" {
					thread = "different-thread"
				}
				binding := &domain.SessionBinding{ID: "imported-session", ContextID: task.ID, AgentID: f.agentID, BackendID: "local", ProviderSessionID: thread, State: domain.SessionBindingActive, Version: 1, CreatedAt: repositoryTestTime, UpdatedAt: repositoryTestTime}
				if _, err := r.CreateTaskWithSession(ctx, task, msg, mail, event, binding); err != nil {
					t.Fatal(err)
				}
				if scenario != "unfinished_task" {
					if _, _, err := r.RequestTaskCancel(ctx, task.ID, 1, f.ownerPrincipal, &domain.MailboxItem{ID: "cancel-import"}, journalEvent("cancel-import-task", "task.cancel_requested", f.ownerPrincipal, f.organizationID), journalEvent("cancel-import-mail", "mailbox.cancel_created", f.ownerPrincipal, f.organizationID)); err != nil {
						t.Fatal(err)
					}
				}
			}
			before, err := s.Binding(ctx, f.ownerPrincipal, f.agentID)
			if err != nil {
				t.Fatal(err)
			}
			journalBefore := externalCount(t, r, "event_journal")
			if scenario == "rollback" {
				r.faultInjector = func(p FaultPoint) error {
					if p == FaultBeforeCommit {
						return errors.New("migration commit fault")
					}
					return nil
				}
			}
			migrated, token, err := s.Bind(ctx, f.ownerPrincipal, in)
			r.faultInjector = nil
			if scenario != "success" {
				if err == nil {
					t.Fatal("unsafe migration accepted")
				}
				if scenario == "stale" && !errors.Is(err, domain.ErrStaleVersion) {
					t.Fatalf("expected stale generation: %v", err)
				}
				reasons := map[string]string{"active": "revoked external binding", "unfinished_task": "finish managed tasks", "pending_request": "pending external messages", "unread_result": "pending external messages", "thread_mismatch": "original external thread", "rollback": "migration commit fault"}
				if reason := reasons[scenario]; reason != "" && !strings.Contains(err.Error(), reason) {
					t.Fatalf("wrong rejection, expected %q: %v", reason, err)
				}
				preserved, readErr := s.Binding(ctx, f.ownerPrincipal, f.agentID)
				if readErr != nil || preserved == nil {
					t.Fatalf("missing preserved binding: %v", readErr)
				}
				if preserved.ID != before.ID || preserved.Mode != before.Mode || preserved.State != before.State || preserved.Generation != before.Generation || preserved.ThreadID != before.ThreadID || preserved.TokenDigest != before.TokenDigest || externalCount(t, r, "event_journal") != journalBefore {
					t.Fatal("failed migration changed binding or journal")
				}
				return
			}
			if err != nil || migrated.ID != old.ID || migrated.ThreadID != old.ThreadID || migrated.AgentID != old.AgentID || migrated.PrincipalID != old.PrincipalID || migrated.Generation != old.Generation+2 || migrated.Mode != "managed" || migrated.State != "active" {
				t.Fatalf("migration lost original identity: %+v %v", migrated, err)
			}
			if _, err = s.Status(ctx, oldToken); !errors.Is(err, domain.ErrUnauthorized) {
				t.Fatal("old Desktop token survived migration")
			}
			if _, err = s.Status(ctx, token); err != nil {
				t.Fatal(err)
			}
			for _, revoke := range []bool{false, true} {
				if revoke {
					if err = s.Revoke(ctx, f.ownerPrincipal, f.agentID, migrated.Generation); err != nil {
						t.Fatal(err)
					}
					migrated.Generation++
				}
				if _, _, err = s.Bind(ctx, f.ownerPrincipal, domain.BindExternalSessionInput{AgentID: f.agentID, HostID: old.HostID, ThreadID: old.ThreadID, AllowedPeerAgentIDs: []string{"pay"}, ExpectedGeneration: migrated.Generation}); err == nil {
					t.Fatal("reverse managed to external migration accepted")
				}
			}
		})
	}
}

func TestManagedRunMustResumeTheFixedBackendAndTaskContext(t *testing.T) {
	x := newManagedFixture(t)
	ctx := context.Background()
	m := x.send(t, "fixed-execution")
	task, err := x.r.GetTask(ctx, m.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := x.r.begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, scenario := range []string{"valid", "run_backend", "resolved_backend", "new_session", "other_context", "invalid_json"} {
		t.Run(scenario, func(t *testing.T) {
			spec := domain.ResolvedExecutionSpec{Version: 1, Spec: domain.ExecutionSpec{BackendID: "local", Session: domain.SessionSpec{Mode: domain.SessionModeResume, ContextID: task.ID}}}
			run := &domain.RunAttempt{TaskID: task.ID, AgentID: task.TargetAgentID, BackendID: "local"}
			switch scenario {
			case "run_backend":
				run.BackendID = "another-runtime"
			case "resolved_backend":
				spec.Spec.BackendID = "another-runtime"
			case "new_session":
				spec.Spec.Session.Mode = domain.SessionModeNew
			case "other_context":
				spec.Spec.Session.ContextID = "another-task"
			}
			raw, _ := json.Marshal(spec)
			run.ResolvedExecutionJSON = string(raw)
			if scenario == "invalid_json" {
				run.ResolvedExecutionJSON = "{"
			}
			err := validateManagedTaskAdmissionTx(ctx, tx, task, run, repositoryTestTime)
			if scenario == "valid" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !managedAdmissionReviewable(err) {
				t.Fatalf("unsafe planner fallback not reviewable: %v", err)
			}
		})
	}
}
