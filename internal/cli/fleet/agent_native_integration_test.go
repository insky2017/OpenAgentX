package fleet

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	fleetmodel "openagentx/internal/fleet"
)

// Exercises the real tmux boundary and checks that native dispatch occurs only
// after adoption succeeds. The bridge seam does not run a model or a Worker.
func TestIsolatedTmuxNativeOpenIdentity(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	for _, scenario := range []string{"exact-pane", "same-identity", "overview", "pane-one", "other-identity", "duplicate-name", "duplicate-marker", "non-oax", "missing-pane", "missing-target"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			runner := fleetmodel.ExecRunner{SocketName: fmt.Sprintf("oax-native-identity-%d-%d", os.Getpid(), time.Now().UnixNano())}
			t.Cleanup(func() { _, _ = runner.Run(context.Background(), "kill-server") })
			run := func(args ...string) string {
				t.Helper()
				out, err := runner.Run(ctx, args...)
				if err != nil {
					t.Fatal(err)
				}
				return strings.TrimSpace(out)
			}
			session := "OAX"
			if scenario == "non-oax" {
				session = "other"
			}
			target := run("new-session", "-d", "-P", "-F", "#{window_id}", "-s", session, "-n", "scratch", "sleep", "86400")
			run("set-option", "-w", "-t", target, "pane-base-index", "0")
			run("set-option", "-w", "-t", target, "automatic-rename", "off")
			run("split-window", "-d", "-t", target, "sleep", "86400")
			pane := run("display-message", "-p", "-t", target+".0", "#{pane_id}")
			// Keep another window active: an implicit tmux target would bind the wrong one.
			other := run("new-window", "-P", "-F", "#{window_id}", "-t", "="+session, "-n", "unrelated", "sleep", "86400")
			run("set-option", "-w", "-t", other, "automatic-rename", "off")
			switch scenario {
			case "same-identity", "other-identity":
				agent := "quote"
				if scenario == "other-identity" {
					agent = "risk"
				}
				run("rename-window", "-t", target, agent)
				run("set-option", "-w", "-t", target, fleetmodel.ManagedOption, "1")
				run("set-option", "-w", "-t", target, fleetmodel.AgentIDOption, agent)
			case "overview":
				run("rename-window", "-t", target, "overview")
			case "pane-one":
				pane = run("display-message", "-p", "-t", target+".1", "#{pane_id}")
			case "duplicate-name":
				run("rename-window", "-t", other, "quote")
			case "duplicate-marker":
				run("rename-window", "-t", target, "quote")
				for _, id := range []string{target, other} {
					run("set-option", "-w", "-t", id, fleetmodel.ManagedOption, "1")
					run("set-option", "-w", "-t", id, fleetmodel.AgentIDOption, "quote")
				}
			case "missing-pane":
				pane = ""
			case "missing-target":
				pane = "%99999999"
			}
			before := run("list-panes", "-a", "-F", "#{pane_id}:#{pane_index}:#{pane_pid}")
			beforeTarget := run("show-options", "-w", "-t", target)
			beforeOther := run("show-options", "-w", "-t", other)
			t.Setenv("TMUX", "isolated-native-test")
			t.Setenv("TMUX_PANE", pane)
			called := false
			var stderr bytes.Buffer
			deps := Dependencies{Tmux: runner, IsInteractive: func() bool { return true }, Err: &stderr, OpenNative: func(context.Context, NativeOpenRequest) error { called = true; return nil }}
			err := openAgentNative(ctx, agentOptions{id: "quote"}, deps)
			success := scenario == "exact-pane" || scenario == "same-identity" || scenario == "non-oax"
			if success {
				if err != nil || !called {
					t.Fatalf("native dispatch rejected: called=%v err=%v", called, err)
				}
			} else if err == nil || called {
				t.Fatalf("unsafe native dispatch: called=%v err=%v", called, err)
			}
			if after := run("list-panes", "-a", "-F", "#{pane_id}:#{pane_index}:#{pane_pid}"); after != before {
				t.Fatalf("pane processes changed: %s -> %s", before, after)
			}
			if after := run("show-options", "-w", "-t", other); after != beforeOther {
				t.Fatalf("unrelated window options changed: %s -> %s", beforeOther, after)
			}
			if success && scenario != "non-oax" {
				for name, want := range map[string]string{fleetmodel.ManagedOption: "1", fleetmodel.AgentIDOption: "quote", "automatic-rename": "off", "allow-rename": "off", "remain-on-exit": "on", "pane-border-format": fleetmodel.NativeTerminalLabel} {
					if got := run("show-options", "-w", "-v", "-t", target, name); got != want {
						t.Fatalf("%s=%q want %q", name, got, want)
					}
				}
				if got := run("display-message", "-p", "-t", target, "#{window_name}"); got != "quote" {
					t.Fatalf("window name=%s", got)
				}
			} else if after := run("show-options", "-w", "-t", target); after != beforeTarget {
				t.Fatalf("rejected/skipped native adoption changed options: %s -> %s", beforeTarget, after)
			}
			t.Logf("isolated server=%s scenario=%s exact pane=%s bridge called=%v processes=%q result=%v", runner.SocketName, scenario, pane, called, before, err)
		})
	}
}
