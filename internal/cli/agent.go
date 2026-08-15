package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"agentbus/internal/client"
	"agentbus/internal/connector"
	"agentbus/internal/domain"
	"agentbus/internal/service"
)

func runAgent(args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: agentbus agent <register|list|get|whoami|attach|bootstrap|launch> [flags]\n")
		return 1
	}

	sub := args[0]
	subArgs := args[1:]

	switch sub {
	case "register":
		return runAgentRegister(subArgs)
	case "list":
		return runAgentList(subArgs)
	case "get":
		return runAgentGet(subArgs)
	case "whoami":
		return runAgentWhoami(subArgs)
	case "attach":
		return runAgentAttach(subArgs)
	case "bootstrap":
		return runAgentBootstrap(subArgs)
	case "launch":
		return runAgentLaunch(subArgs)
	default:
		fmt.Fprintf(os.Stderr, "Unknown agent subcommand: %s\n", sub)
		return 1
	}
}

func runAgentRegister(args []string) int {
	fs := flag.NewFlagSet("agent register", flag.ContinueOnError)
	id := fs.String("id", "", "Agent ID (required)")
	role := fs.String("role", "", "Agent role (required)")
	conn := fs.String("connector", domain.ConnectorTmux, "Connector type ('tmux' or 'none')")
	address := fs.String("address", "", "Connector address (e.g. %50, %51, session:window.pane)")
	socketPath := fs.String("socket", "", "Unix socket path")

	if err := fs.Parse(ReorderArgs(args)); err != nil {
		return 1
	}

	if strings.TrimSpace(*id) == "" {
		PrintError(fmt.Errorf("flag --id is required"))
		return 1
	}
	if strings.TrimSpace(*role) == "" {
		PrintError(fmt.Errorf("flag --role is required"))
		return 1
	}
	if *conn == domain.ConnectorTmux && strings.TrimSpace(*address) == "" {
		PrintError(fmt.Errorf("flag --address is required when connector is 'tmux'"))
		return 1
	}

	c := client.NewClient(*socketPath)
	agent, err := c.RegisterAgent(context.Background(), &domain.Agent{
		ID:        *id,
		Role:      *role,
		Connector: *conn,
		Address:   *address,
	})
	if err != nil {
		PrintError(err)
		return 1
	}

	PrintJSON(agent)
	return 0
}

func runAgentList(args []string) int {
	fs := flag.NewFlagSet("agent list", flag.ContinueOnError)
	socketPath := fs.String("socket", "", "Unix socket path")

	if err := fs.Parse(ReorderArgs(args)); err != nil {
		return 1
	}

	c := client.NewClient(*socketPath)
	agents, err := c.ListAgents(context.Background())
	if err != nil {
		PrintError(err)
		return 1
	}

	PrintJSON(agents)
	return 0
}

func runAgentGet(args []string) int {
	fs := flag.NewFlagSet("agent get", flag.ContinueOnError)
	idFlag := fs.String("id", "", "Agent ID")
	socketPath := fs.String("socket", "", "Unix socket path")

	if err := fs.Parse(ReorderArgs(args)); err != nil {
		return 1
	}

	agentID := *idFlag
	if agentID == "" && len(fs.Args()) > 0 {
		agentID = fs.Args()[0]
	}

	if strings.TrimSpace(agentID) == "" {
		PrintError(fmt.Errorf("agent ID is required (e.g. agentbus agent get <agent-id> or --id <agent-id>)"))
		return 1
	}

	c := client.NewClient(*socketPath)
	agent, err := c.GetAgent(context.Background(), agentID)
	if err != nil {
		PrintError(err)
		return 1
	}

	PrintJSON(agent)
	return 0
}

func runAgentWhoami(args []string) int {
	fs := flag.NewFlagSet("agent whoami", flag.ContinueOnError)
	configPath := fs.String("config", "", "Path to agent.yaml manifest (required)")

	if err := fs.Parse(ReorderArgs(args)); err != nil {
		return 1
	}

	if strings.TrimSpace(*configPath) == "" {
		PrintError(fmt.Errorf("flag --config is required"))
		return 1
	}

	manifest, profile, agent, err := domain.LoadManifest(*configPath, client.DefaultAgentBusDir())
	if err != nil {
		PrintError(err)
		return 1
	}

	res := map[string]any{
		"manifest": manifest,
		"profile":  profile,
		"agent":    agent,
		"recommended_env": map[string]string{
			"AGENTBUS_AGENT_ID":     manifest.ID,
			"AGENTBUS_ROLE":         manifest.Role,
			"AGENTBUS_CONFIG":       profile.ConfigPath,
			"AGENTBUS_INSTRUCTIONS": profile.InstructionsPath,
			"AGENTBUS_SOCKET":       client.ResolveSocketPath(""),
		},
	}

	PrintJSON(res)
	return 0
}

func runAgentAttach(args []string) int {
	fs := flag.NewFlagSet("agent attach", flag.ContinueOnError)
	configPath := fs.String("config", "", "Path to agent.yaml manifest (required)")
	address := fs.String("address", "", "Override tmux address (e.g. %50, %51)")
	noNotify := fs.Bool("no-notify", false, "Do not inject bootstrap notification into pane")
	socketPath := fs.String("socket", "", "Unix socket path")

	if err := fs.Parse(ReorderArgs(args)); err != nil {
		return 1
	}

	if strings.TrimSpace(*configPath) == "" {
		PrintError(fmt.Errorf("flag --config is required"))
		return 1
	}

	manifest, profile, agent, err := domain.LoadManifest(*configPath, client.DefaultAgentBusDir())
	if err != nil {
		PrintError(err)
		return 1
	}

	// Address resolution precedence: --address > manifest.address > TMUX_PANE (if auto/empty)
	addr := strings.TrimSpace(*address)
	if addr != "" {
		agent.Address = addr
		manifest.Address = addr
	} else if agent.Connector == domain.ConnectorTmux && (agent.Address == "" || agent.Address == "auto") {
		if tmuxPane := os.Getenv("TMUX_PANE"); tmuxPane != "" {
			agent.Address = tmuxPane
			manifest.Address = tmuxPane
		}
	}

	c := client.NewClient(*socketPath)
	resp, err := c.AttachAgent(context.Background(), service.AttachAgentRequest{
		Agent:    agent,
		Profile:  profile,
		NoNotify: *noNotify,
	})
	if err != nil {
		PrintError(err)
		return 1
	}

	PrintJSON(resp)
	return 0
}

func runAgentBootstrap(args []string) int {
	fs := flag.NewFlagSet("agent bootstrap", flag.ContinueOnError)
	idFlag := fs.String("id", "", "Agent ID (required)")
	socketPath := fs.String("socket", "", "Unix socket path")

	if err := fs.Parse(ReorderArgs(args)); err != nil {
		return 1
	}

	agentID := *idFlag
	if agentID == "" && len(fs.Args()) > 0 {
		agentID = fs.Args()[0]
	}

	if strings.TrimSpace(agentID) == "" {
		PrintError(fmt.Errorf("agent ID is required (e.g. agentbus agent bootstrap --id <agent-id>)"))
		return 1
	}

	c := client.NewClient(*socketPath)
	resp, err := c.BootstrapAgent(context.Background(), agentID)
	if err != nil {
		PrintError(err)
		return 1
	}

	PrintJSON(resp)
	return 0
}

func runAgentLaunch(args []string) int {
	// Separate flags before '--' and command after '--'
	var flagArgs []string
	var cmdArgs []string

	dashDashIdx := -1
	for i, arg := range args {
		if arg == "--" {
			dashDashIdx = i
			break
		}
	}

	if dashDashIdx >= 0 {
		flagArgs = args[:dashDashIdx]
		cmdArgs = args[dashDashIdx+1:]
	} else {
		flagArgs = args
	}

	fs := flag.NewFlagSet("agent launch", flag.ContinueOnError)
	configPath := fs.String("config", "", "Path to agent.yaml manifest (required)")
	address := fs.String("address", "", "Override tmux address (default $TMUX_PANE)")
	delayStr := fs.String("bootstrap-delay", "2s", "Delay before bootstrap injection (0-30s)")
	socketPath := fs.String("socket", "", "Unix socket path")

	if err := fs.Parse(ReorderArgs(flagArgs)); err != nil {
		return 1
	}

	if strings.TrimSpace(*configPath) == "" {
		PrintError(fmt.Errorf("flag --config is required"))
		return 1
	}

	if len(cmdArgs) == 0 {
		PrintError(fmt.Errorf("command after '--' is required (e.g. agentbus agent launch --config agent.yaml -- <cmd> [args...])"))
		return 1
	}

	bootstrapDelay, err := time.ParseDuration(*delayStr)
	if err != nil || bootstrapDelay < 0 || bootstrapDelay > 30*time.Second {
		PrintError(fmt.Errorf("invalid --bootstrap-delay: must be between 0s and 30s"))
		return 1
	}

	// 1. Must check non-empty TMUX_PANE
	tmuxPane := strings.TrimSpace(os.Getenv("TMUX_PANE"))
	if tmuxPane == "" {
		PrintError(fmt.Errorf("agent launch requires a valid tmux pane environment ($TMUX_PANE is empty)"))
		return 1
	}

	// 2. Load manifest and verify connector == tmux
	manifest, profile, agent, err := domain.LoadManifest(*configPath, client.DefaultAgentBusDir())
	if err != nil {
		PrintError(err)
		return 1
	}

	if manifest.Connector != domain.ConnectorTmux {
		PrintError(fmt.Errorf("agent launch only supports 'connector: tmux', got '%s'", manifest.Connector))
		return 1
	}

	// 3. Address conflict check
	explicitAddr := strings.TrimSpace(*address)
	manifestAddr := strings.TrimSpace(manifest.Address)

	if explicitAddr != "" && explicitAddr != tmuxPane {
		PrintError(fmt.Errorf("address conflict: --address '%s' does not match current $TMUX_PANE '%s'", explicitAddr, tmuxPane))
		return 1
	}
	if manifestAddr != "" && manifestAddr != "auto" && manifestAddr != tmuxPane {
		PrintError(fmt.Errorf("address conflict: manifest address '%s' does not match current $TMUX_PANE '%s'", manifestAddr, tmuxPane))
		return 1
	}
	if explicitAddr != "" && manifestAddr != "" && manifestAddr != "auto" && explicitAddr != manifestAddr {
		PrintError(fmt.Errorf("address conflict: --address '%s' does not match manifest address '%s'", explicitAddr, manifestAddr))
		return 1
	}

	resolvedAddr := tmuxPane
	agent.Address = resolvedAddr
	manifest.Address = resolvedAddr

	resolvedSocket := client.ResolveSocketPath(*socketPath)

	// Prepare environment
	env := os.Environ()
	env = append(env,
		fmt.Sprintf("AGENTBUS_AGENT_ID=%s", manifest.ID),
		fmt.Sprintf("AGENTBUS_ROLE=%s", manifest.Role),
		fmt.Sprintf("AGENTBUS_CONFIG=%s", profile.ConfigPath),
		fmt.Sprintf("AGENTBUS_INSTRUCTIONS=%s", profile.InstructionsPath),
		fmt.Sprintf("AGENTBUS_SOCKET=%s", resolvedSocket),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env

	if err := cmd.Start(); err != nil {
		PrintError(fmt.Errorf("failed to start command '%s': %w", cmdArgs[0], err))
		return 1
	}

	childDone := make(chan error, 1)
	go func() {
		childDone <- cmd.Wait()
	}()

	terminateChild := func() {
		if cmd.Process == nil {
			return
		}
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-childDone:
			return
		case <-time.After(1 * time.Second):
			_ = cmd.Process.Kill()
			<-childDone
		}
	}

	// 1. Wait for bootstrap delay or premature child exit or context cancel
	if bootstrapDelay > 0 {
		select {
		case <-ctx.Done():
			terminateChild()
			return 1
		case err := <-childDone:
			PrintError(fmt.Errorf("child process exited prematurely before bootstrap delay: %v", err))
			return 1
		case <-time.After(bootstrapDelay):
		}
	}

	// 2. Cancellable attach concurrent with child execution
	type attachResult struct {
		resp *service.AttachAgentResponse
		err  error
	}

	attachCtx, cancelAttach := context.WithCancel(ctx)
	defer cancelAttach()

	attachCh := make(chan attachResult, 1)
	go func() {
		c := client.NewClient(resolvedSocket)
		resp, err := c.AttachAgent(attachCtx, service.AttachAgentRequest{
			Agent:    agent,
			Profile:  profile,
			NoNotify: false,
		})
		attachCh <- attachResult{resp: resp, err: err}
	}()

	select {
	case <-ctx.Done():
		cancelAttach()
		terminateChild()
		return 1
	case err := <-childDone:
		cancelAttach()
		PrintError(fmt.Errorf("child process exited before attach completed (exit_err=%v)", err))
		return 1
	case res := <-attachCh:
		if res.err != nil {
			PrintError(fmt.Errorf("agent attach failed: %w", res.err))
			terminateChild()
			return 1
		}
		if res.resp.Disposition != connector.DispositionNotified {
			PrintError(fmt.Errorf("agent launch expected disposition 'notified', got '%s' (delivery_error=%s)", res.resp.Disposition, res.resp.DeliveryError))
			terminateChild()
			return 1
		}
	}

	// 3. Attach succeeded! Now wait for child process execution and exit propagation
	select {
	case <-ctx.Done():
		terminateChild()
		return 1
	case err := <-childDone:
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				return exitErr.ExitCode()
			}
			return 1
		}
		return 0
	}
}
