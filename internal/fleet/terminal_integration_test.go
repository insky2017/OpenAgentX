package fleet

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type terminalFailRunner struct {
	CommandRunner
	option string
	failed bool
}

func (r *terminalFailRunner) Run(ctx context.Context, args ...string) (string, error) {
	if !r.failed && len(args) > 4 && args[0] == "set-option" && args[4] == r.option {
		r.failed = true
		return "", fmt.Errorf("injected terminal option failure: %s", r.option)
	}
	return r.CommandRunner.Run(ctx, args...)
}

func TestIsolatedTmuxTerminalRepairAndRollback(t *testing.T) {
	for _, failure := range []string{"", "automatic-rename", "allow-rename", "remain-on-exit", ManagedOption, AgentIDOption, "pane-border-format"} {
		t.Run("failure="+failure, func(t *testing.T) {
			ctx, runner := isolatedTmux(t)
			createManagedIntegrationWindow(t, ctx, runner, SessionName, OverviewWindow, "")
			out, err := runner.Run(ctx, "new-window", "-d", "-P", "-F", "#{window_id}", "-t", "="+SessionName, "-n", "scratch", "sleep", integrationSentinelSeconds)
			if err != nil {
				t.Fatal(err)
			}
			id := strings.TrimSpace(out)
			for _, args := range [][]string{
				{"set-option", "-w", "-t", id, "pane-base-index", "0"},
				{"set-option", "-w", "-t", id, "automatic-rename", "off"},
				{"set-option", "-w", "-u", "-t", id, "allow-rename"},
				{"set-option", "-w", "-t", id, "remain-on-exit", "off"},
				{"split-window", "-d", "-t", id, "sleep", integrationSentinelSeconds},
			} {
				if _, err := runner.Run(ctx, args...); err != nil {
					t.Fatal(err)
				}
			}
			// Local automatic-rename starts set to on after the process is already named.
			if _, err := runner.Run(ctx, "set-option", "-w", "-t", id, "automatic-rename", "on"); err != nil {
				t.Fatal(err)
			}
			// A long-lived command can trigger automatic rename asynchronously. Capture
			// the actual name immediately before preflight, and compare the exact state.
			runner.CurrentTarget = id + ".0"
			plain := Workspace{Runner: runner}
			before := requireIntegrationWindow(t, ctx, plain, id)
			panesBefore, err := runner.Run(ctx, "list-panes", "-t", id, "-F", "#{pane_id}:#{pane_index}:#{pane_pid}")
			if err != nil {
				t.Fatal(err)
			}
			injected := &terminalFailRunner{CommandRunner: runner, option: failure}
			warning := ""
			workspace := Workspace{Runner: injected, PaneLabel: func(string) string { return NativeTerminalLabel }, Warn: func(err error) { warning = err.Error() }}
			location, err := workspace.PreflightAttach(ctx)
			if err != nil {
				t.Fatal(err)
			}
			result, err := workspace.BindCurrent(ctx, location, "quote", false)
			after := requireIntegrationWindow(t, ctx, plain, id)
			panesAfter, paneErr := runner.Run(ctx, "list-panes", "-t", id, "-F", "#{pane_id}:#{pane_index}:#{pane_pid}")
			if paneErr != nil || panesBefore != panesAfter {
				t.Fatalf("pane process changed: %q -> %q (%v)", panesBefore, panesAfter, paneErr)
			}
			t.Logf("isolated server=%s pane processes preserved=%q failure=%q result=%+v err=%v", runner.SocketName, panesAfter, failure, result, err)
			if failure != "" && failure != "pane-border-format" {
				if !injected.failed || err == nil || !strings.Contains(err.Error(), "original state was restored") {
					t.Fatalf("expected compensated failure: %v", err)
				}
				if after.Name != before.Name || after.Managed != before.Managed || after.AgentID != before.AgentID {
					t.Fatalf("mapping not restored: before=%+v after=%+v", before, after)
				}
				for _, option := range terminalWindowOptions {
					if after.terminalOptions[option[0]] != before.terminalOptions[option[0]] {
						t.Fatalf("option presence/value not restored: %s before=%+v after=%+v", option[0], before.terminalOptions, after.terminalOptions)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := verifyWindowIdentity(after, "quote"); err != nil {
				t.Fatal(err)
			}
			if failure == "pane-border-format" && warning == "" {
				t.Fatal("border failure must warn")
			}
			// Drift on a compatible identity must repair without replacing either pane.
			if _, err := runner.Run(ctx, "set-option", "-w", "-u", "-t", id, "allow-rename"); err != nil {
				t.Fatal(err)
			}
			location, err = workspace.PreflightAttach(ctx)
			if err != nil {
				t.Fatal(err)
			}
			result, err = workspace.BindCurrent(ctx, location, "quote", false)
			if err != nil || result.Status != BindingReused || !result.Mutated {
				t.Fatalf("same identity was not repaired: %+v %v", result, err)
			}
			if err := verifyWindowIdentity(requireIntegrationWindow(t, ctx, plain, id), "quote"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
