package acp

import (
	"context"
	"os/exec"
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
