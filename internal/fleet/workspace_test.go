package fleet

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type fakeWindow struct {
	id      string
	name    string
	panes   []int
	managed OptionValue
	agent   OptionValue
}

type fakeRunner struct {
	session        bool
	currentSession string
	currentID      string
	currentPane    int
	windows        []*fakeWindow
	calls          []string
	nextID         int
	fail           func(string, int) bool
	before         func(*fakeRunner, string, int)
}

func (r *fakeRunner) Run(_ context.Context, args ...string) (string, error) {
	call := strings.Join(args, " ")
	r.calls = append(r.calls, call)
	count := len(r.calls)
	if r.before != nil {
		r.before(r, call, count)
	}
	if r.fail != nil && r.fail(call, count) {
		return "", errors.New("injected tmux failure")
	}
	switch args[0] {
	case "has-session":
		if !r.session {
			return "", errors.New("missing")
		}
	case "display-message":
		window := r.window(r.currentID)
		if window == nil {
			return "", errors.New("current window missing")
		}
		session := r.currentSession
		if session == "" {
			session = SessionName
		}
		return fmt.Sprintf("%s\t%s\t%s\t%d\n", session, window.id, window.name, r.currentPane), nil
	case "list-windows":
		var output strings.Builder
		for _, window := range r.windows {
			fmt.Fprintf(&output, "%s\t%s\n", window.id, window.name)
		}
		return output.String(), nil
	case "list-panes":
		window := r.window(valueAfter(args, "-t"))
		if window == nil {
			return "", errors.New("window missing")
		}
		var output strings.Builder
		for _, pane := range window.panes {
			fmt.Fprintf(&output, "%d\n", pane)
		}
		return output.String(), nil
	case "show-options":
		window := r.window(valueAfter(args, "-t"))
		if window == nil {
			return "", errors.New("window missing")
		}
		var output strings.Builder
		if window.managed.Set {
			fmt.Fprintf(&output, "%s %s\n", ManagedOption, window.managed.Value)
		}
		if window.agent.Set {
			fmt.Fprintf(&output, "%s %s\n", AgentIDOption, window.agent.Value)
		}
		return output.String(), nil
	case "new-session":
		r.session = true
		window := r.addWindow(valueAfter(args, "-n"), []int{0})
		r.currentID = window.id
		return window.id + "\n", nil
	case "new-window":
		window := r.addWindow(valueAfter(args, "-n"), []int{0})
		return window.id + "\n", nil
	case "set-option":
		window := r.window(valueAfter(args, "-t"))
		if window == nil {
			return "", errors.New("window missing")
		}
		unset := containsArg(args, "-u")
		for index, arg := range args {
			if arg != ManagedOption && arg != AgentIDOption {
				continue
			}
			value := OptionValue{}
			if !unset && index+1 < len(args) {
				value = OptionValue{Set: true, Value: args[index+1]}
			}
			if arg == ManagedOption {
				window.managed = value
			} else {
				window.agent = value
			}
		}
	case "rename-window":
		window := r.window(valueAfter(args, "-t"))
		if window == nil {
			return "", errors.New("window missing")
		}
		window.name = args[len(args)-1]
	default:
		return "", fmt.Errorf("unexpected tmux command %q", args[0])
	}
	return "", nil
}

func (r *fakeRunner) addWindow(name string, panes []int) *fakeWindow {
	r.nextID++
	window := &fakeWindow{id: "@" + strconv.Itoa(r.nextID), name: name, panes: append([]int(nil), panes...)}
	r.windows = append(r.windows, window)
	return window
}

func (r *fakeRunner) window(id string) *fakeWindow {
	for _, window := range r.windows {
		if window.id == id {
			return window
		}
	}
	return nil
}

func valueAfter(args []string, flag string) string {
	for index := range args {
		if args[index] == flag && index+1 < len(args) {
			return args[index+1]
		}
	}
	return ""
}

func containsArg(args []string, expected string) bool {
	for _, arg := range args {
		if arg == expected {
			return true
		}
	}
	return false
}

func managedWindow(id, name string, panes ...int) *fakeWindow {
	window := &fakeWindow{id: id, name: name, panes: append([]int(nil), panes...), managed: OptionValue{Set: true, Value: "1"}}
	if name != OverviewWindow {
		window.agent = OptionValue{Set: true, Value: name}
	}
	return window
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

func TestWorkspaceCreatesMissingOAXSessionWithMarkers(t *testing.T) {
	runner := &fakeRunner{}
	report, err := workspace(runner).Reconcile(context.Background(), workspaceManifest())
	if err != nil {
		t.Fatal(err)
	}
	if !report.SessionCreated || strings.Join(report.Created, ",") != "overview,quote,risk" {
		t.Fatalf("unexpected report: %+v", report)
	}
	if runner.window("@1").agent.Set || runner.window("@2").agent.Value != "quote" || runner.window("@3").agent.Value != "risk" {
		t.Fatalf("created markers do not match workspace contract: %+v", runner.windows)
	}
	joined := strings.Join(runner.calls, "\n")
	if !strings.Contains(joined, "new-session -d -P -F #{window_id} -s OAX -n overview") ||
		!strings.Contains(joined, "new-window -d -P -F #{window_id} -t =OAX -n quote openagentx console attach") {
		t.Fatalf("workspace did not use OAX/formal Console command: %v", runner.calls)
	}
	assertNoForbiddenTmux(t, runner.calls)
}

func TestWorkspacePreservesExtraPanesUnmanagedAndOrphanedWindows(t *testing.T) {
	runner := &fakeRunner{session: true, nextID: 4, windows: []*fakeWindow{
		managedWindow("@1", OverviewWindow, 0, 1, 2),
		managedWindow("@2", "risk", 0, 1),
		{id: "@3", name: "notes", panes: []int{0, 1, 2}},
		managedWindow("@4", "old-agent", 0, 2),
	}}
	report, err := workspace(runner).Reconcile(context.Background(), workspaceManifest())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(report.Created, ",") != "quote" || !reflect.DeepEqual(report.Unmanaged, []string{"notes"}) || !reflect.DeepEqual(report.Orphaned, []string{"old-agent"}) {
		t.Fatalf("unexpected report: %+v", report)
	}
	if !reflect.DeepEqual(runner.window("@1").panes, []int{0, 1, 2}) || !reflect.DeepEqual(runner.window("@3").panes, []int{0, 1, 2}) {
		t.Fatalf("extra panes changed: %+v", runner.windows)
	}
	assertNoForbiddenTmux(t, runner.calls)
}

func TestWorkspaceUsesNamesAndMarkersAcrossWindowReorder(t *testing.T) {
	runner := &fakeRunner{session: true, windows: []*fakeWindow{
		managedWindow("@8", "risk", 0), managedWindow("@3", "quote", 0, 1), managedWindow("@5", OverviewWindow, 0),
	}}
	report, err := workspace(runner).Reconcile(context.Background(), workspaceManifest())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Created) != 0 || len(report.Reused) != 3 {
		t.Fatalf("reordered windows were not reused: %+v", report)
	}
	assertNoMutationTmux(t, runner.calls)
}

func TestWorkspaceFailsClosedBeforeMutationOnEveryConflictClass(t *testing.T) {
	tests := map[string][]*fakeWindow{
		"duplicate name":         {managedWindow("@1", OverviewWindow, 0), managedWindow("@2", "quote", 0), managedWindow("@3", "quote", 0)},
		"missing pane zero":      {managedWindow("@1", OverviewWindow, 1), managedWindow("@2", "quote", 0)},
		"unmanaged target":       {managedWindow("@1", OverviewWindow, 0), {id: "@2", name: "quote", panes: []int{0}}},
		"invalid managed marker": {managedWindow("@1", OverviewWindow, 0), {id: "@2", name: "quote", panes: []int{0}, managed: OptionValue{Set: true, Value: "yes"}, agent: OptionValue{Set: true, Value: "quote"}}},
		"marker without managed": {managedWindow("@1", OverviewWindow, 0), {id: "@2", name: "notes", panes: []int{0}, agent: OptionValue{Set: true, Value: "quote"}}},
		"name marker mismatch":   {managedWindow("@1", OverviewWindow, 0), {id: "@2", name: "quote", panes: []int{0}, managed: OptionValue{Set: true, Value: "1"}, agent: OptionValue{Set: true, Value: "risk"}}},
		"overview agent marker":  {{id: "@1", name: OverviewWindow, panes: []int{0}, managed: OptionValue{Set: true, Value: "1"}, agent: OptionValue{Set: true, Value: "quote"}}},
	}
	for name, windows := range tests {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{session: true, windows: windows}
			if _, err := workspace(runner).Reconcile(context.Background(), workspaceManifest()); err == nil {
				t.Fatal("expected conflict")
			}
			assertNoMutationTmux(t, runner.calls)
		})
	}
}

func TestWorkspaceSecondPreflightCatchesTOCTOUWithoutMutation(t *testing.T) {
	runner := &fakeRunner{session: true, windows: []*fakeWindow{managedWindow("@1", OverviewWindow, 0)}}
	listCalls := 0
	runner.before = func(r *fakeRunner, call string, _ int) {
		if strings.HasPrefix(call, "list-windows") {
			listCalls++
			if listCalls == 2 {
				r.windows = append(r.windows, &fakeWindow{id: "@9", name: "quote", panes: []int{0}})
			}
		}
	}
	if _, err := workspace(runner).Reconcile(context.Background(), workspaceManifest()); err == nil {
		t.Fatal("expected second-preflight conflict")
	}
	assertNoMutationTmux(t, runner.calls)
}

func TestWorkspaceInspectUsesPerWindowPaneAndOptionQueries(t *testing.T) {
	runner := &fakeRunner{session: true, windows: []*fakeWindow{managedWindow("@7", "quote", 0, 1, 2)}}
	windows, err := workspace(runner).Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(windows) != 1 || !reflect.DeepEqual(windows[0].PaneIndices, []int{0, 1, 2}) || windows[0].AgentID.Value != "quote" {
		t.Fatalf("unexpected structured inspection: %+v", windows)
	}
	joined := strings.Join(runner.calls, "\n")
	if !strings.Contains(joined, "list-panes -t @7 -F #{pane_index}") || !strings.Contains(joined, "show-options -w -t @7") {
		t.Fatalf("missing explicit pane/option queries: %v", runner.calls)
	}
	for _, forbidden := range []string{"pane_current_command", "pane_id", "window_index"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("inspection used forbidden identity field %q: %v", forbidden, runner.calls)
		}
	}
}

func TestWorkspaceInspectFailsClosedOnEachStructuredQuery(t *testing.T) {
	for name, prefix := range map[string]string{
		"session": "has-session",
		"windows": "list-windows",
		"panes":   "list-panes",
		"options": "show-options",
	} {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{session: true, windows: []*fakeWindow{managedWindow("@1", "quote", 0)}}
			runner.fail = func(call string, _ int) bool { return strings.HasPrefix(call, prefix) }
			if _, err := workspace(runner).Inspect(context.Background()); err == nil {
				t.Fatal("expected structured query failure")
			}
			assertNoMutationTmux(t, runner.calls)
		})
	}
}

func assertNoForbiddenTmux(t *testing.T, calls []string) {
	t.Helper()
	for _, call := range calls {
		for _, forbidden := range []string{"kill-", "delete", "move-window", "send-keys", "capture-pane", "paste-buffer"} {
			if strings.Contains(call, forbidden) {
				t.Fatalf("forbidden tmux command used: %s", call)
			}
		}
	}
}

func assertNoMutationTmux(t *testing.T, calls []string) {
	t.Helper()
	for _, call := range calls {
		for _, mutation := range []string{"new-session", "new-window", "set-option", "rename-window"} {
			if strings.Contains(call, mutation) {
				t.Fatalf("conflict caused tmux mutation: %v", calls)
			}
		}
	}
}
