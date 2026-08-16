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

func TestNotifyBootstrap(t *testing.T) {
	runner := &mockRunner{
		probeOutput: "%51 0\n",
	}
	c := connector.NewTmuxConnector(runner)

	ctx := context.Background()
	res := c.NotifyBootstrap(ctx, "%51", "agentbus-agent", "agentbus", 2, "/path/to/ROLE.md")

	if res.Disposition != connector.DispositionNotified {
		t.Fatalf("expected disposition 'notified', got '%s' (err: %s)", res.Disposition, res.Error)
	}

	runner.mu.Lock()
	defer runner.mu.Unlock()

	expectedText := "[AgentBus Bootstrap] agent_id=agentbus-agent role=agentbus generation=2. Read /path/to/ROLE.md, then use AgentBus CLI: session ready --agent agentbus-agent --generation 2"
	if runner.calls[1].Stdin != expectedText {
		t.Fatalf("expected stdin '%s', got '%s'", expectedText, runner.calls[1].Stdin)
	}
}

func TestNotifyTaskWithIdentityReminder(t *testing.T) {
	runner := &mockRunner{
		probeOutput: "%52 0\n",
	}
	c := connector.NewTmuxConnector(runner)

	ctx := context.Background()
	res := c.NotifyTask(ctx, "%52", "quote-service", "quote", "/path/to/quote/ROLE.md", "task-abc-123", true)

	if res.Disposition != connector.DispositionNotified {
		t.Fatalf("expected disposition 'notified', got '%s' (err: %s)", res.Disposition, res.Error)
	}

	runner.mu.Lock()
	defer runner.mu.Unlock()

	expectedText := "[AgentBus][agent=quote-service role=quote] New task task-abc-123. If role context is uncertain, read /path/to/quote/ROLE.md. Use AgentBus CLI: task get task-abc-123 --agent quote-service"
	if runner.calls[1].Stdin != expectedText {
		t.Fatalf("expected stdin '%s', got '%s'", expectedText, runner.calls[1].Stdin)
	}
}

func TestNotifySupplement(t *testing.T) {
	runner := &mockRunner{
		probeOutput: "%50 0\n",
	}
	c := connector.NewTmuxConnector(runner)

	ctx := context.Background()
	res := c.NotifyTask(ctx, "%50", "orchestrator", "orchestrator", "/path/to/orch/ROLE.md", "task-xyz", false)

	if res.Disposition != connector.DispositionNotified {
		t.Fatalf("expected disposition 'notified', got '%s'", res.Disposition)
	}

	runner.mu.Lock()
	defer runner.mu.Unlock()

	expectedText := "[AgentBus][agent=orchestrator role=orchestrator] New message for task task-xyz. If role context is uncertain, read /path/to/orch/ROLE.md. Use AgentBus CLI: task get task-xyz --agent orchestrator"
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
