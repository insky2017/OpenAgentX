package fleet

import (
	"context"
	"fmt"
	"io"
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
	return deps.OpenNative(ctx, NativeOpenRequest{AgentID: o.id, WorkerConfig: filepath.Join(o.paths.workerDir, o.id+".yaml"), Socket: o.paths.socket, Credentials: o.paths.credentials, In: deps.In, Out: deps.Out, Err: deps.Err})
}
