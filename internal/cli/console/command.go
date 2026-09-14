package console

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	consoleclient "openagentx/internal/client/console"
	"openagentx/internal/credentialstore"
	"openagentx/internal/domain"
	fleetmodel "openagentx/internal/fleet"
	"openagentx/internal/localprofile"
)

type Dependencies struct {
	Out                io.Writer
	Err                io.Writer
	In                 io.Reader
	ReadPassword       func(string) (string, error)
	IsInteractive      func() bool
	NewClient          func(string) (Client, error)
	NewCredentialStore func(string) (CredentialStore, error)
	Tmux               fleetmodel.CommandRunner
	Context            context.Context
}

type Client interface {
	ProbeInstallation(context.Context) (openapi.CLIInstallationResponse, error)
	LoginCredential(context.Context, string, string) (openapi.CLILoginResponse, error)
	UseCredential(context.Context, string, string) error
	Logout(context.Context) error
	Attach(context.Context, string, string) (consoleapi.AttachResponse, error)
	Follow(context.Context, string, string, func(consoleapi.AttachResponse) error, func(openapi.JournalEventReadModel) error) error
	Dispatch(context.Context, openapi.CreateTaskRequest) (openapi.CreateTaskResponse, error)
	Steer(context.Context, string, openapi.CreateMessageRequest) (openapi.CreateMessageResponse, error)
	Cancel(context.Context, string, openapi.CancelTaskRequest) (openapi.CancelTaskResponse, error)
	DecideApproval(context.Context, string, openapi.DecideApprovalRequest) (openapi.DecideApprovalResponse, error)
	WorkerCommand(context.Context, string, int64, domain.WorkerCommandKind, string, bool) (openapi.WorkerCommandResponse, error)
}

type CredentialStore interface {
	Save(credentialstore.Credential) error
	Load(string, string, string) (credentialstore.Credential, error)
	LoadCurrentForSocket(string) (credentialstore.Credential, error)
	Delete(credentialstore.Credential) (bool, error)
}

func DefaultDependencies() Dependencies {
	return Dependencies{Out: os.Stdout, Err: os.Stderr, In: os.Stdin, Tmux: fleetmodel.ExecRunner{},
		IsInteractive: func() bool { return term.IsTerminal(int(os.Stdin.Fd())) },
		NewClient:     func(socketPath string) (Client, error) { return consoleclient.NewUnixClient(socketPath) },
		NewCredentialStore: func(path string) (CredentialStore, error) {
			return credentialstore.New(path, credentialstore.Options{})
		},
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
	if deps.NewCredentialStore == nil {
		deps.NewCredentialStore = defaults.NewCredentialStore
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
	flags.Var(&credentialsFlag, "credentials", localprofile.PathUsage(localprofile.CredentialsPath, "CLI credential file"))
	var username, agentID, taskID, approvalID, organizationID, content string
	var version int64
	var diagnostic, once, confirmForce bool
	username = ""
	organizationID = "default"
	switch command {
	case "login":
		username = "owner"
		flags.StringVar(&username, "username", username, "Web user name")
	case "logout":
	case "attach":
		flags.StringVar(&username, "username", username, "CLI user name (default: current selection)")
		flags.StringVar(&agentID, "agent", "", "Agent ID")
		flags.StringVar(&organizationID, "organization", organizationID, "Organization ID")
		flags.BoolVar(&diagnostic, "diagnostic", false, "Enable authorized diagnostic projection")
		flags.BoolVar(&once, "once", false, "Print current state without following events")
	case "dispatch":
		flags.StringVar(&username, "username", username, "CLI user name (default: current selection)")
		flags.StringVar(&agentID, "agent", "", "Agent ID")
		flags.StringVar(&organizationID, "organization", organizationID, "Organization ID")
		flags.StringVar(&content, "content", "", "Task content")
	case "steer":
		flags.StringVar(&username, "username", username, "CLI user name (default: current selection)")
		flags.StringVar(&taskID, "task", "", "Task ID")
		flags.StringVar(&content, "content", "", "Steering content")
		flags.Int64Var(&version, "version", 0, "Expected Task version")
	case "cancel":
		flags.StringVar(&username, "username", username, "CLI user name (default: current selection)")
		flags.StringVar(&taskID, "task", "", "Task ID")
		flags.Int64Var(&version, "version", 0, "Expected Task version")
	case "approve", "reject":
		flags.StringVar(&username, "username", username, "CLI user name (default: current selection)")
		flags.StringVar(&approvalID, "approval", "", "Approval request ID")
		flags.Int64Var(&version, "version", 0, "Expected approval version")
	case "down":
		flags.StringVar(&username, "username", username, "CLI user name (default: current selection)")
		flags.StringVar(&agentID, "agent", "", "Agent ID")
	case "force-stop":
		flags.StringVar(&username, "username", username, "CLI user name (default: current selection)")
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
	credentials, resolveErr := resolver.Resolve(localprofile.CredentialsPath, credentialsFlag.Override())
	if resolveErr != nil {
		fmt.Fprintf(deps.Err, "resolve Console credentials: %v\n", resolveErr)
		return 2
	}
	baseContext := deps.Context
	if baseContext == nil {
		baseContext = context.Background()
	}
	ctx, cancel := signal.NotifyContext(baseContext, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	store, err := deps.NewCredentialStore(credentials.Path)
	if err != nil {
		fmt.Fprintf(deps.Err, "open Console credential store: %v\n", err)
		return 1
	}
	if command == "login" {
		if !deps.IsInteractive() {
			fmt.Fprintln(deps.Err, "openagentx console login requires an interactive TTY")
			return 2
		}
		client, clientErr := deps.NewClient(socket.Path)
		if clientErr != nil {
			fmt.Fprintln(deps.Err, clientErr)
			return 1
		}
		password, passwordErr := deps.ReadPassword("Console password: ")
		if passwordErr != nil {
			fmt.Fprintf(deps.Err, "read Console password: %v\n", passwordErr)
			return 1
		}
		issued, loginErr := client.LoginCredential(ctx, strings.TrimSpace(username), password)
		if loginErr != nil {
			fmt.Fprintf(deps.Err, "Console login failed: %v\n", loginErr)
			return 1
		}
		credential := credentialstore.Credential{SocketPath: socket.Path, InstallationID: issued.InstallationID,
			Username: issued.Principal.Username, TokenID: issued.Principal.TokenID, Token: issued.Token,
			AbsoluteExpires: issued.AbsoluteExpiresAt}
		if err := store.Save(credential); err != nil {
			_ = client.Logout(ctx)
			fmt.Fprintf(deps.Err, "store Console credential: %v\n", err)
			return 1
		}
		fmt.Fprintf(deps.Out, "CLI session established for %s; expires %s\n", credential.Username, credential.AbsoluteExpires.UTC().Format(time.RFC3339))
		return 0
	}
	if command == "logout" {
		return executeLogout(ctx, socket.Path, store, deps)
	}
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
	client, err := deps.NewClient(socket.Path)
	if err != nil {
		fmt.Fprintln(deps.Err, err)
		return 1
	}
	probe, err := client.ProbeInstallation(ctx)
	if err != nil {
		fmt.Fprintf(deps.Err, "probe Console installation: %v\n", err)
		return 1
	}
	credential, err := store.Load(socket.Path, probe.InstallationID, strings.TrimSpace(username))
	if err != nil {
		fmt.Fprintln(deps.Err, "Console credential is unavailable; run openagentx console login")
		return 1
	}
	if err := client.UseCredential(ctx, credential.InstallationID, credential.Token); err != nil {
		fmt.Fprintf(deps.Err, "validate Console credential audience: %v\n", err)
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

func executeLogout(ctx context.Context, socketPath string, store CredentialStore, deps Dependencies) int {
	credential, err := store.LoadCurrentForSocket(socketPath)
	if errors.Is(err, credentialstore.ErrNotFound) {
		fmt.Fprintln(deps.Out, "No local CLI credential is stored for this socket")
		return 0
	}
	if err != nil {
		fmt.Fprintf(deps.Err, "load Console credential: %v\n", err)
		return 1
	}
	warning := ""
	client, clientErr := deps.NewClient(socketPath)
	if clientErr != nil {
		warning = "daemon unavailable; server-side token status is unknown"
	} else {
		probe, probeErr := client.ProbeInstallation(ctx)
		if probeErr != nil {
			warning = "daemon unavailable; server-side token status is unknown"
		} else if probe.InstallationID != credential.InstallationID {
			warning = "installation identity changed; token was not sent to the replacement daemon"
		} else if useErr := client.UseCredential(ctx, credential.InstallationID, credential.Token); useErr != nil {
			warning = "credential audience validation failed; token was not sent"
		} else if logoutErr := client.Logout(ctx); logoutErr != nil {
			warning = "server-side token was already invalid or could not be revoked"
		}
	}
	if _, deleteErr := store.Delete(credential); deleteErr != nil {
		fmt.Fprintf(deps.Err, "delete local Console credential: %v\n", deleteErr)
		return 1
	}
	if warning != "" {
		fmt.Fprintf(deps.Err, "warning: local CLI credential deleted; %s\n", warning)
	} else {
		fmt.Fprintln(deps.Out, "CLI session revoked and local credential deleted")
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
