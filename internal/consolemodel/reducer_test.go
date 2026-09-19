package consolemodel

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

func workerEvent(sequence, generation int64, workerID string, status domain.WorkerStatus, heartbeat, lease time.Time) openapi.JournalEventReadModel {
	return openapi.JournalEventReadModel{
		Sequence: sequence, ID: "event-worker", AggregateType: "worker_instance",
		AggregateID: workerID, EventType: "worker.heartbeat",
		Worker: &openapi.WorkerReadModel{WorkerInstanceID: workerID, AgentID: "quote",
			Generation: generation, Status: status, LastHeartbeatAt: heartbeat, LeaseUntil: lease},
	}
}

func generation(value int64) *int64 { return &value }

func runEvent(sequence int64, runID, workerID string, workerGeneration int64, status domain.RunAttemptStatus) openapi.JournalEventReadModel {
	now := time.Date(2026, 9, 20, 8, 0, 0, int(sequence), time.UTC)
	return openapi.JournalEventReadModel{Sequence: sequence, ID: "event-" + runID,
		AggregateType: "run_attempt", AggregateID: runID, EventType: "run_attempt.updated",
		Run: &openapi.RunAttemptReadModel{ID: runID, TaskID: "task-" + runID, AgentID: "quote",
			Version: 1, Status: status, WorkerInstanceID: workerID, WorkerGeneration: generation(workerGeneration),
			TurnResultState: "not_recorded", StartedAt: now, UpdatedAt: now}}
}

func TestReducerRejectsGenerationRollbackAndAdvancesIgnoredCursor(t *testing.T) {
	now := time.Date(2026, 9, 14, 16, 0, 0, 0, time.UTC)
	reducer, err := New(consoleapi.AttachResponse{AgentID: "quote", WorkerInstanceID: "worker-48",
		Generation: 48, WorkerStatus: domain.WorkerStatusOnline, LastHeartbeatAt: now,
		LeaseUntil: now.Add(time.Minute), SnapshotSequence: 1204})
	if err != nil {
		t.Fatal(err)
	}
	result, err := reducer.Apply(workerEvent(1205, 42, "worker-42", domain.WorkerStatusOffline, now, now))
	if err != nil {
		t.Fatal(err)
	}
	state := reducer.Snapshot()
	if !result.CursorAdvanced || result.Timeline != nil || reducer.Cursor() != 1205 ||
		state.Generation != 48 || state.WorkerInstanceID != "worker-48" || state.WorkerStatus != domain.WorkerStatusOnline {
		t.Fatalf("old generation changed current state: result=%+v cursor=%d state=%+v", result, reducer.Cursor(), state)
	}
}

func TestReducerHandlesDuplicateBackwardAndSameGenerationConflict(t *testing.T) {
	now := time.Now().UTC()
	reducer, err := New(consoleapi.AttachResponse{AgentID: "quote", WorkerInstanceID: "worker-current",
		Generation: 8, WorkerStatus: domain.WorkerStatusOnline, LeaseUntil: now.Add(time.Minute), SnapshotSequence: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := reducer.Apply(workerEvent(10, 8, "worker-current", domain.WorkerStatusOnline, now, now.Add(time.Minute))); err != nil || result.CursorAdvanced {
		t.Fatalf("duplicate result=%+v err=%v", result, err)
	}
	if _, err := reducer.Apply(workerEvent(9, 8, "worker-current", domain.WorkerStatusOnline, now, now.Add(time.Minute))); err == nil {
		t.Fatal("backward event was accepted")
	}
	result, err := reducer.Apply(workerEvent(11, 8, "worker-conflict", domain.WorkerStatusDraining, now, now.Add(time.Minute)))
	if err != nil || !result.CursorAdvanced || result.Timeline != nil {
		t.Fatalf("same-generation conflict result=%+v err=%v", result, err)
	}
	if state := reducer.Snapshot(); state.WorkerInstanceID != "worker-current" || state.WorkerStatus != domain.WorkerStatusOnline {
		t.Fatalf("same-generation conflict changed state: %+v", state)
	}
}

func TestReducerCoalescesHeartbeatBurstAndReportsMeaningfulTransitions(t *testing.T) {
	now := time.Now().UTC()
	reducer, err := New(consoleapi.AttachResponse{AgentID: "quote", WorkerInstanceID: "worker-2",
		Generation: 2, WorkerStatus: domain.WorkerStatusOnline, LeaseUntil: now.Add(time.Minute), SnapshotSequence: 20})
	if err != nil {
		t.Fatal(err)
	}
	for sequence := int64(21); sequence <= 40; sequence++ {
		result, applyErr := reducer.Apply(workerEvent(sequence, 2, "worker-2", domain.WorkerStatusOnline,
			now.Add(time.Duration(sequence)*time.Second), now.Add(2*time.Minute)))
		if applyErr != nil || result.Timeline != nil {
			t.Fatalf("heartbeat %d produced Timeline=%+v err=%v", sequence, result.Timeline, applyErr)
		}
	}
	transition, err := reducer.Apply(workerEvent(41, 2, "worker-2", domain.WorkerStatusDraining, now.Add(time.Minute), now.Add(3*time.Minute)))
	if err != nil || transition.Timeline == nil {
		t.Fatalf("draining transition=%+v err=%v", transition, err)
	}
	replacement, err := reducer.Apply(workerEvent(42, 3, "worker-3", domain.WorkerStatusOnline, now.Add(2*time.Minute), now.Add(4*time.Minute)))
	if err != nil || replacement.Timeline == nil {
		t.Fatalf("replacement=%+v err=%v", replacement, err)
	}
	state := reducer.Snapshot()
	if state.Generation != 3 || state.WorkerInstanceID != "worker-3" || state.WorkerStatus != domain.WorkerStatusOnline {
		t.Fatalf("replacement state=%+v", state)
	}
}

func TestReducerUpdatesBackendHealthFromCurrentWorkerHeartbeat(t *testing.T) {
	now := time.Now().UTC()
	reducer, err := New(consoleapi.AttachResponse{AgentID: "quote", WorkerInstanceID: "worker-2",
		Generation: 2, WorkerStatus: domain.WorkerStatusOnline,
		BackendHealth: map[string]openruntime.BackendHealth{"local": openruntime.BackendHealthy}, SnapshotSequence: 20})
	if err != nil {
		t.Fatal(err)
	}
	healthChanged := workerEvent(21, 2, "worker-2", domain.WorkerStatusOnline, now, now.Add(time.Minute))
	healthChanged.Worker.BackendHealth = map[string]openruntime.BackendHealth{"local": openruntime.BackendUnavailable}
	result, err := reducer.Apply(healthChanged)
	if err != nil || result.Timeline == nil || reducer.Snapshot().BackendHealth["local"] != openruntime.BackendUnavailable {
		t.Fatalf("Backend health transition result=%+v state=%+v err=%v", result, reducer.Snapshot(), err)
	}
	unchanged := workerEvent(22, 2, "worker-2", domain.WorkerStatusOnline, now.Add(time.Second), now.Add(2*time.Minute))
	unchanged.Worker.BackendHealth = map[string]openruntime.BackendHealth{"local": openruntime.BackendUnavailable}
	if result, err = reducer.Apply(unchanged); err != nil || result.Timeline != nil {
		t.Fatalf("unchanged Backend health produced Timeline: result=%+v err=%v", result, err)
	}
	invalid := workerEvent(23, 2, "worker-2", domain.WorkerStatusOnline, now.Add(2*time.Second), now.Add(3*time.Minute))
	invalid.Worker.BackendHealth = map[string]openruntime.BackendHealth{"local": "unknown"}
	if _, err = reducer.Apply(invalid); err == nil || reducer.Cursor() != 22 || len(reducer.Snapshot().BackendHealth) != 0 {
		t.Fatalf("invalid Backend health err=%v cursor=%d state=%+v", err, reducer.Cursor(), reducer.Snapshot())
	}
	missing := workerEvent(23, 2, "worker-2", domain.WorkerStatusOnline, now.Add(3*time.Second), now.Add(4*time.Minute))
	if result, err = reducer.Apply(missing); err != nil || !result.CursorAdvanced || len(reducer.Snapshot().BackendHealth) != 0 {
		t.Fatalf("nil Backend health retained old state: result=%+v state=%+v err=%v", result, reducer.Snapshot(), err)
	}
}

func TestReducerSwitchesAndClearsActiveRunFromSafeProjection(t *testing.T) {
	now := time.Now().UTC()
	reducer, err := New(consoleapi.AttachResponse{AgentID: "quote", WorkerInstanceID: "worker-1",
		Generation: 1, WorkerStatus: domain.WorkerStatusOnline, SnapshotSequence: 50})
	if err != nil {
		t.Fatal(err)
	}
	for sequence, run := range []openapi.RunAttemptReadModel{
		{ID: "run-1", TaskID: "task-1", AgentID: "quote", Version: 1, Status: domain.RunAttemptRunning, WorkerInstanceID: "worker-1", WorkerGeneration: generation(1), TurnResultState: "not_recorded", StartedAt: now, UpdatedAt: now},
		{ID: "run-2", TaskID: "task-2", AgentID: "quote", Version: 1, Status: domain.RunAttemptWaitingApproval, WorkerInstanceID: "worker-1", WorkerGeneration: generation(1), TurnResultState: "not_recorded", StartedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)},
	} {
		result, applyErr := reducer.Apply(openapi.JournalEventReadModel{Sequence: int64(51 + sequence), ID: run.ID,
			AggregateType: "run_attempt", AggregateID: run.ID, EventType: "run_attempt.updated", Run: &run})
		if applyErr != nil || result.Timeline == nil {
			t.Fatalf("active run event result=%+v err=%v", result, applyErr)
		}
	}
	if active := reducer.Snapshot().ActiveRun; active == nil || active.RunID != "run-2" || active.Status != domain.RunAttemptWaitingApproval {
		t.Fatalf("active run did not switch: %+v", active)
	}
	finished := openapi.RunAttemptReadModel{ID: "run-2", TaskID: "task-2", AgentID: "quote", Version: 2, Status: domain.RunAttemptSucceeded,
		WorkerInstanceID: "worker-1", WorkerGeneration: generation(1), TurnResultState: "not_recorded",
		StartedAt: now.Add(time.Second), UpdatedAt: now.Add(2 * time.Second)}
	if _, err := reducer.Apply(openapi.JournalEventReadModel{Sequence: 53, ID: "run-2-finished",
		AggregateType: "run_attempt", AggregateID: "run-2", EventType: "run_attempt.finished", Run: &finished}); err != nil {
		t.Fatal(err)
	}
	if active := reducer.Snapshot().ActiveRun; active != nil {
		t.Fatalf("terminal run remained active: %+v", active)
	}
}

func TestReducerFencesOldWorkerRunWhileAdvancingTimelineCursor(t *testing.T) {
	currentGeneration := int64(48)
	reducer, err := New(consoleapi.AttachResponse{AgentID: "quote", WorkerInstanceID: "worker-48",
		Generation: currentGeneration, WorkerStatus: domain.WorkerStatusOnline, SnapshotSequence: 100,
		ActiveRun: &consoleapi.RunSnapshot{RunID: "run-current", TaskID: "task-current",
			Status: domain.RunAttemptRunning, WorkerInstanceID: "worker-48", WorkerGeneration: &currentGeneration}})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []openapi.JournalEventReadModel{
		runEvent(101, "run-old-generation", "worker-42", 42, domain.RunAttemptRunning),
		runEvent(102, "run-same-generation-conflict", "worker-conflict", 48, domain.RunAttemptWaitingApproval),
	} {
		result, applyErr := reducer.Apply(event)
		if applyErr != nil || !result.CursorAdvanced || result.Timeline == nil {
			t.Fatalf("old Run result=%+v err=%v", result, applyErr)
		}
		active := reducer.Snapshot().ActiveRun
		if active == nil || active.RunID != "run-current" || active.WorkerInstanceID != "worker-48" {
			t.Fatalf("old Run replaced current ActiveRun: %+v", active)
		}
	}
	if reducer.Cursor() != 102 {
		t.Fatalf("ignored Run cursor=%d", reducer.Cursor())
	}
}

func TestReducerClearsWorkerScopedStateOnReplacementAndOffline(t *testing.T) {
	now := time.Now().UTC()
	for _, testCase := range []struct {
		name  string
		event openapi.JournalEventReadModel
	}{
		{name: "replacement", event: workerEvent(201, 49, "worker-49", domain.WorkerStatusOnline, now.Add(time.Minute), now.Add(3*time.Minute))},
		{name: "offline", event: workerEvent(201, 48, "worker-48", domain.WorkerStatusOffline, now.Add(time.Minute), now)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			currentGeneration := int64(48)
			reducer, err := New(consoleapi.AttachResponse{AgentID: "quote", Mode: consoleapi.ModeDiagnostic,
				WorkerInstanceID: "worker-48", Generation: currentGeneration, WorkerStatus: domain.WorkerStatusOnline,
				BackendHealth: map[string]openruntime.BackendHealth{"legacy": openruntime.BackendHealthy},
				Diagnostic:    &consoleapi.DiagnosticView{StartedAt: now.Add(-time.Hour), UpdatedAt: now},
				ActiveRun: &consoleapi.RunSnapshot{RunID: "run-48", TaskID: "task-48", Status: domain.RunAttemptRunning,
					WorkerInstanceID: "worker-48", WorkerGeneration: &currentGeneration}, SnapshotSequence: 200})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := reducer.Apply(testCase.event); err != nil {
				t.Fatal(err)
			}
			state := reducer.Snapshot()
			if len(state.BackendHealth) != 0 || state.Diagnostic != nil || state.ActiveRun != nil {
				t.Fatalf("%s retained stale Worker state: %+v", testCase.name, state)
			}
		})
	}
}

func TestReducerFailsClosedBeforeCursorAdvanceOnInvalidProjection(t *testing.T) {
	reducer, err := New(consoleapi.AttachResponse{AgentID: "quote", WorkerInstanceID: "worker-8",
		Generation: 8, WorkerStatus: domain.WorkerStatusOnline, SnapshotSequence: 10})
	if err != nil {
		t.Fatal(err)
	}
	invalidWorker := workerEvent(11, 8, "worker-8", "unknown", time.Time{}, time.Time{})
	if _, err := reducer.Apply(invalidWorker); err == nil || reducer.Cursor() != 10 {
		t.Fatalf("invalid Worker projection err=%v cursor=%d", err, reducer.Cursor())
	}
	invalidRun := runEvent(11, "run-invalid", "worker-8", 8, domain.RunAttemptRunning)
	invalidRun.Run.WorkerGeneration = nil
	if _, err := reducer.Apply(invalidRun); err == nil || reducer.Cursor() != 10 {
		t.Fatalf("invalid Run projection err=%v cursor=%d", err, reducer.Cursor())
	}
	badGeneration := int64(7)
	if _, err := New(consoleapi.AttachResponse{AgentID: "quote", WorkerInstanceID: "worker-8",
		Generation: 8, WorkerStatus: domain.WorkerStatusOnline,
		ActiveRun: &consoleapi.RunSnapshot{RunID: "run-old", TaskID: "task-old", Status: domain.RunAttemptRunning,
			WorkerInstanceID: "worker-7", WorkerGeneration: &badGeneration}}); err == nil {
		t.Fatal("invalid Attach Run identity was accepted")
	}
}

func TestReducerTimelineCanOnlyContainSafeOutputProjection(t *testing.T) {
	reducer, err := New(consoleapi.AttachResponse{AgentID: "quote", Mode: consoleapi.ModeDiagnostic,
		WorkerStatus: domain.WorkerStatusOffline, SnapshotSequence: 3})
	if err != nil {
		t.Fatal(err)
	}
	result, err := reducer.Apply(openapi.JournalEventReadModel{Sequence: 4, ID: "runtime-safe",
		AggregateType: "runtime", AggregateID: "run-1", EventType: "runtime.output",
		Output: &openapi.SafeOutputReadModel{Text: "token=[REDACTED]", Diagnostic: "stderr=[REDACTED]",
			HasOutput: true, HasError: true}})
	if err != nil || result.Timeline == nil {
		t.Fatalf("safe output result=%+v err=%v", result, err)
	}
	encoded, err := json.Marshal(result.Timeline)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"raw-secret", "raw_stderr", "hidden_reasoning", "environment"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("Timeline leaked %q: %s", forbidden, encoded)
		}
	}
}
