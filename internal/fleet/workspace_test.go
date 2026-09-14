package fleet

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeRunner struct {
	session bool
	windows string
	calls   []string
}

func (r *fakeRunner) Run(_ context.Context, args ...string) (string, error) {
	call := strings.Join(args, " ")
	r.calls = append(r.calls, call)
	if args[0] == "has-session" && !r.session {
		return "", errors.New("missing")
	}
	if args[0] == "list-windows" {
		return r.windows, nil
	}
	return "", nil
}

func workspaceManifest() Manifest {
	return Manifest{Version: 1, Session: SessionName, Agents: []Agent{
		{AgentID: "quote", IdentityFile: "quote.identity.yaml", WorkerConfig: "quote.worker.yaml", Enabled: true},
		{AgentID: "risk", IdentityFile: "risk.identity.yaml", WorkerConfig: "risk.worker.yaml"},
	}}
}

func workspace(runner CommandRunner) Workspace {
	return Workspace{Runner: runner, ConsoleCommand: func(agentID string) []string {
		return []string{"openagentx", "console", "attach", "--socket", "/run/openagentx.sock", "--agent", agentID}
	}}
}

func TestWorkspaceCreatesMissingSessionWithoutDestructiveCommands(t *testing.T) {
	runner := &fakeRunner{}
	report, err := workspace(runner).Reconcile(context.Background(), workspaceManifest())
	if err != nil {
		t.Fatal(err)
	}
	if !report.SessionCreated || strings.Join(report.Created, ",") != "overview,quote,risk" {
		t.Fatalf("unexpected report: %+v", report)
	}
	assertNoDestructiveTmux(t, runner.calls)
	joined := strings.Join(runner.calls, "\n")
	if !strings.Contains(joined, "new-window -d -t =agentx -n quote openagentx console attach --socket /run/openagentx.sock --agent quote") {
		t.Fatalf("Agent pane did not start the formal Console client: %v", runner.calls)
	}
}

func TestWorkspacePreservesExtraAndCreatesOnlyMissingWindow(t *testing.T) {
	runner := &fakeRunner{session: true, windows: "risk\t1\t0\topenagentx\t1\noverview\t1\t0\tzsh\t1\nnotes\t1\t0\tvim\t\nold-agent\t1\t0\topenagentx\t1\n"}
	report, err := workspace(runner).Reconcile(context.Background(), workspaceManifest())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(report.Created, ",") != "quote" || strings.Join(report.Unmanaged, ",") != "notes" || strings.Join(report.Orphaned, ",") != "old-agent" {
		t.Fatalf("unexpected report: %+v", report)
	}
	assertNoDestructiveTmux(t, runner.calls)
}

func TestWorkspaceUsesNamesAcrossWindowReorder(t *testing.T) {
	runner := &fakeRunner{session: true, windows: "risk\t1\t0\topenagentx\t1\nquote\t1\t0\tzsh\t1\noverview\t1\t0\tbash\t1\n"}
	report, err := workspace(runner).Reconcile(context.Background(), workspaceManifest())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Created) != 0 || len(report.Reused) != 3 {
		t.Fatalf("reordered windows were not reused: %+v", report)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "new-window") {
			t.Fatalf("window reorder caused mutation: %s", call)
		}
	}
}

func TestWorkspaceFailsClosedBeforeMutationOnDuplicateOrConflict(t *testing.T) {
	for name, windows := range map[string]string{
		"duplicate":  "overview\t1\t0\tzsh\t1\nquote\t1\t0\tzsh\t1\nquote\t1\t0\tzsh\t1\n",
		"panes":      "overview\t1\t0\tzsh\t1\nquote\t2\t0\tzsh\t1\n",
		"pane-index": "overview\t1\t0\tzsh\t1\nquote\t1\t1\tzsh\t1\n",
		"unknown":    "overview\t1\t0\tzsh\t1\nquote\t1\t0\tpython\t\n",
	} {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{session: true, windows: windows}
			if _, err := workspace(runner).Reconcile(context.Background(), workspaceManifest()); err == nil {
				t.Fatal("expected conflict")
			}
			if len(runner.calls) != 2 {
				t.Fatalf("conflict caused side effects: %v", runner.calls)
			}
		})
	}
}

func TestWorkspacePreflightDoesNotMutateMissingOrConflictingSession(t *testing.T) {
	for name, runner := range map[string]*fakeRunner{
		"missing":  {},
		"conflict": {session: true, windows: "overview\t1\t0\tzsh\t1\nquote\t1\t0\tpython\t\n"},
	} {
		t.Run(name, func(t *testing.T) {
			_ = workspace(runner).Preflight(context.Background(), workspaceManifest())
			for _, call := range runner.calls {
				if strings.Contains(call, "new-session") || strings.Contains(call, "new-window") || strings.Contains(call, "set-option") {
					t.Fatalf("preflight mutated tmux state: %v", runner.calls)
				}
			}
		})
	}
}

func assertNoDestructiveTmux(t *testing.T, calls []string) {
	t.Helper()
	for _, call := range calls {
		for _, forbidden := range []string{"kill-", "delete", "rename", "move-window", "send-keys", "capture-pane", "paste-buffer"} {
			if strings.Contains(call, forbidden) {
				t.Fatalf("destructive or control-plane tmux command used: %s", call)
			}
		}
	}
}
