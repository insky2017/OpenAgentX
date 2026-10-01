package agy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
	"openagentx/internal/safeoutput"
)

func TestParseStreamJSONNormalizesEventsAndResult(t *testing.T) {
	var events []openruntime.RuntimeEvent
	sink := openruntime.EventSinkFunc(func(_ context.Context, event openruntime.RuntimeEvent) error {
		events = append(events, event)
		return nil
	})
	result, err := parseStreamJSON(strings.NewReader(`{"event":"init","init":{"conversation_id":"conv-1"}}
{"event":"step_update","step_update":{"text":"hello"}}
{"event":"result","result":{"conversation_id":"conv-1","status":"SUCCESS","response":"done","usage":{"input":2}}}`), sink)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != openruntime.TurnResultSucceeded || result.ProviderSessionID != "conv-1" || result.Result != "done" || !result.FinalReply || result.SideEffectsKnown || result.RuntimeSideEffectsKnown != nil {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(events) != 3 || events[0].Type != "agy.init" || events[1].Type != "agy.step_update" || events[2].Type != "agy.result" {
		t.Fatalf("unexpected events: %+v", events)
	}
	if !json.Valid(events[0].Payload) {
		t.Fatal("normalized event payload is not JSON")
	}
	for _, event := range events {
		if strings.Contains(string(event.Payload), "conv-1") {
			t.Fatalf("public Runtime Event leaked raw AGY fields: %s", event.Payload)
		}
	}
	if !strings.Contains(string(events[1].Payload), `"text":"hello"`) || !strings.Contains(string(events[2].Payload), `"text":"done"`) {
		t.Fatalf("safe structured output was not projected: %+v", events)
	}
}

func TestParseStreamJSONRejectsMalformedAndEmptyInput(t *testing.T) {
	for _, input := range []string{"", "not-json", `{"event":"init","init":{"conversation_id":"session-1"}}`, `{"event":"step_update","step_update":{"status":"completed","text":"partial"}}`} {
		if _, err := parseStreamJSON(strings.NewReader(input), nil); err == nil {
			t.Fatalf("input %q unexpectedly parsed", input)
		}
	}
}

func TestParseStreamJSONCapturesNestedAgyError(t *testing.T) {
	result, err := parseStreamJSON(strings.NewReader(`{"event":"result","result":{"status":"ERROR","error":"invalid stream message"}}`), nil)
	if err != nil || result.Status != openruntime.TurnResultFailed || result.Error != "invalid stream message" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestParseStreamJSONPreservesExplicitUnknownSideEffects(t *testing.T) {
	result, err := parseStreamJSON(strings.NewReader(`{"event":"result","result":{"status":"SUCCESS","response":"done","side_effects_known":false}}`), nil)
	if err != nil || result.Status != openruntime.TurnResultSucceeded || result.SideEffectsKnown || result.RuntimeSideEffectsKnown == nil || *result.RuntimeSideEffectsKnown {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestParseStreamJSONDegradesUnknownEventType(t *testing.T) {
	var events []openruntime.RuntimeEvent
	sink := openruntime.EventSinkFunc(func(_ context.Context, event openruntime.RuntimeEvent) error {
		events = append(events, event)
		return nil
	})
	input := `{"event":"secret_session_dump","secret_session_dump":{"status":"running-token","text":"secret-value"}}
{"event":"result","result":{"status":"SUCCESS","response":"done"}}`
	result, err := parseStreamJSON(strings.NewReader(input), sink)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Type != "agy.event" || strings.Contains(string(events[0].Payload), "secret") || strings.Contains(string(events[0].Payload), "running-token") {
		t.Fatalf("unknown event was not safely degraded: %+v", events)
	}
	if result.Result != "done" || strings.Contains(result.Result, "secret-value") {
		t.Fatalf("unknown event polluted terminal result")
	}
}

func TestParseStreamJSONBoundsEventsAndFailsClosedOnSinkOrOutputLimit(t *testing.T) {
	var input strings.Builder
	for index := 0; index < openruntime.MaxPublicOutputEvents+20; index++ {
		input.WriteString(`{"event":"step_update","step_update":{"status":"running","text":"line"}}` + "\n")
	}
	input.WriteString(`{"event":"result","result":{"status":"SUCCESS","response":"done"}}` + "\n")
	emitted := 0
	result, err := parseStreamJSON(strings.NewReader(input.String()), openruntime.EventSinkFunc(func(context.Context, openruntime.RuntimeEvent) error {
		emitted++
		return nil
	}))
	if err != nil || result.Status != openruntime.TurnResultSucceeded || emitted != openruntime.MaxPublicOutputEvents {
		t.Fatalf("result status=%s err=%v emitted=%d", result.Status, err, emitted)
	}

	sinkFailure := errors.New("persist output")
	result, err = parseStreamJSON(strings.NewReader(`{"event":"step_update","step_update":{"status":"running","text":"safe"}}
{"event":"result","result":{"status":"SUCCESS","response":"done"}}`), openruntime.EventSinkFunc(func(context.Context, openruntime.RuntimeEvent) error {
		return sinkFailure
	}))
	if !errors.Is(err, sinkFailure) || result.Status != openruntime.TurnResultSucceeded || result.Result != "done" || result.FinalReply {
		t.Fatalf("sink failure result status=%s body=%q err=%v", result.Status, result.Result, err)
	}

	oversized := `{"event":"result","result":{"status":"SUCCESS","response":"` + strings.Repeat("x", maxCapturedOutput+1) + `"}}`
	result, err = parseStreamJSON(strings.NewReader(oversized), nil)
	if err == nil || !strings.Contains(err.Error(), "exceeded the configured limit") || !result.ResultTruncated ||
		len(result.Result) > safeoutput.MaxTextBytes+len(safeoutput.TruncatedMarker) {
		t.Fatalf("oversized result len=%d truncated=%t err=%v", len(result.Result), result.ResultTruncated, err)
	}
}

func TestParseStreamJSONRejectsContradictoryTerminal(t *testing.T) {
	result, err := parseStreamJSON(strings.NewReader(`{"event":"result","result":{"status":"SUCCESS","response":"misleading","error":"failed"}}`), nil)
	if err == nil || !strings.Contains(err.Error(), "contradicted") || result.Status != openruntime.TurnResultSucceeded {
		t.Fatalf("result status=%s err=%v", result.Status, err)
	}
}

func TestAgyBatchAdapterUsesDirectArgvConversationAndClassifiesExit(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "argv.log")
	stdinPath := filepath.Join(dir, "stdin.log")
	scriptPath := filepath.Join(dir, "agy-fixture")
	script := "#!/bin/sh\nprintf '%s\\0' \"$@\" > \"$AGY_TEST_ARGV_LOG\"\ncat > \"$AGY_TEST_STDIN_LOG\"\nprintf '%s\\n' '{\"event\":\"result\",\"result\":{\"conversation_id\":\"conv-fixture\",\"status\":\"SUCCESS\",\"response\":\"fixture-ok\"}}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	adapter := NewAdapterForTest(Config{Binary: scriptPath, Models: []string{"model-real"}, WorkingDir: dir, Environment: []string{"AGY_TEST_ARGV_LOG=" + logPath, "AGY_TEST_STDIN_LOG=" + stdinPath}})
	spec := domain.ExecutionSpec{AdapterID: "agy-batch", BackendID: "agy-local", Model: "model-real",
		Reasoning: domain.ReasoningSpec{Mode: domain.ReasoningEffort, Value: "high"},
		Session:   domain.SessionSpec{Mode: domain.SessionModeResume, ContextID: "ctx"}, Timeout: time.Minute,
		BackendOptions: json.RawMessage(`{}`)}
	binding := &domain.SessionBinding{ID: "binding-1", ContextID: "ctx", AgentID: "quote", BackendID: "agy-local", ProviderSessionID: "conv-existing", State: domain.SessionBindingActive, Version: 1}
	handle, err := adapter.StartTurn(context.Background(), openruntime.TurnRequest{
		Task:           domain.Task{ID: "task-1", TargetAgentID: "quote", Content: "do work"},
		Messages:       []domain.Message{{Content: "new detail"}},
		RunAttempt:     domain.RunAttempt{ID: "run-1", TaskID: "task-1", AgentID: "quote", Version: 1, Status: domain.RunAttemptStarting, LeaseUntil: time.Now().Add(time.Minute), ExecutionSpecVersion: 1, StartedAt: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now()},
		SessionBinding: binding, Execution: domain.ResolvedExecutionSpec{Version: 1, Spec: spec},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, waitErr := handle.Wait(context.Background())
	if waitErr != nil || result.Status != openruntime.TurnResultSucceeded || result.ProviderSessionID != "conv-fixture" || !result.FinalReply {
		t.Fatalf("result=%+v err=%v", result, waitErr)
	}
	argv, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := []string{"--input-format", "stream-json", "--output-format", "stream-json", "--print-timeout", "1m0s", "--dangerously-skip-permissions", "--model", "model-real", "--effort", "high", "--conversation", "conv-existing", "--print", ""}
	gotArgs := strings.Split(strings.TrimSuffix(string(argv), "\x00"), "\x00")
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("unexpected direct argv: got=%q want=%q", gotArgs, wantArgs)
	}
	stdin, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes := strings.Split(strings.TrimSuffix(string(stdin), "\n"), "\n"); len(bytes) != 1 {
		t.Fatalf("stdin must contain exactly one NDJSON message: %q", stdin)
	}
	var input struct {
		Event   string `json:"event"`
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(stdin, &input); err != nil || input.Event != "user" ||
		!strings.Contains(input.Message.Content, "Workspace root: \""+dir+"\"") ||
		!strings.Contains(input.Message.Content, "For every run_command call, set Cwd to the workspace root explicitly") ||
		!strings.HasSuffix(input.Message.Content, "Task:\ndo work\n\nFollow-up:\nnew detail") {
		t.Fatalf("unexpected stream input: input=%+v err=%v raw=%q", input, err, stdin)
	}
}

func TestAgyBatchAdapterUsesBackendDefaultWithoutModelOrEffort(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "argv.log")
	scriptPath := filepath.Join(dir, "agy-fixture")
	script := "#!/bin/sh\nprintf '%s\\0' \"$@\" > \"$AGY_TEST_ARGV_LOG\"\ncat >/dev/null\necho '{\"event\":\"result\",\"result\":{\"status\":\"SUCCESS\",\"response\":\"ok\"}}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	adapter := NewAdapterForTest(Config{Binary: scriptPath, Environment: []string{"AGY_TEST_ARGV_LOG=" + logPath}})
	handle, err := adapter.StartTurn(context.Background(), testTurnRequest(domain.ExecutionSpec{
		AdapterID: "agy-batch", BackendID: "agy-local", Model: "default",
		Reasoning: domain.ReasoningSpec{Mode: domain.ReasoningBackendDefault},
		Session:   domain.SessionSpec{Mode: domain.SessionModeNew}, Timeout: 90 * time.Second, BackendOptions: json.RawMessage(`{}`),
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := handle.Wait(context.Background()); err != nil || result.Status != openruntime.TurnResultSucceeded {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	argv, _ := os.ReadFile(logPath)
	gotArgs := strings.Split(strings.TrimSuffix(string(argv), "\x00"), "\x00")
	wantArgs := []string{"--input-format", "stream-json", "--output-format", "stream-json", "--print-timeout", "1m30s", "--dangerously-skip-permissions", "--print", ""}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("backend default argv=%q want=%q", gotArgs, wantArgs)
	}
}

func TestAgyBatchAdapterRejectsUnsupportedBudget(t *testing.T) {
	adapter := NewAdapterForTest(Config{Binary: "unused"})
	descriptor, err := adapter.Descriptor(context.Background())
	if err != nil || descriptor.Approval != openruntime.ApprovalPreflight {
		t.Fatalf("descriptor=%+v err=%v", descriptor, err)
	}
	for _, mode := range descriptor.ReasoningModes {
		if mode == domain.ReasoningBudgetTokens {
			t.Fatal("descriptor must not advertise AGY token budgets")
		}
	}
	for _, spec := range []domain.ExecutionSpec{
		{AdapterID: "agy-batch", BackendID: "agy", Model: "default", Reasoning: domain.ReasoningSpec{Mode: domain.ReasoningBudgetTokens, Value: "1000"}, Session: domain.SessionSpec{Mode: domain.SessionModeNew}, Timeout: time.Minute},
		{AdapterID: "agy-batch", BackendID: "agy", Model: "default", Reasoning: domain.ReasoningSpec{Mode: domain.ReasoningBackendDefault}, Session: domain.SessionSpec{Mode: domain.SessionModeNew}, Timeout: time.Minute, Budget: domain.ExecutionBudget{MaxTokens: 1000}},
		{AdapterID: "agy-batch", BackendID: "agy", Model: "default", Reasoning: domain.ReasoningSpec{Mode: domain.ReasoningBackendDefault}, Session: domain.SessionSpec{Mode: domain.SessionModeNew}, Timeout: time.Minute, Sandbox: "workspace-write"},
	} {
		if err := adapter.Validate(context.Background(), spec); !errors.Is(err, domain.ErrUnsupportedCapability) {
			t.Fatalf("budget spec error=%v", err)
		}
	}
}

func TestAgyBatchAdapterRejectsModelOutsideDescriptor(t *testing.T) {
	adapter := NewAdapterForTest(Config{Binary: "unused", Models: []string{"model-allowed"}})
	spec := validSpec()
	spec.Model = "model-other"
	if err := adapter.Validate(context.Background(), spec); !errors.Is(err, domain.ErrUnsupportedCapability) {
		t.Fatalf("unsupported model error=%v", err)
	}
}

func TestAgyBatchAdapterEnforcesExecutionTimeout(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "agy-slow")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nexec sleep 10\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	spec := validSpec()
	spec.Timeout = 30 * time.Millisecond
	adapter := NewAdapterForTest(Config{Binary: scriptPath})
	started := time.Now()
	handle, err := adapter.StartTurn(context.Background(), testTurnRequest(spec), nil)
	if err != nil {
		t.Fatal(err)
	}
	result, waitErr := handle.Wait(context.Background())
	if waitErr == nil || result.Status != openruntime.TurnResultUncertain || result.FinalReply || time.Since(started) > time.Second {
		t.Fatalf("result=%+v err=%v elapsed=%s", result, waitErr, time.Since(started))
	}
}

func TestAgyBatchAdapterTimeoutAndNonZeroAreNotSuccess(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "agy-failure")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\ncat >/dev/null\necho '{\"type\":\"result\",\"status\":\"succeeded\",\"result\":\"misleading\"}'\necho 'provider rejected request for do work token=top-secret via http://user:pass@server:1' >&2\nexit 7\n"), 0o700); err != nil {
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
	if waitErr == nil || result.Status != openruntime.TurnResultUncertain || result.SideEffectsKnown || result.FinalReply {
		t.Fatalf("non-zero AGY result=%+v err=%v", result, waitErr)
	}
	if !strings.Contains(result.Error, "provider rejected request") || strings.Contains(result.Error, "do work") || strings.Contains(result.Error, "top-secret") || strings.Contains(result.Error, "user:pass") {
		t.Fatalf("stderr diagnostic was not preserved and redacted: %q", result.Error)
	}
}

func TestAgyBatchAdapterCancelTerminatesChildProcesses(t *testing.T) {
	testAgyBatchAdapterCancellation(t, false)
}

func TestAgyBatchAdapterTimeoutTerminatesTraceeBeforeTracer(t *testing.T) {
	testAgyBatchAdapterCancellation(t, true)
}

func testAgyBatchAdapterCancellation(t *testing.T, timeout bool) {
	t.Helper()
	dir := t.TempDir()
	readyPath := filepath.Join(dir, "child.ready")
	pidPath := filepath.Join(dir, "child.pid")
	latePIDPath := filepath.Join(dir, "late-child.pid")
	markerPath := filepath.Join(dir, "signals.log")
	scriptPath := filepath.Join(dir, "agy-cancel-fixture")
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "--version" ]; then exit 0; fi
cat >/dev/null
export AGY_TEST_MARKER="%s"
export AGY_TEST_PID="%s"
export AGY_TEST_LATE_PID="%s"
sh -c "trap 'echo child-term >> \"\$AGY_TEST_MARKER\"; (trap \"\" TERM; echo \$\$ > \"\$AGY_TEST_LATE_PID\"; while :; do sleep 1; done) & exit 0' TERM; echo \$\$ > \"\$AGY_TEST_PID\"; while :; do sleep 0.05; done" >/dev/null 2>&1 </dev/null &
while [ ! -s "%s" ]; do sleep 0.05; done
: > "%s"
trap 'if [ -r "/proc/$AGY_TEST_PID/stat" ] && [ "$(awk '\''{print $3}'\'' "/proc/$AGY_TEST_PID/stat")" != "Z" ]; then state=alive; else state=dead; fi; if [ -r "/proc/$AGY_TEST_LATE_PID/stat" ] && [ "$(awk '\''{print $3}'\'' "/proc/$AGY_TEST_LATE_PID/stat")" != "Z" ]; then late_state=alive; else late_state=dead; fi; printf "leader-term-child-$state-late-$late_state\\n" >> "%s"; exit 143' TERM
while :; do sleep 0.1; done
`, markerPath, pidPath, latePIDPath, pidPath, readyPath, markerPath)
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	spec := validSpec()
	if timeout {
		spec.Timeout = 750 * time.Millisecond
	}
	adapter := NewAdapterForTest(Config{Binary: scriptPath, WorkingDir: dir, CancelGrace: cancelTestGrace})
	handle, err := adapter.StartTurn(context.Background(), testTurnRequest(spec), nil)
	if err != nil {
		t.Fatal(err)
	}
	leader, ok := handle.(*turnHandle)
	if !ok || leader.command.Process == nil {
		t.Fatalf("handle is not a *turnHandle with a live process: %T", handle)
	}
	var childPID int
	var latePID int
	t.Cleanup(func() {
		_ = signalProcessGroup(leader.command.Process.Pid, syscall.SIGKILL)
		if childPID != 0 {
			_ = syscall.Kill(childPID, syscall.SIGKILL)
		}
		if latePID != 0 {
			_ = syscall.Kill(latePID, syscall.SIGKILL)
		}
	})
	waitForFile(t, readyPath, "fixture child did not start")
	pidText, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	childPID, err = strconv.Atoi(strings.TrimSpace(string(pidText)))
	if err != nil {
		t.Fatalf("parse child pid from %q (%q): %v", pidPath, pidText, err)
	}
	if !processRunning(childPID) {
		t.Fatalf("fixture child %d is not running before cancel", childPID)
	}
	if !timeout {
		if err := handle.RequestCancel(context.Background()); err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
		if marker, err := os.ReadFile(markerPath); err == nil && strings.Contains(string(marker), "leader-term") {
			t.Fatalf("leader/tracer received TERM before tracee grace elapsed: %q", marker)
		}
	}
	waitContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, waitErr := handle.Wait(waitContext)
	if timeout {
		if waitErr == nil || result.Status != openruntime.TurnResultUncertain || result.SideEffectsKnown || result.FinalReply {
			t.Fatalf("timeout result=%+v err=%v", result, waitErr)
		}
	} else if waitErr != nil || result.Status != openruntime.TurnResultCanceled || result.SideEffectsKnown || result.FinalReply {
		t.Fatalf("explicit cancel result=%+v err=%v", result, waitErr)
	}
	if !strings.Contains(result.Error, "AGY process exited") {
		t.Fatalf("cancellation lost process exit diagnostic: %+v", result)
	}
	marker, err := os.ReadFile(markerPath)
	if err != nil || !strings.Contains(string(marker), "leader-term-child-dead-late-dead") {
		t.Fatalf("cancellation signal order was not recorded: %q err=%v", marker, err)
	}
	if childIndex := strings.Index(string(marker), "child-term"); childIndex >= 0 && childIndex > strings.Index(string(marker), "leader-term-child-dead-late-dead") {
		t.Fatalf("leader/tracer was signaled before tracee: %q", marker)
	}
	waitForFile(t, latePIDPath, "fixture late child did not spawn during cancellation")
	latePIDText, err := os.ReadFile(latePIDPath)
	if err != nil {
		t.Fatal(err)
	}
	latePID, err = strconv.Atoi(strings.TrimSpace(string(latePIDText)))
	if err != nil {
		t.Fatalf("parse late child pid from %q (%q): %v", latePIDPath, latePIDText, err)
	}
	if processRunning(latePID) {
		t.Fatalf("late child process %d survived cancellation cleanup", latePID)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		if !processRunning(childPID) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("child process %d survived cancellation cleanup", childPID)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if processRunning(leader.command.Process.Pid) {
		t.Fatalf("leader/tracer process %d survived cancellation cleanup", leader.command.Process.Pid)
	}
}

// Use a real process with a deliberately incomplete stream: cancellation can
// interrupt a JSON record and the process can exit non-zero while still being
// conclusively stopped. Cleanup faults and timeouts must retain uncertainty.
func TestAgyBatchAdapterCancellationOfInterruptedStream(t *testing.T) {
	for _, scenario := range []string{"explicit", "cleanup-error", "timeout-first", "timeout-after-explicit"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			readyPath := filepath.Join(dir, "ready")
			scriptPath := filepath.Join(dir, "agy-interrupted")
			script := fmt.Sprintf(`#!/bin/sh
cat >/dev/null
trap 'echo interrupted >&2; exit 1' TERM
printf '{"event":"result","result":'
: > %q
while :; do sleep 0.05; done
`, readyPath)
			if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			adapter := NewAdapterForTest(Config{Binary: scriptPath, WorkingDir: dir, CancelGrace: 100 * time.Millisecond})
			handle, err := adapter.StartTurn(context.Background(), testTurnRequest(validSpec()), nil)
			if err != nil {
				t.Fatal(err)
			}
			turn := handle.(*turnHandle)
			t.Cleanup(func() { _ = signalProcessGroup(turn.command.Process.Pid, syscall.SIGKILL) })
			waitForFile(t, readyPath, "interrupted fixture did not start")
			if scenario == "cleanup-error" {
				// An isolated fault fixture: even if the real process stops, an
				// unverified cleanup operation must prohibit a canceled result.
				turn.cancelController.recordError(errors.New("injected cleanup verification failure"))
			}
			if scenario == "timeout-first" {
				if err := turn.cancelController.cancel(true); err != nil {
					t.Fatal(err)
				}
			}
			if err := handle.RequestCancel(context.Background()); err != nil {
				t.Fatal(err)
			}
			if scenario == "timeout-after-explicit" {
				if err := turn.cancelController.cancel(true); err != nil {
					t.Fatal(err)
				}
			}
			waitContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			result, waitErr := handle.Wait(waitContext)
			if scenario == "explicit" {
				if waitErr != nil || result.Status != openruntime.TurnResultCanceled {
					t.Fatalf("explicit cancel result=%+v err=%v", result, waitErr)
				}
			} else if waitErr == nil || result.Status != openruntime.TurnResultUncertain {
				t.Fatalf("%s must remain uncertain: result=%+v err=%v", scenario, result, waitErr)
			}
			if result.SideEffectsKnown || result.FinalReply || !strings.Contains(result.Error, "AGY process exited") || !strings.Contains(result.Error, "interrupted") {
				t.Fatalf("cancellation evidence lost or overstated: %+v", result)
			}
			if scenario == "cleanup-error" && !strings.Contains(result.Error, "injected cleanup verification failure") {
				t.Fatalf("cleanup failure diagnostic lost: %+v", result)
			}
			if processRunning(turn.command.Process.Pid) {
				t.Fatalf("root process %d survived cancellation", turn.command.Process.Pid)
			}
		})
	}
}

func TestAgyBatchAdapterLateCancelPreservesNaturalFailure(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "agy-natural-failure")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\ncat >/dev/null\necho natural-failure >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	adapter := NewAdapterForTest(Config{Binary: scriptPath, WorkingDir: dir})
	handle, err := adapter.StartTurn(context.Background(), testTurnRequest(validSpec()), nil)
	if err != nil {
		t.Fatal(err)
	}
	before, beforeErr := handle.Wait(context.Background())
	if err := handle.RequestCancel(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, afterErr := handle.Wait(context.Background())
	if beforeErr == nil || afterErr == nil || before.Status != openruntime.TurnResultUncertain || !reflect.DeepEqual(before, after) {
		t.Fatalf("late cancellation changed natural failure: before=%+v (%v), after=%+v (%v)", before, beforeErr, after, afterErr)
	}
	turn := handle.(*turnHandle)
	if err := turn.cancelController.wait(); err != nil || turn.cancelController.explicitCancellationStopped() {
		t.Fatalf("late cancellation was incorrectly verified: %v", err)
	}
}

func TestAgyBatchAdapterExitZeroWithoutTerminalIsUncertain(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "agy-no-terminal")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\ncat >/dev/null\necho '{\"type\":\"init\",\"session_id\":\"session-1\"}'\necho 'turn did not start' >&2\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	adapter := NewAdapterForTest(Config{Binary: scriptPath})
	handle, err := adapter.StartTurn(context.Background(), testTurnRequest(validSpec()), nil)
	if err != nil {
		t.Fatal(err)
	}
	result, waitErr := handle.Wait(context.Background())
	if waitErr == nil || result.Status != openruntime.TurnResultUncertain || result.SideEffectsKnown || !strings.Contains(result.Error, "without a terminal event") || !strings.Contains(result.Error, "turn did not start") {
		t.Fatalf("result=%+v err=%v", result, waitErr)
	}
}

func TestCaptureStderrDrainsWhileRetainingBoundedDiagnostic(t *testing.T) {
	capture := captureStderr(strings.NewReader(strings.Repeat("x", 4096)), 32)
	if capture.err != nil || !capture.truncated || len(capture.output) != 32 {
		t.Fatalf("capture=%+v", capture)
	}
}

func TestRealAgyHelpContract(t *testing.T) {
	if os.Getenv("OPENAGENTX_AGY_HELP_CONTRACT") != "1" {
		t.Skip("set OPENAGENTX_AGY_HELP_CONTRACT=1 to run the local, no-model AGY help contract guard")
	}
	output, err := exec.Command("agy-graft", "--help").CombinedOutput()
	if err != nil {
		t.Fatalf("agy-graft --help: %v", err)
	}
	for _, flag := range []string{"--print", "--input-format", "--output-format", "--conversation", "--model", "--effort", "--print-timeout", "--dangerously-skip-permissions"} {
		if !strings.Contains(string(output), flag) {
			t.Fatalf("agy-graft --help no longer advertises %s", flag)
		}
	}
}

func validSpec() domain.ExecutionSpec {
	return domain.ExecutionSpec{AdapterID: "agy-batch", BackendID: "agy", Model: "default", Reasoning: domain.ReasoningSpec{Mode: domain.ReasoningBackendDefault}, Session: domain.SessionSpec{Mode: domain.SessionModeNew}, Timeout: time.Minute, BackendOptions: json.RawMessage(`{}`)}
}

func testTurnRequest(spec domain.ExecutionSpec) openruntime.TurnRequest {
	return openruntime.TurnRequest{
		Task:       domain.Task{ID: "task-1", TargetAgentID: "quote", Content: "do work"},
		RunAttempt: domain.RunAttempt{ID: "run-1", TaskID: "task-1", AgentID: "quote", Version: 1, Status: domain.RunAttemptStarting, LeaseUntil: time.Now().Add(time.Minute), ExecutionSpecVersion: 1, StartedAt: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now()},
		Execution:  domain.ResolvedExecutionSpec{Version: 1, Spec: spec},
	}
}

const cancelTestGrace = 2 * time.Second

func waitForFile(t *testing.T, path, message string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %s never appeared", message, path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func processRunning(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return true
	}
	if index := strings.LastIndex(string(stat), ")"); index >= 0 {
		stat = stat[index+1:]
	}
	fields := strings.Fields(string(stat))
	return len(fields) > 0 && fields[0] != "Z"
}
