package fleet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	consoleapi "openagentx/internal/api/console"
	fleetmodel "openagentx/internal/fleet"
	"openagentx/internal/runtime/codex"
)

type terminalStartup struct {
	Mode            string `json:"mode"`
	NativeSupported bool   `json:"native_supported"`
	Note            string `json:"note"`
}

// Startup policy describes newly created/dead panes, never the contents of a
// reused live pane. Reconcile remains the sole owner of safe pane replacement.
func terminalStartupModes(prepared []preparedAgent, consoleOnly bool) map[string]terminalStartup {
	modes := make(map[string]terminalStartup, len(prepared))
	for _, agent := range prepared {
		mode := terminalStartup{Mode: "console", NativeSupported: agent.nativeSupported,
			Note: "OAX 状态 Console；此 Runtime 的受管原生交互终端尚未适配（包括 AGY），不能在此使用原生提示词交互"}
		if agent.nativeSupported {
			mode.Note = "已配置 Codex 原生终端桥接；后台须就绪，现有活 pane 保持原样"
			if !consoleOnly {
				mode.Mode = "native"
			}
		}
		modes[agent.entry.AgentID] = mode
	}
	return modes
}

func configureNativeTerminals(workspace *fleetmodel.Workspace, prepared []preparedAgent, paths fleetPaths, deps Dependencies) {
	console := workspace.ConsoleCommand
	native := make(map[string]bool, len(prepared))
	for _, agent := range prepared {
		native[agent.entry.AgentID] = agent.nativeSupported
		if !agent.nativeSupported {
			fmt.Fprintf(deps.Err, "Agent %s：使用 OAX 状态 Console；AGY 等 Runtime 的受管原生终端尚未适配。\n", agent.entry.AgentID)
		}
	}
	// newWorkspace already resolved and validated the canonical executable.
	binary := workspace.OverviewCommand[0]
	workspace.ConsoleCommand = func(agentID string) []string {
		if !native[agentID] {
			return console(agentID)
		}
		return []string{binary, "agent", "open", agentID, "--native", "--socket", paths.socket,
			"--credentials", paths.credentials, "--file", paths.manifest, "--worker-dir", paths.workerDir, "--db", paths.database}
	}
}

// Wait only for panes that will actually be started. Each wait is bounded and
// an unready Agent is omitted from reconciliation so other panes can still open.
func readyNativeWorkspace(ctx context.Context, manifest fleetmodel.Manifest, prepared []preparedAgent, workspace fleetmodel.Workspace, client consoleClient, deps Dependencies) (fleetmodel.Manifest, bool) {
	windows, inspectErr := workspace.Inspect(ctx)
	if inspectErr != nil && !errors.Is(inspectErr, fleetmodel.ErrSessionMissing) {
		fmt.Fprintf(deps.Err, "Fleet workspace inspection failed: %v\n", inspectErr)
		return fleetmodel.Manifest{}, false
	}
	live := make(map[string]bool)
	for _, window := range windows {
		live[window.Name] = !window.PaneZeroDead || !workspace.RespawnDead
	}
	ready := manifest
	ready.Agents = nil
	allReady := true
	for _, agent := range prepared {
		command := workspace.ConsoleCommand(agent.entry.AgentID)
		isNative := len(command) > 4 && command[1] == "agent" && command[4] == "--native"
		if isNative && !live[agent.entry.AgentID] {
			waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			err := waitNativeEndpoint(waitCtx, agent, client, deps)
			cancel()
			if err != nil {
				allReady = false
				fmt.Fprintf(deps.Err, "Agent %s 原生终端后台未就绪：%v；未替换任何活 pane。先运行 agent resume %s --no-open，再运行 fleet workspace --respawn-dead（沿用本次 path overrides）。\n", agent.entry.AgentID, err, agent.entry.AgentID)
				continue
			}
		}
		ready.Agents = append(ready.Agents, agent.entry)
	}
	return ready, allReady
}

func waitNativeEndpoint(ctx context.Context, agent preparedAgent, client consoleClient, deps Dependencies) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		attached, err := client.Attach(ctx, agent.entry.AgentID, consoleapi.ModeNormal)
		if err != nil {
			return fmt.Errorf("读取 Worker 状态失败: %v", safeAuthError(err))
		}
		if !effectivelyOffline(attached, deps.Now()) {
			data, readErr := fleetmodel.ReadSecureFile(agent.nativeStatePath, fleetmodel.SecureFileOptions{MaximumBytes: 1 << 20, RequirePrivate: true})
			var state codex.SessionState
			// Native Open initializes a first thread through OAX when none exists.
			if readErr == nil && json.Unmarshal(data, &state) == nil && state.Endpoint != "" {
				probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
				rpc, dialErr := codex.DialRPC(probeCtx, state.Endpoint)
				cancel()
				if dialErr == nil {
					_ = rpc.Close()
					return nil
				}
			}
		}
		if err := deps.Wait(ctx, time.Second); err != nil {
			return fmt.Errorf("等待 Worker 与 Codex endpoint 超时或已取消: %w", err)
		}
	}
}
