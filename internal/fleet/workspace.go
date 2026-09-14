package fleet

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

var ErrSessionMissing = errors.New("tmux session does not exist")

const (
	ManagedOption = "@openagentx_managed"
	AgentIDOption = "@openagentx_agent_id"

	windowListFormat = "#{window_id}\t#{window_name}"
	paneListFormat   = "#{pane_index}"
	currentFormat    = "#{session_name}\t#{window_id}\t#{window_name}\t#{pane_index}"
)

type CommandRunner interface {
	Run(context.Context, ...string) (string, error)
}

// ExecRunner optionally addresses an isolated tmux server. Product callers
// leave SocketName empty; tests use a unique -L server and never touch default tmux.
type ExecRunner struct {
	SocketName    string
	CurrentTarget string
	Env           []string
}

func (r ExecRunner) Run(ctx context.Context, args ...string) (string, error) {
	commandArgs := append([]string(nil), args...)
	if r.SocketName != "" {
		commandArgs = append([]string{"-L", r.SocketName}, commandArgs...)
	}
	if r.CurrentTarget != "" && len(args) > 0 && args[0] == "display-message" {
		commandArgs = append(commandArgs, "-t", r.CurrentTarget)
	}
	command := exec.CommandContext(ctx, "tmux", commandArgs...)
	if len(r.Env) > 0 {
		command.Env = append(os.Environ(), r.Env...)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("tmux %s: %w: %s", strings.Join(commandArgs, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

type OptionValue struct {
	Set   bool
	Value string
}

type Window struct {
	// ID is an internal tmux handle used only to keep mutations on the window
	// that passed preflight. It is never an Agent identity or API value.
	ID          string
	Name        string
	PaneIndices []int
	Managed     OptionValue
	AgentID     OptionValue
}

func (w Window) HasPane(index int) bool {
	for _, candidate := range w.PaneIndices {
		if candidate == index {
			return true
		}
	}
	return false
}

type CurrentPane struct {
	SessionName string
	WindowID    string
	WindowName  string
	PaneIndex   int
}

type WorkspaceReport struct {
	SessionCreated bool
	Created        []string
	Reused         []string
	Unmanaged      []string
	Orphaned       []string
}

type Workspace struct {
	Runner         CommandRunner
	ConsoleCommand func(agentID string) []string
}

func (w Workspace) Inspect(ctx context.Context) ([]Window, error) {
	if w.Runner == nil {
		return nil, fmt.Errorf("tmux command runner is required")
	}
	if _, err := w.Runner.Run(ctx, "has-session", "-t", "="+SessionName); err != nil {
		return nil, ErrSessionMissing
	}
	output, err := w.Runner.Run(ctx, "list-windows", "-t", "="+SessionName, "-F", windowListFormat)
	if err != nil {
		return nil, fmt.Errorf("list OAX windows: %w", err)
	}
	records, err := parseRecords(output, 2, "window")
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("tmux OAX session returned no windows")
	}
	windows := make([]Window, 0, len(records))
	seenIDs := make(map[string]struct{}, len(records))
	for _, parts := range records {
		window := Window{ID: parts[0], Name: parts[1]}
		if !validWindowID(window.ID) {
			return nil, fmt.Errorf("tmux returned invalid internal window handle")
		}
		if _, duplicate := seenIDs[window.ID]; duplicate {
			return nil, fmt.Errorf("tmux returned duplicate internal window handle")
		}
		seenIDs[window.ID] = struct{}{}
		if strings.TrimSpace(window.Name) == "" || strings.ContainsAny(window.Name, "\r\n\t") {
			return nil, fmt.Errorf("tmux returned invalid window name")
		}
		paneOutput, paneErr := w.Runner.Run(ctx, "list-panes", "-t", window.ID, "-F", paneListFormat)
		if paneErr != nil {
			return nil, fmt.Errorf("list panes for tmux window %q: %w", window.Name, paneErr)
		}
		window.PaneIndices, err = parsePaneIndices(window.Name, paneOutput)
		if err != nil {
			return nil, err
		}
		optionOutput, optionErr := w.Runner.Run(ctx, "show-options", "-w", "-t", window.ID)
		if optionErr != nil {
			return nil, fmt.Errorf("read options for tmux window %q: %w", window.Name, optionErr)
		}
		window.Managed, window.AgentID, err = parseWindowOptions(optionOutput)
		if err != nil {
			return nil, fmt.Errorf("read options for tmux window %q: %w", window.Name, err)
		}
		windows = append(windows, window)
	}
	return windows, nil
}

func (w Workspace) InspectCurrent(ctx context.Context) (CurrentPane, []Window, error) {
	if w.Runner == nil {
		return CurrentPane{}, nil, fmt.Errorf("tmux command runner is required")
	}
	output, err := w.Runner.Run(ctx, "display-message", "-p", "-F", currentFormat)
	if err != nil {
		return CurrentPane{}, nil, fmt.Errorf("inspect current tmux location: %w", err)
	}
	records, err := parseRecords(output, 4, "current pane")
	if err != nil || len(records) != 1 {
		return CurrentPane{}, nil, fmt.Errorf("tmux returned an invalid current pane record")
	}
	parts := records[0]
	paneIndex, err := strconv.Atoi(parts[3])
	if err != nil || paneIndex < 0 {
		return CurrentPane{}, nil, fmt.Errorf("tmux returned an invalid current pane index")
	}
	current := CurrentPane{SessionName: parts[0], WindowID: parts[1], WindowName: parts[2], PaneIndex: paneIndex}
	if current.SessionName != SessionName {
		return CurrentPane{}, nil, fmt.Errorf("current tmux location is %s:%s.%d; switch to %s:<agent-id>.0", current.SessionName, current.WindowName, current.PaneIndex, SessionName)
	}
	if !validWindowID(current.WindowID) || strings.TrimSpace(current.WindowName) == "" {
		return CurrentPane{}, nil, fmt.Errorf("tmux returned an invalid current window")
	}
	windows, err := w.Inspect(ctx)
	if err != nil {
		return CurrentPane{}, nil, err
	}
	var matched *Window
	for index := range windows {
		if windows[index].ID == current.WindowID {
			if matched != nil {
				return CurrentPane{}, nil, fmt.Errorf("current tmux window is ambiguous")
			}
			matched = &windows[index]
		}
	}
	if matched == nil || matched.Name != current.WindowName {
		return CurrentPane{}, nil, fmt.Errorf("current tmux window changed during inspection; retry from %s pane 0", SessionName)
	}
	if current.PaneIndex != 0 {
		return CurrentPane{}, nil, fmt.Errorf("current tmux location is %s:%s.%d; switch to %s:%s.0", SessionName, current.WindowName, current.PaneIndex, SessionName, current.WindowName)
	}
	if !matched.HasPane(0) {
		return CurrentPane{}, nil, fmt.Errorf("tmux window %q has no pane 0; repair it explicitly before Attach", current.WindowName)
	}
	return current, windows, nil
}

func (w Workspace) Preflight(ctx context.Context, manifest Manifest) error {
	if err := manifest.Validate(); err != nil {
		return err
	}
	if w.Runner == nil {
		return fmt.Errorf("tmux command runner is required")
	}
	if w.ConsoleCommand == nil {
		return fmt.Errorf("Console command builder is required")
	}
	for _, agent := range manifest.Agents {
		if len(w.ConsoleCommand(agent.AgentID)) == 0 {
			return fmt.Errorf("Console command for Agent %q is empty", agent.AgentID)
		}
	}
	windows, err := w.Inspect(ctx)
	if errors.Is(err, ErrSessionMissing) {
		return nil
	}
	if err != nil {
		return err
	}
	return validateWindows(manifest, windows)
}

func (w Workspace) Reconcile(ctx context.Context, manifest Manifest) (WorkspaceReport, error) {
	if err := w.Preflight(ctx, manifest); err != nil {
		return WorkspaceReport{}, err
	}
	windows, err := w.Inspect(ctx)
	if errors.Is(err, ErrSessionMissing) {
		return w.createWorkspace(ctx, manifest)
	}
	if err != nil {
		return WorkspaceReport{}, err
	}
	if err := validateWindows(manifest, windows); err != nil {
		return WorkspaceReport{}, err
	}

	expected := expectedWindowNames(manifest)
	byName := windowsByName(windows)
	report := WorkspaceReport{}
	for _, name := range manifest.WindowNames() {
		if len(byName[name]) == 1 {
			report.Reused = append(report.Reused, name)
			continue
		}
		windowID, createErr := w.createWindow(ctx, name)
		if createErr != nil {
			return report, createErr
		}
		report.Created = append(report.Created, name)
		if markErr := w.markCreatedWindow(ctx, windowID, name); markErr != nil {
			return report, markErr
		}
		verified, inspectErr := w.Inspect(ctx)
		if inspectErr != nil {
			return report, fmt.Errorf("verify created tmux window %q: %w", name, inspectErr)
		}
		if verifyErr := validateCreatedWindow(verified, windowID, name); verifyErr != nil {
			return report, verifyErr
		}
	}
	for _, window := range windows {
		if _, managed := expected[window.Name]; managed {
			continue
		}
		if window.Managed.Set {
			report.Orphaned = append(report.Orphaned, window.Name)
		} else {
			report.Unmanaged = append(report.Unmanaged, window.Name)
		}
	}
	sort.Strings(report.Unmanaged)
	sort.Strings(report.Orphaned)
	return report, nil
}

func (w Workspace) createWorkspace(ctx context.Context, manifest Manifest) (WorkspaceReport, error) {
	output, err := w.Runner.Run(ctx, "new-session", "-d", "-P", "-F", "#{window_id}", "-s", SessionName, "-n", OverviewWindow)
	if err != nil {
		return WorkspaceReport{}, err
	}
	overviewID, err := parseCreatedWindowID(output)
	report := WorkspaceReport{SessionCreated: true, Created: []string{OverviewWindow}}
	if err != nil {
		return report, err
	}
	if err := w.markCreatedWindow(ctx, overviewID, OverviewWindow); err != nil {
		return report, err
	}
	verified, err := w.Inspect(ctx)
	if err != nil {
		return report, fmt.Errorf("verify created tmux overview window: %w", err)
	}
	if err := validateCreatedWindow(verified, overviewID, OverviewWindow); err != nil {
		return report, err
	}
	for _, agent := range manifest.Agents {
		windowID, createErr := w.createWindow(ctx, agent.AgentID)
		if createErr != nil {
			return report, createErr
		}
		report.Created = append(report.Created, agent.AgentID)
		if markErr := w.markCreatedWindow(ctx, windowID, agent.AgentID); markErr != nil {
			return report, markErr
		}
		verified, inspectErr := w.Inspect(ctx)
		if inspectErr != nil {
			return report, fmt.Errorf("verify created tmux window %q: %w", agent.AgentID, inspectErr)
		}
		if verifyErr := validateCreatedWindow(verified, windowID, agent.AgentID); verifyErr != nil {
			return report, verifyErr
		}
	}
	return report, nil
}

func (w Workspace) createWindow(ctx context.Context, name string) (string, error) {
	args := []string{"new-window", "-d", "-P", "-F", "#{window_id}", "-t", "=" + SessionName, "-n", name}
	if name != OverviewWindow {
		args = append(args, w.ConsoleCommand(name)...)
	}
	output, err := w.Runner.Run(ctx, args...)
	if err != nil {
		return "", err
	}
	return parseCreatedWindowID(output)
}

func (w Workspace) markCreatedWindow(ctx context.Context, windowID, name string) error {
	commands := [][]string{
		{"set-option", "-w", "-t", windowID, "pane-base-index", "0"},
		{"set-option", "-w", "-t", windowID, "remain-on-exit", "on"},
		{"set-option", "-w", "-t", windowID, ManagedOption, "1"},
	}
	if name == OverviewWindow {
		commands = append(commands, []string{"set-option", "-w", "-u", "-t", windowID, AgentIDOption})
	} else {
		commands = append(commands, []string{"set-option", "-w", "-t", windowID, AgentIDOption, name})
	}
	for _, command := range commands {
		if _, err := w.Runner.Run(ctx, command...); err != nil {
			return fmt.Errorf("configure created tmux window %q: %w", name, err)
		}
	}
	return nil
}

func validateWindows(manifest Manifest, windows []Window) error {
	if err := validateTopology(windows); err != nil {
		return err
	}
	byName := windowsByName(windows)
	for _, name := range manifest.WindowNames() {
		matches := byName[name]
		if len(matches) == 0 {
			continue
		}
		window := matches[0]
		if !window.Managed.Set {
			return fmt.Errorf("unmanaged tmux window %q occupies a Fleet target name", name)
		}
		if name == OverviewWindow {
			if window.AgentID.Set {
				return fmt.Errorf("overview tmux window must not have an Agent marker")
			}
			continue
		}
		if !window.AgentID.Set || window.AgentID.Value != name {
			return fmt.Errorf("tmux window %q marker/name mismatch", name)
		}
	}
	return nil
}

func validateTopology(windows []Window) error {
	names := make(map[string]struct{}, len(windows))
	agents := make(map[string]string, len(windows))
	for _, window := range windows {
		if _, duplicate := names[window.Name]; duplicate {
			return fmt.Errorf("tmux window name %q is duplicated; refusing to guess by index", window.Name)
		}
		names[window.Name] = struct{}{}
		if !window.HasPane(0) {
			return fmt.Errorf("tmux window %q has no stable pane 0; refusing to alter existing workspace", window.Name)
		}
		if window.Managed.Set && window.Managed.Value != "1" {
			return fmt.Errorf("tmux window %q has invalid managed marker", window.Name)
		}
		if window.AgentID.Set {
			if err := validateAgentWindowName(window.AgentID.Value); err != nil {
				return fmt.Errorf("tmux window %q has invalid Agent marker: %w", window.Name, err)
			}
			if existing, duplicate := agents[window.AgentID.Value]; duplicate {
				return fmt.Errorf("Agent marker %q is duplicated by tmux windows %q and %q", window.AgentID.Value, existing, window.Name)
			}
			agents[window.AgentID.Value] = window.Name
		}
		if !window.Managed.Set {
			if window.AgentID.Set {
				return fmt.Errorf("tmux window %q has an Agent marker without managed marker", window.Name)
			}
			continue
		}
		if window.Name == OverviewWindow {
			if window.AgentID.Set {
				return fmt.Errorf("overview tmux window must not have an Agent marker")
			}
			continue
		}
		if !window.AgentID.Set {
			return fmt.Errorf("managed tmux window %q is missing its Agent marker", window.Name)
		}
		if window.AgentID.Value != window.Name {
			return fmt.Errorf("tmux window %q marker/name mismatch", window.Name)
		}
	}
	return nil
}

func validateCreatedWindow(windows []Window, windowID, name string) error {
	if err := validateTopology(windows); err != nil {
		return fmt.Errorf("verify created tmux window %q: %w", name, err)
	}
	for _, window := range windows {
		if window.ID != windowID {
			continue
		}
		if window.Name != name || !window.Managed.Set || window.Managed.Value != "1" || !window.HasPane(0) {
			return fmt.Errorf("created tmux window %q did not retain its required structure", name)
		}
		if name == OverviewWindow && window.AgentID.Set {
			return fmt.Errorf("created overview tmux window has an Agent marker")
		}
		if name != OverviewWindow && (!window.AgentID.Set || window.AgentID.Value != name) {
			return fmt.Errorf("created tmux window %q did not retain its Agent marker", name)
		}
		return nil
	}
	return fmt.Errorf("created tmux window %q disappeared before verification", name)
}

func parseRecords(output string, fieldCount int, label string) ([][]string, error) {
	trimmed := strings.TrimSuffix(output, "\n")
	if trimmed == "" {
		return nil, nil
	}
	if strings.Contains(trimmed, "\r") {
		return nil, fmt.Errorf("tmux returned an invalid %s record", label)
	}
	lines := strings.Split(trimmed, "\n")
	records := make([][]string, 0, len(lines))
	for _, line := range lines {
		parts := strings.Split(line, "\t")
		if len(parts) != fieldCount {
			return nil, fmt.Errorf("tmux returned an invalid %s record", label)
		}
		records = append(records, parts)
	}
	return records, nil
}

func parsePaneIndices(name, output string) ([]int, error) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return nil, fmt.Errorf("tmux window %q returned no panes", name)
	}
	seen := make(map[int]struct{})
	indices := make([]int, 0)
	for _, line := range strings.Split(trimmed, "\n") {
		index, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || index < 0 {
			return nil, fmt.Errorf("tmux returned invalid pane index for window %q", name)
		}
		if _, duplicate := seen[index]; duplicate {
			return nil, fmt.Errorf("tmux returned duplicate pane index for window %q", name)
		}
		seen[index] = struct{}{}
		indices = append(indices, index)
	}
	sort.Ints(indices)
	return indices, nil
}

func parseWindowOptions(output string) (OptionValue, OptionValue, error) {
	var managed, agent OptionValue
	for _, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		if line == "" {
			continue
		}
		name, value, found := strings.Cut(line, " ")
		if !found {
			continue
		}
		switch name {
		case ManagedOption:
			if managed.Set {
				return OptionValue{}, OptionValue{}, fmt.Errorf("duplicate managed option")
			}
			managed = OptionValue{Set: true, Value: value}
		case AgentIDOption:
			if agent.Set {
				return OptionValue{}, OptionValue{}, fmt.Errorf("duplicate Agent option")
			}
			agent = OptionValue{Set: true, Value: value}
		}
	}
	return managed, agent, nil
}

func parseCreatedWindowID(output string) (string, error) {
	id := strings.TrimSpace(output)
	if !validWindowID(id) || strings.ContainsAny(id, "\r\n\t ") {
		return "", fmt.Errorf("tmux did not return a valid created window handle")
	}
	return id, nil
}

func validWindowID(value string) bool {
	if len(value) < 2 || value[0] != '@' {
		return false
	}
	_, err := strconv.ParseUint(value[1:], 10, 64)
	return err == nil
}

func windowsByName(windows []Window) map[string][]Window {
	result := make(map[string][]Window, len(windows))
	for _, window := range windows {
		result[window.Name] = append(result[window.Name], window)
	}
	return result
}

func expectedWindowNames(manifest Manifest) map[string]struct{} {
	result := make(map[string]struct{}, len(manifest.Agents)+1)
	result[OverviewWindow] = struct{}{}
	for _, agent := range manifest.Agents {
		result[agent.AgentID] = struct{}{}
	}
	return result
}
