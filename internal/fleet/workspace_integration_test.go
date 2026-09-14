package fleet

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func isolatedTmux(t *testing.T) (context.Context, ExecRunner) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	server := fmt.Sprintf("openagentx-adr008-task05-%d-%d", os.Getpid(), time.Now().UnixNano())
	runner := ExecRunner{SocketName: server}
	t.Cleanup(func() {
		_, _ = runner.Run(context.Background(), "kill-server")
	})
	return context.Background(), runner
}

func createManagedIntegrationWindow(t *testing.T, ctx context.Context, runner ExecRunner, session, name, agentID string) string {
	t.Helper()
	var output string
	var err error
	if session == SessionName && name == OverviewWindow {
		output, err = runner.Run(ctx, "new-session", "-d", "-P", "-F", "#{window_id}", "-s", session, "-n", name)
	} else {
		output, err = runner.Run(ctx, "new-window", "-d", "-P", "-F", "#{window_id}", "-t", "="+session, "-n", name, "sleep", "30")
	}
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(output)
	for _, args := range [][]string{
		{"set-option", "-w", "-t", id, "pane-base-index", "0"},
		{"set-option", "-w", "-t", id, ManagedOption, "1"},
	} {
		if _, err := runner.Run(ctx, args...); err != nil {
			t.Fatal(err)
		}
	}
	if agentID == "" {
		if _, err := runner.Run(ctx, "set-option", "-w", "-u", "-t", id, AgentIDOption); err != nil {
			t.Fatal(err)
		}
	} else if _, err := runner.Run(ctx, "set-option", "-w", "-t", id, AgentIDOption, agentID); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestIsolatedTmuxReconcilePreservesPaneZeroOneTwoAndIgnoresOldAgentx(t *testing.T) {
	ctx, runner := isolatedTmux(t)
	overviewID := createManagedIntegrationWindow(t, ctx, runner, SessionName, OverviewWindow, "")
	if _, err := runner.Run(ctx, "split-window", "-d", "-t", overviewID); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(ctx, "split-window", "-d", "-t", overviewID); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(ctx, "new-session", "-d", "-s", "agentx", "-n", "quote"); err != nil {
		t.Fatal(err)
	}

	manifest := Manifest{Version: 1, Session: SessionName, Agents: []Agent{{AgentID: "quote", IdentityFile: "quote.identity.yaml", WorkerConfig: "quote.worker.yaml"}}}
	workspace := Workspace{Runner: runner, ConsoleCommand: func(string) []string { return []string{"sleep", "30"} }}
	report, err := workspace.Reconcile(ctx, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(report.Created, []string{"quote"}) || !reflect.DeepEqual(report.Reused, []string{"overview"}) {
		t.Fatalf("unexpected report: %+v", report)
	}
	windows, err := workspace.Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(windows) != 2 {
		t.Fatalf("OAX inspection included another session: %+v", windows)
	}
	for _, window := range windows {
		switch window.Name {
		case OverviewWindow:
			if !reflect.DeepEqual(window.PaneIndices, []int{0, 1, 2}) || window.AgentID.Set {
				t.Fatalf("overview panes/markers changed: %+v", window)
			}
		case "quote":
			if !window.HasPane(0) || !window.Managed.Set || window.AgentID.Value != "quote" {
				t.Fatalf("Agent window is not compatible: %+v", window)
			}
		default:
			t.Fatalf("unexpected OAX window: %+v", window)
		}
	}
}

func TestIsolatedTmuxCreatesOAXWithoutMigratingExistingAgentx(t *testing.T) {
	ctx, runner := isolatedTmux(t)
	if _, err := runner.Run(ctx, "new-session", "-d", "-s", "agentx", "-n", "legacy-window"); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{Version: 1, Session: SessionName, Agents: []Agent{{AgentID: "quote", IdentityFile: "a", WorkerConfig: "b"}}}
	workspace := Workspace{Runner: runner, ConsoleCommand: func(string) []string { return []string{"sleep", "30"} }}
	report, err := workspace.Reconcile(ctx, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !report.SessionCreated || !reflect.DeepEqual(report.Created, []string{"overview", "quote"}) {
		t.Fatalf("unexpected independent OAX report: %+v", report)
	}
	legacy, err := runner.Run(ctx, "list-windows", "-t", "=agentx", "-F", "#{window_name}")
	if err != nil || strings.TrimSpace(legacy) != "legacy-window" {
		t.Fatalf("legacy agentx workspace changed: output=%q err=%v", legacy, err)
	}
}

func TestIsolatedTmuxAttachRejectsWrongPaneAndMissingPaneZero(t *testing.T) {
	t.Run("wrong pane", func(t *testing.T) {
		ctx, runner := isolatedTmux(t)
		windowID := createManagedIntegrationWindow(t, ctx, runner, SessionName, OverviewWindow, "")
		if _, err := runner.Run(ctx, "rename-window", "-t", windowID, "quote"); err != nil {
			t.Fatal(err)
		}
		if _, err := runner.Run(ctx, "set-option", "-w", "-t", windowID, AgentIDOption, "quote"); err != nil {
			t.Fatal(err)
		}
		if _, err := runner.Run(ctx, "split-window", "-d", "-t", windowID); err != nil {
			t.Fatal(err)
		}
		wrongPaneRunner := runner
		wrongPaneRunner.CurrentTarget = windowID + ".1"
		if _, err := (Workspace{Runner: wrongPaneRunner}).PreflightAttach(ctx); err == nil || !strings.Contains(err.Error(), ".1") {
			t.Fatalf("wrong pane was not rejected clearly: %v", err)
		}
	})

	t.Run("missing pane zero", func(t *testing.T) {
		ctx, runner := isolatedTmux(t)
		windowID := createManagedIntegrationWindow(t, ctx, runner, SessionName, OverviewWindow, "")
		if _, err := runner.Run(ctx, "set-option", "-w", "-t", windowID, "pane-base-index", "1"); err != nil {
			t.Fatal(err)
		}
		manifest := Manifest{Version: 1, Session: SessionName, Agents: []Agent{{AgentID: "quote", IdentityFile: "a", WorkerConfig: "b"}}}
		workspace := Workspace{Runner: runner, ConsoleCommand: func(string) []string { return []string{"sleep", "30"} }}
		if err := workspace.Preflight(ctx, manifest); err == nil || !strings.Contains(err.Error(), "pane 0") {
			t.Fatalf("missing pane zero was not rejected: %v", err)
		}
	})
}

func TestIsolatedTmuxRejectsDuplicateNameAndMarkerBeforeReconcileMutation(t *testing.T) {
	for name, configure := range map[string]func(t *testing.T, ctx context.Context, runner ExecRunner){
		"duplicate name": func(t *testing.T, ctx context.Context, runner ExecRunner) {
			createManagedIntegrationWindow(t, ctx, runner, SessionName, "quote", "quote")
			if _, err := runner.Run(ctx, "new-window", "-d", "-n", "quote", "-t", "="+SessionName, "sleep", "30"); err != nil {
				t.Fatal(err)
			}
		},
		"duplicate marker": func(t *testing.T, ctx context.Context, runner ExecRunner) {
			createManagedIntegrationWindow(t, ctx, runner, SessionName, "quote", "quote")
			createManagedIntegrationWindow(t, ctx, runner, SessionName, "risk", "quote")
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, runner := isolatedTmux(t)
			createManagedIntegrationWindow(t, ctx, runner, SessionName, OverviewWindow, "")
			configure(t, ctx, runner)
			manifest := Manifest{Version: 1, Session: SessionName, Agents: []Agent{{AgentID: "quote", IdentityFile: "a", WorkerConfig: "b"}}}
			workspace := Workspace{Runner: runner, ConsoleCommand: func(string) []string { return []string{"sleep", "30"} }}
			if _, err := workspace.Reconcile(ctx, manifest); err == nil {
				t.Fatal("expected isolated tmux conflict")
			}
		})
	}
}
