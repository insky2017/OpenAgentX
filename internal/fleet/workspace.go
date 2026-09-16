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

	windowIDFormat    = "#{window_id}"
	windowNameFormat  = "#{window_name}"
	paneIndexFormat   = "#{pane_index}"
	paneDeadFormat    = "#{pane_dead}"
	sessionNameFormat = "#{session_name}"
)

var provisioningCommand = []string{"sh", "-c", "while :; do sleep 86400; done"}

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
	if r.CurrentTarget != "" && len(args) > 0 && args[0] == "display-message" && !hasFlag(args, "-t") {
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

func hasFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag {
			return true
		}
	}
	return false
}

type OptionValue struct {
	Set   bool
	Value string
}

type Window struct {
	// ID is an internal tmux handle used only to keep mutations on the window
	// that passed preflight. It is never an Agent identity or API value.
	ID           string
	Name         string
	PaneIndices  []int
	PaneZeroDead bool
	PaneZeroSeen bool
	Managed      OptionValue
	AgentID      OptionValue
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
	Respawned      []string
	Dead           []string
	Unmanaged      []string
	Orphaned       []string
}

type Workspace struct {
	Runner         CommandRunner
	ConsoleCommand func(agentID string) []string
	RespawnDead    bool
}

func (w Workspace) Inspect(ctx context.Context) ([]Window, error) {
	if w.Runner == nil {
		return nil, fmt.Errorf("tmux command runner is required")
	}
	if _, err := w.Runner.Run(ctx, "has-session", "-t", "="+SessionName); err != nil {
		return nil, ErrSessionMissing
	}
	output, err := w.Runner.Run(ctx, "list-windows", "-t", "="+SessionName, "-F", windowIDFormat)
	if err != nil {
		return nil, fmt.Errorf("list OAX windows: %w", err)
	}
	windowIDs, err := parseWindowIDs(output)
	if err != nil {
		return nil, err
	}
	if len(windowIDs) == 0 {
		return nil, fmt.Errorf("tmux OAX session returned no windows")
	}
	windows := make([]Window, 0, len(windowIDs))
	for _, windowID := range windowIDs {
		nameOutput, nameErr := w.Runner.Run(ctx, "display-message", "-p", "-t", windowID, "-F", windowNameFormat)
		if nameErr != nil {
			return nil, fmt.Errorf("read name for tmux window: %w", nameErr)
		}
		windowName, nameErr := parseSingleField(nameOutput, "window name")
		if nameErr != nil {
			return nil, nameErr
		}
		window := Window{ID: windowID, Name: windowName}
		paneOutput, paneErr := w.Runner.Run(ctx, "list-panes", "-t", window.ID, "-F", paneIndexFormat)
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
		if window.HasPane(0) && window.Managed == (OptionValue{Set: true, Value: "1"}) &&
			window.Name != OverviewWindow && window.AgentID == (OptionValue{Set: true, Value: window.Name}) {
			deadOutput, deadErr := w.Runner.Run(ctx, "display-message", "-p", "-t", window.ID+".0", "-F", paneDeadFormat)
			if deadErr != nil {
				return nil, fmt.Errorf("read pane 0 state for managed tmux window %q: %w", window.Name, deadErr)
			}
			dead, deadErr := parsePaneDead(deadOutput)
			if deadErr != nil {
				return nil, fmt.Errorf("read pane 0 state for managed tmux window %q: %w", window.Name, deadErr)
			}
			window.PaneZeroDead, window.PaneZeroSeen = dead, true
		}
		windows = append(windows, window)
	}
	return windows, nil
}

func (w Workspace) InspectCurrent(ctx context.Context) (CurrentPane, []Window, error) {
	if w.Runner == nil {
		return CurrentPane{}, nil, fmt.Errorf("tmux command runner is required")
	}
	current, err := w.inspectCurrentHandle(ctx)
	if err != nil {
		return CurrentPane{}, nil, err
	}
	if current.SessionName != SessionName {
		return CurrentPane{}, nil, fmt.Errorf("current tmux location is %s:%s.%d; switch to %s:<agent-id>.0", current.SessionName, current.WindowName, current.PaneIndex, SessionName)
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
	verified, err := w.inspectCurrentHandle(ctx)
	if err != nil {
		return CurrentPane{}, nil, err
	}
	if verified.SessionName != current.SessionName || verified.WindowID != current.WindowID || verified.WindowName != current.WindowName || verified.PaneIndex != current.PaneIndex {
		return CurrentPane{}, nil, fmt.Errorf("current tmux window changed during inspection; retry from %s pane 0", SessionName)
	}
	if matched == nil {
		return CurrentPane{}, nil, fmt.Errorf("current tmux window changed during inspection; retry from %s pane 0", SessionName)
	}
	if matched.Name != current.WindowName {
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

func (w Workspace) inspectCurrentHandle(ctx context.Context) (CurrentPane, error) {
	windowBefore, err := w.readCurrentField(ctx, windowIDFormat, "current window handle")
	if err != nil {
		return CurrentPane{}, err
	}
	session, err := w.readCurrentField(ctx, sessionNameFormat, "current session name")
	if err != nil {
		return CurrentPane{}, err
	}
	windowName, err := w.readCurrentField(ctx, windowNameFormat, "current window name")
	if err != nil {
		return CurrentPane{}, err
	}
	paneText, err := w.readCurrentField(ctx, paneIndexFormat, "current pane index")
	if err != nil {
		return CurrentPane{}, err
	}
	windowAfter, err := w.readCurrentField(ctx, windowIDFormat, "current window handle")
	if err != nil {
		return CurrentPane{}, err
	}
	if windowBefore != windowAfter || !validWindowID(windowBefore) {
		return CurrentPane{}, fmt.Errorf("current tmux window changed during inspection")
	}
	paneIndex, err := strconv.Atoi(paneText)
	if err != nil || paneIndex < 0 {
		return CurrentPane{}, fmt.Errorf("tmux returned an invalid current pane index")
	}
	return CurrentPane{SessionName: session, WindowID: windowBefore, WindowName: windowName, PaneIndex: paneIndex}, nil
}

func (w Workspace) readCurrentField(ctx context.Context, format, label string) (string, error) {
	output, err := w.Runner.Run(ctx, "display-message", "-p", "-F", format)
	if err != nil {
		return "", fmt.Errorf("inspect current tmux location: %w", err)
	}
	value, err := parseSingleField(output, label)
	if err != nil {
		return "", err
	}
	return value, nil
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

	insertBeforeWindowID := ""
	if len(windows) > 0 {
		insertBeforeWindowID = windows[0].ID
	}
	expected := expectedWindowNames(manifest)
	byName := windowsByName(windows)
	report := WorkspaceReport{}
	for _, name := range manifest.WindowNames() {
		if len(byName[name]) == 1 {
			window := byName[name][0]
			if name != OverviewWindow && window.PaneZeroSeen && window.PaneZeroDead {
				if !w.RespawnDead {
					report.Dead = append(report.Dead, name)
					continue
				}
				if err := w.respawnDeadPane(ctx, manifest, window.ID, name); err != nil {
					return report, err
				}
				report.Respawned = append(report.Respawned, name)
				continue
			}
			report.Reused = append(report.Reused, name)
			continue
		}
		windowID, createErr := w.createWindow(ctx, name, insertBeforeWindowID)
		if createErr != nil {
			return report, createErr
		}
		if finishErr := w.finishCreatedWindow(ctx, windowID, name); finishErr != nil {
			return report, finishErr
		}
		report.Created = append(report.Created, name)
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

func (w Workspace) respawnDeadPane(ctx context.Context, manifest Manifest, windowID, agentID string) error {
	windows, err := w.Inspect(ctx)
	if err != nil {
		return fmt.Errorf("recheck dead Console pane for Agent %q: %w", agentID, err)
	}
	if err := validateWindows(manifest, windows); err != nil {
		return err
	}
	window, ok := windowByID(windows, windowID)
	if !ok || window.Name != agentID || !window.PaneZeroSeen || !window.PaneZeroDead ||
		window.Managed != (OptionValue{Set: true, Value: "1"}) || window.AgentID != (OptionValue{Set: true, Value: agentID}) {
		return fmt.Errorf("managed Console pane for Agent %q changed before respawn; no process was replaced", agentID)
	}
	command := w.ConsoleCommand(agentID)
	args := []string{"respawn-pane", "-t", window.ID + ".0", "--"}
	args = append(args, command...)
	if _, err := w.Runner.Run(ctx, args...); err != nil {
		return fmt.Errorf("respawn dead managed Console pane for Agent %q: %w; no live pane was killed", agentID, err)
	}
	verified, err := w.Inspect(ctx)
	if err != nil {
		return fmt.Errorf("verify respawned Console pane for Agent %q: %w", agentID, err)
	}
	current, ok := windowByID(verified, windowID)
	if !ok || current.Name != agentID || !current.PaneZeroSeen || current.PaneZeroDead ||
		current.Managed != (OptionValue{Set: true, Value: "1"}) || current.AgentID != (OptionValue{Set: true, Value: agentID}) {
		return fmt.Errorf("respawned Console pane for Agent %q did not retain its compatible live state", agentID)
	}
	return nil
}

func (w Workspace) createWorkspace(ctx context.Context, manifest Manifest) (WorkspaceReport, error) {
	args := []string{"new-session", "-d", "-P", "-F", windowIDFormat, "-s", SessionName, "-n", OverviewWindow}
	output, err := w.Runner.Run(ctx, args...)
	if err != nil {
		return WorkspaceReport{}, err
	}
	overviewID, err := parseCreatedWindowID(output)
	report := WorkspaceReport{SessionCreated: true}
	if err != nil {
		return report, err
	}
	if err := w.finishCreatedWindow(ctx, overviewID, OverviewWindow); err != nil {
		return report, err
	}
	report.Created = append(report.Created, OverviewWindow)
	for _, agent := range manifest.Agents {
		windowID, createErr := w.createWindow(ctx, agent.AgentID, "")
		if createErr != nil {
			return report, createErr
		}
		if finishErr := w.finishCreatedWindow(ctx, windowID, agent.AgentID); finishErr != nil {
			return report, finishErr
		}
		report.Created = append(report.Created, agent.AgentID)
	}
	return report, nil
}

func (w Workspace) createWindow(ctx context.Context, name, insertBeforeWindowID string) (string, error) {
	args := []string{"new-window", "-d", "-P", "-F", windowIDFormat}
	if insertBeforeWindowID == "" {
		args = append(args, "-t", "="+SessionName)
	} else {
		args = append(args, "-b", "-t", insertBeforeWindowID)
	}
	args = append(args, "-n", name)
	if name != OverviewWindow {
		args = append(args, provisioningCommand...)
	}
	output, err := w.Runner.Run(ctx, args...)
	if err != nil {
		return "", err
	}
	return parseCreatedWindowID(output)
}

func (w Workspace) finishCreatedWindow(ctx context.Context, windowID, name string) error {
	if err := w.markCreatedWindow(ctx, windowID, name); err != nil {
		return fmt.Errorf("%w; the newly created provisioning window was retained for diagnosis", err)
	}
	verified, err := w.Inspect(ctx)
	if err != nil {
		return fmt.Errorf("verify created tmux window %q: %w; the provisioning window was retained for diagnosis", name, err)
	}
	if err := validateCreatedWindow(verified, windowID, name); err != nil {
		return fmt.Errorf("%w; the provisioning window was retained for diagnosis", err)
	}
	if name == OverviewWindow {
		return nil
	}
	command := w.ConsoleCommand(name)
	if len(command) == 0 {
		return fmt.Errorf("start Console in newly created tmux window %q: Console command is empty; the configured window was retained for diagnosis", name)
	}
	args := []string{"respawn-pane", "-k", "-t", windowID + ".0", "--"}
	args = append(args, command...)
	if _, err := w.Runner.Run(ctx, args...); err != nil {
		return fmt.Errorf("start Console in newly created tmux window %q: %w; the configured window was retained for diagnosis", name, err)
	}
	verified, err = w.Inspect(ctx)
	if err != nil {
		return fmt.Errorf("verify started Console tmux window %q: %w; the window was retained for diagnosis", name, err)
	}
	if err := validateCreatedWindow(verified, windowID, name); err != nil {
		return fmt.Errorf("verify started Console: %w; the window was retained for diagnosis", err)
	}
	return nil
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
	if err := validateManagedInventory(windows); err != nil {
		return err
	}
	byName := windowsByName(windows)
	for _, name := range manifest.WindowNames() {
		matches := byName[name]
		if len(matches) > 1 {
			return fmt.Errorf("tmux Fleet target window name %q is duplicated; refusing to guess by index", name)
		}
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

func validateManagedInventory(windows []Window) error {
	agents := make(map[string]string, len(windows))
	for _, window := range windows {
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
		if !window.HasPane(0) {
			return fmt.Errorf("managed tmux window %q has no stable pane 0; refusing to alter existing workspace", window.Name)
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

func validateAttachTopology(windows []Window, currentWindowID string) error {
	if err := validateManagedInventory(windows); err != nil {
		return err
	}
	current, ok := windowByID(windows, currentWindowID)
	if !ok {
		return fmt.Errorf("current tmux window changed during preflight")
	}
	if !current.HasPane(0) {
		return fmt.Errorf("tmux window %q has no pane 0; repair it explicitly before Attach", current.Name)
	}
	nameMatches := 0
	for _, window := range windows {
		if window.Name == current.Name {
			nameMatches++
		}
	}
	if nameMatches != 1 {
		return fmt.Errorf("current tmux window name %q is duplicated; refusing to guess by index", current.Name)
	}
	return nil
}

func validateCreatedWindow(windows []Window, windowID, name string) error {
	if err := validateManagedInventory(windows); err != nil {
		return fmt.Errorf("verify created tmux window %q: %w", name, err)
	}
	nameMatches := 0
	found := false
	for _, window := range windows {
		if window.Name == name {
			nameMatches++
		}
		if window.ID != windowID {
			continue
		}
		found = true
		if window.Name != name || !window.Managed.Set || window.Managed.Value != "1" || !window.HasPane(0) || len(window.PaneIndices) != 1 {
			return fmt.Errorf("created tmux window %q did not retain its required structure", name)
		}
		if name == OverviewWindow && window.AgentID.Set {
			return fmt.Errorf("created overview tmux window has an Agent marker")
		}
		if name != OverviewWindow && (!window.AgentID.Set || window.AgentID.Value != name) {
			return fmt.Errorf("created tmux window %q did not retain its Agent marker", name)
		}
		if nameMatches > 1 {
			return fmt.Errorf("created tmux window name %q is ambiguous", name)
		}
		continue
	}
	if !found || nameMatches != 1 {
		return fmt.Errorf("created tmux window %q disappeared or became ambiguous before verification", name)
	}
	return nil
}

func parseWindowIDs(output string) ([]string, error) {
	trimmed := strings.TrimSuffix(output, "\n")
	if trimmed == "" {
		return nil, nil
	}
	windowIDs := strings.Split(trimmed, "\n")
	seen := make(map[string]struct{}, len(windowIDs))
	for _, windowID := range windowIDs {
		if !validWindowID(windowID) || strings.ContainsAny(windowID, "\r\t ") {
			return nil, fmt.Errorf("tmux returned invalid internal window handle")
		}
		if _, duplicate := seen[windowID]; duplicate {
			return nil, fmt.Errorf("tmux returned duplicate internal window handle")
		}
		seen[windowID] = struct{}{}
	}
	return windowIDs, nil
}

func parseSingleField(output, label string) (string, error) {
	value := strings.TrimSuffix(output, "\n")
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return "", fmt.Errorf("tmux returned an invalid %s", label)
	}
	return value, nil
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

func parsePaneDead(output string) (bool, error) {
	value, err := parseSingleField(output, "pane dead state")
	if err != nil {
		return false, err
	}
	switch value {
	case "0":
		return false, nil
	case "1":
		return true, nil
	default:
		return false, fmt.Errorf("tmux returned an invalid pane dead state")
	}
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
