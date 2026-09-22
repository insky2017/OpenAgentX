package consolemodel

import (
	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	"openagentx/internal/domain"
	"testing"
)

func TestCompletionBasisIsAuthoritativeAndFenced(t *testing.T) {
	r, err := New(consoleapi.AttachResponse{AgentID: "quote", Mode: consoleapi.ModeNormal,
		WorkerStatus: domain.WorkerStatusOffline, SnapshotSequence: 10})
	if err != nil {
		t.Fatal(err)
	}
	task := taskModel("task-result", 1, domain.TaskStatusSucceeded)
	task.Intent = domain.TaskIntentQuery
	task.CompletionBasis = domain.TaskCompletionQueryResultDelivered
	for _, basis := range []domain.TaskCompletionBasis{"unknown", domain.TaskCompletionMutationEffectsKnown} {
		bad := task
		bad.CompletionBasis = basis
		if _, err := r.Apply(taskEventModel(11, bad)); err == nil || r.Cursor() != 10 {
			t.Fatal("invalid basis crossed cursor")
		}
	}
	if err := r.ApplyTaskSnapshot(openapi.ConsoleTaskSnapshot{Task: task, SnapshotSequence: 10}, FocusManual); err != nil {
		t.Fatal(err)
	}
	if r.State().FocusedTask.Detail.CompletionBasis != domain.TaskCompletionQueryResultDelivered {
		t.Fatal("lost delivery evidence")
	}
	bad := task
	bad.CompletionBasis = ""
	if _, err := r.Apply(taskEventModel(11, bad)); err == nil || r.Cursor() != 10 {
		t.Fatal("changed evidence overwrote terminal snapshot")
	}
}
