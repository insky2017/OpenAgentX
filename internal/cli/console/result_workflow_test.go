package console

import (
	"context"
	"strings"
	"testing"
	"time"

	openapi "openagentx/internal/api"
	"openagentx/internal/consolemodel"
	"openagentx/internal/domain"
)

func completedConsoleTask() *consolemodel.TaskState {
	detail := taskProjection("task-parent", 4, domain.TaskStatusSucceeded)
	detail.Intent = domain.TaskIntentQuery
	return &consolemodel.TaskState{TaskID: detail.TaskID, Version: detail.Version, Status: detail.Status, Intent: detail.Intent, Detail: detail, LatestRun: &openapi.RunAttemptReadModel{ID: "run-parent", TaskID: detail.TaskID, Version: 2, Status: domain.RunAttemptSucceeded}}
}

func TestContinueDispatchCarriesParentAndServerContext(t *testing.T) {
	for _, tc := range []struct {
		line    string
		intent  domain.TaskIntent
		content string
	}{
		{"/continue refine the answer", domain.TaskIntentQuery, "refine the answer"},
		{"/continue --intent mutation create the report", domain.TaskIntentMutation, "create the report"},
	} {
		application, client, _, _ := applicationFixture(t, true)
		application.prepared[9] = preparedAttach{client: client, agents: map[string]domain.ConsoleAgentOption{"quote": {AgentID: "quote", OrganizationID: "org-main"}}}
		request, err := parseControlInput(tc.line, "quote", consolemodel.State{FocusedTask: completedConsoleTask()})
		if err != nil {
			t.Fatal(err)
		}
		result := application.controlCmd(9, request)().(controlResultMsg)
		if result.Err != nil || len(client.dispatches) != 1 {
			t.Fatalf("dispatch=%v err=%v", client.dispatches, result.Err)
		}
		got := client.dispatches[0]
		if !got.ContinueContext || got.ParentTaskID != "task-parent" || got.Intent != tc.intent || got.Content != tc.content || got.TargetAgentID != "quote" || got.Meta.IdempotencyKey == "" {
			t.Fatalf("request=%+v", got)
		}
	}
	task := completedConsoleTask()
	task.Status = domain.TaskStatusRunning
	if _, err := parseControlInput("/continue work", "quote", consolemodel.State{FocusedTask: task}); err == nil {
		t.Fatal("running task continued")
	}
	if _, err := parseControlInput("/continue", "quote", consolemodel.State{FocusedTask: completedConsoleTask()}); err == nil {
		t.Fatal("empty continuation accepted")
	}
}

type resultReviewClient struct {
	*fakeConsoleClient
	request openapi.ReviewTaskRequest
	target  string
}

func (c *resultReviewClient) ReviewTaskResult(_ context.Context, target string, request openapi.ReviewTaskRequest) (domain.TaskReview, error) {
	c.request = request
	c.target = target
	return domain.TaskReview{ID: "review-1", TaskID: target, RunID: request.RunID, RunVersion: request.RunVersion, Decision: request.Decision, Note: request.Note, TaskVersion: request.Meta.ExpectedVersion + 1, Sequence: 1220, ReviewedBy: "owner", CreatedAt: time.Now()}, nil
}
func TestConsoleResultReviewBindsTaskRunAndVersions(t *testing.T) {
	for _, tc := range []struct{ line, decision string }{{"/accept checked output", "accepted"}, {"/result-reject missing evidence", "rejected"}} {
		application, base, _, _ := applicationFixture(t, true)
		client := &resultReviewClient{fakeConsoleClient: base}
		application.prepared[9] = preparedAttach{client: client, agents: map[string]domain.ConsoleAgentOption{"quote": {AgentID: "quote", OrganizationID: "org-main"}}}
		request, err := parseControlInput(tc.line, "quote", consolemodel.State{FocusedTask: completedConsoleTask()})
		if err != nil {
			t.Fatal(err)
		}
		result := application.controlCmd(9, request)().(controlResultMsg)
		if result.Err != nil || result.Outcome.Review == nil {
			t.Fatalf("outcome=%+v err=%v", result.Outcome, result.Err)
		}
		if client.target != "task-parent" || client.request.RunID != "run-parent" || client.request.RunVersion != 2 || client.request.Meta.ExpectedVersion != 4 || client.request.Decision != tc.decision || client.request.Meta.IdempotencyKey == "" {
			t.Fatalf("review request=%+v", client.request)
		}
		if !strings.Contains(result.Outcome.summary(request.Kind), "原执行状态保留") {
			t.Fatal("review obscured evidence boundary")
		}
	}
	task := completedConsoleTask()
	task.LatestRun.Status = domain.RunAttemptUncertain
	if _, err := parseControlInput("/accept", "quote", consolemodel.State{FocusedTask: task}); err == nil {
		t.Fatal("unconfirmed execution accepted for review")
	}
	task = completedConsoleTask()
	task.Version++
	if _, err := parseControlInput("/accept", "quote", consolemodel.State{FocusedTask: task}); err == nil {
		t.Fatal("stale detail accepted for review")
	}
}
