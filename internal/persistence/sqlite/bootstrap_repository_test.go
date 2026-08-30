package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	webAuth "openagentx/internal/auth/web"
	"openagentx/internal/domain"
)

func TestInitializeAndApplyAgentAreAtomicAndIdempotent(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	ctx := context.Background()
	digest, err := webAuth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	owner := &domain.Principal{ID: "human-owner-bootstrap", Kind: domain.PrincipalHuman, DisplayName: "Owner", Status: domain.IdentityActive}
	daemon := &domain.Principal{ID: "daemon", Kind: domain.PrincipalSystem, DisplayName: "OpenAgentX daemon", Status: domain.IdentityActive}
	organization := &domain.Organization{ID: "default", Name: "Default Organization", Status: domain.IdentityActive}
	user := &domain.WebUserRecord{ID: "web-owner", PrincipalID: owner.ID, Username: "owner", PasswordDigest: digest, Roles: []domain.WebRole{domain.WebRoleOwner}, Status: domain.IdentityActive}
	created, err := repository.Initialize(ctx, owner, daemon, organization, user, bootstrapEvents("first", owner.ID, organization.ID, user.ID))
	if err != nil || !created {
		t.Fatalf("initialize created=%v err=%v", created, err)
	}
	created, err = repository.Initialize(ctx, owner, daemon, organization, user, bootstrapEvents("replay", owner.ID, organization.ID, user.ID))
	if err != nil || created {
		t.Fatalf("repeat initialize created=%v err=%v", created, err)
	}
	loadedUser, err := repository.GetWebUserByUsername(ctx, "owner")
	if err != nil || loadedUser.PrincipalID != owner.ID || !webAuth.VerifyPassword(loadedUser.PasswordDigest, "correct horse battery staple") {
		t.Fatalf("loaded web user=%+v err=%v", loadedUser, err)
	}

	principal := &domain.Principal{ID: "agent-test", Kind: domain.PrincipalAgent, DisplayName: "Test Agent", Status: domain.IdentityActive}
	agent := &domain.AgentIdentity{ID: "test-agent", PrincipalID: principal.ID, OrganizationID: organization.ID, DisplayName: "Test Agent", Status: domain.AgentIdentityActive, Version: 1}
	profile := &domain.AgentProfileRecord{AgentID: agent.ID, Version: 1, InstructionsPath: "/roles/test.md", WorkspaceRoot: "/workspace", Capabilities: []string{"testing", "coding"}}
	created, err = repository.ApplyAgent(ctx, principal, agent, profile, agentEvents("first", owner.ID, organization.ID, principal.ID, agent.ID))
	if err != nil || !created {
		t.Fatalf("apply Agent created=%v err=%v", created, err)
	}
	created, err = repository.ApplyAgent(ctx, principal, agent, profile, agentEvents("replay", owner.ID, organization.ID, principal.ID, agent.ID))
	if err != nil || created {
		t.Fatalf("repeat apply Agent created=%v err=%v", created, err)
	}
	events, err := repository.ListJournal(ctx, 0, 100)
	if err != nil || len(events) != 6 {
		t.Fatalf("journal count=%d err=%v", len(events), err)
	}
	for _, event := range events {
		if strings.Contains(string(event.Payload), digest) || strings.Contains(string(event.Payload), "correct horse") {
			t.Fatal("password material leaked into Event Journal")
		}
	}
}

func TestInitializeRollsBackAllIdentityStateOnFailure(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	injected := errors.New("stop before commit")
	failing := newRepository(repository.db, Options{Now: repository.now, FaultInjector: func(point FaultPoint) error {
		if point == FaultBeforeCommit {
			return injected
		}
		return nil
	}})
	digest, err := webAuth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	owner := &domain.Principal{ID: "human-owner-rollback", Kind: domain.PrincipalHuman, DisplayName: "Owner", Status: domain.IdentityActive}
	daemon := &domain.Principal{ID: "daemon", Kind: domain.PrincipalSystem, DisplayName: "OpenAgentX daemon", Status: domain.IdentityActive}
	organization := &domain.Organization{ID: "default", Name: "Default", Status: domain.IdentityActive}
	user := &domain.WebUserRecord{ID: "web-owner-rollback", PrincipalID: owner.ID, Username: "owner", PasswordDigest: digest, Roles: []domain.WebRole{domain.WebRoleOwner}, Status: domain.IdentityActive}
	if _, err := failing.Initialize(context.Background(), owner, daemon, organization, user, bootstrapEvents("rollback", owner.ID, organization.ID, user.ID)); !errors.Is(err, injected) {
		t.Fatalf("initialize error=%v", err)
	}
	initialized, err := repository.IsInitialized(context.Background())
	if err != nil || initialized {
		t.Fatalf("initialized=%v err=%v after rollback", initialized, err)
	}
	var principals int
	if err := repository.db.QueryRow(`SELECT COUNT(*) FROM principals`).Scan(&principals); err != nil || principals != 0 {
		t.Fatalf("principal count=%d err=%v after rollback", principals, err)
	}
}

func TestWebSessionPersistsAcrossRepositoryAndManagerRestart(t *testing.T) {
	repository, databasePath := openTestRepository(t, nil)
	ctx := context.Background()
	digest, err := webAuth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	owner := &domain.Principal{ID: "human-owner-session", Kind: domain.PrincipalHuman, DisplayName: "Owner", Status: domain.IdentityActive}
	daemon := &domain.Principal{ID: "daemon", Kind: domain.PrincipalSystem, DisplayName: "OpenAgentX daemon", Status: domain.IdentityActive}
	organization := &domain.Organization{ID: "default", Name: "Default", Status: domain.IdentityActive}
	user := &domain.WebUserRecord{ID: "web-owner-session", PrincipalID: owner.ID, Username: "owner", PasswordDigest: digest, Roles: []domain.WebRole{domain.WebRoleOwner}, Status: domain.IdentityActive}
	if created, err := repository.Initialize(ctx, owner, daemon, organization, user, bootstrapEvents("session", owner.ID, organization.ID, user.ID)); err != nil || !created {
		t.Fatalf("initialize session fixture created=%v err=%v", created, err)
	}
	manager := webAuth.NewManager(webAuth.Config{Store: repository})
	if err := manager.AddUser(webAuth.User{ID: owner.ID, WebUserID: user.ID, Username: user.Username, Roles: []webAuth.Role{webAuth.RoleOwner}, PasswordDigest: digest}); err != nil {
		t.Fatal(err)
	}
	session, err := manager.Login("owner", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	webAuth.SetSessionCookie(response, session)
	request := httptest.NewRequest("GET", "/", nil)
	request.AddCookie(response.Result().Cookies()[0])
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, databasePath, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	replacement := webAuth.NewManager(webAuth.Config{Store: reopened})
	if err := replacement.AddUser(webAuth.User{ID: owner.ID, WebUserID: user.ID, Username: user.Username, Roles: []webAuth.Role{webAuth.RoleOwner}, PasswordDigest: digest}); err != nil {
		t.Fatal(err)
	}
	restored, err := replacement.Authenticate(request)
	if err != nil || webAuth.ValidateCSRF(restored, session.CSRFToken) != nil {
		t.Fatalf("restored Session=%+v err=%v", restored, err)
	}
	if err := replacement.RefreshCSRF(ctx, restored); err != nil {
		t.Fatal(err)
	}
	record, err := reopened.GetWebSession(ctx, restored.IDDigest)
	if err != nil || record.SessionDigest == "" || record.CSRFDigest == "" || strings.Contains(record.CSRFDigest, restored.CSRFToken) {
		t.Fatalf("persisted Session record=%+v err=%v", record, err)
	}
}

func bootstrapEvents(suffix, actor, organization, webUserID string) []*domain.JournalEvent {
	return []*domain.JournalEvent{
		{ID: "event-owner-" + suffix, EventType: "principal.created", ActorPrincipalID: actor, Payload: json.RawMessage(`{}`)},
		{ID: "event-daemon-" + suffix, EventType: "principal.created", ActorPrincipalID: actor, Payload: json.RawMessage(`{}`)},
		{ID: "event-org-" + suffix, OrganizationID: organization, EventType: "organization.created", ActorPrincipalID: actor, Payload: json.RawMessage(`{}`)},
		{ID: "event-web-" + suffix, OrganizationID: organization, EventType: "web_user.created", ActorPrincipalID: actor, Payload: json.RawMessage(`{}`), AggregateID: webUserID},
	}
}

func agentEvents(suffix, actor, organization, principalID, agentID string) []*domain.JournalEvent {
	return []*domain.JournalEvent{
		{ID: "event-agent-principal-" + suffix, OrganizationID: organization, EventType: "principal.created", ActorPrincipalID: actor, Payload: json.RawMessage(`{}`), AggregateID: principalID},
		{ID: "event-agent-" + suffix, OrganizationID: organization, EventType: "agent.created", ActorPrincipalID: actor, Payload: json.RawMessage(`{}`), AggregateID: agentID},
	}
}
