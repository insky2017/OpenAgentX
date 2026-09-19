package console

import (
	"encoding/json"
	"strings"
	"testing"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
	"openagentx/internal/safeoutput"
)

func TestConsoleTaskOutcomeAndRuntimeReplyStatesAreStructuredAndSafe(t *testing.T) {
	task := domain.Task{
		ID: "task-safe", Version: 3, SenderPrincipalID: "owner", TargetAgentID: "quote",
		OrganizationID: "org-main", DispatchMode: domain.DispatchModeDirect,
		IdempotencyKey: "dispatch-safe", Content: "inspect", Status: domain.TaskStatusSucceeded,
		CreatedAt: "2026-09-20T00:00:00Z", UpdatedAt: "2026-09-20T00:01:00Z",
	}
	projected, err := ProjectConsoleTask(task)
	if err != nil || projected.OutcomeState != "not_recorded" {
		t.Fatalf("missing outcome state=%q err=%v", projected.OutcomeState, err)
	}
	unsafe := "token=task-secret " + strings.Repeat("x", safeoutput.MaxTextBytes+1)
	task.Result = &unsafe
	projected, err = ProjectConsoleTask(task)
	if err != nil || projected.OutcomeState != "truncated" || !projected.ResultTruncated || projected.Result == nil ||
		strings.Contains(*projected.Result, "task-secret") {
		t.Fatalf("safe Task projection state=%q truncated=%t present=%t err=%v", projected.OutcomeState,
			projected.ResultTruncated, projected.Result != nil, err)
	}

	resultJSON, err := json.Marshal(openruntime.TurnResult{Status: openruntime.TurnResultSucceeded,
		Result: unsafe, SideEffectsKnown: true})
	if err != nil {
		t.Fatal(err)
	}
	reply, state := consoleTurnResult(string(resultJSON))
	if state != "truncated" || reply == nil || !reply.BodyTruncated || strings.Contains(reply.Body, "task-secret") {
		t.Fatalf("safe Runtime reply state=%q body_truncated=%t present=%t", state, reply != nil && reply.BodyTruncated, reply != nil)
	}
	if reply, state = consoleTurnResult(`{"status":"mystery"}`); state != "invalid" || reply != nil {
		t.Fatalf("invalid Runtime reply state=%q reply=%+v", state, reply)
	}
	validEmpty, err := json.Marshal(openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, SideEffectsKnown: true})
	if err != nil {
		t.Fatal(err)
	}
	if reply, state = consoleTurnResult(string(validEmpty)); state != "empty" || reply == nil {
		t.Fatalf("empty Runtime reply state=%q reply=%+v", state, reply)
	}
}
