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
		format := valueAfter(args, "-F")
		target := valueAfter(args, "-t")
		window := r.window(r.currentID)
		if target != "" {
			window = r.window(target)
		}
		if window == nil {
			return "", errors.New("current window missing")
		}
		switch format {
		case windowIDFormat:
			return window.id + "\n", nil
		case windowNameFormat:
			return window.name + "\n", nil
		case paneIndexFormat:
			return strconv.Itoa(r.currentPane) + "\n", nil
		case sessionNameFormat:
			session := r.currentSession
			if session == "" {
				session = SessionName
			}
			return session + "\n", nil
		default:
			return "", fmt.Errorf("unexpected display format %q", format)
		}
	case "list-windows":
		var output strings.Builder
		for _, window := range r.windows {
			fmt.Fprintln(&output, window.id)
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
	case "respawn-pane":
		window := r.window(strings.TrimSuffix(valueAfter(args, "-t"), ".0"))
		if window == nil || !containsInt(window.panes, 0) {
			return "", errors.New("sentinel pane missing")
		}
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

func containsInt(values []int, expected int) bool {
	for _, value := range values {
		if value == expected {
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
		!strings.Contains(joined, "new-window -d -P -F #{window_id} -t =OAX -n quote sh -c") ||
		!strings.Contains(joined, "respawn-pane -k -t @2.0 -- openagentx console attach") {
		t.Fatalf("workspace did not use OAX/formal Console command: %v", runner.calls)
	}
	if strings.Contains(joined, "-n overview sh -c") {
		t.Fatalf("overview was incorrectly replaced by the Agent provisioning sentinel: %v", runner.calls)
	}
	assertNoForbiddenTmux(t, runner.calls)
}

func TestWorkspaceVerifiesCreatedAgentWindowBeforeStartingConsole(t *testing.T) {
	runner := &fakeRunner{session: true, windows: []*fakeWindow{managedWindow("@1", OverviewWindow, 0)}, nextID: 1}
	manifest := Manifest{Version: 1, Session: SessionName, Agents: []Agent{{AgentID: "quote", IdentityFile: "a", WorkerConfig: "b"}}}
	report, err := workspace(runner).Reconcile(context.Background(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(report.Created, []string{"quote"}) {
		t.Fatalf("unexpected report: %+v", report)
	}
	newIndex := callIndex(runner.calls, "new-window -d -P -F #{window_id} -t =OAX -n quote sh -c")
	managedIndex := callIndex(runner.calls, "set-option -w -t @2 "+ManagedOption+" 1")
	agentIndex := callIndex(runner.calls, "set-option -w -t @2 "+AgentIDOption+" quote")
	verifyIndex := callIndexAfter(runner.calls, "list-panes -t @2 -F #{pane_index}", agentIndex)
	startIndex := callIndex(runner.calls, "respawn-pane -k -t @2.0 -- openagentx console attach")
	if newIndex < 0 || managedIndex <= newIndex || agentIndex <= managedIndex || verifyIndex <= agentIndex || startIndex <= verifyIndex {
		t.Fatalf("Console started before pane/marker verification: %v", runner.calls)
	}
}

func TestWorkspaceConsoleStartFailureRetainsConfiguredWindowWithoutReportingSuccess(t *testing.T) {
	runner := &fakeRunner{session: true, windows: []*fakeWindow{managedWindow("@1", OverviewWindow, 0)}, nextID: 1}
	runner.fail = func(call string, _ int) bool { return strings.HasPrefix(call, "respawn-pane -k -t @2.0") }
	manifest := Manifest{Version: 1, Session: SessionName, Agents: []Agent{{AgentID: "quote", IdentityFile: "a", WorkerConfig: "b"}}}
	report, err := workspace(runner).Reconcile(context.Background(), manifest)
	if err == nil || !strings.Contains(err.Error(), "retained for diagnosis") {
		t.Fatalf("expected diagnostic Console start failure, got report=%+v err=%v", report, err)
	}
	quote := runner.window("@2")
	if quote == nil || quote.name != "quote" || quote.managed.Value != "1" || quote.agent.Value != "quote" || len(report.Created) != 0 {
		t.Fatalf("failed Agent window state was lost or reported successful: report=%+v window=%+v", report, quote)
	}
}

func TestWorkspaceProvisioningFailuresRetainDiagnosticWindowWithoutReportingSuccess(t *testing.T) {
	tests := map[string]struct {
		failurePrefix string
		failListCall  int
		emptyAtStart  bool
	}{
		"pane base":         {failurePrefix: "set-option -w -t @2 pane-base-index"},
		"remain on exit":    {failurePrefix: "set-option -w -t @2 remain-on-exit"},
		"managed marker":    {failurePrefix: "set-option -w -t @2 " + ManagedOption},
		"Agent marker":      {failurePrefix: "set-option -w -t @2 " + AgentIDOption},
		"pre-start verify":  {failListCall: 3},
		"Console start":     {failurePrefix: "respawn-pane -k -t @2.0"},
		"post-start verify": {failListCall: 4},
		"empty command":     {emptyAtStart: true},
	}
	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{session: true, windows: []*fakeWindow{managedWindow("@1", OverviewWindow, 0)}, nextID: 1}
			listCalls := 0
			runner.fail = func(call string, _ int) bool {
				if strings.HasPrefix(call, "list-windows") {
					listCalls++
					if listCalls == testCase.failListCall {
						return true
					}
				}
				return testCase.failurePrefix != "" && strings.HasPrefix(call, testCase.failurePrefix)
			}
			commandCalls := 0
			candidate := Workspace{Runner: runner, ConsoleCommand: func(agentID string) []string {
				commandCalls++
				if testCase.emptyAtStart && commandCalls > 1 {
					return nil
				}
				return []string{"openagentx", "console", "attach", "--agent", agentID}
			}}
			manifest := Manifest{Version: 1, Session: SessionName, Agents: []Agent{{AgentID: "quote", IdentityFile: "a", WorkerConfig: "b"}}}
			report, err := candidate.Reconcile(context.Background(), manifest)
			if err == nil || !strings.Contains(err.Error(), "retained for diagnosis") {
				t.Fatalf("expected retained diagnostic failure, got report=%+v err=%v", report, err)
			}
			if len(report.Created) != 0 || runner.window("@2") == nil {
				t.Fatalf("failed provisioning was reported successful or removed: report=%+v windows=%+v", report, runner.windows)
			}
		})
	}
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

func TestWorkspacePreservesIrrelevantUnmanagedDuplicateNamesWithoutPaneZero(t *testing.T) {
	runner := &fakeRunner{session: true, nextID: 3, windows: []*fakeWindow{
		managedWindow("@1", OverviewWindow, 0),
		{id: "@2", name: "scratch", panes: []int{1}},
		{id: "@3", name: "scratch", panes: []int{1, 2}},
	}}
	report, err := workspace(runner).Reconcile(context.Background(), workspaceManifest())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(report.Unmanaged, []string{"scratch", "scratch"}) || !reflect.DeepEqual(runner.window("@2").panes, []int{1}) || !reflect.DeepEqual(runner.window("@3").panes, []int{1, 2}) {
		t.Fatalf("irrelevant unmanaged windows were not preserved: report=%+v windows=%+v", report, runner.windows)
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
	if !strings.Contains(joined, "list-windows -t =OAX -F #{window_id}") ||
		!strings.Contains(joined, "display-message -p -t @7 -F #{window_name}") ||
		!strings.Contains(joined, "list-panes -t @7 -F #{pane_index}") || !strings.Contains(joined, "show-options -w -t @7") {
		t.Fatalf("missing explicit pane/option queries: %v", runner.calls)
	}
	for _, call := range runner.calls {
		if strings.ContainsAny(call, "\t\r") {
			t.Fatalf("structured inspection used a control-character delimiter: %q", call)
		}
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
		for _, mutation := range []string{"new-session", "new-window", "set-option", "rename-window", "respawn-pane"} {
			if strings.Contains(call, mutation) {
				t.Fatalf("conflict caused tmux mutation: %v", calls)
			}
		}
	}
}

func callIndex(calls []string, prefix string) int {
	return callIndexAfter(calls, prefix, -1)
}

func callIndexAfter(calls []string, prefix string, after int) int {
	for index := after + 1; index < len(calls); index++ {
		if strings.HasPrefix(calls[index], prefix) {
			return index
		}
	}
	return -1
}
