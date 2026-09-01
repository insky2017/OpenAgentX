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
	if result.Status != openruntime.TurnResultSucceeded || result.ProviderSessionID != "conv-1" || result.Result != "hellodone" || !result.SideEffectsKnown {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(events) != 3 || events[0].Type != "agy.init" || events[1].Type != "agy.step_update" || events[2].Type != "agy.result" {
		t.Fatalf("unexpected events: %+v", events)
	}
	if !json.Valid(events[0].Payload) {
		t.Fatal("normalized event payload is not JSON")
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
	if err != nil || result.Status != openruntime.TurnResultSucceeded || result.SideEffectsKnown {
		t.Fatalf("result=%+v err=%v", result, err)
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
	if waitErr != nil || result.Status != openruntime.TurnResultSucceeded || result.ProviderSessionID != "conv-fixture" {
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
	if waitErr == nil || result.Status != openruntime.TurnResultUncertain || time.Since(started) > time.Second {
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
	if waitErr == nil || result.Status != openruntime.TurnResultUncertain || result.SideEffectsKnown {
		t.Fatalf("non-zero AGY result=%+v err=%v", result, waitErr)
	}
	if !strings.Contains(result.Error, "provider rejected request") || strings.Contains(result.Error, "do work") || strings.Contains(result.Error, "top-secret") || strings.Contains(result.Error, "user:pass") {
		t.Fatalf("stderr diagnostic was not preserved and redacted: %q", result.Error)
	}
}

func TestAgyBatchAdapterCancelTerminatesChildProcesses(t *testing.T) {
	dir := t.TempDir()
	readyPath := filepath.Join(dir, "child.ready")
	pidPath := filepath.Join(dir, "child.pid")
	scriptPath := filepath.Join(dir, "agy-cancel-fixture")
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "--version" ]; then exit 0; fi
cat >/dev/null
sh -c 'trap "" TERM; echo $$ > "%s"; while :; do sleep 1; done' >/dev/null 2>&1 </dev/null &
while [ ! -s "%s" ]; do sleep 0.05; done
: > "%s"
trap 'exit 143' TERM
while :; do sleep 0.1; done
`, pidPath, pidPath, readyPath)
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	adapter := NewAdapterForTest(Config{Binary: scriptPath, WorkingDir: dir, CancelGrace: cancelTestGrace})
	handle, err := adapter.StartTurn(context.Background(), testTurnRequest(validSpec()), nil)
	if err != nil {
		t.Fatal(err)
	}
	leader, ok := handle.(*turnHandle)
	if !ok || leader.command.Process == nil {
		t.Fatalf("handle is not a *turnHandle with a live process: %T", handle)
	}
	var childPID int
	t.Cleanup(func() {
		_ = signalProcessGroup(leader.command.Process.Pid, syscall.SIGKILL)
		if childPID != 0 {
			_ = syscall.Kill(childPID, syscall.SIGKILL)
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
	if err := handle.RequestCancel(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !processRunning(childPID) {
		t.Fatalf("fixture child %d died on SIGTERM, so the SIGKILL escalation is not covered", childPID)
	}
	waitContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, waitErr := handle.Wait(waitContext)
	if waitErr == nil || result.Status != openruntime.TurnResultUncertain || result.SideEffectsKnown {
		t.Fatalf("result=%+v err=%v", result, waitErr)
	}
	deadline := time.Now().Add(cancelTestGrace + 15*time.Second)
	for {
		if !processRunning(childPID) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("child process %d survived the post-grace process-group SIGKILL", childPID)
		}
		time.Sleep(20 * time.Millisecond)
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
