package fleet

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"openagentx/internal/domain"
)

type AttachLocation struct {
	WindowName   string
	BoundAgentID string
	windowID     string
}

type BindingStatus string

const (
	BindingReused               BindingStatus = "bound-consistent"
	BindingCreated              BindingStatus = "bound"
	BindingConfirmationRequired BindingStatus = "confirmation-required"
)

type BindingResult struct {
	Status       BindingStatus
	AgentID      string
	OriginalName string
	CurrentName  string
	Mutated      bool
}

type ConfirmationRequiredError struct {
	CurrentAgentID string
	TargetAgentID  string
}

func (e *ConfirmationRequiredError) Error() string {
	return fmt.Sprintf("current window is bound to Agent %q; rebinding to %q requires explicit confirmation in the Task 06 Console UI", e.CurrentAgentID, e.TargetAgentID)
}

type PartialFailureError struct {
	Cause        error
	Compensation error
}

func (e *PartialFailureError) Error() string {
	return fmt.Sprintf("tmux binding partial-failure: %v; automatic restoration also failed: %v; inspect the current OAX window name and %s/%s options before retrying", e.Cause, e.Compensation, ManagedOption, AgentIDOption)
}

func (e *PartialFailureError) Unwrap() error { return e.Cause }

func (w Workspace) PreflightAttach(ctx context.Context) (AttachLocation, error) {
	location, _, err := w.preflightAttach(ctx)
	return location, err
}

func (w Workspace) preflightAttach(ctx context.Context) (AttachLocation, []Window, error) {
	current, windows, err := w.InspectCurrent(ctx)
	if err != nil {
		return AttachLocation{}, nil, err
	}
	if err := validateAttachTopology(windows, current.WindowID); err != nil {
		return AttachLocation{}, nil, err
	}
	window, ok := windowByID(windows, current.WindowID)
	if !ok {
		return AttachLocation{}, nil, fmt.Errorf("current tmux window changed during preflight; retry from %s pane 0", SessionName)
	}
	if window.Name == OverviewWindow {
		return AttachLocation{}, nil, fmt.Errorf("current tmux location is %s:%s.0; switch to an Agent window before Attach", SessionName, OverviewWindow)
	}
	location := AttachLocation{windowID: window.ID, WindowName: window.Name}
	if window.Managed.Set {
		location.BoundAgentID = window.AgentID.Value
	}
	return location, windows, nil
}

// BindCurrent performs a second complete preflight before changing the exact
// current window. confirm is intentionally not exposed by the Task 05 CLI;
// Task 06 may present the frozen confirmation UI around this service method.
func (w Workspace) BindCurrent(ctx context.Context, expected AttachLocation, agentID string, confirm bool) (BindingResult, error) {
	if err := validateAgentWindowName(agentID); err != nil {
		return BindingResult{}, err
	}
	location, windows, err := w.preflightAttach(ctx)
	if err != nil {
		return BindingResult{}, err
	}
	if location.windowID != expected.windowID || expected.windowID == "" {
		return BindingResult{}, fmt.Errorf("current tmux window changed after authorization; no binding was performed")
	}
	if err := validateBindingTarget(windows, location.windowID, agentID); err != nil {
		return BindingResult{}, err
	}
	current, ok := windowByID(windows, location.windowID)
	if !ok {
		return BindingResult{}, fmt.Errorf("current tmux window disappeared before binding")
	}
	if current.Managed.Set && current.AgentID.Value != agentID && !confirm {
		return BindingResult{Status: BindingConfirmationRequired, AgentID: agentID, OriginalName: current.Name, CurrentName: current.Name},
			&ConfirmationRequiredError{CurrentAgentID: current.AgentID.Value, TargetAgentID: agentID}
	}

	mutated, err := w.stabilizeBinding(ctx, current, agentID, func() error { return w.verifyBinding(ctx, current.ID, agentID) })
	if err != nil {
		return BindingResult{}, err
	}
	status := BindingCreated
	if current.Managed.Set && current.AgentID.Value == agentID && current.Name == agentID {
		status = BindingReused
	}
	if err := w.setPaneLabel(ctx, current.ID, agentID); err != nil {
		return BindingResult{}, err
	}
	return BindingResult{Status: status, AgentID: agentID, OriginalName: current.Name, CurrentName: agentID, Mutated: mutated}, nil
}

// stabilizeBinding is shared by in-place Attach and reconciliation. It never
// respawns a process or changes pane indices; rollback preserves option presence.
func (w Workspace) stabilizeBinding(ctx context.Context, original Window, agentID string, verify func() error) (bool, error) {
	mutated, mutationErr := w.applyBinding(ctx, original, agentID)
	if mutationErr == nil {
		mutationErr = verify()
	}
	if mutationErr != nil {
		if !mutated {
			return false, mutationErr
		}
		restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if compensationErr := w.restoreBinding(restoreCtx, original); compensationErr != nil {
			return false, &PartialFailureError{Cause: mutationErr, Compensation: compensationErr}
		}
		return false, fmt.Errorf("tmux binding failed and original state was restored: %w", mutationErr)
	}
	return mutated, nil
}

func validateBindingTarget(windows []Window, currentWindowID, agentID string) error {
	for _, window := range windows {
		if window.ID == currentWindowID {
			continue
		}
		if window.Name == agentID {
			if window.Managed.Set && window.AgentID.Set && window.AgentID.Value == agentID {
				return fmt.Errorf("Agent %q is already bound to another compatible OAX window; switch to %s:%s.0", agentID, SessionName, agentID)
			}
			return fmt.Errorf("unmanaged or incompatible tmux window %q occupies the target name; no binding was performed", agentID)
		}
		if window.AgentID.Set && window.AgentID.Value == agentID {
			return fmt.Errorf("Agent marker %q is already used by another tmux window", agentID)
		}
	}
	return nil
}

var terminalWindowOptions = [][2]string{{"automatic-rename", "off"}, {"allow-rename", "off"}, {"remain-on-exit", "on"}}

func parseTerminalOptions(output string) map[string]OptionValue {
	result := make(map[string]OptionValue)
	for _, line := range strings.Split(output, "\n") {
		name, value, found := strings.Cut(line, " ")
		if !found {
			continue
		}
		for _, option := range terminalWindowOptions {
			if name == option[0] {
				result[name] = OptionValue{Set: true, Value: value}
			}
		}
	}
	return result
}

func (w Workspace) applyBinding(ctx context.Context, original Window, agentID string) (bool, error) {
	mutated := false
	options := append([][2]string{}, terminalWindowOptions...)
	options = append(options, [2]string{ManagedOption, "1"}, [2]string{AgentIDOption, agentID})
	for _, option := range options {
		old := original.terminalOptions[option[0]]
		if option[0] == ManagedOption {
			old = original.Managed
		}
		if option[0] == AgentIDOption {
			old = original.AgentID
		}
		if old == (OptionValue{Set: true, Value: option[1]}) {
			continue
		}
		mutated = true // A failing transport can still have applied the command.
		if _, err := w.Runner.Run(ctx, "set-option", "-w", "-t", original.ID, option[0], option[1]); err != nil {
			return mutated, fmt.Errorf("set %s: %w", option[0], err)
		}
	}
	if original.Name != agentID {
		mutated = true
		if _, err := w.Runner.Run(ctx, "rename-window", "-t", original.ID, agentID); err != nil {
			return mutated, fmt.Errorf("rename current window: %w", err)
		}
	}
	return mutated, nil
}

func verifyWindowIdentity(window Window, agentID string) error {
	if window.Name != agentID || window.Managed != (OptionValue{Set: true, Value: "1"}) || window.AgentID != (OptionValue{Set: true, Value: agentID}) || !window.HasPane(0) {
		return fmt.Errorf("tmux binding verification did not observe the requested Agent mapping")
	}
	for _, option := range terminalWindowOptions {
		if window.terminalOptions[option[0]] != (OptionValue{Set: true, Value: option[1]}) {
			return fmt.Errorf("tmux binding verification did not observe %s=%s", option[0], option[1])
		}
	}
	return nil
}

func (w Workspace) verifyWindowBinding(ctx context.Context, windowID, agentID string) error {
	windows, err := w.Inspect(ctx)
	if err != nil {
		return err
	}
	if err := validateAttachTopology(windows, windowID); err != nil {
		return err
	}
	window, ok := windowByID(windows, windowID)
	if !ok {
		return fmt.Errorf("bound tmux window disappeared")
	}
	return verifyWindowIdentity(window, agentID)
}

func (w Workspace) verifyBinding(ctx context.Context, windowID, agentID string) error {
	current, windows, err := w.InspectCurrent(ctx)
	if err != nil {
		return err
	}
	if current.WindowID != windowID {
		return fmt.Errorf("current tmux window changed during binding verification")
	}
	if err := validateAttachTopology(windows, windowID); err != nil {
		return err
	}
	window, ok := windowByID(windows, windowID)
	if !ok {
		return fmt.Errorf("bound tmux window disappeared")
	}
	return verifyWindowIdentity(window, agentID)
}

func (w Workspace) restoreBinding(ctx context.Context, original Window) error {
	var failures []error
	if _, err := w.Runner.Run(ctx, "rename-window", "-t", original.ID, original.Name); err != nil {
		failures = append(failures, fmt.Errorf("restore window name: %w", err))
	}
	if err := w.restoreOption(ctx, original.ID, ManagedOption, original.Managed); err != nil {
		failures = append(failures, err)
	}
	if err := w.restoreOption(ctx, original.ID, AgentIDOption, original.AgentID); err != nil {
		failures = append(failures, err)
	}
	for _, option := range terminalWindowOptions {
		if err := w.restoreOption(ctx, original.ID, option[0], original.terminalOptions[option[0]]); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (w Workspace) restoreOption(ctx context.Context, windowID, name string, original OptionValue) error {
	var err error
	if original.Set {
		_, err = w.Runner.Run(ctx, "set-option", "-w", "-t", windowID, name, original.Value)
	} else {
		_, err = w.Runner.Run(ctx, "set-option", "-w", "-u", "-t", windowID, name)
	}
	if err != nil {
		return fmt.Errorf("restore %s: %w", name, err)
	}
	return nil
}

func validateAgentWindowName(agentID string) error {
	if agentID != strings.TrimSpace(agentID) {
		return fmt.Errorf("agent_id must not contain surrounding whitespace")
	}
	if err := domain.ValidateIdentifier("agent_id", agentID); err != nil {
		return err
	}
	if agentID == OverviewWindow {
		return fmt.Errorf("Agent ID %q conflicts with reserved overview window", agentID)
	}
	return nil
}

func windowByID(windows []Window, windowID string) (Window, bool) {
	for _, window := range windows {
		if window.ID == windowID {
			return window, true
		}
	}
	return Window{}, false
}
