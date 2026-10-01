package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	openapi "openagentx/internal/api"
	"openagentx/internal/auth/web"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

func reviewPanel(t *testing.T, fixture panelIntegrationFixture) authenticatedPanel {
	t.Helper()
	return authenticatedPanel{handler: panelIntegrationHandler(t, fixture), cookie: fixture.cookie, session: fixture.session}
}

func finishPanelReviewRun(t *testing.T, fixture panelIntegrationFixture, status openruntime.TurnResultStatus) (*domain.Task, *domain.RunAttempt) {
	t.Helper()
	ctx := context.Background()
	result := openruntime.TurnResult{Status: status, Result: "artifact ready; effects await human assessment"}
	if err := fixture.repository.FinishRun(ctx, fixture.guard, fixture.run.ID, 2, fixture.run.Version, result, nil, 0, nil,
		panelEvent("review-finish-task", "task.settled", "task", fixture.task.ID, "human-owner"),
		panelEvent("review-finish-run", "run_attempt.finished", "run_attempt", fixture.run.ID, "human-owner")); err != nil {
		t.Fatal(err)
	}
	task, err := fixture.repository.GetTask(ctx, fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	run, err := fixture.repository.GetRunAttempt(ctx, fixture.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	return task, run
}

func postReview(t *testing.T, panel authenticatedPanel, taskID string, req openapi.ReviewTaskRequest, mutate func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/control/v1/tasks/"+taskID+"/review", bytes.NewReader(body))
	request.AddCookie(panel.cookie)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", panel.session.CSRFToken)
	request.Header.Set("Idempotency-Key", req.Meta.IdempotencyKey)
	if mutate != nil {
		mutate(request)
	}
	response := httptest.NewRecorder()
	panel.handler.ServeHTTP(response, request)
	return response
}

func TestTaskReviewHTTPPersistsAcceptedRejectedAndKeepsExecution(t *testing.T) {
	fixture := newPanelIntegrationFixture(t, false)
	panel := reviewPanel(t, fixture)
	task, run := finishPanelReviewRun(t, fixture, openruntime.TurnResultSucceeded)
	if task.Status != domain.TaskStatusUncertain || run.Status != domain.RunAttemptSucceeded {
		t.Fatal("fixture must represent completed execution with unverified business effects")
	}
	ctx := context.Background()
	version := task.Version
	var firstRequest openapi.ReviewTaskRequest
	var firstReview domain.TaskReview
	for _, decision := range []string{"accepted", "rejected"} {
		req := openapi.ReviewTaskRequest{Meta: openapi.CommandMeta{IdempotencyKey: "review-" + decision, ExpectedVersion: version}, RunID: run.ID, RunVersion: run.Version, Decision: decision, Note: "verified artifact output"}
		response := postReview(t, panel, task.ID, req, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("review %s status=%d body=%s", decision, response.Code, response.Body.String())
		}
		var review domain.TaskReview
		if err := json.Unmarshal(response.Body.Bytes(), &review); err != nil {
			t.Fatal(err)
		}
		if review.Decision != decision || review.TaskVersion != version+1 || review.RunID != run.ID || review.RunVersion != run.Version || review.ReviewedBy != "human-owner" || review.Sequence <= 0 || review.ResultSHA256 == "" {
			t.Fatalf("invalid review receipt=%+v", review)
		}
		persisted, err := fixture.repository.GetTaskReview(ctx, task.ID)
		if err != nil || !reflect.DeepEqual(persisted, &review) {
			t.Fatalf("persisted review=%+v err=%v", persisted, err)
		}
		replayed := postReview(t, panel, task.ID, req, nil)
		if replayed.Code != http.StatusOK || replayed.Body.String() != response.Body.String() {
			t.Fatalf("idempotent review status=%d body=%s", replayed.Code, replayed.Body.String())
		}
		version = review.TaskVersion
		if decision == "accepted" {
			firstRequest, firstReview = req, review
		}
		// Refresh the official task detail and independently read its execution.
		request := httptest.NewRequest(http.MethodGet, openapi.ObserveTasksPath+"/"+task.ID, nil)
		request.AddCookie(panel.cookie)
		detailResponse := httptest.NewRecorder()
		panel.handler.ServeHTTP(detailResponse, request)
		var detail openapi.TaskReadModel
		if detailResponse.Code != http.StatusOK || json.Unmarshal(detailResponse.Body.Bytes(), &detail) != nil || detail.Review == nil || !reflect.DeepEqual(detail.Review, &review) || detail.Task.Status != task.Status || detail.Task.Version != version {
			t.Fatalf("detail did not preserve review/execution: status=%d body=%s", detailResponse.Code, detailResponse.Body.String())
		}
		current, err := fixture.repository.GetTask(ctx, task.ID)
		if err != nil || current.Status != task.Status || !reflect.DeepEqual(current.Result, task.Result) || !reflect.DeepEqual(current.Error, task.Error) || current.CompletionBasis != task.CompletionBasis {
			t.Fatalf("review rewrote Task=%+v err=%v", current, err)
		}
		currentRun, err := fixture.repository.GetRunAttempt(ctx, run.ID)
		if err != nil || !reflect.DeepEqual(currentRun, run) {
			t.Fatalf("review rewrote Run=%+v err=%v", currentRun, err)
		}
	}
	// Replaying the earlier accepted command must not overwrite the newer rejection.
	oldResponse := postReview(t, panel, task.ID, firstRequest, nil)
	var oldReceipt domain.TaskReview
	if oldResponse.Code != http.StatusOK || json.Unmarshal(oldResponse.Body.Bytes(), &oldReceipt) != nil || !reflect.DeepEqual(oldReceipt, firstReview) {
		t.Fatalf("old receipt replay status=%d", oldResponse.Code)
	}
	latest, err := fixture.repository.GetTaskReview(ctx, task.ID)
	if err != nil || latest.Decision != "rejected" || latest.TaskVersion != version {
		t.Fatalf("old replay replaced latest review=%+v err=%v", latest, err)
	}
	for _, name := range []string{"idempotency-payload", "task-version", "run-version", "run-id"} {
		req := firstRequest
		req.Meta.ExpectedVersion = version
		switch name {
		case "idempotency-payload":
			req.Note = "changed payload"
		case "task-version":
			req.Meta.IdempotencyKey = "review-stale-task"
			req.Meta.ExpectedVersion--
		case "run-version":
			req.Meta.IdempotencyKey = "review-stale-run"
			req.RunVersion--
		case "run-id":
			req.Meta.IdempotencyKey = "review-other-run"
			req.RunID = "other-run"
		}
		response := postReview(t, panel, task.ID, req, nil)
		if response.Code != http.StatusConflict {
			t.Fatalf("%s status=%d body=%s", name, response.Code, response.Body.String())
		}
	}
	journal, err := fixture.repository.ListTaskJournal(ctx, task.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, event := range journal {
		if event.EventType == "task.result_reviewed" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("review journal count=%d, want two assessments", count)
	}
}

func TestTaskReviewHTTPAuthorizationAndCSRFProtectPersistence(t *testing.T) {
	fixture := newPanelIntegrationFixture(t, false)
	panel := reviewPanel(t, fixture)
	task, run := finishPanelReviewRun(t, fixture, openruntime.TurnResultSucceeded)
	req := openapi.ReviewTaskRequest{Meta: openapi.CommandMeta{IdempotencyKey: "review-auth", ExpectedVersion: task.Version}, RunID: run.ID, RunVersion: run.Version, Decision: "accepted"}
	before, err := fixture.repository.LatestJournalSequence(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*http.Request)
		want   int
	}{
		{"anonymous", func(r *http.Request) { r.Header.Del("Cookie") }, http.StatusUnauthorized},
		{"missing-csrf", func(r *http.Request) { r.Header.Del("X-CSRF-Token") }, http.StatusForbidden},
		{"wrong-csrf", func(r *http.Request) { r.Header.Set("X-CSRF-Token", "invalid-test-csrf") }, http.StatusForbidden},
		{"idempotency-header-mismatch", func(r *http.Request) { r.Header.Set("Idempotency-Key", "different-review") }, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := postReview(t, panel, task.ID, req, tc.mutate)
			if response.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, tc.want, response.Body.String())
			}
		})
	}
	viewer := newAuthenticatedPanel(t, web.RoleViewer, fixture.repository)
	if response := postReview(t, viewer, task.ID, req, nil); response.Code != http.StatusForbidden {
		t.Fatalf("viewer review status=%d", response.Code)
	}
	after, err := fixture.repository.LatestJournalSequence(context.Background())
	if err != nil || before != after {
		t.Fatalf("unauthorized review changed Journal: before=%d after=%d err=%v", before, after, err)
	}
	current, err := fixture.repository.GetTask(context.Background(), task.ID)
	if err != nil || !reflect.DeepEqual(current, task) {
		t.Fatal("unauthorized review changed Task")
	}
}

func TestTaskReviewHTTPRejectsActiveAndUncertainExecution(t *testing.T) {
	for _, state := range []string{"active", "uncertain"} {
		t.Run(state, func(t *testing.T) {
			fixture := newPanelIntegrationFixture(t, false)
			panel := reviewPanel(t, fixture)
			task, err := fixture.repository.GetTask(context.Background(), fixture.task.ID)
			if err != nil {
				t.Fatal(err)
			}
			run := fixture.run
			if state == "uncertain" {
				task, run = finishPanelReviewRun(t, fixture, openruntime.TurnResultUncertain)
			}
			req := openapi.ReviewTaskRequest{Meta: openapi.CommandMeta{IdempotencyKey: "review-unconfirmed", ExpectedVersion: task.Version}, RunID: run.ID, RunVersion: run.Version, Decision: "accepted"}
			response := postReview(t, panel, task.ID, req, nil)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("unconfirmed review status=%d body=%s", response.Code, response.Body.String())
			}
			stored, err := fixture.repository.GetTaskReview(context.Background(), task.ID)
			if err != nil || stored != nil {
				t.Fatalf("unconfirmed review persisted=%+v err=%v", stored, err)
			}
		})
	}
}
