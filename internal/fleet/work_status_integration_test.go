package fleet

import (
	"strings"
	"testing"
)

// Uses the real isolated tmux server. Runtime state injection here only tests
// the display boundary; Worker/real-model evidence is recorded separately.
func TestWorkStatusIsolatedTmuxPreservesFormatsAndWindowIsolation(t *testing.T) {
	ctx, runner := isolatedTmux(t)
	command := func(args ...string) string {
		t.Helper()
		out, err := runner.Run(ctx, args...)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSuffix(out, "\n")
	}
	a := command("new-session", "-d", "-P", "-F", "#{window_id}", "-s", "OAX", "-n", "a", "sleep", "300")
	b := command("new-window", "-d", "-P", "-F", "#{window_id}", "-t", "OAX", "-n", "b", "sleep", "300")
	ordinary := command("new-window", "-P", "-F", "#{window_id}", "-t", "OAX", "-n", "ordinary", "sleep", "300")
	original := "#[fg=blue]#I:#W#{?window_flags,*,}"
	current := "#[bold]CURRENT:#W"
	for _, window := range []string{a, b, ordinary} {
		command("set-option", "-w", "-t", window, "window-status-format", original)
		command("set-option", "-w", "-t", window, "window-status-current-format", current)
	}
	for agent, window := range map[string]string{"a": a, "b": b} {
		command("set-option", "-w", "-t", window, ManagedOption, "1")
		command("set-option", "-w", "-t", window, AgentIDOption, agent)
	}
	sa := &WorkStatus{runner: runner, agentID: "a", binary: "/tmp/oax test's binary"}
	sb := &WorkStatus{runner: runner, agentID: "b", binary: sa.binary}
	// An inherited user format must be read with -A, not treated as empty.
	command("set-option", "-gw", "window-status-current-format", current)
	command("set-option", "-wu", "-t", b, "window-status-current-format")
	sa.project(false)
	sb.project(false)
	for _, window := range []string{a, b} {
		if got := command("show-options", "-w", "-v", "-t", window, WorkStateOption); got != "idle" {
			t.Fatal(got)
		}
		for option, want := range map[string]string{"window-status-format": original, "window-status-current-format": current} {
			got := command("show-options", "-w", "-v", "-t", window, option)
			if !strings.HasPrefix(got, want) || strings.Count(got, "tmux-spinner") != 1 {
				t.Fatalf("%s=%s", option, got)
			}
		}
	}
	sa.project(true)
	sa.project(true)
	if got := command("show-options", "-w", "-v", "-t", a, WorkStateOption); got != "running" {
		t.Fatal(got)
	}
	if got := command("show-options", "-w", "-v", "-t", b, WorkStateOption); got != "idle" {
		t.Fatal(got)
	}
	sb.project(true)
	sa.project(false)
	if got := command("show-options", "-w", "-v", "-t", a, WorkStateOption); got != "idle" {
		t.Fatal(got)
	}
	if got := command("show-options", "-w", "-v", "-t", b, WorkStateOption); got != "running" {
		t.Fatal(got)
	}
	// A user edit made after installation remains the new base on reconciliation.
	command("set-option", "-w", "-t", a, "window-status-format", "CUSTOM:#W")
	sa.project(false)
	got := command("show-options", "-w", "-v", "-t", a, "window-status-format")
	if !strings.HasPrefix(got, "CUSTOM:#W") || strings.Count(got, "tmux-spinner") != 1 {
		t.Fatal(got)
	}
	for option, want := range map[string]string{"window-status-format": original, "window-status-current-format": current} {
		if got := command("show-options", "-w", "-v", "-t", ordinary, option); got != want {
			t.Fatalf("ordinary %s changed: %s", option, got)
		}
	}
	if got := command("display-message", "-p", "-t", ordinary, "#{window_active}"); got != "1" {
		t.Fatal("active window changed")
	}
	if got := command("display-message", "-p", "-t", a, "#{window_name}"); got != "a" {
		t.Fatal("window name changed")
	}
	if got := command("show-options", "-v", "-t", "OAX", "status-interval"); got != "1" {
		t.Fatal(got)
	}
	command("kill-server")
	// Missing server remains a no-op; it must not start a new tmux server.
	sa.project(true)
}
