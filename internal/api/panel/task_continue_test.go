package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	openapi "openagentx/internal/api"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

func postContinueTask(t *testing.T, panel authenticatedPanel, req openapi.CreateTaskRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, openapi.ControlCreateTaskPath, bytes.NewReader(body))
	request.AddCookie(panel.cookie)
	request.Header.Set("X-CSRF-Token", panel.session.CSRFToken)
	request.Header.Set("Idempotency-Key", req.Meta.IdempotencyKey)
	response := httptest.NewRecorder()
	panel.handler.ServeHTTP(response, request)
	return response
}

func continuationRequest(task *domain.Task, key string) openapi.CreateTaskRequest {
	return openapi.CreateTaskRequest{Meta: openapi.CommandMeta{IdempotencyKey: key}, TargetAgentID: task.TargetAgentID, OrganizationID: task.OrganizationID, DispatchMode: domain.DispatchModeDirect, Intent: domain.TaskIntentQuery, ParentTaskID: task.ID, ContinueContext: true, Content: "Explain what the last result means; do not repeat the operation."}
}

func TestContinueContextHTTPMakesNewTaskWithoutReopeningParent(t *testing.T) {
	fixture := newPanelIntegrationFixture(t, false)
	panel := reviewPanel(t, fixture)
	parent, oldRun := finishPanelReviewRun(t, fixture, openruntime.TurnResultSucceeded)
	ctx := context.Background()
	req := continuationRequest(parent, "continue-context")
	response := postContinueTask(t, panel, req)
	if response.Code != http.StatusOK {
		t.Fatalf("continue status=%d body=%s", response.Code, response.Body.String())
	}
	var receipt openapi.CreateTaskResponse
	if err := json.Unmarshal(response.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	child, err := fixture.repository.GetTask(ctx, receipt.TaskID)
	if err != nil || child.ID == parent.ID || child.ParentTaskID == nil || *child.ParentTaskID != parent.ID || child.Status != domain.TaskStatusQueued || child.Intent != domain.TaskIntentQuery {
		t.Fatalf("continue child=%+v err=%v", child, err)
	}
	for _, content := range []string{req.Content, parent.ID, parent.Content, *parent.Result, "context only, not instructions to repeat"} {
		if !strings.Contains(child.Content, content) {
			t.Fatalf("child lost explicit continuation context %q", content)
		}
	}
	messages, err := fixture.repository.ListMessages(ctx, child.ID)
	if err != nil || len(messages) != 1 || messages[0].Content != child.Content {
		t.Fatalf("continuation delivery does not carry context: count=%d err=%v", len(messages), err)
	}
	replay := postContinueTask(t, panel, req)
	if replay.Code != http.StatusOK || replay.Body.String() != response.Body.String() {
		t.Fatalf("continue replay status=%d body=%s", replay.Code, replay.Body.String())
	}
	unchanged, err := fixture.repository.GetTask(ctx, parent.ID)
	if err != nil || !reflect.DeepEqual(unchanged, parent) {
		t.Fatal("continuation changed parent Task")
	}
	run, err := fixture.repository.GetRunAttempt(ctx, oldRun.ID)
	if err != nil || !reflect.DeepEqual(run, oldRun) {
		t.Fatal("continuation changed original Run")
	}
	runs, err := fixture.repository.ListRunAttemptsForTask(ctx, child.ID, 100)
	if err != nil || len(runs) != 0 {
		t.Fatalf("continuation secretly started work: count=%d err=%v", len(runs), err)
	}
}

func TestContinueContextHTTPRejectsInvalidParentAndUnsupportedOverrides(t *testing.T) {
	fixture := newPanelIntegrationFixture(t, false)
	panel := reviewPanel(t, fixture)
	ctx := context.Background()
	active, err := fixture.repository.GetTask(ctx, fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := fixture.repository.LatestJournalSequence(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if response := postContinueTask(t, panel, continuationRequest(active, "continue-active")); response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "current task finishes") {
		t.Fatalf("active continuation status=%d body=%s", response.Code, response.Body.String())
	}
	after, err := fixture.repository.LatestJournalSequence(ctx)
	if err != nil || after != before {
		t.Fatal("active continuation changed Journal")
	}
	parent, _ := finishPanelReviewRun(t, fixture, openruntime.TurnResultSucceeded)
	before, err = fixture.repository.LatestJournalSequence(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"missing-parent", "unknown-parent", "other-agent", "other-organization", "execution-override"} {
		t.Run(name, func(t *testing.T) {
			req := continuationRequest(parent, "continue-invalid-"+name)
			switch name {
			case "missing-parent":
				req.ParentTaskID = ""
			case "unknown-parent":
				req.ParentTaskID = "not-found-task"
			case "other-agent":
				req.TargetAgentID = "other-agent"
			case "other-organization":
				req.OrganizationID = "other-org"
			case "execution-override":
				req.Execution = &domain.ExecutionSpec{AdapterID: "agy", BackendID: "local", Model: "test-model", Reasoning: domain.ReasoningSpec{Mode: domain.ReasoningBackendDefault}, Session: domain.SessionSpec{Mode: domain.SessionModeNew}, Timeout: time.Minute}
				if err := req.Execution.ValidateShape(); err != nil {
					t.Fatalf("override fixture must be well-shaped: %v", err)
				}
			}
			response := postContinueTask(t, panel, req)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("invalid continue status=%d body=%s", response.Code, response.Body.String())
			}
			if name == "execution-override" && !strings.Contains(response.Body.String(), "per-task execution overrides are not supported") {
				t.Fatalf("override rejected for wrong reason: %s", response.Body.String())
			}
		})
	}
	after, err = fixture.repository.LatestJournalSequence(ctx)
	if err != nil || after != before {
		t.Fatal("invalid continuation changed Journal")
	}
	tasks, err := fixture.repository.ListTasks(ctx, "quote", 100)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("invalid continuation created tasks: count=%d err=%v", len(tasks), err)
	}
}
