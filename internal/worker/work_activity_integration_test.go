package worker

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"openagentx/internal/domain"
	"openagentx/internal/fleet"
	openruntime "openagentx/internal/runtime"
)

func TestWorkerActivityThroughRealTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	runner, client, adapter := newRunnerFixture(t, false)
	// This test uses the actual Worker and tmux, with the controllable Runtime
	// fixture only for deterministic approval/completion/failure scenarios.
	tmux := fleet.ExecRunner{SocketName: "oax-activity-" + strconv.FormatInt(time.Now().UnixNano(), 36)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	call := func(args ...string) string {
		t.Helper()
		out, err := tmux.Run(ctx, args...)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(out)
	}
	window := call("new-session", "-d", "-P", "-F", "#{window_id}", "-s", "OAX", "-n", "quote", "sleep", "300")
	defer tmux.Run(context.Background(), "kill-server")
	call("set-option", "-w", "-t", window, fleet.ManagedOption, "1")
	call("set-option", "-w", "-t", window, fleet.AgentIDOption, "quote")
	status := fleet.StartWorkStatus(tmux, "quote", "/unused/openagentx")
	defer status.Close()
	runner.config.ObserveWork = status.Observe
	workerExit := make(chan error, 1)
	go func() { workerExit <- runner.Run(ctx) }()
	waitState := func(want string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			out, err := tmux.Run(ctx, "show-options", "-w", "-v", "-t", window, fleet.WorkStateOption)
			if err == nil && strings.TrimSpace(out) == want {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatal("did not observe " + want)
	}
	waitSignal(t, client.heartbeatHit, "initial heartbeat")
	waitState("idle")
	client.enqueueMailbox(workItem("mailbox-work-1", 1))
	handle, err := adapter.NextHandle(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	waitState("running")
	if err := handle.Emit(testContext(t), openruntime.RuntimeEvent{Type: "approval.requested", Payload: json.RawMessage(`{"approval_request_id":"approval-control"}`), OccurredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	waitState("idle")
	client.enqueueMailbox(controlItem("mailbox-approval", 2, domain.MailboxKindApproval, "run-1", "", "decision-control"))
	if _, err := handle.NextApproval(testContext(t)); err != nil {
		t.Fatal(err)
	}
	waitState("running")
	handle.Complete(openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "Awaiting user input", SideEffectsKnown: true})
	waitSignal(t, client.finishHit, "first finish")
	waitState("idle")
	client.enqueueMailbox(workItem("mailbox-work-2", 3))
	handle, err = adapter.NextHandle(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	waitState("running")
	call("kill-server")
	handle.Complete(openruntime.TurnResult{Status: openruntime.TurnResultFailed, Error: "fixture failure", SideEffectsKnown: true})
	waitSignal(t, client.finishHit, "finish with closed tmux")
	// A missing tmux binary also cannot prevent the next controlled turn.
	t.Setenv("PATH", t.TempDir())
	client.enqueueMailbox(workItem("mailbox-work-3", 4))
	handle, err = adapter.NextHandle(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	handle.Complete(openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "completed without tmux", SideEffectsKnown: true})
	waitSignal(t, client.finishHit, "finish with missing tmux")
	cancel()
	if err := waitWorkerExit(t, workerExit); err != nil {
		t.Fatal(err)
	}
}
