package agy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

func TestParseStreamJSONNormalizesEventsAndResult(t *testing.T) {
	var events []openruntime.RuntimeEvent
	sink := openruntime.EventSinkFunc(func(_ context.Context, event openruntime.RuntimeEvent) error {
		events = append(events, event)
		return nil
	})
	result, err := parseStreamJSON(strings.NewReader(`{"type":"assistant","conversationId":"conv-1","text":"hello"}
{"type":"result","status":"succeeded","result":"done","usage":{"input":2}}`), sink)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != openruntime.TurnResultSucceeded || result.ProviderSessionID != "conv-1" || result.Result != "hellodone" || !result.SideEffectsKnown {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(events) != 2 || events[0].Type != "agy.assistant" || events[1].Type != "agy.result" {
		t.Fatalf("unexpected events: %+v", events)
	}
	if !json.Valid(events[0].Payload) {
		t.Fatal("normalized event payload is not JSON")
	}
}

func TestParseStreamJSONRejectsMalformedAndEmptyInput(t *testing.T) {
	for _, input := range []string{"", "not-json"} {
		if _, err := parseStreamJSON(strings.NewReader(input), nil); err == nil {
			t.Fatalf("input %q unexpectedly parsed", input)
		}
	}
}

func TestAgyBatchAdapterUsesDirectArgvConversationAndClassifiesExit(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "argv.log")
	scriptPath := filepath.Join(dir, "agy-fixture")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" > \"$AGY_TEST_ARGV_LOG\"\nprintf '%s\\n' '{\"type\":\"result\",\"conversationId\":\"conv-fixture\",\"status\":\"succeeded\",\"result\":\"fixture-ok\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	adapter := NewAdapterForTest(Config{Binary: scriptPath, WorkingDir: dir, Environment: []string{"AGY_TEST_ARGV_LOG=" + logPath}})
	spec := domain.ExecutionSpec{AdapterID: "agy-batch", BackendID: "agy-local", Model: "default",
		Reasoning: domain.ReasoningSpec{Mode: domain.ReasoningBackendDefault},
		Session:   domain.SessionSpec{Mode: domain.SessionModeResume, ContextID: "ctx"}, Timeout: time.Minute,
		BackendOptions: json.RawMessage(`{}`)}
	binding := &domain.SessionBinding{ID: "binding-1", ContextID: "ctx", AgentID: "quote", BackendID: "agy-local", ProviderSessionID: "conv-existing", State: domain.SessionBindingActive, Version: 1}
	handle, err := adapter.StartTurn(context.Background(), openruntime.TurnRequest{
		Task:           domain.Task{ID: "task-1", TargetAgentID: "quote", Content: "do work"},
		RunAttempt:     domain.RunAttempt{ID: "run-1", TaskID: "task-1", AgentID: "quote", Version: 1, Status: domain.RunAttemptStarting, LeaseUntil: time.Now().Add(time.Minute), ExecutionSpecVersion: 1, StartedAt: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now()},
		SessionBinding: binding, Execution: domain.ResolvedExecutionSpec{Version: 1, Spec: spec},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, waitErr := handle.Wait(context.Background())
	if waitErr != nil || result.Status != openruntime.TurnResultSucceeded || result.ProviderSessionID != "conv-fixture" {
		t.Fatalf("result=%+v err=%v", result, waitErr)
	}
	argv, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(argv), "--print --output-format stream-json --conversation conv-existing") {
		t.Fatalf("unexpected direct argv: %q", argv)
	}
}

func TestAgyBatchAdapterTimeoutAndNonZeroAreNotSuccess(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "agy-failure")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho '{\"type\":\"result\",\"status\":\"succeeded\",\"result\":\"misleading\"}'\necho 'secret stderr' >&2\nexit 7\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	adapter := NewAdapterForTest(Config{Binary: scriptPath, StderrLimit: 128})
	descriptor, _ := adapter.Descriptor(context.Background())
	handle, err := adapter.StartTurn(context.Background(), openruntime.TurnRequest{
		Task:       domain.Task{ID: "task-1", TargetAgentID: "quote", Content: "do work"},
		RunAttempt: domain.RunAttempt{ID: "run-1", TaskID: "task-1", AgentID: "quote", Version: 1, Status: domain.RunAttemptStarting, LeaseUntil: time.Now().Add(time.Minute), ExecutionSpecVersion: 1, StartedAt: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now()},
		Execution:  domain.ResolvedExecutionSpec{Version: 1, Spec: domain.ExecutionSpec{AdapterID: descriptor.AdapterID, BackendID: "agy", Model: "default", Reasoning: domain.ReasoningSpec{Mode: domain.ReasoningBackendDefault}, Session: domain.SessionSpec{Mode: domain.SessionModeNew, ContextID: "ctx"}, Timeout: time.Minute, BackendOptions: json.RawMessage(`{}`)}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, waitErr := handle.Wait(context.Background())
	if waitErr == nil || result.Status != openruntime.TurnResultUncertain || result.SideEffectsKnown {
		t.Fatalf("non-zero AGY result=%+v err=%v", result, waitErr)
	}
	if strings.Contains(result.Error, "secret stderr") {
		t.Fatal("stderr detail should not be copied into uncertain result")
	}
}
