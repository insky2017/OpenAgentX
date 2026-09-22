package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	"openagentx/internal/auth/web"
	"openagentx/internal/controlplane"
	"openagentx/internal/domain"
	"openagentx/internal/persistence/sqlite"
)

func newIntentPanel(t *testing.T) (authenticatedPanel, *sqlite.Repository) {
	t.Helper()
	ctx := context.Background()
	repository, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "intent.db"), sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	event := func(id, typ string) *domain.JournalEvent {
		return &domain.JournalEvent{ID: id, EventType: typ, ActorPrincipalID: "human-owner", Payload: json.RawMessage(`{}`)}
	}
	for _, principal := range []domain.Principal{
		{ID: "human-owner", Kind: domain.PrincipalHuman, DisplayName: "Owner", Status: domain.IdentityActive},
		{ID: "agent-principal", Kind: domain.PrincipalAgent, DisplayName: "Quote", Status: domain.IdentityActive},
	} {
		if err := repository.CreatePrincipal(ctx, &principal, event("event-"+principal.ID, "principal.created")); err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.CreateOrganization(ctx, &domain.Organization{ID: "org-main", Name: "Main", Status: domain.IdentityActive}, event("event-org", "organization.created")); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateAgent(ctx,
		&domain.AgentIdentity{ID: "quote", PrincipalID: "agent-principal", OrganizationID: "org-main", DisplayName: "Quote", Status: domain.AgentIdentityActive, Version: 1},
		&domain.AgentProfileRecord{AgentID: "quote", Version: 1, InstructionsPath: "/fixture/role.md", WorkspaceRoot: t.TempDir(), Capabilities: []string{"coding"}},
		event("event-agent", "agent.created")); err != nil {
		t.Fatal(err)
	}
	panel := newAuthenticatedPanel(t, web.RoleOwner, repository)
	panel.handler.commands, err = controlplane.NewCommandService(repository, nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	return panel, repository
}

func postIntentTask(t *testing.T, panel authenticatedPanel, key string, intent any, omit bool) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{"meta": map[string]any{"idempotency_key": key}, "target_agent_id": "quote", "organization_id": "org-main", "dispatch_mode": "direct", "content": "report the time"}
	if !omit {
		body["intent"] = intent
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, openapi.ControlCreateTaskPath, bytes.NewReader(encoded))
	request.AddCookie(panel.cookie)
	request.Header.Set("X-CSRF-Token", panel.session.CSRFToken)
	request.Header.Set("Idempotency-Key", key)
	response := httptest.NewRecorder()
	panel.handler.ServeHTTP(response, request)
	return response
}

func TestTaskIntentOfficialCreatePersistsAndProjects(t *testing.T) {
	panel, repository := newIntentPanel(t)
	ctx := context.Background()
	for index, tc := range []struct {
		name   string
		intent any
		omit   bool
		want   domain.TaskIntent
	}{
		{name: "omitted", omit: true, want: domain.TaskIntentMutation},
		{name: "mutation", intent: "mutation", want: domain.TaskIntentMutation},
		{name: "query", intent: "query", want: domain.TaskIntentQuery},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := fmt.Sprintf("intent-%d", index)
			response := postIntentTask(t, panel, key, tc.intent, tc.omit)
			if response.Code != http.StatusOK {
				t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
			}
			var receipt openapi.CreateTaskResponse
			if err := json.Unmarshal(response.Body.Bytes(), &receipt); err != nil {
				t.Fatal(err)
			}
			stored, err := repository.GetTask(ctx, receipt.TaskID)
			if err != nil || stored.Intent != tc.want {
				t.Fatalf("stored Task intent mismatch: %v", err)
			}
			projection, err := consoleapi.ProjectConsoleTask(*stored)
			if err != nil || projection.Intent != tc.want {
				t.Fatal("Console projection lost persisted intent")
			}
			request := httptest.NewRequest(http.MethodGet, openapi.ObserveTasksPath+"/"+receipt.TaskID, nil)
			request.AddCookie(panel.cookie)
			observed := httptest.NewRecorder()
			panel.handler.ServeHTTP(observed, request)
			var detail openapi.TaskReadModel
			if observed.Code != http.StatusOK || json.Unmarshal(observed.Body.Bytes(), &detail) != nil || detail.Task.Intent != tc.want {
				t.Fatal("official Observe detail lost persisted intent")
			}
			journal, err := repository.ListTaskJournal(ctx, receipt.TaskID, 0, 10)
			if err != nil || len(journal) != 1 {
				t.Fatalf("create journal count=%d err=%v", len(journal), err)
			}
			var payload struct {
				Intent domain.TaskIntent `json:"intent"`
			}
			if json.Unmarshal(journal[0].Payload, &payload) != nil || payload.Intent != tc.want {
				t.Fatal("create journal lost the declared intent")
			}
			// An omitted default and an explicit mutation replay are equivalent.
			replayed := postIntentTask(t, panel, key, string(tc.want), false)
			if replayed.Code != http.StatusOK || replayed.Body.String() != response.Body.String() {
				t.Fatal("matching intent was not idempotent")
			}
			other := domain.TaskIntentQuery
			if tc.want == other {
				other = domain.TaskIntentMutation
			}
			conflict := postIntentTask(t, panel, key, string(other), false)
			if conflict.Code != http.StatusBadRequest || !strings.Contains(conflict.Body.String(), domain.ErrIdempotencyConflict.Error()) {
				t.Fatal("different intent reused an idempotency key")
			}
		})
	}
}

func TestTaskIntentOfficialCreateRejectsInvalidInputWithoutEffects(t *testing.T) {
	panel, repository := newIntentPanel(t)
	ctx := context.Background()
	before, err := repository.LatestJournalSequence(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for index, intent := range []any{"", nil, "QUERY", "invalid-input-must-not-echo", 1, true, []string{"query"}} {
		response := postIntentTask(t, panel, fmt.Sprintf("invalid-intent-%d", index), intent, false)
		if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "invalid-input-must-not-echo") {
			t.Fatalf("invalid intent status=%d", response.Code)
		}
	}
	viewer := newAuthenticatedPanel(t, web.RoleViewer, repository)
	if response := postIntentTask(t, viewer, "viewer-query", "query", false); response.Code != http.StatusForbidden {
		t.Fatalf("query bypassed viewer boundary: %d", response.Code)
	}
	rows, err := repository.ListTasks(ctx, "quote", 10)
	if err != nil || len(rows) != 0 {
		t.Fatalf("rejected requests created tasks: count=%d err=%v", len(rows), err)
	}
	after, err := repository.LatestJournalSequence(ctx)
	if err != nil || after != before {
		t.Fatal("rejected requests changed the journal")
	}
}
