package consolemodel

import (
	"testing"

	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	"openagentx/internal/domain"
)

func TestTaskIntentCannotChangeAcrossControlAndEventUpdates(t *testing.T) {
	r, err := New(consoleapi.AttachResponse{AgentID: "quote", Mode: consoleapi.ModeNormal,
		WorkerStatus: domain.WorkerStatusOffline, SnapshotSequence: 10})
	if err != nil {
		t.Fatal(err)
	}
	task := taskModel("task-intent", 1, domain.TaskStatusQueued)
	task.Intent = domain.TaskIntentQuery
	if err := r.ApplyTaskSnapshot(openapi.ConsoleTaskSnapshot{Task: task, SnapshotSequence: 10}, FocusManual); err != nil {
		t.Fatal(err)
	}
	if err := r.ApplyControlTask(ControlTaskUpdate{AgentID: "quote", TaskID: task.TaskID,
		Version: 2, Status: domain.TaskStatusRunning}); err != nil {
		t.Fatal(err)
	}
	for _, intent := range []domain.TaskIntent{domain.TaskIntentMutation, "", "invalid"} {
		changed := taskModel(task.TaskID, 2, domain.TaskStatusRunning)
		changed.Intent = intent
		if _, err := r.Apply(taskEventModel(11, changed)); err == nil || r.Cursor() != 10 {
			t.Fatal("invalid or changed intent crossed the cursor")
		}
	}
	current := taskModel(task.TaskID, 2, domain.TaskStatusRunning)
	current.Intent = domain.TaskIntentQuery
	if _, err := r.Apply(taskEventModel(11, current)); err != nil {
		t.Fatal(err)
	}
	if r.State().FocusedTask.Intent != domain.TaskIntentQuery || r.Cursor() != 11 {
		t.Fatal("current intent did not survive the control response")
	}
}

func TestLegacyTaskIntentIsMutationAndExplicitDuplicateIsIdempotent(t *testing.T) {
	r, err := New(consoleapi.AttachResponse{AgentID: "quote", Mode: consoleapi.ModeNormal,
		WorkerStatus: domain.WorkerStatusOffline, SnapshotSequence: 10})
	if err != nil {
		t.Fatal(err)
	}
	task := taskModel("task-legacy-intent", 1, domain.TaskStatusQueued)
	if err := r.ApplyTaskSnapshot(openapi.ConsoleTaskSnapshot{Task: task, SnapshotSequence: 10}, FocusManual); err != nil {
		t.Fatal(err)
	}
	task.Intent = domain.TaskIntentMutation
	result, err := r.Apply(taskEventModel(11, task))
	if err != nil || result.Timeline != nil || r.State().FocusedTask.Intent != domain.TaskIntentMutation {
		t.Fatal("legacy intent did not preserve mutation semantics")
	}
}

func TestTaskOptionsRetainAndFenceDeclaredIntent(t *testing.T) {
	r, err := New(consoleapi.AttachResponse{AgentID: "quote", Mode: consoleapi.ModeNormal,
		WorkerStatus: domain.WorkerStatusOffline, SnapshotSequence: 10})
	if err != nil {
		t.Fatal(err)
	}
	task := taskModel("task-intent-option", 1, domain.TaskStatusQueued)
	option := openapi.ConsoleTaskOption{TaskID: task.TaskID, Version: task.Version, Status: task.Status,
		Intent: domain.TaskIntentQuery, Summary: task.Content, UpdatedAt: task.UpdatedAt}
	if err := r.ReplaceTaskOptions([]openapi.ConsoleTaskOption{option}); err != nil {
		t.Fatal(err)
	}
	if err := r.FocusTask(task.TaskID); err != nil {
		t.Fatal(err)
	}
	for _, intent := range []domain.TaskIntent{"", domain.TaskIntentMutation, "invalid"} {
		changed := option
		changed.Version++
		changed.Intent = intent
		if err := r.ReplaceTaskOptions([]openapi.ConsoleTaskOption{changed}); err == nil {
			t.Fatal("Task option changed a declared intent")
		}
		if r.State().FocusedTask.Intent != domain.TaskIntentQuery || r.State().FocusedTask.Version != 1 {
			t.Fatal("rejected option mutated reducer state")
		}
	}
	task.Intent = domain.TaskIntentMutation
	if err := r.ApplyTaskSnapshot(openapi.ConsoleTaskSnapshot{Task: task, SnapshotSequence: 10}, FocusManual); err == nil {
		t.Fatal("snapshot replaced the intent established by Task options")
	}
	task.Intent = domain.TaskIntentQuery
	if err := r.ApplyTaskSnapshot(openapi.ConsoleTaskSnapshot{Task: task, SnapshotSequence: 10}, FocusManual); err != nil {
		t.Fatal(err)
	}
}
