package panel

import (
	"encoding/json"
	"strings"
	"testing"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
	"openagentx/internal/safeoutput"
)

func TestWebTaskAndRuntimeReplyUseSharedSafeProjection(t *testing.T) {
	unsafe := "password=web-secret " + strings.Repeat("x", safeoutput.MaxTextBytes+1)
	task := domain.Task{ID: "task-web", Version: 2, TargetAgentID: "quote", Content: "inspect",
		Status: domain.TaskStatusFailed, Result: &unsafe}
	projectedTask := taskReadModel(task)
	if projectedTask.OutcomeState != "truncated" || !projectedTask.ResultTruncated || projectedTask.Result == nil ||
		strings.Contains(*projectedTask.Result, "web-secret") {
		t.Fatalf("Web Task projection state=%q truncated=%t present=%t", projectedTask.OutcomeState,
			projectedTask.ResultTruncated, projectedTask.Result != nil)
	}
	resultJSON, err := json.Marshal(openruntime.TurnResult{Status: openruntime.TurnResultFailed,
		Error: unsafe, SideEffectsKnown: false})
	if err != nil {
		t.Fatal(err)
	}
	reply, state := turnResultReadModel(string(resultJSON))
	if state != "truncated" || reply == nil || !reply.ErrorTruncated || strings.Contains(reply.Error, "web-secret") {
		t.Fatalf("Web Runtime projection state=%q error_truncated=%t present=%t", state,
			reply != nil && reply.ErrorTruncated, reply != nil)
	}
	if reply, state = turnResultReadModel(`{"status":"mystery"}`); state != "invalid" || reply != nil {
		t.Fatalf("invalid Runtime projection state=%q reply=%+v", state, reply)
	}
}
