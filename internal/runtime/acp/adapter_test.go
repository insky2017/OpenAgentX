package acp

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	openruntime "openagentx/internal/runtime"
)

func TestSuccessfulACPProcessDoesNotClaimKnownSideEffects(t *testing.T) {
	command := exec.Command("sh", "-c", `printf '%s\n' '{"status":"succeeded","result":"done"}'`)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	handle := &turnHandle{cmd: command, stdout: stdout, done: make(chan struct{})}
	handle.collect()
	result, err := handle.Wait(context.Background())
	if err != nil || result.Status != openruntime.TurnResultSucceeded || result.Result != "done" || result.SideEffectsKnown {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestACPRequiresValidTerminalAndPreservesSinkFailure(t *testing.T) {
	tests := []struct {
		name  string
		input string
		sink  openruntime.EventSink
	}{
		{name: "empty"},
		{name: "malformed", input: "not-json\n" + `{"status":"succeeded","result":"misleading"}` + "\n"},
		{name: "no terminal", input: `{"type":"turn.output","status":"running","payload":{"text":"working"}}` + "\n"},
		{name: "sink failure", input: `{"type":"turn.output","status":"running","payload":{"text":"working"}}` + "\n" +
			`{"status":"succeeded","result":"done"}` + "\n", sink: openruntime.EventSinkFunc(func(context.Context, openruntime.RuntimeEvent) error {
			return errors.New("persist output")
		})},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			result, err := collectFixture(t, "cat", testCase.input, testCase.sink)
			if err == nil || result.Status != openruntime.TurnResultUncertain || result.SideEffectsKnown {
				t.Fatalf("result status=%s err=%v", result.Status, err)
			}
		})
	}
}

func TestACPBoundsEventsAndRedactsFailureDiagnostic(t *testing.T) {
	var input strings.Builder
	for index := 0; index < openruntime.MaxPublicOutputEvents+20; index++ {
		input.WriteString(`{"type":"turn.output","status":"running","payload":{"text":"line"}}` + "\n")
	}
	input.WriteString(`{"status":"succeeded","result":"done"}` + "\n")
	emitted := 0
	result, err := collectFixture(t, "cat", input.String(), openruntime.EventSinkFunc(func(context.Context, openruntime.RuntimeEvent) error {
		emitted++
		return nil
	}))
	if err != nil || result.Status != openruntime.TurnResultSucceeded || emitted != openruntime.MaxPublicOutputEvents {
		t.Fatalf("result status=%s err=%v emitted=%d", result.Status, err, emitted)
	}

	result, err = collectFixture(t, `cat >/dev/null; echo 'Authorization: Bearer stderr-secret' >&2; exit 7`, "", nil)
	if err == nil || result.Status != openruntime.TurnResultUncertain || strings.Contains(result.Error, "stderr-secret") || !strings.Contains(result.Error, "[REDACTED]") {
		t.Fatalf("failure status=%s redacted=%t err=%v", result.Status, !strings.Contains(result.Error, "stderr-secret"), err)
	}
}

func collectFixture(t *testing.T, script string, input string, sink openruntime.EventSink) (openruntime.TurnResult, error) {
	t.Helper()
	command := exec.Command("sh", "-c", script)
	command.Stdin = strings.NewReader(input)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr := &boundedBuffer{limit: maxACPStderr}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	handle := &turnHandle{cmd: command, stdout: stdout, stderr: stderr, sink: sink, done: make(chan struct{})}
	handle.collect()
	return handle.Wait(context.Background())
}
