package connector_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"agentbus/internal/connector"
)

type mockCall struct {
	Stdin string
	Name  string
	Args  []string
}

type mockRunner struct {
	mu          sync.Mutex
	calls       []mockCall
	probeOutput string
	probeErr    error
	loadErr     error
	pasteErr    error
	sendErr     error
}

func (m *mockRunner) Run(ctx context.Context, stdin string, name string, args ...string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, mockCall{Stdin: stdin, Name: name, Args: args})

	if len(args) > 0 {
		switch args[0] {
		case "display-message":
			return m.probeOutput, m.probeErr
		case "load-buffer":
			return "", m.loadErr
		case "paste-buffer":
			return "", m.pasteErr
		case "send-keys":
			return "", m.sendErr
		}
	}
	return "", nil
}

func TestAddressValidation(t *testing.T) {
	validAddresses := []string{
		"%0",
		"%51",
		"%12345",
		"AgentBus:0.0",
		"my-session:1.%50",
		"session:window.pane",
	}
	for _, addr := range validAddresses {
		if err := connector.ValidateAddress(addr); err != nil {
			t.Errorf("expected valid address for '%s', got error: %v", addr, err)
		}
	}

	invalidAddresses := []string{
		"",
		"   ",
		"pane; rm -rf /",
		"pane$(whoami)",
		"pane`id`",
		"pane\nnewline",
		"pane\r\n",
	}
	for _, addr := range invalidAddresses {
		if err := connector.ValidateAddress(addr); err == nil {
			t.Errorf("expected error for invalid address '%s', got nil", addr)
		}
	}
}

func TestNotifyNewTask(t *testing.T) {
	runner := &mockRunner{
		probeOutput: "%51 0\n",
	}
	c := connector.NewTmuxConnector(runner)

	ctx := context.Background()
	res := c.Notify(ctx, "%51", "quote", "task-abc-123", true)

	if res.Disposition != connector.DispositionNotified {
		t.Fatalf("expected disposition 'notified', got '%s' (err: %s)", res.Disposition, res.Error)
	}
	if res.PaneID != "%51" {
		t.Fatalf("expected pane_id '%%51', got '%s'", res.PaneID)
	}

	runner.mu.Lock()
	defer runner.mu.Unlock()

	if len(runner.calls) != 4 {
		t.Fatalf("expected 4 tmux calls, got %d", len(runner.calls))
	}

	// 1. Check probe call
	if runner.calls[0].Name != "tmux" || runner.calls[0].Args[0] != "display-message" || runner.calls[0].Args[3] != "%51" {
		t.Fatalf("unexpected probe call: %+v", runner.calls[0])
	}

	// 2. Check load-buffer call
	loadCall := runner.calls[1]
	if loadCall.Name != "tmux" || loadCall.Args[0] != "load-buffer" {
		t.Fatalf("unexpected load call: %+v", loadCall)
	}
	expectedText := "[AgentBus] New task task-abc-123. Use AgentBus CLI: task get task-abc-123 --agent quote"
	if loadCall.Stdin != expectedText {
		t.Fatalf("expected stdin '%s', got '%s'", expectedText, loadCall.Stdin)
	}

	// 3. Check paste-buffer call
	pasteCall := runner.calls[2]
	if pasteCall.Name != "tmux" || pasteCall.Args[0] != "paste-buffer" || pasteCall.Args[5] != "%51" {
		t.Fatalf("unexpected paste call: %+v", pasteCall)
	}

	// 4. Check send-keys call
	sendCall := runner.calls[3]
	if sendCall.Name != "tmux" || sendCall.Args[0] != "send-keys" || sendCall.Args[3] != "Enter" {
		t.Fatalf("unexpected send-keys call: %+v", sendCall)
	}
}

func TestNotifySupplement(t *testing.T) {
	runner := &mockRunner{
		probeOutput: "%50 0\n",
	}
	c := connector.NewTmuxConnector(runner)

	ctx := context.Background()
	res := c.Notify(ctx, "%50", "coordinator", "task-xyz", false)

	if res.Disposition != connector.DispositionNotified {
		t.Fatalf("expected disposition 'notified', got '%s'", res.Disposition)
	}

	runner.mu.Lock()
	defer runner.mu.Unlock()

	expectedText := "[AgentBus] Task task-xyz has a new message. Use AgentBus CLI: task get task-xyz --agent coordinator"
	if runner.calls[1].Stdin != expectedText {
		t.Fatalf("expected stdin '%s', got '%s'", expectedText, runner.calls[1].Stdin)
	}
}

func TestNotifyDeadPane(t *testing.T) {
	runner := &mockRunner{
		probeOutput: "%51 1\n",
	}
	c := connector.NewTmuxConnector(runner)

	ctx := context.Background()
	res := c.Notify(ctx, "%51", "quote", "task-dead", true)

	if res.Disposition != connector.DispositionDeliveryFailed {
		t.Fatalf("expected delivery_failed for dead pane, got %s", res.Disposition)
	}
	if !strings.Contains(res.Error, "pane_dead=1") {
		t.Fatalf("expected error message to contain 'pane_dead=1', got %s", res.Error)
	}
}

func TestNotifyProbeFailure(t *testing.T) {
	runner := &mockRunner{
		probeErr: errors.New("pane not found"),
	}
	c := connector.NewTmuxConnector(runner)

	ctx := context.Background()
	res := c.Notify(ctx, "%99", "quote", "task-fail", true)

	if res.Disposition != connector.DispositionDeliveryFailed {
		t.Fatalf("expected delivery_failed, got %s", res.Disposition)
	}
	if !strings.Contains(res.Error, "pane not found") {
		t.Fatalf("expected error message to contain 'pane not found', got %s", res.Error)
	}
}

func TestNotifyPasteFailure(t *testing.T) {
	runner := &mockRunner{
		probeOutput: "%51 0\n",
		pasteErr:    errors.New("paste failed"),
	}
	c := connector.NewTmuxConnector(runner)

	ctx := context.Background()
	res := c.Notify(ctx, "%51", "quote", "task-paste-fail", true)

	if res.Disposition != connector.DispositionDeliveryFailed {
		t.Fatalf("expected delivery_failed, got %s", res.Disposition)
	}
	if !strings.Contains(res.Error, "paste failed") {
		t.Fatalf("expected error message to contain 'paste failed', got %s", res.Error)
	}
}

func TestNotifyBufferLoadFailure(t *testing.T) {
	runner := &mockRunner{
		probeOutput: "%51 0\n",
		loadErr:     errors.New("load buffer failed"),
	}
	c := connector.NewTmuxConnector(runner)

	ctx := context.Background()
	res := c.Notify(ctx, "%51", "quote", "task-load-fail", true)

	if res.Disposition != connector.DispositionDeliveryFailed {
		t.Fatalf("expected delivery_failed, got %s", res.Disposition)
	}
	if !strings.Contains(res.Error, "load buffer failed") {
		t.Fatalf("expected error message to contain 'load buffer failed', got %s", res.Error)
	}
}

func TestNotifySendKeysFailure(t *testing.T) {
	runner := &mockRunner{
		probeOutput: "%51 0\n",
		sendErr:     errors.New("send keys failed"),
	}
	c := connector.NewTmuxConnector(runner)

	ctx := context.Background()
	res := c.Notify(ctx, "%51", "quote", "task-send-fail", true)

	if res.Disposition != connector.DispositionDeliveryFailed {
		t.Fatalf("expected delivery_failed, got %s", res.Disposition)
	}
	if !strings.Contains(res.Error, "send keys failed") {
		t.Fatalf("expected error message to contain 'send keys failed', got %s", res.Error)
	}
}
