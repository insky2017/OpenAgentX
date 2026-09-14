package fleet

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func attachRunner(current *fakeWindow, extras ...*fakeWindow) *fakeRunner {
	windows := append([]*fakeWindow{current}, extras...)
	return &fakeRunner{session: true, currentID: current.id, currentPane: 0, windows: windows}
}

func TestAttachPreflightRequiresExactOAXPaneZeroAndCompatibleTopology(t *testing.T) {
	tests := map[string]struct {
		runner *fakeRunner
		text   string
	}{
		"wrong session": {runner: func() *fakeRunner {
			r := attachRunner(managedWindow("@1", "quote", 0))
			r.currentSession = "agentx"
			return r
		}(), text: "switch to OAX"},
		"wrong pane":        {runner: func() *fakeRunner { r := attachRunner(managedWindow("@1", "quote", 0, 1)); r.currentPane = 1; return r }(), text: "OAX:quote.1"},
		"missing pane zero": {runner: attachRunner(managedWindow("@1", "quote", 1, 2)), text: "no pane 0"},
		"overview":          {runner: attachRunner(managedWindow("@1", OverviewWindow, 0)), text: "switch to an Agent window"},
		"duplicate window":  {runner: attachRunner(managedWindow("@1", "quote", 0), managedWindow("@2", "quote", 0)), text: "duplicated"},
		"marker mismatch":   {runner: attachRunner(&fakeWindow{id: "@1", name: "quote", panes: []int{0}, managed: OptionValue{Set: true, Value: "1"}, agent: OptionValue{Set: true, Value: "risk"}}), text: "marker/name mismatch"},
	}
	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			location, err := (Workspace{Runner: testCase.runner}).PreflightAttach(context.Background())
			if err == nil || location.windowID != "" || !strings.Contains(err.Error(), testCase.text) {
				t.Fatalf("location=%+v err=%v", location, err)
			}
			assertNoMutationTmux(t, testCase.runner.calls)
		})
	}
}

func TestAttachPreflightOutsideTmuxFailsClosed(t *testing.T) {
	runner := &fakeRunner{fail: func(call string, _ int) bool { return strings.HasPrefix(call, "display-message") }}
	if _, err := (Workspace{Runner: runner}).PreflightAttach(context.Background()); err == nil || !strings.Contains(err.Error(), "inspect current tmux location") {
		t.Fatalf("outside tmux did not fail closed: %v", err)
	}
	assertNoMutationTmux(t, runner.calls)
}

func TestAttachPreflightPreservesIrrelevantUnmanagedDuplicateNamesWithoutPaneZero(t *testing.T) {
	current := managedWindow("@1", "quote", 0, 1)
	runner := attachRunner(current,
		&fakeWindow{id: "@2", name: "scratch", panes: []int{1}},
		&fakeWindow{id: "@3", name: "scratch", panes: []int{1, 2}},
	)
	location, err := (Workspace{Runner: runner}).PreflightAttach(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if location.WindowName != "quote" || location.BoundAgentID != "quote" {
		t.Fatalf("unexpected Attach location: %+v", location)
	}
	assertNoMutationTmux(t, runner.calls)
}

func TestAttachPreflightRejectsAmbiguousCurrentUnmanagedName(t *testing.T) {
	current := &fakeWindow{id: "@1", name: "scratch", panes: []int{0}}
	runner := attachRunner(current, &fakeWindow{id: "@2", name: "scratch", panes: []int{1}})
	if _, err := (Workspace{Runner: runner}).PreflightAttach(context.Background()); err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("ambiguous current window did not fail closed: %v", err)
	}
	assertNoMutationTmux(t, runner.calls)
}

func TestBindCurrentBindsUnmanagedWindowAndPreservesAdditionalPanes(t *testing.T) {
	current := &fakeWindow{id: "@7", name: "shell", panes: []int{0, 1, 2}}
	runner := attachRunner(current, managedWindow("@2", OverviewWindow, 0))
	workspace := Workspace{Runner: runner}
	location, err := workspace.PreflightAttach(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result, err := workspace.BindCurrent(context.Background(), location, "quote", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != BindingCreated || !result.Mutated || current.name != "quote" || current.managed.Value != "1" || current.agent.Value != "quote" {
		t.Fatalf("unexpected binding result=%+v window=%+v", result, current)
	}
	if !reflect.DeepEqual(current.panes, []int{0, 1, 2}) {
		t.Fatalf("binding changed additional panes: %v", current.panes)
	}
	assertNoForbiddenTmux(t, runner.calls)
}

func TestBindCurrentReusesSameAgentWithoutMutation(t *testing.T) {
	current := managedWindow("@7", "quote", 0, 2)
	runner := attachRunner(current)
	workspace := Workspace{Runner: runner}
	location, err := workspace.PreflightAttach(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	before := len(runner.calls)
	result, err := workspace.BindCurrent(context.Background(), location, "quote", false)
	if err != nil || result.Status != BindingReused || result.Mutated {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	assertNoMutationTmux(t, runner.calls[before:])
}

func TestBindCurrentOtherAgentRequiresConfirmationWithoutMutation(t *testing.T) {
	current := managedWindow("@7", "risk", 0)
	runner := attachRunner(current)
	workspace := Workspace{Runner: runner}
	location, err := workspace.PreflightAttach(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	before := len(runner.calls)
	result, err := workspace.BindCurrent(context.Background(), location, "quote", false)
	var confirmation *ConfirmationRequiredError
	if !errors.As(err, &confirmation) || result.Status != BindingConfirmationRequired {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	assertNoMutationTmux(t, runner.calls[before:])

	result, err = workspace.BindCurrent(context.Background(), location, "quote", true)
	if err != nil || result.Status != BindingCreated || current.name != "quote" || current.agent.Value != "quote" {
		t.Fatalf("confirmed result=%+v window=%+v err=%v", result, current, err)
	}
}

func TestBindCurrentRejectsTargetConflictsBeforeMutation(t *testing.T) {
	tests := map[string][]*fakeWindow{
		"compatible occupied": {managedWindow("@2", "quote", 0)},
		"unmanaged occupied":  {{id: "@2", name: "quote", panes: []int{0}}},
	}
	for name, extras := range tests {
		t.Run(name, func(t *testing.T) {
			current := &fakeWindow{id: "@1", name: "shell", panes: []int{0}}
			runner := attachRunner(current, extras...)
			workspace := Workspace{Runner: runner}
			location, err := workspace.PreflightAttach(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			before := len(runner.calls)
			if _, err := workspace.BindCurrent(context.Background(), location, "quote", false); err == nil {
				t.Fatal("expected target conflict")
			}
			assertNoMutationTmux(t, runner.calls[before:])
		})
	}
}

func TestBindCurrentDetectsCurrentWindowTOCTOU(t *testing.T) {
	current := &fakeWindow{id: "@1", name: "shell", panes: []int{0}}
	other := &fakeWindow{id: "@2", name: "notes", panes: []int{0}}
	runner := attachRunner(current, other)
	workspace := Workspace{Runner: runner}
	location, err := workspace.PreflightAttach(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	runner.currentID = other.id
	before := len(runner.calls)
	if _, err := workspace.BindCurrent(context.Background(), location, "quote", false); err == nil || !strings.Contains(err.Error(), "changed after authorization") {
		t.Fatalf("expected TOCTOU rejection, got %v", err)
	}
	assertNoMutationTmux(t, runner.calls[before:])
}

func TestBindCurrentCompensatesEveryMutationFailure(t *testing.T) {
	tests := map[string]string{
		"managed marker": "set-option -w -t @1 " + ManagedOption,
		"agent marker":   "set-option -w -t @1 " + AgentIDOption,
		"rename":         "rename-window -t @1 quote",
		"verify":         "display-message -p -F",
	}
	for name, failingCall := range tests {
		t.Run(name, func(t *testing.T) {
			current := &fakeWindow{id: "@1", name: "shell", panes: []int{0, 1}}
			runner := attachRunner(current)
			workspace := Workspace{Runner: runner}
			location, err := workspace.PreflightAttach(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			failed := false
			displayCalls := 0
			runner.fail = func(call string, _ int) bool {
				if strings.HasPrefix(call, "display-message -p -F") {
					displayCalls++
					if name == "verify" && displayCalls < 2 {
						return false
					}
				}
				if !failed && strings.HasPrefix(call, failingCall) {
					failed = true
					return true
				}
				return false
			}
			if _, err := workspace.BindCurrent(context.Background(), location, "quote", false); err == nil || strings.Contains(err.Error(), "partial-failure") {
				t.Fatalf("expected compensated failure, got %v", err)
			}
			if current.name != "shell" || current.managed.Set || current.agent.Set || !reflect.DeepEqual(current.panes, []int{0, 1}) {
				t.Fatalf("original state was not restored: %+v", current)
			}
		})
	}
}

func TestBindCurrentReportsPartialFailureWhenCompensationFails(t *testing.T) {
	current := &fakeWindow{id: "@1", name: "shell", panes: []int{0}}
	runner := attachRunner(current)
	workspace := Workspace{Runner: runner}
	location, err := workspace.PreflightAttach(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	runner.fail = func(call string, _ int) bool {
		return strings.HasPrefix(call, "rename-window -t @1 quote") || strings.HasPrefix(call, "rename-window -t @1 shell")
	}
	_, err = workspace.BindCurrent(context.Background(), location, "quote", false)
	var partial *PartialFailureError
	if !errors.As(err, &partial) || !strings.Contains(err.Error(), "partial-failure") {
		t.Fatalf("expected visible partial failure, got %v", err)
	}
}

func TestBindCurrentRestoresExistingNameAndOptionPresence(t *testing.T) {
	current := managedWindow("@1", "risk", 0, 1)
	runner := attachRunner(current)
	workspace := Workspace{Runner: runner}
	location, err := workspace.PreflightAttach(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	failed := false
	runner.fail = func(call string, _ int) bool {
		if !failed && strings.HasPrefix(call, "rename-window -t @1 quote") {
			failed = true
			return true
		}
		return false
	}
	if _, err := workspace.BindCurrent(context.Background(), location, "quote", true); err == nil {
		t.Fatal("expected injected rename failure")
	}
	if current.name != "risk" || current.managed != (OptionValue{Set: true, Value: "1"}) || current.agent != (OptionValue{Set: true, Value: "risk"}) {
		t.Fatalf("existing binding was not restored exactly: %+v", current)
	}
}
