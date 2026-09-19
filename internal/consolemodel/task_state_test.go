package consolemodel

import (
	"fmt"
	"strings"
	"testing"
	"time"

	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

var taskTestTime = time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)

func taskModel(id string, version int64, status domain.TaskStatus) openapi.ConsoleTaskReadModel {
	model := openapi.ConsoleTaskReadModel{
		TaskID: id, Version: version, AgentID: "quote", Status: status, Content: "safe " + id,
		OutcomeState: "pending", CreatedAt: taskTestTime.Format(time.RFC3339Nano),
		UpdatedAt: taskTestTime.Add(time.Duration(version) * time.Second).Format(time.RFC3339Nano),
	}
	if taskStatusTerminal(status) {
		model.OutcomeState = "not_recorded"
	}
	return model
}

func taskEventModel(sequence int64, task openapi.ConsoleTaskReadModel) openapi.JournalEventReadModel {
	return openapi.JournalEventReadModel{Sequence: sequence, ID: fmt.Sprintf("event-task-%d", sequence),
		AggregateType: "task", AggregateID: task.TaskID, EventType: "task.updated", Task: &task}
}

func runModel(id, taskID, workerID string, generation, version int64, status domain.RunAttemptStatus) openapi.RunAttemptReadModel {
	started := taskTestTime.Add(time.Duration(version) * time.Minute)
	return openapi.RunAttemptReadModel{ID: id, TaskID: taskID, AgentID: "quote", Version: version, Status: status,
		WorkerInstanceID: workerID, WorkerGeneration: &generation, TurnResultState: "not_recorded",
		StartedAt: started, UpdatedAt: started.Add(time.Second)}
}

func TestTaskReducerFocusSourcesAndDispatchEventOrdering(t *testing.T) {
	suggested := openapi.ConsoleTaskOption{TaskID: "task-suggested", Version: 2, Status: domain.TaskStatusRunning,
		Summary: "suggested", UpdatedAt: taskTestTime.Format(time.RFC3339Nano)}
	reducer, err := New(consoleapi.AttachResponse{AgentID: "quote", Mode: consoleapi.ModeNormal,
		WorkerStatus: domain.WorkerStatusOffline, SuggestedTask: &suggested, SnapshotSequence: 10})
	if err != nil {
		t.Fatal(err)
	}
	state := reducer.State()
	if state.FocusedTask == nil || state.FocusedTask.TaskID != "task-suggested" || state.FocusSource != FocusAttachSuggestion {
		t.Fatalf("Attach focus=%+v source=%q", state.FocusedTask, state.FocusSource)
	}

	manual := taskModel("task-manual", 3, domain.TaskStatusWaitingInput)
	if err := reducer.ApplyTaskSnapshot(openapi.ConsoleTaskSnapshot{Task: manual, SnapshotSequence: 10}, FocusManual); err != nil {
		t.Fatal(err)
	}
	if state = reducer.State(); state.FocusedTask == nil || state.FocusedTask.TaskID != "task-manual" || state.FocusSource != FocusManual {
		t.Fatalf("manual focus=%+v source=%q", state.FocusedTask, state.FocusSource)
	}

	// Event first and control response second is a valid race. Dispatch must focus
	// the response Task without regressing the newer event state.
	dispatched := taskModel("task-dispatched", 2, domain.TaskStatusRunning)
	if _, err := reducer.Apply(taskEventModel(11, dispatched)); err != nil {
		t.Fatal(err)
	}
	if err := reducer.ApplyControlTask(ControlTaskUpdate{AgentID: "quote", TaskID: "task-dispatched",
		Version: 1, Status: domain.TaskStatusQueued, Focus: true}); err != nil {
		t.Fatal(err)
	}
	state = reducer.State()
	if state.FocusedTask == nil || state.FocusedTask.TaskID != "task-dispatched" ||
		state.FocusedTask.Version != 2 || state.FocusedTask.Status != domain.TaskStatusRunning || state.FocusSource != FocusDispatch {
		t.Fatalf("dispatch/event ordering regressed focus: %+v source=%q", state.FocusedTask, state.FocusSource)
	}
	if err := reducer.FocusTask("task-manual"); err != nil {
		t.Fatal(err)
	}
	if reducer.State().FocusedTask.TaskID != "task-manual" {
		t.Fatal("explicit manual selection did not replace dispatch focus")
	}
}

func TestTaskReducerVersionTerminalAndCursorMatrix(t *testing.T) {
	reducer, err := New(consoleapi.AttachResponse{AgentID: "quote", Mode: consoleapi.ModeNormal,
		WorkerStatus: domain.WorkerStatusOffline, SnapshotSequence: 20})
	if err != nil {
		t.Fatal(err)
	}
	if err := reducer.ApplyControlTask(ControlTaskUpdate{AgentID: "quote", TaskID: "task-main",
		Version: 1, Status: domain.TaskStatusQueued, Focus: true}); err != nil {
		t.Fatal(err)
	}
	statuses := []domain.TaskStatus{domain.TaskStatusQueued, domain.TaskStatusRunning,
		domain.TaskStatusWaitingInput, domain.TaskStatusRunning}
	for index, status := range statuses {
		result, applyErr := reducer.Apply(taskEventModel(int64(21+index), taskModel("task-main", int64(index+1), status)))
		if applyErr != nil || !result.CursorAdvanced || result.Timeline == nil {
			t.Fatalf("transition %d status=%s result=%+v err=%v", index, status, result, applyErr)
		}
	}
	current := taskModel("task-main", 4, domain.TaskStatusRunning)
	beforeTimeline := len(reducer.State().Timeline)
	result, err := reducer.Apply(taskEventModel(25, current))
	if err != nil || !result.CursorAdvanced || result.Timeline != nil || len(reducer.State().Timeline) != beforeTimeline {
		t.Fatalf("same-version duplicate result=%+v err=%v", result, err)
	}
	result, err = reducer.Apply(taskEventModel(26, taskModel("task-main", 2, domain.TaskStatusWaitingInput)))
	if err != nil || !result.CursorAdvanced || result.Timeline != nil || reducer.State().ActiveTasks[0].Version != 4 {
		t.Fatalf("lower-version event result=%+v err=%v state=%+v", result, err, reducer.State())
	}
	conflict := taskModel("task-main", 4, domain.TaskStatusCancelRequested)
	if _, err = reducer.Apply(taskEventModel(27, conflict)); err == nil || reducer.Cursor() != 26 {
		t.Fatalf("same-version conflict err=%v cursor=%d", err, reducer.Cursor())
	}

	terminal := taskModel("task-main", 5, domain.TaskStatusSucceeded)
	reply := "safe final answer"
	terminal.Result, terminal.OutcomeState = &reply, "available"
	if _, err = reducer.Apply(taskEventModel(27, terminal)); err != nil {
		t.Fatal(err)
	}
	state := reducer.State()
	if len(state.ActiveTasks) != 0 || len(state.RecentTasks) != 1 || state.FocusedTask == nil ||
		state.FocusedTask.Status != domain.TaskStatusSucceeded || state.FocusedTask.Detail == nil ||
		state.FocusedTask.Detail.Result == nil || *state.FocusedTask.Detail.Result != reply {
		t.Fatalf("terminal state=%+v", state)
	}
	overwrite := taskModel("task-main", 6, domain.TaskStatusFailed)
	if _, err = reducer.Apply(taskEventModel(28, overwrite)); err == nil || reducer.Cursor() != 27 {
		t.Fatalf("terminal overwrite err=%v cursor=%d", err, reducer.Cursor())
	}
}

func TestTaskReducerPreservesAuthoritativeTaskOptionOrder(t *testing.T) {
	reducer, err := New(consoleapi.AttachResponse{AgentID: "quote", Mode: consoleapi.ModeNormal,
		WorkerStatus: domain.WorkerStatusOffline})
	if err != nil {
		t.Fatal(err)
	}
	options := []openapi.ConsoleTaskOption{
		{TaskID: "task-newest", Version: 3, Status: domain.TaskStatusRunning, Summary: "newest",
			UpdatedAt: taskTestTime.Add(3 * time.Minute).Format(time.RFC3339Nano)},
		{TaskID: "task-middle", Version: 2, Status: domain.TaskStatusWaitingInput, Summary: "middle",
			UpdatedAt: taskTestTime.Add(2 * time.Minute).Format(time.RFC3339Nano)},
		{TaskID: "task-oldest", Version: 1, Status: domain.TaskStatusQueued, Summary: "oldest",
			UpdatedAt: taskTestTime.Add(time.Minute).Format(time.RFC3339Nano)},
	}
	if err := reducer.ReplaceTaskOptions(options); err != nil {
		t.Fatal(err)
	}
	active := reducer.State().ActiveTasks
	if len(active) != 3 || active[0].TaskID != "task-newest" || active[1].TaskID != "task-middle" ||
		active[2].TaskID != "task-oldest" {
		t.Fatalf("Task option order=%+v", active)
	}
}

func TestTaskReducerAppliesMailboxMessageApprovalAndRuntimeReply(t *testing.T) {
	reducer, err := New(consoleapi.AttachResponse{AgentID: "quote", Mode: consoleapi.ModeNormal,
		WorkerInstanceID: "worker-48", Generation: 48, WorkerStatus: domain.WorkerStatusOnline,
		SnapshotSequence: 30})
	if err != nil {
		t.Fatal(err)
	}
	queued := taskModel("task-main", 1, domain.TaskStatusQueued)
	mailbox := openapi.ConsoleMailboxReadModel{MailboxItemID: "mailbox-1", State: domain.MailboxStatePending,
		CreatedAt: taskTestTime}
	event := openapi.JournalEventReadModel{Sequence: 31, ID: "event-mailbox", AggregateType: "mailbox_item",
		AggregateID: mailbox.MailboxItemID, EventType: "mailbox.pending", Task: &queued, Mailbox: &mailbox}
	if _, err := reducer.Apply(event); err != nil {
		t.Fatal(err)
	}
	running := taskModel("task-main", 2, domain.TaskStatusRunning)
	message := openapi.ConsoleMessageReadModel{MessageID: "message-1", Version: 1, Sequence: 1,
		Kind: domain.MessageKindInstruction, Content: "safe instruction", CreatedAt: taskTestTime.Format(time.RFC3339Nano)}
	if _, err := reducer.Apply(openapi.JournalEventReadModel{Sequence: 32, ID: "event-message",
		AggregateType: "message", AggregateID: message.MessageID, EventType: "message.created",
		Task: &running, Message: &message}); err != nil {
		t.Fatal(err)
	}
	waiting := taskModel("task-main", 3, domain.TaskStatusWaitingApproval)
	approval := openapi.ConsoleApprovalReadModel{ApprovalRequestID: "approval-1", Mode: domain.ApprovalModeNative,
		State: domain.ApprovalRequestPending, TargetRunID: "run-1", ExpectedRunVersion: 1,
		ExpiresAt: taskTestTime.Add(time.Hour), CreatedAt: taskTestTime.Add(time.Minute)}
	if _, err := reducer.Apply(openapi.JournalEventReadModel{Sequence: 33, ID: "event-approval",
		AggregateType: "approval_request", AggregateID: approval.ApprovalRequestID, EventType: "approval.requested",
		Task: &waiting, Approval: &approval}); err != nil {
		t.Fatal(err)
	}
	run := runModel("run-1", "task-main", "worker-48", 48, 1, domain.RunAttemptWaitingApproval)
	if _, err := reducer.Apply(openapi.JournalEventReadModel{Sequence: 34, ID: "event-run-1",
		AggregateType: "run_attempt", AggregateID: run.ID, EventType: "run_attempt.waiting_approval", Run: &run}); err != nil {
		t.Fatal(err)
	}
	state := reducer.State()
	record := state.ActiveTasks[0]
	if record.WorkDelivery == nil || record.LatestMessage == nil || record.PendingApproval == nil ||
		record.LatestRun == nil || record.LatestRun.ID != "run-1" {
		t.Fatalf("composite Task state=%+v", record)
	}

	terminalRun := runModel("run-1", "task-main", "worker-48", 48, 2, domain.RunAttemptSucceeded)
	terminalRun.StartedAt = run.StartedAt
	known := true
	terminalRun.TurnResultState = "available"
	terminalRun.TurnResult = &openapi.TurnResultReadModel{RuntimeStatus: openruntime.TurnResultSucceeded,
		Body: "safe runtime reply", RuntimeSideEffectsKnown: &known,
		SideEffectsSource: "runtime_reported", BusinessVerificationSource: "not_recorded"}
	if _, err := reducer.Apply(openapi.JournalEventReadModel{Sequence: 35, ID: "event-run-terminal",
		AggregateType: "run_attempt", AggregateID: terminalRun.ID, EventType: "run_attempt.succeeded", Run: &terminalRun}); err != nil {
		t.Fatal(err)
	}
	state = reducer.State()
	if state.ActiveTasks[0].LatestRun == nil || state.ActiveTasks[0].LatestRun.TurnResult == nil ||
		state.ActiveTasks[0].LatestRun.TurnResult.Body != "safe runtime reply" || state.Console.ActiveRun != nil {
		t.Fatalf("Runtime reply was not retained separately: %+v", state)
	}
}

func TestTaskReducerFencesRunAndClearsOnlyUnprovenWorkerState(t *testing.T) {
	reducer, err := New(consoleapi.AttachResponse{AgentID: "quote", Mode: consoleapi.ModeNormal,
		WorkerInstanceID: "worker-48", Generation: 48, WorkerStatus: domain.WorkerStatusOnline,
		BackendHealth: map[string]openruntime.BackendHealth{"local": openruntime.BackendHealthy}, SnapshotSequence: 40})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reducer.Apply(taskEventModel(41, taskModel("task-main", 1, domain.TaskStatusRunning))); err != nil {
		t.Fatal(err)
	}
	old := runModel("run-old", "task-main", "worker-42", 42, 1, domain.RunAttemptRunning)
	old.StartedAt = taskTestTime
	old.UpdatedAt = taskTestTime.Add(time.Second)
	result, err := reducer.Apply(openapi.JournalEventReadModel{Sequence: 42, ID: "event-run-old",
		AggregateType: "run_attempt", AggregateID: old.ID, EventType: "run_attempt.running", Run: &old})
	if err != nil || result.Timeline == nil || reducer.State().ActiveTasks[0].LatestRun != nil {
		t.Fatalf("old Run result=%+v err=%v state=%+v", result, err, reducer.State())
	}
	current := runModel("run-current", "task-main", "worker-48", 48, 1, domain.RunAttemptRunning)
	if _, err := reducer.Apply(openapi.JournalEventReadModel{Sequence: 43, ID: "event-run-current",
		AggregateType: "run_attempt", AggregateID: current.ID, EventType: "run_attempt.running", Run: &current}); err != nil {
		t.Fatal(err)
	}
	lateOld := runModel("run-earlier", "task-main", "worker-48", 48, 1, domain.RunAttemptRunning)
	lateOld.StartedAt = taskTestTime
	lateOld.UpdatedAt = taskTestTime.Add(time.Second)
	if result, err := reducer.Apply(openapi.JournalEventReadModel{Sequence: 44, ID: "event-run-earlier",
		AggregateType: "run_attempt", AggregateID: lateOld.ID, EventType: "run_attempt.running", Run: &lateOld}); err != nil || result.Timeline == nil || reducer.State().Console.ActiveRun == nil ||
		reducer.State().Console.ActiveRun.RunID != "run-current" || reducer.State().ActiveTasks[0].LatestRun.ID != "run-current" {
		t.Fatalf("late old Run changed current state: result=%+v err=%v state=%+v", result, err, reducer.State())
	}
	terminal := runModel("run-terminal", "task-main", "worker-48", 48, 1, domain.RunAttemptSucceeded)
	terminal.StartedAt = current.StartedAt.Add(time.Minute)
	terminal.UpdatedAt = terminal.StartedAt.Add(time.Second)
	terminal.TurnResultState = "empty"
	terminal.TurnResult = &openapi.TurnResultReadModel{RuntimeStatus: openruntime.TurnResultSucceeded,
		SideEffectsSource: "not_recorded", BusinessVerificationSource: "not_recorded"}
	if _, err := reducer.Apply(openapi.JournalEventReadModel{Sequence: 45, ID: "event-run-persisted",
		AggregateType: "run_attempt", AggregateID: terminal.ID, EventType: "run_attempt.succeeded", Run: &terminal}); err != nil {
		t.Fatal(err)
	}
	if reducer.State().Console.ActiveRun != nil {
		t.Fatalf("newer terminal Run left stale ActiveRun: %+v", reducer.State().Console.ActiveRun)
	}
	replacement := workerEvent(46, 49, "worker-49", domain.WorkerStatusOnline, taskTestTime, taskTestTime.Add(time.Minute))
	replacement.ID = "event-worker-replacement"
	if _, err := reducer.Apply(replacement); err != nil {
		t.Fatal(err)
	}
	state := reducer.State()
	if state.Console.ActiveRun != nil || len(state.Console.BackendHealth) != 0 {
		t.Fatalf("replacement retained unproven Worker state: %+v", state)
	}
	if run := state.ActiveTasks[0].LatestRun; run == nil || run.ID != "run-terminal" || run.TurnResultState != "empty" {
		t.Fatalf("replacement deleted persisted Runtime reply: %+v", run)
	}
}

func TestTaskSnapshotFencesActiveRunButRetainsTerminalReply(t *testing.T) {
	newReducer := func(t *testing.T) *Reducer {
		t.Helper()
		reducer, err := New(consoleapi.AttachResponse{AgentID: "quote", Mode: consoleapi.ModeNormal,
			WorkerInstanceID: "worker-49", Generation: 49, WorkerStatus: domain.WorkerStatusOnline,
			SnapshotSequence: 47})
		if err != nil {
			t.Fatal(err)
		}
		return reducer
	}

	t.Run("active Run and native Approval from old Worker are cleared", func(t *testing.T) {
		reducer := newReducer(t)
		task := taskModel("task-old-active", 3, domain.TaskStatusWaitingApproval)
		run := runModel("run-old-active", task.TaskID, "worker-48", 48, 1, domain.RunAttemptWaitingApproval)
		approval := openapi.ConsoleApprovalReadModel{ApprovalRequestID: "approval-old", Mode: domain.ApprovalModeNative,
			State: domain.ApprovalRequestPending, TargetRunID: run.ID, ExpectedRunVersion: run.Version,
			ExpiresAt: taskTestTime.Add(time.Hour), CreatedAt: taskTestTime.Add(time.Minute)}
		if err := reducer.ApplyTaskSnapshot(openapi.ConsoleTaskSnapshot{Task: task, LatestRun: &run,
			PendingApproval: &approval, SnapshotSequence: 47}, FocusManual); err != nil {
			t.Fatal(err)
		}
		state := reducer.State()
		if state.FocusedTask == nil || state.FocusedTask.LatestRun != nil || state.FocusedTask.PendingApproval != nil {
			t.Fatalf("old Worker active state survived Task snapshot fencing: %+v", state.FocusedTask)
		}
	})

	t.Run("terminal Run reply from old Worker is retained", func(t *testing.T) {
		reducer := newReducer(t)
		task := taskModel("task-old-terminal", 4, domain.TaskStatusSucceeded)
		reply := "safe persisted reply"
		task.Result, task.OutcomeState = &reply, "available"
		run := runModel("run-old-terminal", task.TaskID, "worker-48", 48, 2, domain.RunAttemptSucceeded)
		known := true
		run.TurnResultState = "available"
		run.TurnResult = &openapi.TurnResultReadModel{RuntimeStatus: openruntime.TurnResultSucceeded,
			Body: reply, RuntimeSideEffectsKnown: &known, SideEffectsSource: "runtime_reported",
			BusinessVerificationSource: "not_recorded"}
		if err := reducer.ApplyTaskSnapshot(openapi.ConsoleTaskSnapshot{Task: task, LatestRun: &run,
			SnapshotSequence: 47}, FocusManual); err != nil {
			t.Fatal(err)
		}
		state := reducer.State()
		if state.FocusedTask == nil || state.FocusedTask.LatestRun == nil ||
			state.FocusedTask.LatestRun.ID != run.ID || state.FocusedTask.LatestRun.TurnResult == nil ||
			state.FocusedTask.LatestRun.TurnResult.Body != reply {
			t.Fatalf("persisted terminal reply was not retained: %+v", state.FocusedTask)
		}
	})
}

func TestTaskReducerRejectsStaleStreamAndNormalDiagnosticWithoutAck(t *testing.T) {
	reducer, err := New(consoleapi.AttachResponse{AgentID: "quote", Mode: consoleapi.ModeNormal,
		WorkerStatus: domain.WorkerStatusOffline, SnapshotSequence: 50})
	if err != nil {
		t.Fatal(err)
	}
	oldEpoch := reducer.StreamEpoch()
	newEpoch, err := reducer.BeginStream(consoleapi.ModeDiagnostic)
	if err != nil {
		t.Fatal(err)
	}
	valid := taskEventModel(51, taskModel("task-main", 1, domain.TaskStatusQueued))
	if _, err := reducer.ApplyForStream(oldEpoch, valid); err == nil || reducer.Cursor() != 50 {
		t.Fatalf("old stream event err=%v cursor=%d", err, reducer.Cursor())
	}
	if err := reducer.ApplySnapshotForStream(newEpoch, consoleapi.AttachResponse{AgentID: "quote", Mode: consoleapi.ModeDiagnostic,
		WorkerStatus: domain.WorkerStatusOffline, SnapshotSequence: 50}); err != nil {
		t.Fatal(err)
	}
	if err := reducer.SetConnection(oldEpoch, ConnectionConnected); err == nil {
		t.Fatal("old stream connection state was accepted")
	}
	if err := reducer.SetConnection(newEpoch, ConnectionConnected); err != nil || reducer.State().Connection != ConnectionConnected {
		t.Fatalf("new stream connection err=%v state=%+v", err, reducer.State())
	}

	normal, err := New(consoleapi.AttachResponse{AgentID: "quote", Mode: consoleapi.ModeNormal,
		WorkerStatus: domain.WorkerStatusOffline, SnapshotSequence: 60})
	if err != nil {
		t.Fatal(err)
	}
	diagnostic := openapi.JournalEventReadModel{Sequence: 61, ID: "event-runtime", AggregateType: "runtime",
		AggregateID: "run-1", EventType: "runtime.output",
		Output: &openapi.SafeOutputReadModel{Text: "safe", Diagnostic: "redacted diagnostic", HasOutput: true, HasError: true}}
	if _, err := normal.Apply(diagnostic); err == nil || normal.Cursor() != 60 {
		t.Fatalf("Normal accepted Diagnostic output err=%v cursor=%d", err, normal.Cursor())
	}
}

func TestTaskReducerRejectedSnapshotIsAtomic(t *testing.T) {
	reducer, err := New(consoleapi.AttachResponse{AgentID: "quote", Mode: consoleapi.ModeNormal,
		WorkerStatus: domain.WorkerStatusOffline, SnapshotSequence: 70})
	if err != nil {
		t.Fatal(err)
	}
	if err := reducer.ApplyControlTask(ControlTaskUpdate{AgentID: "quote", TaskID: "task-terminal",
		Version: 1, Status: domain.TaskStatusQueued, Focus: true}); err != nil {
		t.Fatal(err)
	}
	terminal := taskModel("task-terminal", 2, domain.TaskStatusSucceeded)
	if _, err := reducer.Apply(taskEventModel(71, terminal)); err != nil {
		t.Fatal(err)
	}
	conflict := openapi.ConsoleTaskOption{TaskID: "task-terminal", Version: 3, Status: domain.TaskStatusRunning,
		Summary: "conflict", UpdatedAt: taskTestTime.Add(time.Hour).Format(time.RFC3339Nano)}
	if err := reducer.ApplySnapshot(consoleapi.AttachResponse{AgentID: "quote", Mode: consoleapi.ModeNormal,
		WorkerStatus: domain.WorkerStatusOffline, SuggestedTask: &conflict, SnapshotSequence: 72}); err == nil {
		t.Fatal("conflicting snapshot was accepted")
	}
	state := reducer.State()
	if reducer.Cursor() != 71 || state.Console.SnapshotSequence != 70 || state.FocusedTask == nil ||
		state.FocusedTask.Version != 2 || state.FocusedTask.Status != domain.TaskStatusSucceeded {
		t.Fatalf("rejected snapshot partially mutated state: cursor=%d state=%+v", reducer.Cursor(), state)
	}
}

func TestTaskReducerRejectsInvalidSafeResultBeforeCursorAdvance(t *testing.T) {
	badAvailable := taskModel("task-bad-available", 1, domain.TaskStatusSucceeded)
	badAvailable.OutcomeState = "available"
	badTruncated := taskModel("task-bad-truncated", 1, domain.TaskStatusFailed)
	badTruncated.OutcomeState = "truncated"
	badTruncated.ResultTruncated = true
	plain := "missing marker"
	badTruncated.Result = &plain

	for name, task := range map[string]openapi.ConsoleTaskReadModel{
		"available without content": badAvailable,
		"truncated without marker":  badTruncated,
	} {
		t.Run(name, func(t *testing.T) {
			reducer, err := New(consoleapi.AttachResponse{AgentID: "quote", Mode: consoleapi.ModeNormal,
				WorkerStatus: domain.WorkerStatusOffline, SnapshotSequence: 80})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := reducer.Apply(taskEventModel(81, task)); err == nil || reducer.Cursor() != 80 {
				t.Fatalf("invalid Task outcome err=%v cursor=%d", err, reducer.Cursor())
			}
		})
	}

	reducer, err := New(consoleapi.AttachResponse{AgentID: "quote", Mode: consoleapi.ModeNormal,
		WorkerInstanceID: "worker-1", Generation: 1, WorkerStatus: domain.WorkerStatusOnline, SnapshotSequence: 90})
	if err != nil {
		t.Fatal(err)
	}
	run := runModel("run-bad", "task-bad", "worker-1", 1, 1, domain.RunAttemptSucceeded)
	run.TurnResultState = "truncated"
	run.TurnResult = &openapi.TurnResultReadModel{RuntimeStatus: openruntime.TurnResultSucceeded,
		Body: "missing marker", BodyTruncated: true,
		SideEffectsSource: "not_recorded", BusinessVerificationSource: "not_recorded"}
	if _, err := reducer.Apply(openapi.JournalEventReadModel{Sequence: 91, ID: "event-run-bad",
		AggregateType: "run_attempt", AggregateID: run.ID, EventType: "run_attempt.succeeded", Run: &run}); err == nil || reducer.Cursor() != 90 {
		t.Fatalf("invalid Runtime reply err=%v cursor=%d", err, reducer.Cursor())
	}
}

func TestTaskReducerCollectionsAndTimelineRemainBounded(t *testing.T) {
	suggested := openapi.ConsoleTaskOption{TaskID: "task-focus", Version: 1, Status: domain.TaskStatusQueued,
		Summary: "focus", UpdatedAt: taskTestTime.Format(time.RFC3339Nano)}
	reducer, err := New(consoleapi.AttachResponse{AgentID: "quote", Mode: consoleapi.ModeNormal,
		WorkerStatus: domain.WorkerStatusOffline, SuggestedTask: &suggested})
	if err != nil {
		t.Fatal(err)
	}
	sequence := int64(1)
	for index := 0; index < 320; index++ {
		task := taskModel(fmt.Sprintf("task-%03d", index), 1, domain.TaskStatusQueued)
		task.Content = strings.Repeat("safe-output-", 90)
		if _, err := reducer.Apply(taskEventModel(sequence, task)); err != nil {
			t.Fatalf("Task %d: %v", index, err)
		}
		sequence++
	}
	for index := 0; index < 320; index++ {
		event := openapi.JournalEventReadModel{Sequence: sequence, ID: fmt.Sprintf("event-output-%03d", index),
			AggregateType: "runtime", AggregateID: "run-output", EventType: "runtime.output",
			Output: &openapi.SafeOutputReadModel{Text: strings.Repeat("x", maxTimelineTextBytes*2), HasOutput: true}}
		if _, err := reducer.Apply(event); err != nil {
			t.Fatalf("output %d: %v", index, err)
		}
		sequence++
	}
	state := reducer.State()
	if len(state.ActiveTasks) > maxActiveTasks || len(state.RecentTasks) > maxRecentTasks ||
		state.TaskBytes > maxTrackedTaskBytes || len(state.Timeline) > maxTimelineEntries || state.TimelineBytes > maxTimelineBytes {
		t.Fatalf("unbounded state active=%d recent=%d task_bytes=%d timeline=%d/%d bytes",
			len(state.ActiveTasks), len(state.RecentTasks), state.TaskBytes, len(state.Timeline), state.TimelineBytes)
	}
	for _, item := range state.Timeline {
		if item.encodedSize > maxTimelineEntryBytes {
			t.Fatalf("Timeline item exceeds byte cap: %+v", item)
		}
		if item.Output != nil && (!item.Output.TextTruncated || len(item.Output.Text) > maxTimelineTextBytes) {
			t.Fatalf("Timeline output did not preserve local truncation metadata: %+v", item.Output)
		}
	}
	if state.FocusedTask == nil || state.FocusedTask.TaskID != "task-focus" {
		t.Fatalf("bounded eviction left dangling focus: %+v", state.FocusedTask)
	}
	state.FocusedTask.Summary = "mutated-copy"
	if reducer.State().FocusedTask.Summary == "mutated-copy" {
		t.Fatal("State returned an alias to reducer-owned Task state")
	}
}

func FuzzTaskReducerMalformedProjectionDoesNotAdvanceCursor(f *testing.F) {
	f.Add("queued", int64(1), "safe")
	f.Add("mystery", int64(-1), "\xff")
	f.Fuzz(func(t *testing.T, status string, version int64, content string) {
		reducer, err := New(consoleapi.AttachResponse{AgentID: "quote", Mode: consoleapi.ModeNormal,
			WorkerStatus: domain.WorkerStatusOffline, SnapshotSequence: 1})
		if err != nil {
			t.Fatal(err)
		}
		task := taskModel("task-fuzz", version, domain.TaskStatus(status))
		task.Content = content
		_, applyErr := reducer.Apply(taskEventModel(2, task))
		if applyErr != nil && reducer.Cursor() != 1 {
			t.Fatalf("rejected projection advanced cursor to %d", reducer.Cursor())
		}
		state := reducer.State()
		if len(state.ActiveTasks) > maxActiveTasks || len(state.RecentTasks) > maxRecentTasks ||
			len(state.Timeline) > maxTimelineEntries || state.TimelineBytes > maxTimelineBytes {
			t.Fatalf("fuzz input exceeded reducer bounds: %+v", state)
		}
	})
}
