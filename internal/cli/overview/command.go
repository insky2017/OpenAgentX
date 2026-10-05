// Package overview implements a read-only terminal overview of existing Agents.
package overview

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"
	"openagentx/internal/credentialstore"
	fleetmodel "openagentx/internal/fleet"
	"openagentx/internal/localprofile"
)

type Dependencies struct {
	In            io.Reader
	Out, Err      io.Writer
	Context       context.Context
	IsInteractive func() bool
	RunProgram    func(context.Context, tea.Model, io.Reader, io.Writer) (tea.Model, error)
	Tmux          fleetmodel.CommandRunner
}

func DefaultDependencies() Dependencies {
	return Dependencies{In: os.Stdin, Out: os.Stdout, Err: os.Stderr,
		IsInteractive: func() bool { return term.IsTerminal(int(os.Stdin.Fd())) },
		Tmux:          fleetmodel.ExecRunner{CurrentTarget: os.Getenv("TMUX_PANE")},
		RunProgram: func(ctx context.Context, model tea.Model, in io.Reader, out io.Writer) (tea.Model, error) {
			return tea.NewProgram(model, tea.WithContext(ctx), tea.WithInput(in), tea.WithOutput(out), tea.WithAltScreen()).Run()
		},
	}
}

func Execute(args []string, deps Dependencies) int {
	defaults := DefaultDependencies()
	if deps.In == nil {
		deps.In = defaults.In
	}
	if deps.Out == nil {
		deps.Out = defaults.Out
	}
	if deps.Err == nil {
		deps.Err = defaults.Err
	}
	if deps.IsInteractive == nil {
		deps.IsInteractive = defaults.IsInteractive
	}
	if deps.RunProgram == nil {
		deps.RunProgram = defaults.RunProgram
	}
	if deps.Tmux == nil {
		deps.Tmux = defaults.Tmux
	}
	flags := flag.NewFlagSet("overview", flag.ContinueOnError)
	flags.SetOutput(deps.Err)
	var socketFlag, credentialsFlag, workerFlag localprofile.PathFlag
	flags.Var(&socketFlag, "socket", localprofile.PathUsage(localprofile.SocketPath, "控制面 Unix socket"))
	flags.Var(&credentialsFlag, "credentials", localprofile.PathUsage(localprofile.CredentialsPath, "现有 CLI 登录凭据"))
	flags.Var(&workerFlag, "worker-dir", localprofile.PathUsage(localprofile.WorkerConfigDir, "只读核对终端来源的 Worker 配置目录"))
	flags.Usage = func() {
		fmt.Fprintln(deps.Out, "Usage: openagentx overview [--socket PATH] [--credentials PATH] [--worker-dir PATH]\n只读总览；↑↓ 选择 / Tab 详情 / Enter 跳转已有终端 / r 刷新 / q 退出。\n路径优先级：显式参数 > 资源环境变量 > OPENAGENTX_HOME > ~/.openagentx")
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		flags.Usage()
		return 2
	}
	if !deps.IsInteractive() {
		fmt.Fprintln(deps.Err, "overview 需要交互终端；脚本请使用 agent status --json 或 Observe API。")
		return 2
	}
	resolver := localprofile.DefaultResolver()
	socket, err := resolver.Resolve(localprofile.SocketPath, socketFlag.Override())
	if err != nil {
		fmt.Fprintln(deps.Err, err)
		return 2
	}
	credentials, err := resolver.Resolve(localprofile.CredentialsPath, credentialsFlag.Override())
	if err != nil {
		fmt.Fprintln(deps.Err, err)
		return 2
	}
	if err = localprofile.EnsureDistinct(map[string]string{"socket": socket.Path, "credentials": credentials.Path}); err != nil {
		fmt.Fprintln(deps.Err, err)
		return 2
	}
	workers, err := resolver.Resolve(localprofile.WorkerConfigDir, workerFlag.Override())
	if err != nil {
		fmt.Fprintln(deps.Err, err)
		return 2
	}
	store, err := credentialstore.New(credentials.Path, credentialstore.Options{})
	if err != nil {
		fmt.Fprintln(deps.Err, "无法读取 CLI 凭据配置")
		return 1
	}
	base := deps.Context
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := signal.NotifyContext(base, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	service := &reader{socket: socket.Path, credentials: credentials.Path, store: store, now: time.Now}
	model := newModel(ctx, service, &navigator{runner: deps.Tmux, socket: socket.Path, credentials: credentials.Path, sourcePane: os.Getenv("TMUX_PANE"), workerDir: workers.Path})
	_, err = deps.RunProgram(ctx, model, deps.In, deps.Out)
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, tea.ErrInterrupted) {
		fmt.Fprintln(deps.Err, "总览终端退出："+safeText(err.Error(), 180))
		return 1
	}
	return 0
}
