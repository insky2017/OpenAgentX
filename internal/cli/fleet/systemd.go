package fleet

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	fleetmodel "openagentx/internal/fleet"
)

func workerUnit(agentID string) string {
	return "openagentx-worker@" + agentID + ".service"
}

func verifyUserUnits(ctx context.Context, prepared []preparedAgent, deps Dependencies) error {
	expectedBinary, err := canonicalUserBinary(deps)
	if err != nil {
		return err
	}
	home := filepath.Dir(filepath.Dir(filepath.Dir(expectedBinary)))
	for _, agent := range prepared {
		if !agent.entry.Enabled {
			continue
		}
		unit := workerUnit(agent.entry.AgentID)
		loadState, err := deps.RunSystemctl(ctx, "--user", "show", unit, "--property=LoadState", "--value")
		if err != nil || strings.TrimSpace(loadState) != "loaded" {
			return fmt.Errorf("user unit %s is not loaded", unit)
		}
		execStart, err := deps.RunSystemctl(ctx, "--user", "show", unit, "--property=ExecStart", "--value")
		if err != nil {
			return fmt.Errorf("read actual ExecStart for user unit %s: %w", unit, err)
		}
		argv, err := parseSystemdExecArgv(execStart)
		if err != nil {
			return fmt.Errorf("user unit %s ExecStart: %w", unit, err)
		}
		expected := []string{expectedBinary, "worker", "run", "--config", agent.workerPath}
		if !equalStrings(argv, expected) {
			return fmt.Errorf("user unit %s actual ExecStart does not exactly use binary %q and canonical config %q", unit, expectedBinary, agent.workerPath)
		}
		workingDirectory, err := deps.RunSystemctl(ctx, "--user", "show", unit, "--property=WorkingDirectory", "--value")
		if err != nil || strings.TrimSpace(workingDirectory) != home {
			return fmt.Errorf("user unit %s WorkingDirectory must be canonical home %q", unit, home)
		}
		environmentFiles, err := deps.RunSystemctl(ctx, "--user", "show", unit, "--property=EnvironmentFiles", "--value")
		if err != nil {
			return fmt.Errorf("read actual EnvironmentFiles for user unit %s: %w", unit, err)
		}
		expectedEnvironment := strings.TrimSuffix(agent.workerPath, filepath.Ext(agent.workerPath)) + ".env"
		if !exactOptionalEnvironmentFile(environmentFiles, expectedEnvironment) {
			return fmt.Errorf("user unit %s EnvironmentFiles must exactly reference canonical file %q", unit, expectedEnvironment)
		}
	}
	return nil
}

func canonicalUserBinary(deps Dependencies) (string, error) {
	home, err := deps.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return "", fmt.Errorf("resolve user-systemd home: %v", err)
	}
	binary := filepath.Join(filepath.Clean(home), ".local", "bin", "openagentx")
	if err := fleetmodel.ValidateExecutableFile(binary); err != nil {
		return "", err
	}
	return binary, nil
}

func exactOptionalEnvironmentFile(output, expected string) bool {
	value := strings.TrimSpace(output)
	for _, suffix := range []string{" (ignore_errors=yes)", " (ignore_errors=true)"} {
		value = strings.TrimSuffix(value, suffix)
	}
	return value == expected
}

func parseSystemdExecArgv(output string) ([]string, error) {
	trimmed := strings.TrimSpace(output)
	marker := "argv[]="
	start := strings.Index(trimmed, marker)
	if start < 0 {
		return nil, fmt.Errorf("missing structured argv")
	}
	value := trimmed[start+len(marker):]
	if end := strings.Index(value, " ;"); end >= 0 {
		value = value[:end]
	}
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\\\"'") {
		return nil, fmt.Errorf("unsupported or empty structured argv")
	}
	argv := strings.Fields(value)
	if len(argv) != 5 {
		return nil, fmt.Errorf("unexpected Worker argv shape")
	}
	return argv, nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
