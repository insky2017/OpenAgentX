package fleet

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

var ErrSessionMissing = errors.New("tmux session does not exist")

type CommandRunner interface {
	Run(context.Context, ...string) (string, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "tmux", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

type Window struct {
	Name           string
	PaneCount      int
	PaneIndex      int
	CurrentCommand string
	Managed        bool
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
	output, err := w.Runner.Run(ctx, "list-windows", "-t", "="+SessionName, "-F", "#{window_name}\t#{window_panes}\t#{pane_index}\t#{pane_current_command}\t#{@openagentx_managed}")
	if err != nil {
		return nil, err
	}
	var windows []Window
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 5)
		if len(parts) != 5 || strings.TrimSpace(parts[0]) == "" {
			return nil, fmt.Errorf("tmux returned an invalid window record")
		}
		panes, parseErr := strconv.Atoi(parts[1])
		if parseErr != nil || panes < 1 {
			return nil, fmt.Errorf("tmux returned invalid pane count for window %q", parts[0])
		}
		paneIndex, parseErr := strconv.Atoi(parts[2])
		if parseErr != nil || paneIndex < 0 {
			return nil, fmt.Errorf("tmux returned invalid pane index for window %q", parts[0])
		}
		windows = append(windows, Window{Name: parts[0], PaneCount: panes, PaneIndex: paneIndex, CurrentCommand: parts[3], Managed: parts[4] == "1"})
	}
	return windows, nil
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
		if _, createErr := w.Runner.Run(ctx, "new-session", "-d", "-s", SessionName, "-n", OverviewWindow); createErr != nil {
			return WorkspaceReport{}, createErr
		}
		report := WorkspaceReport{SessionCreated: true, Created: []string{OverviewWindow}}
		if err := w.markCreatedWindow(ctx, OverviewWindow); err != nil {
			return report, err
		}
		for _, agent := range manifest.Agents {
			args := []string{"new-window", "-d", "-t", "=" + SessionName, "-n", agent.AgentID}
			args = append(args, w.ConsoleCommand(agent.AgentID)...)
			if _, createErr := w.Runner.Run(ctx, args...); createErr != nil {
				return report, createErr
			}
			report.Created = append(report.Created, agent.AgentID)
			if err := w.markCreatedWindow(ctx, agent.AgentID); err != nil {
				return report, err
			}
		}
		return report, nil
	}
	if err != nil {
		return WorkspaceReport{}, err
	}

	expected := make(map[string]struct{}, len(manifest.Agents)+1)
	expected[OverviewWindow] = struct{}{}
	for _, agent := range manifest.Agents {
		expected[agent.AgentID] = struct{}{}
	}
	byName := make(map[string][]Window, len(windows))
	for _, window := range windows {
		byName[window.Name] = append(byName[window.Name], window)
	}
	report := WorkspaceReport{}
	for _, name := range manifest.WindowNames() {
		if len(byName[name]) == 1 {
			report.Reused = append(report.Reused, name)
			continue
		}
		args := []string{"new-window", "-d", "-t", "=" + SessionName, "-n", name}
		if name != OverviewWindow {
			args = append(args, w.ConsoleCommand(name)...)
		}
		if _, createErr := w.Runner.Run(ctx, args...); createErr != nil {
			return report, createErr
		}
		report.Created = append(report.Created, name)
		if err := w.markCreatedWindow(ctx, name); err != nil {
			return report, err
		}
	}
	for _, window := range windows {
		if _, managed := expected[window.Name]; managed {
			continue
		}
		if window.Managed {
			report.Orphaned = append(report.Orphaned, window.Name)
		} else {
			report.Unmanaged = append(report.Unmanaged, window.Name)
		}
	}
	return report, nil
}

func validateWindows(manifest Manifest, windows []Window) error {
	expected := make(map[string]struct{}, len(manifest.Agents)+1)
	expected[OverviewWindow] = struct{}{}
	for _, agent := range manifest.Agents {
		expected[agent.AgentID] = struct{}{}
	}
	byName := make(map[string][]Window, len(windows))
	for _, window := range windows {
		byName[window.Name] = append(byName[window.Name], window)
	}
	for name := range expected {
		matches := byName[name]
		if len(matches) > 1 {
			return fmt.Errorf("tmux window %q is duplicated; refusing to guess by index", name)
		}
		if len(matches) == 1 && matches[0].PaneCount != 1 {
			return fmt.Errorf("tmux window %q has %d panes; refusing to alter incompatible layout", name, matches[0].PaneCount)
		}
		if len(matches) == 1 && matches[0].PaneIndex != 0 {
			return fmt.Errorf("tmux window %q does not use stable pane 0", name)
		}
		if len(matches) == 1 && (!matches[0].Managed || !compatibleCommand(matches[0].CurrentCommand)) {
			return fmt.Errorf("tmux window %q is not a compatible managed Console window", name)
		}
	}
	return nil
}

func (w Workspace) markCreatedWindow(ctx context.Context, name string) error {
	target := "=" + SessionName + ":=" + name
	if _, err := w.Runner.Run(ctx, "set-option", "-w", "-t", target, "pane-base-index", "0"); err != nil {
		return err
	}
	if _, err := w.Runner.Run(ctx, "set-option", "-w", "-t", target, "remain-on-exit", "on"); err != nil {
		return err
	}
	if _, err := w.Runner.Run(ctx, "set-option", "-w", "-t", target, "@openagentx_managed", "1"); err != nil {
		return err
	}
	return nil
}

func compatibleCommand(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" || command == "openagentx" {
		return true
	}
	switch command {
	case "sh", "bash", "zsh", "fish", "dash":
		return true
	default:
		return false
	}
}
