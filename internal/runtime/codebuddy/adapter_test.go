package codebuddy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
	"openagentx/internal/runtime/conformance"
)

func TestAdapterRunsCodeBuddyTurnWithoutPromptInArguments(t *testing.T) {
	directory := t.TempDir()
	argsPath := filepath.Join(directory, "args")
	promptPath := filepath.Join(directory, "prompt")
	binary := writeFixture(t, directory, `#!/bin/sh
if [ "$1" = "--version" ]; then echo "fixture 1"; exit 0; fi
printf '%s\n' "$@" > "$OAX_ARGS"
cat > "$OAX_PROMPT"
printf 'verified by CodeBuddy fixture\n'
`)
	adapter, err := NewAdapter(Config{
		Binary: binary, WorkingDir: directory, Models: []string{"hy4-preview"}, DefaultEffort: "high",
		Environment: []string{"OAX_ARGS=" + argsPath, "OAX_PROMPT=" + promptPath},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	handle, err := adapter.StartTurn(context.Background(), turnRequest("inspect repository"), nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := handle.Wait(context.Background())
	if err != nil || result.Status != openruntime.TurnResultSucceeded || result.Result != "verified by CodeBuddy fixture" || result.SideEffectsKnown {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	argumentText := string(args)
	for _, expected := range []string{"--print", "--output-format", "text", "--model", "hy4-preview", "--effort", "high", "--permission-mode", "acceptEdits"} {
		if !strings.Contains(argumentText, expected) {
			t.Fatalf("arguments do not contain %q: %s", expected, argumentText)
		}
	}
	if strings.Contains(argumentText, "inspect repository") {
		t.Fatal("Task prompt leaked into process arguments")
	}
	prompt, err := os.ReadFile(promptPath)
	if err != nil || !strings.Contains(string(prompt), "inspect repository") || !strings.Contains(string(prompt), "task-codebuddy") {
		t.Fatalf("prompt=%q err=%v", prompt, err)
	}
}

func TestAdapterPublishesBoundedRedactedOutputBeforeTurnCompletes(t *testing.T) {
	directory := t.TempDir()
	releasePath := filepath.Join(directory, "release")
	binary := writeFixture(t, directory, `#!/bin/sh
if [ "$1" = "--version" ]; then exit 0; fi
cat >/dev/null
printf 'working token=runtime-secret\n'
	while [ ! -f "$OAX_READY" ]; do sleep 0.01; done
printf '{"token":"json-secret","result":"done"}\n'
`)
	adapter, err := NewAdapter(Config{
		Binary: binary, WorkingDir: directory, Models: []string{"hy4-preview"},
		Environment: []string{"OAX_READY=" + releasePath},
	})
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan openruntime.RuntimeEvent, 4)
	handle, err := adapter.StartTurn(context.Background(), turnRequest("stream"), openruntime.EventSinkFunc(func(_ context.Context, event openruntime.RuntimeEvent) error {
		events <- event
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	leader := handle.(*turnHandle)
	t.Cleanup(func() {
		if leader.command.Process != nil {
			_ = signalProcessGroup(leader.command.Process.Pid, syscall.SIGKILL)
		}
	})
	select {
	case event := <-events:
		if event.Type != "turn.output" || strings.Contains(string(event.Payload), "runtime-secret") || !strings.Contains(string(event.Payload), "token=[REDACTED]") {
			t.Fatalf("unsafe live event: %+v", event)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("live output was not published before process completion")
	}
	if err := os.WriteFile(releasePath, []byte("release"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := handle.Wait(context.Background())
	if err != nil || result.Status != openruntime.TurnResultSucceeded {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	second := <-events
	for _, forbidden := range []string{"json-secret", "runtime-secret"} {
		if strings.Contains(string(second.Payload), forbidden) {
			t.Fatalf("live event leaked %q: %s", forbidden, second.Payload)
		}
	}
}

func TestLiveOutputBufferBoundsEventsAndRecordsSinkFailure(t *testing.T) {
	var emitted int
	buffer := newLiveOutputBuffer(1<<20, openruntime.EventSinkFunc(func(context.Context, openruntime.RuntimeEvent) error {
		emitted++
		return nil
	}))
	_, _ = buffer.Write([]byte(strings.Repeat("line\n", maxLiveOutputEvents+20)))
	buffer.Flush()
	if emitted != maxLiveOutputEvents {
		t.Fatalf("live event count=%d want=%d", emitted, maxLiveOutputEvents)
	}

	sinkFailure := errors.New("event sink unavailable")
	failing := newLiveOutputBuffer(1024, openruntime.EventSinkFunc(func(context.Context, openruntime.RuntimeEvent) error {
		return sinkFailure
	}))
	_, _ = failing.Write([]byte("first\nsecond\n"))
	if !errors.Is(failing.EventError(), sinkFailure) {
		t.Fatalf("sink failure=%v", failing.EventError())
	}
}

func TestAdapterClassifiesFailureAndCancellation(t *testing.T) {
	t.Run("failure", func(t *testing.T) {
		directory := t.TempDir()
		binary := writeFixture(t, directory, `#!/bin/sh
if [ "$1" = "--version" ]; then exit 0; fi
cat >/dev/null
echo 'backend unavailable' >&2
exit 7
`)
		adapter, err := NewAdapter(Config{Binary: binary, WorkingDir: directory, Models: []string{"hy4-preview"}})
		if err != nil {
			t.Fatal(err)
		}
		handle, err := adapter.StartTurn(context.Background(), turnRequest("fail"), nil)
		if err != nil {
			t.Fatal(err)
		}
		result, waitErr := handle.Wait(context.Background())
		if waitErr == nil || result.Status != openruntime.TurnResultUncertain || result.SideEffectsKnown || !strings.Contains(result.Error, "backend unavailable") {
			t.Fatalf("result=%+v err=%v", result, waitErr)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		directory := t.TempDir()
		binary := writeFixture(t, directory, `#!/bin/sh
if [ "$1" = "--version" ]; then exit 0; fi
trap 'exit 143' TERM
cat >/dev/null
while :; do sleep 1; done
`)
		adapter, err := NewAdapter(Config{Binary: binary, WorkingDir: directory, Models: []string{"hy4-preview"}})
		if err != nil {
			t.Fatal(err)
		}
		handle, err := adapter.StartTurn(context.Background(), turnRequest("cancel"), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := handle.RequestCancel(context.Background()); err != nil {
			t.Fatal(err)
		}
		waitContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		result, waitErr := handle.Wait(waitContext)
		if waitErr != nil || result.Status != openruntime.TurnResultCanceled || result.SideEffectsKnown {
			t.Fatalf("result=%+v err=%v", result, waitErr)
		}
	})

	// The fixture child ignores SIGTERM, so only the post-grace
	// process-group SIGKILL can stop it. The leader exits on SIGTERM, which
	// closes turnHandle.done while the child is still alive: the escalation
	// must not be dropped in that case.
	t.Run("cancel terminates child processes", func(t *testing.T) {
		directory := t.TempDir()
		readyPath := filepath.Join(directory, "child.ready")
		pidPath := filepath.Join(directory, "child.pid")
		binary := writeFixture(t, directory, fmt.Sprintf(`#!/bin/sh
if [ "$1" = "--version" ]; then echo "fixture 1"; exit 0; fi
cat >/dev/null
sh -c 'trap "" TERM; echo $$ > "%s"; while :; do sleep 1; done' >/dev/null 2>&1 </dev/null &
while [ ! -s "%s" ]; do sleep 0.05; done
: > "%s"
trap 'exit 143' TERM
while :; do sleep 0.1; done
`, pidPath, pidPath, readyPath))
		adapter, err := NewAdapter(Config{
			Binary: binary, WorkingDir: directory, Models: []string{"hy4-preview"},
			Environment: []string{"OAX_CHILD_PID=" + pidPath, "OAX_READY=" + readyPath},
			CancelGrace: cancelTestGrace,
		})
		if err != nil {
			t.Fatal(err)
		}
		handle, err := adapter.StartTurn(context.Background(), turnRequest("cancel children"), nil)
		if err != nil {
			t.Fatal(err)
		}
		leader, ok := handle.(*turnHandle)
		if !ok || leader.command.Process == nil {
			t.Fatalf("handle is not a *turnHandle with a live process: %T", handle)
		}
		var childPID int
		t.Cleanup(func() {
			// Never leak the fixture group, even when an assertion fails early.
			_ = signalProcessGroup(leader.command.Process.Pid, syscall.SIGKILL)
			if childPID != 0 {
				_ = syscall.Kill(childPID, syscall.SIGKILL)
			}
		})
		// Handshake: only cancel once the fixture confirms the child is
		// running, otherwise the test cannot distinguish "child not started
		// yet" from "child survived cancellation".
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
		if waitErr != nil || result.Status != openruntime.TurnResultCanceled || result.SideEffectsKnown {
			t.Fatalf("result=%+v err=%v", result, waitErr)
		}
		// The leader is already gone and h.done is closed at this point; the
		// child can only die from the escalation firing on schedule.
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
	})
}

// cancelTestGrace is a test-only short grace period so the SIGKILL escalation
// is exercised without slowing the suite down.
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

// processRunning reports whether pid is still live. A zombie counts as not
// running: the process has exited and only awaits a reap by init.
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

func TestCodeBuddyAdapterConformance(t *testing.T) {
	directory := t.TempDir()
	binary := writeFixture(t, directory, "#!/bin/sh\nexit 0\n")
	adapter, err := NewAdapter(Config{Binary: binary, Models: []string{"hy4-preview"}})
	if err != nil {
		t.Fatal(err)
	}
	conformance.Run(t, adapter, turnRequest("conformance").Execution.Spec)
}

func TestAdapterBoundsOutput(t *testing.T) {
	directory := t.TempDir()
	binary := writeFixture(t, directory, `#!/bin/sh
if [ "$1" = "--version" ]; then exit 0; fi
cat >/dev/null
head -c 8192 /dev/zero | tr '\0' 'x'
`)
	adapter, err := NewAdapter(Config{Binary: binary, Models: []string{"hy4-preview"}, OutputLimit: 64})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := adapter.StartTurn(context.Background(), turnRequest("overflow"), nil)
	if err != nil {
		t.Fatal(err)
	}
	result, waitErr := handle.Wait(context.Background())
	if waitErr == nil || result.Status != openruntime.TurnResultUncertain || result.SideEffectsKnown ||
		!strings.Contains(result.Error, "output exceeded the configured limit") {
		t.Fatalf("result=%+v err=%v", result, waitErr)
	}
}

func TestAdapterRejectsUnsupportedModelAndEffort(t *testing.T) {
	directory := t.TempDir()
	binary := writeFixture(t, directory, "#!/bin/sh\nexit 0\n")
	adapter, err := NewAdapter(Config{Binary: binary, Models: []string{"hy4-preview"}})
	if err != nil {
		t.Fatal(err)
	}
	request := turnRequest("validate")
	request.Execution.Spec.Model = "other-model"
	if err := adapter.Validate(context.Background(), request.Execution.Spec); !errors.Is(err, domain.ErrUnsupportedCapability) {
		t.Fatalf("unsupported model err=%v", err)
	}
	request.Execution.Spec.Model = "hy4-preview"
	request.Execution.Spec.Reasoning.Value = "infinite"
	if err := adapter.Validate(context.Background(), request.Execution.Spec); !errors.Is(err, domain.ErrUnsupportedCapability) {
		t.Fatalf("unsupported effort err=%v", err)
	}
}

func turnRequest(content string) openruntime.TurnRequest {
	spec := domain.ExecutionSpec{
		AdapterID: "codebuddy-cli", BackendID: "primary", Model: "hy4-preview",
		Reasoning: domain.ReasoningSpec{Mode: domain.ReasoningEffort, Value: "high"},
		Session:   domain.SessionSpec{Mode: domain.SessionModeNew, ContextID: "task-codebuddy"},
		Timeout:   time.Minute,
	}
	return openruntime.TurnRequest{
		Task:      domain.Task{ID: "task-codebuddy", Content: content},
		Execution: domain.ResolvedExecutionSpec{Version: 1, Spec: spec},
	}
}

func writeFixture(t *testing.T, directory, content string) string {
	t.Helper()
	path := filepath.Join(directory, "codebuddy-fixture")
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
