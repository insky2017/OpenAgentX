package panel

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	openapi "openagentx/internal/api"
	"openagentx/internal/auth/web"
	"openagentx/internal/controlplane"
	"openagentx/internal/domain"
	"openagentx/internal/persistence/sqlite"
	openruntime "openagentx/internal/runtime"
	"openagentx/internal/runtime/descriptors"
)

func panelEvent(id, typ, aggregate, aggregateID, actor string) *domain.JournalEvent {
	return &domain.JournalEvent{ID: id, EventType: typ, AggregateType: aggregate, AggregateID: aggregateID, ActorPrincipalID: actor, OrganizationID: "org-main", Payload: json.RawMessage(`{}`)}
}

type panelIntegrationFixture struct {
	repository  *sqlite.Repository
	worker      *domain.WorkerInstance
	guard       domain.WorkerWriteGuard
	workerSvc   *controlplane.WorkerService
	workerToken string
	task        *domain.Task
	run         *domain.RunAttempt
	approval    *domain.ApprovalRequest
	manager     *web.Manager
	cookie      *http.Cookie
	session     *web.Session
}

func newPanelIntegrationFixture(t *testing.T, withApproval bool) panelIntegrationFixture {
	t.Helper()
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	repository, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "panel.db"), sqlite.Options{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	journal := func(id, typ, aggregate, aggregateID string) *domain.JournalEvent {
		return &domain.JournalEvent{ID: id, EventType: typ, AggregateType: aggregate, AggregateID: aggregateID, ActorPrincipalID: "human-owner", Payload: json.RawMessage(`{}`)}
	}
	if err := repository.CreatePrincipal(context.Background(), &domain.Principal{ID: "human-owner", Kind: domain.PrincipalHuman, DisplayName: "Owner", Status: domain.IdentityActive}, journal("principal", "principal.created", "principal", "human-owner")); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreatePrincipal(context.Background(), &domain.Principal{ID: "agent-principal", Kind: domain.PrincipalAgent, DisplayName: "Agent", Status: domain.IdentityActive}, journal("agent-principal", "principal.created", "principal", "agent-principal")); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateOrganization(context.Background(), &domain.Organization{ID: "org-main", Name: "Main", Status: domain.IdentityActive}, journal("org", "organization.created", "organization", "org-main")); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateAgent(context.Background(), &domain.AgentIdentity{ID: "quote", PrincipalID: "agent-principal", OrganizationID: "org-main", DisplayName: "Quote", Status: domain.AgentIdentityActive, Version: 1}, &domain.AgentProfileRecord{AgentID: "quote", Version: 1, InstructionsPath: "/tmp/ROLE.md", WorkspaceRoot: "/tmp", Capabilities: []string{"coding"}}, journal("agent", "agent.created", "agent", "quote")); err != nil {
		t.Fatal(err)
	}
	descriptor := descriptors.CodexACP()
	descriptor.Steer = openruntime.SteerNative
	workerToken := "panel-worker-token-012345678901234567890123"
	workerDigest := sha256.Sum256([]byte(workerToken))
	registration := domain.WorkerRegistration{WorkerInstanceID: "worker-panel", AgentID: "quote", Transport: domain.WorkerTransportUnix, PrincipalID: "agent-principal", Capabilities: []string{"coding"}, SessionTokenDigest: hex.EncodeToString(workerDigest[:]), TokenExpiresAt: now.Add(time.Hour), LeaseUntil: now.Add(time.Hour)}
	worker, err := repository.RegisterWorker(context.Background(), registration, []openruntime.BackendRegistration{{BackendID: "local", Descriptor: descriptor, Health: openruntime.BackendHealthy}}, journal("worker", "worker.registered", "worker_instance", registration.WorkerInstanceID))
	if err != nil {
		t.Fatal(err)
	}
	guard := domain.WorkerWriteGuard{WorkerInstanceID: worker.ID, AgentID: worker.AgentID, PrincipalID: worker.AuthenticatedPrincipal, SessionTokenDigest: registration.SessionTokenDigest, Generation: worker.Generation, FencingToken: worker.FencingToken, CheckedAt: now}
	if _, err := repository.HeartbeatWorker(context.Background(), guard, domain.WorkerStatusOnline, nil, now.Add(time.Hour), now.Add(time.Hour), journal("worker-online", "worker.heartbeat", "worker_instance", worker.ID)); err != nil {
		t.Fatal(err)
	}
	task := &domain.Task{ID: "task-panel", SenderPrincipalID: "human-owner", TargetAgentID: "quote", OrganizationID: "org-main", DispatchMode: domain.DispatchModeDirect, IdempotencyKey: "panel-task", Content: "panel integration"}
	message := &domain.Message{ID: "message-panel", TaskID: task.ID, SenderPrincipalID: "human-owner", TargetAgentID: "quote", Kind: domain.MessageKindInstruction, Content: task.Content}
	item := &domain.MailboxItem{ID: "mailbox-panel", TargetAgentID: "quote", Kind: domain.MailboxKindTask, Lane: domain.MailboxLaneWork, TaskID: task.ID, State: domain.MailboxStatePending, CreatedAt: now}
	created, err := repository.CreateTask(context.Background(), task, message, item, journal("task-created", "task.created", "task", task.ID))
	if err != nil {
		t.Fatal(err)
	}
	run := &domain.RunAttempt{ID: "run-panel", TaskID: created.Task.ID, AgentID: "quote", Version: 1, Status: domain.RunAttemptRunning, WorkerInstanceID: worker.ID, FencingToken: worker.FencingToken, LeaseUntil: now.Add(time.Hour), ExecutionSpecVersion: 1, RequestedExecutionJSON: `{}`, ResolvedExecutionJSON: `{}`, AdapterID: descriptor.AdapterID, BackendID: "local", Model: descriptor.Models[0], ReasoningMode: domain.ReasoningBackendDefault}
	if _, err := repository.BeginRunAttempt(context.Background(), created.Task.Version, run, journal("task-running", "task.running", "task", task.ID), journal("run-started", "run_attempt.started", "run_attempt", run.ID)); err != nil {
		t.Fatal(err)
	}
	workerSvc, err := controlplane.NewWorkerService(repository, nil, controlplane.WorkerServiceOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	var approval *domain.ApprovalRequest
	if withApproval {
		batch := openapi.EventBatch{WorkerInstanceID: worker.ID, Generation: worker.Generation, FencingToken: worker.FencingToken, ExpectedRunVersion: run.Version, Events: []openruntime.RuntimeEvent{{Type: "approval.requested", Payload: json.RawMessage(`{"approval_request_id":"approval-panel","scope_digest":"panel-scope","expires_at":"2026-08-30T13:00:00Z"}`), OccurredAt: now}}}
		if err := workerSvc.AppendEvents(context.Background(), "agent-principal", workerToken, run.ID, batch); err != nil {
			t.Fatal(err)
		}
		approval, err = repository.GetApprovalRequest(context.Background(), "approval-panel")
		if err != nil {
			t.Fatal(err)
		}
	}
	digest, err := web.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	manager := web.NewManager(web.Config{})
	if err := manager.AddUser(web.User{ID: "human-owner", WebUserID: "web-owner", Username: "owner", Roles: []web.Role{web.RoleOwner}, PasswordDigest: digest}); err != nil {
		t.Fatal(err)
	}
	session, err := manager.Login("owner", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	cookieResponse := httptest.NewRecorder()
	web.SetSessionCookie(cookieResponse, session)
	return panelIntegrationFixture{repository: repository, worker: worker, guard: guard, workerSvc: workerSvc, workerToken: workerToken, task: &created.Task, run: run, approval: approval, manager: manager, cookie: cookieResponse.Result().Cookies()[0], session: session}
}

func panelIntegrationHandler(t *testing.T, fixture panelIntegrationFixture) *Handler {
	commands, err := controlplane.NewCommandService(fixture.repository, nil, func() time.Time { return time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(fixture.repository, commands, fixture.manager)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func panelPOST(t *testing.T, client *http.Client, url string, cookie *http.Cookie, csrf, idem string, body any, out any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	req.Header.Set("Idempotency-Key", idem)
	req.AddCookie(cookie)
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if out != nil {
		defer response.Body.Close()
		if err := json.NewDecoder(response.Body).Decode(out); err != nil {
			t.Fatalf("decode response status=%d: %v", response.StatusCode, err)
		}
	}
	return response
}

func assertJournalTypes(t *testing.T, repository *sqlite.Repository, want ...string) {
	t.Helper()
	events, err := repository.ListJournal(context.Background(), 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, event := range events {
		seen[event.EventType] = true
	}
	for _, typ := range want {
		if !seen[typ] {
			t.Fatalf("journal missing event type %q", typ)
		}
	}
}

func TestPanelHTTPCancelSuccessTraversesRepositoryAndWorkerSettlement(t *testing.T) {
	fixture := newPanelIntegrationFixture(t, false)
	server := httptest.NewServer(panelIntegrationHandler(t, fixture))
	defer server.Close()
	client, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	httpClient := &http.Client{Jar: client}
	var response openapi.CancelTaskResponse
	idem := "panel-cancel-idem"
	httpResponse := panelPOST(t, httpClient, server.URL+"/api/control/v1/tasks/"+fixture.task.ID+"/cancel", fixture.cookie, fixture.session.CSRFToken, idem,
		openapi.CancelTaskRequest{Meta: openapi.CommandMeta{IdempotencyKey: idem, ExpectedVersion: 2}, RequestedBy: "human-owner"}, &response)
	if httpResponse.StatusCode != http.StatusOK || response.Task.Status != domain.TaskStatusCancelRequested || response.Sequence <= 0 {
		t.Fatalf("cancel status=%d response=%+v", httpResponse.StatusCode, response)
	}
	assertJournalTypes(t, fixture.repository, "task.cancel_requested", "mailbox.cancel_created")
	claimed, err := fixture.repository.TryClaimMailbox(context.Background(), fixture.guard, 0, time.Date(2026, 8, 30, 13, 0, 0, 0, time.UTC), panelEvent("claim-panel-cancel", "mailbox.claimed", "mailbox_item", "", "agent-principal"))
	if err != nil || claimed == nil || claimed.Kind != domain.MailboxKindCancel || claimed.TargetRunID != fixture.run.ID {
		t.Fatalf("cancel claim=%+v err=%v", claimed, err)
	}
	if err := fixture.repository.FinishRun(context.Background(), fixture.guard, fixture.run.ID, 2, fixture.run.Version,
		openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "late", SideEffectsKnown: true}, nil, 0, nil,
		panelEvent("finish-panel-task", "task.canceled", "task", fixture.task.ID, "agent-principal"),
		panelEvent("finish-panel-run", "run_attempt.finished", "run_attempt", fixture.run.ID, "agent-principal")); err != nil {
		t.Fatal(err)
	}
	if err := fixture.repository.AcceptMailboxItem(context.Background(), fixture.guard, claimed.ID, domain.MailboxStateClaimed, domain.MailboxStateAccepted,
		panelEvent("accept-panel-cancel", "mailbox.accepted", "mailbox_item", claimed.ID, "agent-principal")); err != nil {
		t.Fatal(err)
	}
	task, err := fixture.repository.GetTask(context.Background(), fixture.task.ID)
	if err != nil || task.Status != domain.TaskStatusCanceled {
		t.Fatalf("settled cancel task=%+v err=%v", task, err)
	}
	mailbox, err := fixture.repository.GetMailboxItem(context.Background(), claimed.ID)
	if err != nil || mailbox.State != domain.MailboxStateAccepted {
		t.Fatalf("settled cancel mailbox=%+v err=%v", mailbox, err)
	}
	assertJournalTypes(t, fixture.repository, "mailbox.claimed", "run_attempt.finished", "mailbox.accepted")
}

func TestPanelHTTPNativeApprovalSuccessTraversesPayloadAndSettlement(t *testing.T) {
	fixture := newPanelIntegrationFixture(t, true)
	server := httptest.NewServer(panelIntegrationHandler(t, fixture))
	defer server.Close()
	client, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	httpClient := &http.Client{Jar: client}
	var response openapi.DecideApprovalResponse
	idem := "panel-approval-idem"
	httpResponse := panelPOST(t, httpClient, server.URL+"/api/control/v1/approvals/"+fixture.approval.ID+"/decisions", fixture.cookie, fixture.session.CSRFToken, idem,
		openapi.DecideApprovalRequest{Meta: openapi.CommandMeta{IdempotencyKey: idem, ExpectedVersion: 2}, DecidedBy: "human-owner", Decision: domain.ApprovalDecisionApprove}, &response)
	if httpResponse.StatusCode != http.StatusOK || response.Decision.ID == "" || response.Sequence <= 0 {
		t.Fatalf("approval status=%d response=%+v", httpResponse.StatusCode, response)
	}
	assertJournalTypes(t, fixture.repository, "approval.decided", "mailbox.approval_created")
	claimed, err := fixture.repository.TryClaimMailbox(context.Background(), fixture.guard, 0, time.Date(2026, 8, 30, 13, 0, 0, 0, time.UTC), panelEvent("claim-panel-approval", "mailbox.claimed", "mailbox_item", "", "agent-principal"))
	if err != nil || claimed == nil || claimed.Kind != domain.MailboxKindApproval || claimed.TargetRunID != fixture.run.ID {
		t.Fatalf("approval claim=%+v err=%v", claimed, err)
	}
	payload, err := fixture.repository.ResolveMailboxPayload(context.Background(), fixture.guard, claimed.ID)
	if err != nil || payload.ApprovalDecision == nil || payload.ApprovalDecision.ID != response.Decision.ID {
		t.Fatalf("approval payload=%+v err=%v", payload, err)
	}
	if err := fixture.repository.AcceptMailboxItem(context.Background(), fixture.guard, claimed.ID, domain.MailboxStateClaimed, domain.MailboxStateAccepted,
		panelEvent("accept-panel-approval", "mailbox.accepted", "mailbox_item", claimed.ID, "agent-principal")); err != nil {
		t.Fatal(err)
	}
	if err := fixture.repository.FinishRun(context.Background(), fixture.guard, fixture.run.ID, 2, fixture.run.Version,
		openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "approved", SideEffectsKnown: true}, nil, 0, nil,
		panelEvent("finish-approval-task", "task.succeeded", "task", fixture.task.ID, "agent-principal"),
		panelEvent("finish-approval-run", "run_attempt.finished", "run_attempt", fixture.run.ID, "agent-principal")); err != nil {
		t.Fatal(err)
	}
	decision, err := fixture.repository.GetApprovalDecision(context.Background(), response.Decision.ID)
	if err != nil || decision.State != domain.ApprovalDecisionApplied {
		t.Fatalf("approval decision=%+v err=%v", decision, err)
	}
	task, err := fixture.repository.GetTask(context.Background(), fixture.task.ID)
	if err != nil || task.Status != domain.TaskStatusSucceeded {
		t.Fatalf("approval settled task=%+v err=%v", task, err)
	}
	assertJournalTypes(t, fixture.repository, "mailbox.claimed", "mailbox.accepted", "run_attempt.finished")
}
