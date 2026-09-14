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
	"openagentx/internal/localprofile"
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
		if !deps.IsInteractive() {
			fmt.Fprintln(deps.Err, "openagentx console requires an interactive TTY; use a direct Console subcommand")
			return 2
		}
		fmt.Fprintln(deps.Err, "interactive Console menu is not available in this build")
		return 1
	}
	command := args[0]
	if command == "help" || command == "--help" || command == "-h" {
		usage(deps.Out)
		return 0
	}
	switch command {
	case "login", "logout", "attach", "dispatch", "steer", "cancel", "approve", "reject", "down", "force-stop":
	default:
		usage(deps.Err)
		return 2
	}
	flags := flag.NewFlagSet("console "+command, flag.ContinueOnError)
	flags.SetOutput(deps.Err)
	var socketFlag localprofile.PathFlag
	var credentialsFlag localprofile.PathFlag
	flags.Var(&socketFlag, "socket", localprofile.PathUsage(localprofile.SocketPath, "OpenAgentX Unix socket"))
	var username, agentID, taskID, approvalID, organizationID, content string
	var version int64
	var diagnostic, once, confirmForce bool
	username = "owner"
	organizationID = "default"
	switch command {
	case "login":
		flags.Var(&credentialsFlag, "credentials", localprofile.PathUsage(localprofile.CredentialsPath, "CLI credential file"))
		flags.StringVar(&username, "username", username, "Web user name")
	case "logout":
		flags.Var(&credentialsFlag, "credentials", localprofile.PathUsage(localprofile.CredentialsPath, "CLI credential file"))
	case "attach":
		flags.StringVar(&username, "username", username, "Web user name")
		flags.StringVar(&agentID, "agent", "", "Agent ID")
		flags.StringVar(&organizationID, "organization", organizationID, "Organization ID")
		flags.BoolVar(&diagnostic, "diagnostic", false, "Enable authorized diagnostic projection")
		flags.BoolVar(&once, "once", false, "Print current state without following events")
	case "dispatch":
		flags.StringVar(&username, "username", username, "Web user name")
		flags.StringVar(&agentID, "agent", "", "Agent ID")
		flags.StringVar(&organizationID, "organization", organizationID, "Organization ID")
		flags.StringVar(&content, "content", "", "Task content")
	case "steer":
		flags.StringVar(&username, "username", username, "Web user name")
		flags.StringVar(&taskID, "task", "", "Task ID")
		flags.StringVar(&content, "content", "", "Steering content")
		flags.Int64Var(&version, "version", 0, "Expected Task version")
	case "cancel":
		flags.StringVar(&username, "username", username, "Web user name")
		flags.StringVar(&taskID, "task", "", "Task ID")
		flags.Int64Var(&version, "version", 0, "Expected Task version")
	case "approve", "reject":
		flags.StringVar(&username, "username", username, "Web user name")
		flags.StringVar(&approvalID, "approval", "", "Approval request ID")
		flags.Int64Var(&version, "version", 0, "Expected approval version")
	case "down":
		flags.StringVar(&username, "username", username, "Web user name")
		flags.StringVar(&agentID, "agent", "", "Agent ID")
	case "force-stop":
		flags.StringVar(&username, "username", username, "Web user name")
		flags.StringVar(&agentID, "agent", "", "Agent ID")
		flags.BoolVar(&confirmForce, "confirm-force-stop", false, "Acknowledge force-stop risk")
	}
	if err := flags.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		usage(deps.Err)
		return 2
	}
	if flags.NArg() != 0 {
		usage(deps.Err)
		return 2
	}
	resolver := localprofile.DefaultResolver()
	socket, err := resolver.Resolve(localprofile.SocketPath, socketFlag.Override())
	if err != nil {
		fmt.Fprintf(deps.Err, "resolve Console socket: %v\n", err)
		return 2
	}
	if command == "login" || command == "logout" {
		credentials, resolveErr := resolver.Resolve(localprofile.CredentialsPath, credentialsFlag.Override())
		if resolveErr != nil {
			fmt.Fprintf(deps.Err, "resolve Console credentials: %v\n", resolveErr)
			return 2
		}
		if command == "login" {
			if !deps.IsInteractive() {
				fmt.Fprintln(deps.Err, "openagentx console login requires an interactive TTY")
				return 2
			}
			fmt.Fprintln(deps.Err, "CLI credential login is not available in this build")
			return 1
		}
		if _, statErr := os.Stat(credentials.Path); os.IsNotExist(statErr) {
			fmt.Fprintf(deps.Err, "Console credential not found: %s\n", credentials.Path)
			return 1
		} else if statErr != nil {
			fmt.Fprintf(deps.Err, "inspect Console credential: %v\n", statErr)
			return 1
		}
		fmt.Fprintln(deps.Err, "CLI credential logout is not available in this build")
		return 1
	}
	baseContext := deps.Context
	if baseContext == nil {
		baseContext = context.Background()
	}
	ctx, cancel := signal.NotifyContext(baseContext, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if command == "attach" {
		if strings.TrimSpace(agentID) == "" {
			resolved, resolveErr := resolveAgentFromTmux(ctx, deps.Tmux)
			if resolveErr != nil {
				fmt.Fprintf(deps.Err, "resolve Console Agent: %v; use --agent explicitly\n", resolveErr)
				return 1
			}
			agentID = resolved
		}
		if !once && !deps.IsInteractive() {
			fmt.Fprintln(deps.Err, "continuous console attach requires an interactive TTY; use --once for non-interactive output")
			return 2
		}
	}
	password, err := deps.ReadPassword("Console password: ")
	if err != nil {
		fmt.Fprintf(deps.Err, "read Console password: %v\n", err)
		return 1
	}
	client, err := deps.NewClient(socket.Path)
	if err != nil {
		fmt.Fprintln(deps.Err, err)
		return 1
	}
	if err := client.Login(ctx, strings.TrimSpace(username), password); err != nil {
		fmt.Fprintf(deps.Err, "Console login failed: %v\n", err)
		return 1
	}
	idem := consoleclient.IdempotencyKey("console-" + command)
	var result any
	switch command {
	case "attach":
		mode := consoleapi.ModeNormal
		if diagnostic {
			mode = consoleapi.ModeDiagnostic
		}
		if once {
			attached, callErr := client.Attach(ctx, agentID, mode)
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
		err = runInteractiveAttach(ctx, cancel, client, agentID, organizationID, mode, deps)
	case "dispatch":
		if agentID == "" || content == "" {
			usage(deps.Err)
			return 2
		}
		result, err = client.Dispatch(ctx, openapi.CreateTaskRequest{Meta: openapi.CommandMeta{IdempotencyKey: idem}, TargetAgentID: agentID, OrganizationID: organizationID, DispatchMode: domain.DispatchModeDirect, Content: content})
	case "steer":
		if taskID == "" || content == "" || version <= 0 {
			usage(deps.Err)
			return 2
		}
		result, err = client.Steer(ctx, taskID, openapi.CreateMessageRequest{Meta: openapi.CommandMeta{IdempotencyKey: idem, ExpectedVersion: version}, Content: content})
	case "cancel":
		if taskID == "" || version <= 0 {
			usage(deps.Err)
			return 2
		}
		result, err = client.Cancel(ctx, taskID, openapi.CancelTaskRequest{Meta: openapi.CommandMeta{IdempotencyKey: idem, ExpectedVersion: version}})
	case "approve", "reject":
		if approvalID == "" || version <= 0 {
			usage(deps.Err)
			return 2
		}
		decision := domain.ApprovalDecisionApprove
		if command == "reject" {
			decision = domain.ApprovalDecisionReject
		}
		result, err = client.DecideApproval(ctx, approvalID, openapi.DecideApprovalRequest{Meta: openapi.CommandMeta{IdempotencyKey: idem, ExpectedVersion: version}, Decision: decision})
	case "down", "force-stop":
		if agentID == "" {
			usage(deps.Err)
			return 2
		}
		attached, attachErr := client.Attach(ctx, agentID, consoleapi.ModeNormal)
		if attachErr != nil {
			err = attachErr
			break
		}
		if attached.WorkerInstanceID == "" {
			result = attached
			break
		}
		kind := domain.WorkerCommandStop
		if command == "force-stop" {
			if !confirmForce {
				fmt.Fprintln(deps.Err, "force-stop requires --confirm-force-stop")
				return 2
			}
			kind = domain.WorkerCommandForceStop
		}
		result, err = client.WorkerCommand(ctx, attached.WorkerInstanceID, attached.Generation, kind, idem, confirmForce)
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
	fmt.Fprintln(writer, "Usage: openagentx console")
	fmt.Fprintln(writer, "       openagentx console login [--socket <path>] [--credentials <path>]")
	fmt.Fprintln(writer, "       openagentx console logout [--socket <path>] [--credentials <path>]")
	fmt.Fprintln(writer, "       openagentx console attach [--socket <path>] [--agent <agent-id>] [--diagnostic] [--once]")
	fmt.Fprintln(writer, "       legacy controls: dispatch|steer|cancel|approve|reject|down|force-stop")
	fmt.Fprintf(writer, "Default socket source: $%s > $%s > ~/.openagentx/run/openagentx.sock\n", localprofile.EnvSocketPath, localprofile.EnvHome)
	fmt.Fprintf(writer, "Default credential source: $%s > $%s > ~/.openagentx/credentials.json\n", localprofile.EnvCredentialsPath, localprofile.EnvHome)
}
