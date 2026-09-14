package console

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/term"
	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	consoleclient "openagentx/internal/client/console"
	"openagentx/internal/domain"
	fleetmodel "openagentx/internal/fleet"
)

type Dependencies struct {
	Out           io.Writer
	Err           io.Writer
	In            io.Reader
	ReadPassword  func(string) (string, error)
	IsInteractive func() bool
	NewClient     func(string) (Client, error)
	Tmux          fleetmodel.CommandRunner
	Context       context.Context
}

type Client interface {
	Login(context.Context, string, string) error
	Attach(context.Context, string, string) (consoleapi.AttachResponse, error)
	Follow(context.Context, string, string, int64, func(consoleapi.AttachResponse) error, func(openapi.JournalEventReadModel) error) error
	Dispatch(context.Context, openapi.CreateTaskRequest) (openapi.CreateTaskResponse, error)
	Steer(context.Context, string, openapi.CreateMessageRequest) (openapi.CreateMessageResponse, error)
	Cancel(context.Context, string, openapi.CancelTaskRequest) (openapi.CancelTaskResponse, error)
	DecideApproval(context.Context, string, openapi.DecideApprovalRequest) (openapi.DecideApprovalResponse, error)
	WorkerCommand(context.Context, string, int64, domain.WorkerCommandKind, string, bool) (openapi.WorkerCommandResponse, error)
}

func DefaultDependencies() Dependencies {
	return Dependencies{Out: os.Stdout, Err: os.Stderr, In: os.Stdin, Tmux: fleetmodel.ExecRunner{},
		IsInteractive: func() bool { return term.IsTerminal(int(os.Stdin.Fd())) },
		NewClient:     func(socketPath string) (Client, error) { return consoleclient.NewUnixClient(socketPath) },
		ReadPassword: func(prompt string) (string, error) {
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return "", fmt.Errorf("interactive terminal is required for password input")
			}
			fmt.Fprint(os.Stderr, prompt)
			value, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			return string(value), err
		}}
}

func Execute(args []string, deps Dependencies) int {
	defaults := DefaultDependencies()
	if deps.Out == nil {
		deps.Out = defaults.Out
	}
	if deps.Err == nil {
		deps.Err = defaults.Err
	}
	if deps.In == nil {
		deps.In = defaults.In
	}
	if deps.ReadPassword == nil {
		deps.ReadPassword = defaults.ReadPassword
	}
	if deps.IsInteractive == nil {
		deps.IsInteractive = defaults.IsInteractive
	}
	if deps.NewClient == nil {
		deps.NewClient = defaults.NewClient
	}
	if deps.Tmux == nil {
		deps.Tmux = defaults.Tmux
	}
	if len(args) == 0 {
		usage(deps.Err)
		return 2
	}
	flags := flag.NewFlagSet("console "+args[0], flag.ContinueOnError)
	flags.SetOutput(deps.Err)
	socket := flags.String("socket", "", "OpenAgentX Unix socket")
	username := flags.String("username", "owner", "Web user name")
	agentID := flags.String("agent", "", "Agent ID")
	taskID := flags.String("task", "", "Task ID")
	approvalID := flags.String("approval", "", "Approval request ID")
	organizationID := flags.String("organization", "default", "Organization ID")
	content := flags.String("content", "", "Task or steering content")
	version := flags.Int64("version", 0, "Expected Task, Run, or approval version")
	diagnostic := flags.Bool("diagnostic", false, "Enable authorized diagnostic projection")
	once := flags.Bool("once", false, "Print current state without following events")
	confirmForce := flags.Bool("confirm-force-stop", false, "Acknowledge force-stop risk")
	if err := flags.Parse(args[1:]); err != nil || strings.TrimSpace(*socket) == "" {
		usage(deps.Err)
		return 2
	}
	baseContext := deps.Context
	if baseContext == nil {
		baseContext = context.Background()
	}
	ctx, cancel := signal.NotifyContext(baseContext, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if args[0] == "attach" {
		if strings.TrimSpace(*agentID) == "" {
			resolved, resolveErr := resolveAgentFromTmux(ctx, deps.Tmux)
			if resolveErr != nil {
				fmt.Fprintf(deps.Err, "resolve Console Agent: %v; use --agent explicitly\n", resolveErr)
				return 1
			}
			*agentID = resolved
		}
		if !*once && !deps.IsInteractive() {
			fmt.Fprintln(deps.Err, "continuous console attach requires an interactive TTY; use --once for non-interactive output")
			return 2
		}
	}
	password, err := deps.ReadPassword("Console password: ")
	if err != nil {
		fmt.Fprintf(deps.Err, "read Console password: %v\n", err)
		return 1
	}
	client, err := deps.NewClient(*socket)
	if err != nil {
		fmt.Fprintln(deps.Err, err)
		return 1
	}
	if err := client.Login(ctx, strings.TrimSpace(*username), password); err != nil {
		fmt.Fprintf(deps.Err, "Console login failed: %v\n", err)
		return 1
	}
	idem := consoleclient.IdempotencyKey("console-" + args[0])
	var result any
	switch args[0] {
	case "attach":
		mode := consoleapi.ModeNormal
		if *diagnostic {
			mode = consoleapi.ModeDiagnostic
		}
		if *once {
			attached, callErr := client.Attach(ctx, *agentID, mode)
			if callErr != nil {
				err = callErr
				break
			}
			if encodeErr := writeJSON(deps.Out, attached); encodeErr != nil {
				err = encodeErr
				break
			}
			return 0
		}
		err = runInteractiveAttach(ctx, cancel, client, *agentID, *organizationID, mode, deps)
	case "dispatch":
		if *agentID == "" || *content == "" {
			usage(deps.Err)
			return 2
		}
		result, err = client.Dispatch(ctx, openapi.CreateTaskRequest{Meta: openapi.CommandMeta{IdempotencyKey: idem}, TargetAgentID: *agentID, OrganizationID: *organizationID, DispatchMode: domain.DispatchModeDirect, Content: *content})
	case "steer":
		if *taskID == "" || *content == "" || *version <= 0 {
			usage(deps.Err)
			return 2
		}
		result, err = client.Steer(ctx, *taskID, openapi.CreateMessageRequest{Meta: openapi.CommandMeta{IdempotencyKey: idem, ExpectedVersion: *version}, Content: *content})
	case "cancel":
		if *taskID == "" || *version <= 0 {
			usage(deps.Err)
			return 2
		}
		result, err = client.Cancel(ctx, *taskID, openapi.CancelTaskRequest{Meta: openapi.CommandMeta{IdempotencyKey: idem, ExpectedVersion: *version}})
	case "approve", "reject":
		if *approvalID == "" || *version <= 0 {
			usage(deps.Err)
			return 2
		}
		decision := domain.ApprovalDecisionApprove
		if args[0] == "reject" {
			decision = domain.ApprovalDecisionReject
		}
		result, err = client.DecideApproval(ctx, *approvalID, openapi.DecideApprovalRequest{Meta: openapi.CommandMeta{IdempotencyKey: idem, ExpectedVersion: *version}, Decision: decision})
	case "down", "force-stop":
		if *agentID == "" {
			usage(deps.Err)
			return 2
		}
		attached, attachErr := client.Attach(ctx, *agentID, consoleapi.ModeNormal)
		if attachErr != nil {
			err = attachErr
			break
		}
		if attached.WorkerInstanceID == "" {
			result = attached
			break
		}
		kind := domain.WorkerCommandStop
		if args[0] == "force-stop" {
			if !*confirmForce {
				fmt.Fprintln(deps.Err, "force-stop requires --confirm-force-stop")
				return 2
			}
			kind = domain.WorkerCommandForceStop
		}
		result, err = client.WorkerCommand(ctx, attached.WorkerInstanceID, attached.Generation, kind, idem, *confirmForce)
	default:
		usage(deps.Err)
		return 2
	}
	if err != nil {
		fmt.Fprintf(deps.Err, "Console command failed: %v\n", err)
		return 1
	}
	if result != nil {
		if err := writeJSON(deps.Out, result); err != nil {
			fmt.Fprintln(deps.Err, err)
			return 1
		}
	}
	return 0
}

func usage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage: openagentx console <attach|dispatch|steer|cancel|approve|reject|down|force-stop> --socket <path> [options]")
}
