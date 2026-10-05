package fleet

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	fleetmodel "openagentx/internal/fleet"
	"path/filepath"
)

// NativeOpenRequest carries only local references. A bridge must route writes
// through the authoritative Task/Run admission path and arbitrate one writer.
type NativeOpenRequest struct {
	AgentID, WorkerConfig, Socket, Credentials string
	In                                         io.Reader
	Out, Err                                   io.Writer
}

func openAgentNative(ctx context.Context, o agentOptions, deps Dependencies) error {
	if !deps.IsInteractive() {
		return fmt.Errorf("--native requires an interactive TTY")
	}
	if deps.OpenNative == nil {
		return fmt.Errorf("受管原生终端桥接尚未就绪；未启动裸 CLI。可运行 openagentx agent open %s --console 查看受管状态", o.id)
	}
	if os.Getenv("TMUX") != "" {
		pane := os.Getenv("TMUX_PANE")
		if !strings.HasPrefix(pane, "%") {
			return fmt.Errorf("cannot identify exact current tmux pane: TMUX_PANE is missing or invalid")
		}
		if _, err := strconv.ParseUint(strings.TrimPrefix(pane, "%"), 10, 64); err != nil {
			return fmt.Errorf("cannot identify exact current tmux pane: TMUX_PANE is invalid")
		}
		workspace := fleetmodel.Workspace{Runner: deps.Tmux, CurrentTarget: pane,
			PaneLabel: func(string) string { return fleetmodel.NativeTerminalLabel },
			Warn:      func(err error) { fmt.Fprintf(deps.Err, "终端标签告警: %v\n", err) },
		}
		session, err := workspace.CurrentSession(ctx)
		if err != nil {
			return err
		}
		if session == fleetmodel.SessionName {
			location, err := workspace.PreflightAttach(ctx)
			if err != nil {
				return err
			}
			if _, err := workspace.BindCurrent(ctx, location, o.id, false); err != nil {
				return err
			}
		}
	}
	return deps.OpenNative(ctx, NativeOpenRequest{AgentID: o.id, WorkerConfig: filepath.Join(o.paths.workerDir, o.id+".yaml"), Socket: o.paths.socket, Credentials: o.paths.credentials, In: deps.In, Out: deps.Out, Err: deps.Err})
}
