package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"openagentx/internal/controlplane"
	"openagentx/internal/domain"
)

func externalFixture(t *testing.T) (*Repository, repositoryFixture, *controlplane.ExternalSessionService) {
	t.Helper()
	r, _ := openTestRepository(t, nil)
	f := seedRepository(t, r)
	ctx := context.Background()
	_, err := r.db.Exec(`INSERT INTO web_users VALUES('external-owner',?,'owner','test-only','["owner"]','active',?,?,?)`, f.ownerPrincipal, formatTime(repositoryTestTime), formatTime(repositoryTestTime), formatTime(repositoryTestTime))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"pay", "other"} {
		p := &domain.Principal{ID: "principal-" + id, Kind: domain.PrincipalAgent, DisplayName: id, Status: domain.IdentityActive}
		if err = r.CreatePrincipal(ctx, p, journalEvent("principal-event-"+id, "principal.created", f.ownerPrincipal, "")); err != nil {
			t.Fatal(err)
		}
		a := &domain.AgentIdentity{ID: id, PrincipalID: p.ID, OrganizationID: f.organizationID, DisplayName: id, Status: domain.AgentIdentityActive, Version: 1}
		profile := &domain.AgentProfileRecord{AgentID: id, Version: 1, InstructionsPath: "/srv/" + id + "/ROLE.md", WorkspaceRoot: "/srv/" + id}
		if err = r.CreateAgent(ctx, a, profile, journalEvent("agent-event-"+id, "agent.created", f.ownerPrincipal, f.organizationID)); err != nil {
			t.Fatal(err)
		}
	}
	s, err := controlplane.NewExternalSessionService(r, nil, func() time.Time { return repositoryTestTime })
	if err != nil {
		t.Fatal(err)
	}
	return r, f, s
}
func externalBind(t *testing.T, s *controlplane.ExternalSessionService, owner, agent string, peers ...string) (*domain.ExternalSessionBinding, string) {
	t.Helper()
	b, secret, err := s.Bind(context.Background(), owner, domain.BindExternalSessionInput{AgentID: agent, HostID: "desktop-local", ThreadID: "thread-" + agent, AllowedPeerAgentIDs: peers})
	if err != nil {
		t.Fatal(err)
	}
	return b, secret
}
func externalCount(t *testing.T, r *Repository, table string) int {
	t.Helper()
	var n int
	if err := r.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestExternalMessagesPersistIdentityIdempotencyAndReplyWithoutLoop(t *testing.T) {
	r, f, s := externalFixture(t)
	ctx := context.Background()
	_, quote := externalBind(t, s, f.ownerPrincipal, f.agentID, "pay")
	_, pay := externalBind(t, s, f.ownerPrincipal, "pay", f.agentID)
	_, other := externalBind(t, s, f.ownerPrincipal, "other", "pay")
	in := domain.SendExternalMessageInput{TargetAgentID: "pay", Kind: domain.ExternalMessageConsultation, Content: "What contract is available?", IdempotencyKey: "consult-1"}
	m, err := s.Send(ctx, quote, in)
	if err != nil {
		t.Fatal(err)
	}
	if m.SenderAgentID != f.agentID || m.SenderGeneration != 1 || m.DeliveryState != "pending" {
		t.Fatalf("wrong message: %+v", m)
	}
	before := externalCount(t, r, "event_journal")
	replay, err := s.Send(ctx, quote, in)
	if err != nil || replay.ID != m.ID || externalCount(t, r, "event_journal") != before {
		t.Fatalf("idempotent send err=%v", err)
	}
	in.Content = "different"
	if _, err = s.Send(ctx, quote, in); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("changed key err=%v", err)
	}
	if _, err = s.Ack(ctx, quote, m.ID); err == nil {
		t.Fatal("sender acknowledged recipient inbox")
	}
	if _, err = s.Get(ctx, other, m.ID); err == nil {
		t.Fatal("unrelated agent read message")
	}
	inbox, err := s.Inbox(ctx, pay, 0, 100)
	if err != nil || len(inbox) != 1 || inbox[0].DeliveryState != "pending" {
		t.Fatalf("inbox=%v err=%v", inbox, err)
	}
	acked, err := s.Ack(ctx, pay, m.ID)
	if err != nil || acked.AcknowledgedAt == nil {
		t.Fatalf("ack err=%v", err)
	}
	before = externalCount(t, r, "event_journal")
	if _, err = s.Ack(ctx, pay, m.ID); err != nil || externalCount(t, r, "event_journal") != before {
		t.Fatalf("ack replay err=%v", err)
	}
	reply := domain.SendExternalMessageInput{Kind: domain.ExternalMessageResult, ReplyToMessageID: m.ID, Content: "Contract v1 is ready; source evidence attached.", IdempotencyKey: "result-1"}
	result, err := s.Send(ctx, pay, reply)
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetAgentID != f.agentID || result.ReplyToMessageID != m.ID {
		t.Fatal("result did not route to requester")
	}
	reply.IdempotencyKey = "duplicate-result"
	if _, err = s.Send(ctx, pay, reply); err == nil {
		t.Fatal("second terminal result accepted")
	}
	reply.ReplyToMessageID = result.ID
	reply.IdempotencyKey = "loop"
	if _, err = s.Send(ctx, quote, reply); err == nil {
		t.Fatal("result reply loop accepted")
	}
	if externalCount(t, r, "tasks") != 0 || externalCount(t, r, "run_attempts") != 0 || externalCount(t, r, "mailbox_items") != 0 {
		t.Fatal("communication created managed execution")
	}
	var principal string
	if err = r.db.QueryRow(`SELECT actor_principal_id FROM event_journal WHERE aggregate_id=? AND event_type='external_message.sent'`, m.ID).Scan(&principal); err != nil || principal != f.agentPrincipal {
		t.Fatalf("wrong journal actor %s err=%v", principal, err)
	}
	rows, err := s.Inbox(ctx, quote, m.Sequence, 100)
	if err != nil || len(rows) != 1 || rows[0].ID != result.ID {
		t.Fatalf("reply inbox err=%v rows=%v", err, rows)
	}
}

func TestExternalBindingRejectsStrandedTasksAndManagedDispatch(t *testing.T) {
	ctx := context.Background()
	t.Run("queued-before-bind", func(t *testing.T) {
		r, f, s := externalFixture(t)
		createTask(t, r, f, "queued-before-external")
		before := externalCount(t, r, "event_journal")
		_, _, err := s.Bind(ctx, f.ownerPrincipal, domain.BindExternalSessionInput{AgentID: f.agentID, HostID: "desktop-local", ThreadID: "thread-quote", AllowedPeerAgentIDs: []string{"pay"}})
		if err == nil || externalCount(t, r, "external_session_bindings") != 0 || externalCount(t, r, "event_journal") != before {
			t.Fatal("binding stranded a queued task")
		}
	})
	t.Run("managed-dispatch-after-bind", func(t *testing.T) {
		r, f, s := externalFixture(t)
		externalBind(t, s, f.ownerPrincipal, f.agentID, "pay")
		before := nativeTaskCounts(t, r)
		task, msg, mail, event := newTaskDelivery(f, "blocked-external-dispatch")
		_, err := r.CreateTask(ctx, task, msg, mail, event)
		nativeTaskErrorCode(t, err, "CONFLICT")
		nativeTaskUnchanged(t, r, before)
		_, err = nativeTaskCommand(t, r).CreateTask(ctx, f.ownerPrincipal, nativeTaskRequest(f, "blocked-native", "thread-quote"))
		nativeTaskErrorCode(t, err, "CONFLICT")
		nativeTaskUnchanged(t, r, before)
	})
}

func TestExternalReopenPreservesPendingMessageAndCredential(t *testing.T) {
	r, f, s := externalFixture(t)
	ctx := context.Background()
	_, quote := externalBind(t, s, f.ownerPrincipal, f.agentID, "pay")
	_, pay := externalBind(t, s, f.ownerPrincipal, "pay", f.agentID)
	m, err := s.Send(ctx, quote, domain.SendExternalMessageInput{TargetAgentID: "pay", Kind: domain.ExternalMessageRequest, Content: "inspect only", IdempotencyKey: "restart"})
	if err != nil {
		t.Fatal(err)
	}
	var sequence int
	var name, path string
	if err = r.db.QueryRow("PRAGMA database_list").Scan(&sequence, &name, &path); err != nil {
		t.Fatal(err)
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path, Options{Now: func() time.Time { return repositoryTestTime.Add(time.Minute) }})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	s, err = controlplane.NewExternalSessionService(reopened, nil, func() time.Time { return repositoryTestTime.Add(time.Minute) })
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, pay, m.ID)
	if err != nil || got.DeliveryState != "pending" || got.Content != m.Content {
		t.Fatalf("restart lost durable state: %v", err)
	}
}

func TestExternalCredentialRotationDisabledIdentityAndOwnerBoundary(t *testing.T) {
	r, f, s := externalFixture(t)
	ctx := context.Background()
	b, token := externalBind(t, s, f.ownerPrincipal, f.agentID, "pay")
	encoded, _ := json.Marshal(b)
	if strings.Contains(string(encoded), b.TokenDigest) || strings.Contains(string(encoded), token) {
		t.Fatal("credential in JSON metadata")
	}
	if _, err := s.Status(ctx, "bad"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("bad credential err=%v", err)
	}
	in := domain.BindExternalSessionInput{AgentID: f.agentID, HostID: b.HostID, ThreadID: b.ThreadID, AllowedPeerAgentIDs: []string{"pay"}, ExpectedGeneration: 1}
	if _, _, err := s.Bind(ctx, f.agentPrincipal, in); err == nil {
		t.Fatal("agent allowed to bind as owner")
	}
	rotated, newToken, err := s.Bind(ctx, f.ownerPrincipal, in)
	if err != nil || rotated.Generation != 2 {
		t.Fatalf("rotate err=%v", err)
	}
	if _, err = s.Status(ctx, token); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatal("old token survived rotation")
	}
	if _, _, err = s.Bind(ctx, f.ownerPrincipal, in); !errors.Is(err, domain.ErrStaleVersion) {
		t.Fatalf("stale rotate err=%v", err)
	}
	if _, err = r.db.Exec(`UPDATE principals SET status='disabled' WHERE principal_id=?`, f.agentPrincipal); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Status(ctx, newToken); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatal("disabled identity authenticated")
	}
	if _, err = r.db.Exec(`UPDATE principals SET status='active' WHERE principal_id=?`, f.agentPrincipal); err != nil {
		t.Fatal(err)
	}
	if err = s.Revoke(ctx, f.ownerPrincipal, f.agentID, 2); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Status(ctx, newToken); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatal("revoked token authenticated")
	}
}

func TestExternalFailedRotationKeepsPriorCredentialAndExpiryRejects(t *testing.T) {
	r, f, s := externalFixture(t)
	ctx := context.Background()
	b, token := externalBind(t, s, f.ownerPrincipal, f.agentID, "pay")
	before := externalCount(t, r, "event_journal")
	r.faultInjector = func(point FaultPoint) error {
		if point == FaultBeforeCommit {
			return errors.New("rotation rollback")
		}
		return nil
	}
	input := domain.BindExternalSessionInput{AgentID: f.agentID, HostID: b.HostID, ThreadID: b.ThreadID, AllowedPeerAgentIDs: []string{"pay"}, ExpectedGeneration: 1}
	if _, secret, err := s.Bind(ctx, f.ownerPrincipal, input); err == nil || secret != "" {
		t.Fatal("failed bind returned secret")
	}
	r.faultInjector = nil
	got, err := s.Status(ctx, token)
	if err != nil || got.Generation != 1 || externalCount(t, r, "event_journal") != before {
		t.Fatal("failed rotation revoked old credential")
	}
	input.ThreadID += " "
	if _, _, err = s.Bind(ctx, f.ownerPrincipal, input); err == nil {
		t.Fatal("whitespace thread alias accepted")
	}
	r.now = func() time.Time { return repositoryTestTime.Add(31 * 24 * time.Hour) }
	if _, err = s.Status(ctx, token); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatal("expired credential authenticated")
	}
}

func TestExternalCommandsRollbackJournalAndConcurrentIdempotency(t *testing.T) {
	r, f, s := externalFixture(t)
	ctx := context.Background()
	_, quote := externalBind(t, s, f.ownerPrincipal, f.agentID, "pay")
	_, pay := externalBind(t, s, f.ownerPrincipal, "pay", f.agentID)
	in := domain.SendExternalMessageInput{TargetAgentID: "pay", Kind: domain.ExternalMessageRequest, Content: "Read the interface only", IdempotencyKey: "atomic"}
	for _, point := range []FaultPoint{FaultAfterStateWrite, FaultBeforeCommit} {
		before := externalCount(t, r, "event_journal")
		r.faultInjector = func(p FaultPoint) error {
			if p == point {
				return errors.New("injected external failure")
			}
			return nil
		}
		if _, err := s.Send(ctx, quote, in); err == nil {
			t.Fatal("faulted send succeeded")
		}
		if externalCount(t, r, "external_messages") != 0 || externalCount(t, r, "event_journal") != before {
			t.Fatal("partial message/journal committed")
		}
	}
	r.faultInjector = nil
	const writers = 8
	var wg sync.WaitGroup
	ids := make(chan string, writers)
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, err := s.Send(ctx, quote, in)
			if err != nil {
				errs <- err
				return
			}
			ids <- m.ID
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	id := ""
	for got := range ids {
		if id != "" && id != got {
			t.Fatal("duplicate messages")
		}
		id = got
	}
	if externalCount(t, r, "external_messages") != 1 {
		t.Fatal("wrong message count")
	}
	before := externalCount(t, r, "event_journal")
	r.faultInjector = func(p FaultPoint) error {
		if p == FaultBeforeCommit {
			return errors.New("ack failed")
		}
		return nil
	}
	if _, err := s.Ack(ctx, pay, id); err == nil {
		t.Fatal("faulted ack succeeded")
	}
	r.faultInjector = nil
	m, err := s.Get(ctx, pay, id)
	if err != nil || m.DeliveryState != "pending" || externalCount(t, r, "event_journal") != before {
		t.Fatal("partial acknowledgement")
	}
}

func TestExternalAndManagedSessionsExcludeEachOtherInDatabase(t *testing.T) {
	for _, managedFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(managedFirst), func(t *testing.T) {
			r, f, s := externalFixture(t)
			ctx := context.Background()
			binding := &domain.SessionBinding{ID: "managed-binding", ContextID: "managed-task", AgentID: "pay", BackendID: "codex", ProviderSessionID: "thread-" + f.agentID, State: domain.SessionBindingActive, Version: 1}
			if managedFirst {
				if err := r.CreateSessionBinding(ctx, binding, journalEvent("managed-event", "session_binding.created", f.ownerPrincipal, f.organizationID)); err != nil {
					t.Fatal(err)
				}
				if _, _, err := s.Bind(ctx, f.ownerPrincipal, domain.BindExternalSessionInput{AgentID: f.agentID, HostID: "desktop-local", ThreadID: binding.ProviderSessionID, AllowedPeerAgentIDs: []string{"pay"}}); err == nil {
					t.Fatal("external collided with managed thread")
				}
			} else {
				externalBind(t, s, f.ownerPrincipal, f.agentID, "pay")
				if err := r.CreateSessionBinding(ctx, binding, journalEvent("managed-event", "session_binding.created", f.ownerPrincipal, f.organizationID)); err == nil {
					t.Fatal("managed collided with external thread")
				}
				if _, _, err := s.Bind(ctx, f.ownerPrincipal, domain.BindExternalSessionInput{AgentID: "pay", HostID: "desktop-local", ThreadID: binding.ProviderSessionID, AllowedPeerAgentIDs: []string{f.agentID}}); err == nil {
					t.Fatal("same host/thread bound twice")
				}
				now := formatTime(repositoryTestTime)
				_, err := r.db.Exec(`INSERT INTO worker_instances(worker_instance_id,agent_id,generation,transport,authenticated_principal,capabilities_json,status,session_token_digest,session_token_expires_at,last_heartbeat_at,lease_until,fencing_token,started_at,updated_at) VALUES('blocked-worker',?,1,'local','human-owner','[]','online','test-digest',?,?,?,1,?,?)`, f.agentID, now, now, now, now, now)
				if err == nil || !strings.Contains(err.Error(), "conflicts with external session") {
					t.Fatalf("worker guard err=%v", err)
				}
			}
		})
	}
}
