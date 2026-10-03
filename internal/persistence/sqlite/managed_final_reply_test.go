package sqlite

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	"openagentx/internal/controlplane"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
	"openagentx/internal/safeoutput"
)

func TestManagedFinalReplyFinishPreservesGoldenAndRejectsOversize(t *testing.T) {
	// Sanitized actual Pay turn 01a10234-6021-7bd1-99ea-1f1df765589f:
	// 4391 raw bytes, 4278 after the existing redaction rules. The former 4 KiB
	// summary limit incorrectly turned this complete query answer into uncertain.
	golden, err := os.ReadFile("../../safeoutput/testdata/pay-consultation-final.txt")
	if err != nil || len(golden) <= safeoutput.MaxTextBytes || len(golden) >= safeoutput.MaxResultBytes {
		t.Fatalf("invalid real regression fixture: bytes=%d err=%v", len(golden), err)
	}
	for _, oversized := range []bool{false, true} {
		name := "complete_golden"
		if oversized {
			name = "over_final_limit"
		}
		t.Run(name, func(t *testing.T) {
			x := newManagedFixture(t)
			ctx := context.Background()
			message := x.send(t, "long-reply")
			run, guard := x.begin(t, message.TaskID, "pay")
			service, err := controlplane.NewWorkerService(x.r, nil, controlplane.WorkerServiceOptions{Now: func() time.Time { return repositoryTestTime }})
			if err != nil {
				t.Fatal(err)
			}
			body := string(golden) + "\npassword=fixture-secret\n"
			want := string(golden) + "\npassword=[REDACTED]\n"
			if oversized {
				body = "password=fixture-secret\n" + strings.Repeat("界", safeoutput.MaxResultBytes/3+1)
			}
			request := openapi.FinishRunRequest{WorkerInstanceID: guard.WorkerInstanceID, Generation: guard.Generation, FencingToken: guard.FencingToken, ExpectedTaskVersion: 2, ExpectedRunVersion: 1,
				Result: openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, FinalReply: true, Result: body}}
			if err = service.Finish(ctx, guard.PrincipalID, "managed-worker-token-pay", run.ID, request); err != nil {
				t.Fatal(err)
			}
			task, err := x.r.GetTask(ctx, message.TaskID)
			if err != nil || task.Result == nil || strings.Contains(*task.Result, "fixture-secret") {
				t.Fatalf("unsafe or missing persisted result: %v", err)
			}
			persisted, err := x.r.GetRunAttempt(ctx, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			observedTask, err := consoleapi.ProjectConsoleTask(*task)
			if err != nil {
				t.Fatal(err)
			}
			observedRun, err := consoleapi.ProjectConsoleRun(*persisted, guard.Generation)
			if err != nil || observedRun.TurnResult == nil || strings.Contains(observedRun.TurnResult.Body, "fixture-secret") {
				t.Fatalf("unsafe or missing Run API result: %v", err)
			}
			inbox, err := x.s.Inbox(ctx, x.tokens[x.f.agentID], 0, 100)
			if err != nil || len(inbox) != 1 || inbox[0].ReplyToMessageID != message.ID {
				t.Fatalf("missing correlated result: %v", err)
			}
			var result struct {
				Result string `json:"result"`
			}
			if err = json.Unmarshal([]byte(inbox[0].Content), &result); err != nil {
				t.Fatal(err)
			}
			if oversized {
				if task.Status != domain.TaskStatusUncertain || task.Error == nil || *task.Error != "query_result_unverified" || observedRun.TurnResult.FinalReply || !observedRun.TurnResult.BodyTruncated || observedTask.OutcomeState != "truncated" || inbox[0].TaskID != "" || externalCount(t, x.r, "managed_message_tasks") != 1 {
					t.Fatal("oversized answer incorrectly completed or triggered continuation")
				}
				return
			}
			if task.Status != domain.TaskStatusSucceeded || task.CompletionBasis != domain.TaskCompletionQueryResultDelivered || *task.Result != want || observedTask.Result == nil || *observedTask.Result != want || observedTask.ResultTruncated || observedRun.TurnResult.Body != want || !observedRun.TurnResult.FinalReply || observedRun.TurnResult.BodyTruncated || result.Result != want {
				t.Fatal("complete answer lost between Finish, persistence, observation and result message")
			}
			continuation, err := x.r.GetTask(ctx, inbox[0].TaskID)
			if err != nil || !strings.Contains(continuation.Content, inbox[0].Content) || strings.Contains(continuation.Content, "fixture-secret") || externalCount(t, x.r, "managed_message_tasks") != 2 {
				t.Fatalf("full sanitized answer did not reach continuation: %v", err)
			}
			targets, err := x.r.ManagedCollaborationWakeTargets(ctx, run.ID)
			if err != nil || len(targets) != 1 || targets[0] != x.f.agentID {
				t.Fatalf("missing automatic continuation wake target: %v", err)
			}
		})
	}
}
