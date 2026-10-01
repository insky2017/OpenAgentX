package consolemodel

import (
	"testing"
	"time"

	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	"openagentx/internal/domain"
)

func TestReviewVersionAdvancesWithoutChangingExecutionEvidence(t *testing.T) {
	r, err := New(consoleapi.AttachResponse{AgentID: "quote", Mode: consoleapi.ModeNormal, WorkerStatus: domain.WorkerStatusOffline})
	if err != nil {
		t.Fatal(err)
	}
	task := taskModel("task-review", 3, domain.TaskStatusSucceeded)
	result := "original answer"
	task.Result = &result
	task.OutcomeState = "available"
	if err := r.ApplyTaskSnapshot(openapi.ConsoleTaskSnapshot{Task: task}, FocusManual); err != nil {
		t.Fatal(err)
	}
	task.Version = 4
	task.UpdatedAt = taskTestTime.Add(time.Minute).Format(time.RFC3339Nano)
	task.Review = &domain.TaskReview{TaskID: task.TaskID, TaskVersion: 4, RunID: "run-review", RunVersion: 2, Decision: "accepted", Note: "checked"}
	if err := r.ApplyTaskSnapshot(openapi.ConsoleTaskSnapshot{Task: task}, FocusManual); err != nil {
		t.Fatal(err)
	}
	if r.State().FocusedTask.Detail.Review.Decision != "accepted" {
		t.Fatal("review missing")
	}
	clone := r.State()
	clone.FocusedTask.Detail.Review.Decision = "rejected"
	if r.State().FocusedTask.Detail.Review.Decision != "accepted" {
		t.Fatal("review pointer aliases reducer")
	}
	// Journal task projections may omit a review already loaded from detail.
	task.Review = nil
	if _, err := r.Apply(taskEventModel(1, task)); err != nil {
		t.Fatal(err)
	}
	if r.State().FocusedTask.Detail.Review.Decision != "accepted" {
		t.Fatal("journal removed review")
	}
	changed := "forged answer"
	task.Version++
	task.Result = &changed
	if err := r.ApplyTaskSnapshot(openapi.ConsoleTaskSnapshot{Task: task}, FocusManual); err == nil {
		t.Fatal("review rewrote result")
	}
	task.Result = &result
	task.Status = domain.TaskStatusFailed
	if err := r.ApplyTaskSnapshot(openapi.ConsoleTaskSnapshot{Task: task}, FocusManual); err == nil {
		t.Fatal("review rewrote status")
	}
}
