package console

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	openapi "openagentx/internal/api"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

type resultWorkflowState struct {
	testState
	review   *domain.TaskReview
	backends []openruntime.BackendRegistration
}

func (s *resultWorkflowState) GetTaskReview(context.Context, string) (*domain.TaskReview, error) {
	return s.review, nil
}
func (s *resultWorkflowState) ListWorkerBackends(context.Context, string) ([]openruntime.BackendRegistration, error) {
	return s.backends, nil
}
func TestConsoleReviewProjectionAndReadinessUseAuthoritativeFacts(t *testing.T) {
	now := time.Now().UTC()
	task := consoleTask("task-result", "quote", now.Format(time.RFC3339Nano), "result", 4, domain.TaskStatusSucceeded)
	run := &domain.RunAttempt{ID: "run-result", TaskID: task.ID, AgentID: "quote", Version: 2, Status: domain.RunAttemptSucceeded, WorkerInstanceID: "worker-result", FencingToken: 1, LeaseUntil: now.Add(time.Minute), ExecutionSpecVersion: 1, AdapterID: "fake", BackendID: "local", Model: "fixture", ReasoningMode: domain.ReasoningBackendDefault, RequestedExecutionJSON: `{}`, ResolvedExecutionJSON: `{}`, StartedAt: now, FinishedAt: &now, CreatedAt: now, UpdatedAt: now}
	state := &resultWorkflowState{testState: testState{taskSnapshots: map[string]domain.ConsoleTaskSnapshot{task.ID: {Task: task, LatestRun: run, LatestRunWorkerGeneration: 1}}, snapshot: domain.ConsoleSnapshot{Agent: domain.AgentIdentity{ID: "quote", Status: domain.AgentIdentityActive}, Worker: &domain.WorkerInstance{ID: "worker-result", AgentID: "quote", Generation: 1, Status: domain.WorkerStatusOnline, LeaseUntil: now.Add(time.Minute)}}}, review: &domain.TaskReview{TaskID: task.ID, TaskVersion: 4, RunID: run.ID, RunVersion: 2, Decision: "accepted"}, backends: []openruntime.BackendRegistration{{BackendID: "agy", Health: openruntime.BackendHealthy}}}
	fixture := newConsoleFixtureWithState(t, state.testState)
	fixture.handler.state = state
	response := fixture.request("/api/console/v1/agents/quote/tasks/task-result")
	var detail openapi.ConsoleTaskSnapshot
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &detail) != nil || detail.Task.Review == nil || detail.Task.Review.Decision != "accepted" {
		t.Fatalf("detail=%s", response.Body.String())
	}
	state.review.TaskVersion = 5
	if got := fixture.request("/api/console/v1/agents/quote/tasks/task-result"); got.Code != http.StatusConflict {
		t.Fatalf("mixed versions status=%d", got.Code)
	}
	response = fixture.request(AttachPath + "?agent_id=quote")
	var attach AttachResponse
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &attach) != nil || attach.Readiness == nil || !attach.Readiness.Ready || attach.Readiness.Reason != "可开始工作" {
		t.Fatalf("readiness=%s", response.Body.String())
	}
	state.backends = nil
	response = fixture.request(AttachPath + "?agent_id=quote")
	if json.Unmarshal(response.Body.Bytes(), &attach) != nil || attach.Readiness.Ready || attach.Readiness.Reason != "运行环境或网络尚未就绪" {
		t.Fatalf("unready=%s", response.Body.String())
	}
}
