package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

func TestFinishRunCompletionContractAndControlPriority(t *testing.T) {
	for _, tc := range []struct {
		name    string
		intent  domain.TaskIntent
		result  openruntime.TurnResult
		control string
		status  domain.TaskStatus
		basis   domain.TaskCompletionBasis
		reason  string
	}{
		{"query", domain.TaskIntentQuery, openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, FinalReply: true, Result: "answer"}, "", domain.TaskStatusSucceeded, domain.TaskCompletionQueryResultDelivered, ""},
		{"query-no-final", domain.TaskIntentQuery, openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "increment"}, "", domain.TaskStatusUncertain, "", "query_result_unverified"},
		{"query-empty", domain.TaskIntentQuery, openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, FinalReply: true, Result: "  \n"}, "", domain.TaskStatusUncertain, "", "query_result_unverified"},
		{"query-truncated", domain.TaskIntentQuery, openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, FinalReply: true, Result: "part", ResultTruncated: true}, "", domain.TaskStatusUncertain, "", "query_result_unverified"},
		{"query-error", domain.TaskIntentQuery, openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, FinalReply: true, Result: "answer", Error: "diagnostic"}, "", domain.TaskStatusUncertain, "", "query_result_unverified"},
		{"query-cancel", domain.TaskIntentQuery, openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, FinalReply: true, Result: "answer"}, "cancel", domain.TaskStatusCanceled, "", ""},
		{"query-cancel-uncertain", domain.TaskIntentQuery, openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "part"}, "cancel", domain.TaskStatusUncertain, "", "query_result_unverified"},
		{"query-pending", domain.TaskIntentQuery, openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, FinalReply: true, Result: "answer"}, "message", domain.TaskStatusWaitingInput, "", ""},
		{"query-pending-uncertain", domain.TaskIntentQuery, openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "part"}, "message", domain.TaskStatusUncertain, "", "query_result_unverified"},
		{"query-failed", domain.TaskIntentQuery, openruntime.TurnResult{Status: openruntime.TurnResultFailed, FinalReply: true, Result: "answer", Error: "failed"}, "", domain.TaskStatusFailed, "", "failed"},
		{"query-waiting", domain.TaskIntentQuery, openruntime.TurnResult{Status: openruntime.TurnResultWaitingInput, FinalReply: true, Result: "question"}, "", domain.TaskStatusWaitingInput, "", ""},
		{"mutation-unknown", domain.TaskIntentMutation, openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, FinalReply: true, Result: "answer"}, "", domain.TaskStatusUncertain, "", "business_effect_unverified"},
		{"mutation-known", domain.TaskIntentMutation, openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, SideEffectsKnown: true}, "", domain.TaskStatusSucceeded, domain.TaskCompletionMutationEffectsKnown, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			fail := false
			injected := errors.New("settlement rollback")
			repository, _ := openTestRepository(t, func(p FaultPoint) error {
				if fail && p == FaultBeforeCommit {
					return injected
				}
				return nil
			})
			fixture := seedRepository(t, repository)
			descriptor := messageDescriptor(openruntime.SteerQueued)
			worker, guard := registerMessageWorker(t, repository, fixture, descriptor)
			task, message, mailbox, event := newTaskDelivery(fixture, tc.name)
			task.Intent = tc.intent
			oldError := "previous turn error"
			task.Error = &oldError
			created, err := repository.CreateTask(ctx, task, message, mailbox, event)
			if err != nil {
				t.Fatal(err)
			}
			run := &domain.RunAttempt{ID: "run-" + tc.name, TaskID: task.ID, AgentID: fixture.agentID, Version: 1, Status: domain.RunAttemptRunning, WorkerInstanceID: worker.ID, FencingToken: worker.FencingToken, LeaseUntil: repositoryTestTime.Add(time.Hour), ExecutionSpecVersion: 1, RequestedExecutionJSON: `{}`, ResolvedExecutionJSON: `{}`, AdapterID: descriptor.AdapterID, BackendID: "local", Model: descriptor.Models[0], ReasoningMode: domain.ReasoningBackendDefault}
			if _, err := repository.BeginRunAttempt(ctx, created.Task.Version, run, journalEvent("running-"+tc.name, "task.running", fixture.ownerPrincipal, fixture.organizationID), journalEvent("started-"+tc.name, "run_attempt.started", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
				t.Fatal(err)
			}
			if tc.control == "cancel" {
				if _, _, err := repository.RequestTaskCancel(ctx, task.ID, 2, fixture.ownerPrincipal, &domain.MailboxItem{ID: "cancel-" + tc.name}, journalEvent("cancel-task-"+tc.name, "task.cancel_requested", fixture.ownerPrincipal, fixture.organizationID), journalEvent("cancel-mailbox-"+tc.name, "mailbox.cancel_created", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
					t.Fatal(err)
				}
			} else if tc.control == "message" {
				if _, err := createRoutedMessage(t, repository, fixture, task.ID, 2, tc.name); err != nil {
					t.Fatal(err)
				}
			}
			taskEvent := journalEvent("settled-"+tc.name, "task.settled", fixture.ownerPrincipal, fixture.organizationID)
			taskEvent.Payload = json.RawMessage(`{"existing":"preserved"}`)
			runEvent := journalEvent("finished-"+tc.name, "run_attempt.finished", fixture.ownerPrincipal, fixture.organizationID)
			finish := func() error {
				return repository.FinishRun(ctx, guard, run.ID, 2, 1, tc.result, nil, 0, nil, taskEvent, runEvent)
			}
			switch tc.name {
			case "query-empty", "query-truncated", "query-error", "query-failed", "query-waiting":
				if err := finish(); err == nil {
					t.Fatal("contradictory FinalReply claim was accepted")
				}
				current, err := repository.GetTask(ctx, task.ID)
				if err != nil || current.Status != domain.TaskStatusRunning || current.Version != 2 || current.CompletionBasis != "" {
					t.Fatalf("rejected FinalReply changed task: %+v %v", current, err)
				}
				tc.result.FinalReply = false
			}
			if tc.name == "query" {
				fail = true
				if err := finish(); !errors.Is(err, injected) {
					t.Fatalf("rollback error=%v", err)
				}
				fail = false
				current, err := repository.GetTask(ctx, task.ID)
				if err != nil || current.Status != domain.TaskStatusRunning || current.CompletionBasis != "" || current.Version != 2 {
					t.Fatalf("partial Task settlement: %+v %v", current, err)
				}
				savedRun, err := repository.GetRunAttempt(ctx, run.ID)
				if err != nil || savedRun.Version != 1 || savedRun.ResultJSON != "" {
					t.Fatalf("partial Run settlement: %+v %v", savedRun, err)
				}
				var count int
				if err := repository.db.QueryRow(`SELECT COUNT(*) FROM event_journal WHERE event_id IN (?,?)`, taskEvent.ID, runEvent.ID).Scan(&count); err != nil || count != 0 {
					t.Fatalf("partial journal: %d %v", count, err)
				}
			}
			if err := finish(); err != nil {
				t.Fatal(err)
			}
			if err := finish(); err != nil {
				t.Fatalf("idempotent finish: %v", err)
			}
			current, err := repository.GetTask(ctx, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if current.Status != tc.status || current.CompletionBasis != tc.basis || nullableValue(current.Error) != tc.reason {
				t.Fatalf("settled Task=%+v error=%q", current, nullableValue(current.Error))
			}
			var payload string
			if err := repository.db.QueryRow(`SELECT payload_json FROM event_journal WHERE event_id=?`, taskEvent.ID).Scan(&payload); err != nil {
				t.Fatal(err)
			}
			var fields map[string]string
			if err := json.Unmarshal([]byte(payload), &fields); err != nil || fields["intent"] != string(tc.intent) || fields["completion_basis"] != string(tc.basis) || fields["existing"] != "preserved" {
				t.Fatalf("settlement journal %s err=%v", payload, err)
			}
			var count int
			if err := repository.db.QueryRow(`SELECT COUNT(*) FROM event_journal WHERE aggregate_id=? AND event_type='task.settled'`, task.ID).Scan(&count); err != nil || count != 1 {
				t.Fatalf("duplicate settlement: %d %v", count, err)
			}
		})
	}
}
