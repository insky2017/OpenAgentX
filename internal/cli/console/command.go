package console

import (
	"bufio"
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

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"
	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	consoleclient "openagentx/internal/client/console"
	"openagentx/internal/credentialstore"
	"openagentx/internal/domain"
	fleetmodel "openagentx/internal/fleet"
	"openagentx/internal/localprofile"
)

type ProgramRunner func(context.Context, tea.Model, io.Reader, io.Writer) (tea.Model, error)

type Dependencies struct {
	Out                io.Writer
	Err                io.Writer
	In                 io.Reader
	ReadUsername       func(string) (string, error)
	ReadPassword       func(string) (string, error)
	IsInteractive      func() bool
	Now                func() time.Time
	NewClient          func(string) (Client, error)
	NewCredentialStore func(string) (CredentialStore, error)
	RunProgram         ProgramRunner
	Tmux               fleetmodel.CommandRunner
	Context            context.Context
}

type Client interface {
	ProbeInstallation(context.Context) (openapi.CLIInstallationResponse, error)
	LoginCredential(context.Context, string, string) (openapi.CLILoginResponse, error)
	UseCredential(context.Context, string, string) error
	Session(context.Context) (openapi.CLISessionResponse, error)
	Logout(context.Context) error
	ListAgentOptions(context.Context) ([]domain.ConsoleAgentOption, error)
	ListTaskOptions(context.Context, string) ([]openapi.ConsoleTaskOption, error)
	TaskSnapshot(context.Context, string, string) (openapi.ConsoleTaskSnapshot, error)
	Attach(context.Context, string, string) (consoleapi.AttachResponse, error)
	Follow(context.Context, string, string, func(consoleapi.AttachResponse) error,
		func(openapi.JournalEventReadModel) error, func(consoleclient.FollowState) error) error
	Dispatch(context.Context, openapi.CreateTaskRequest) (openapi.CreateTaskResponse, error)
	Steer(context.Context, string, openapi.CreateMessageRequest) (openapi.CreateMessageResponse, error)
	Cancel(context.Context, string, openapi.CancelTaskRequest) (openapi.CancelTaskResponse, error)
	DecideApproval(context.Context, string, openapi.DecideApprovalRequest) (openapi.DecideApprovalResponse, error)
}

type CredentialStore interface {
	Save(credentialstore.Credential) error
	Replace(func() (credentialstore.Credential, error)) (credentialstore.Credential, error)
	Load(string, string, string) (credentialstore.Credential, error)
	LoadCurrentForSocket(string) (credentialstore.Credential, error)
	Delete(credentialstore.Credential) (bool, error)
}

func DefaultDependencies() Dependencies {
	return Dependencies{
		Out: os.Stdout, Err: os.Stderr, In: os.Stdin,
		Tmux: fleetmodel.ExecRunner{CurrentTarget: os.Getenv("TMUX_PANE")}, Now: time.Now,
		IsInteractive: func() bool { return term.IsTerminal(int(os.Stdin.Fd())) },
		NewClient:     func(socketPath string) (Client, error) { return consoleclient.NewUnixClient(socketPath) },
		NewCredentialStore: func(path string) (CredentialStore, error) {
			return credentialstore.New(path, credentialstore.Options{})
		},
		ReadUsername: func(prompt string) (string, error) {
			fmt.Fprint(os.Stderr, prompt)
			value, err := bufio.NewReader(os.Stdin).ReadString('\n')
			return strings.TrimSpace(value), err
		},
		ReadPassword: func(prompt string) (string, error) {
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return "", fmt.Errorf("interactive terminal is required for password input")
			}
			fmt.Fprint(os.Stderr, prompt)
			value, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			return string(value), err
		},
		RunProgram: func(ctx context.Context, model tea.Model, input io.Reader, output io.Writer) (tea.Model, error) {
			return tea.NewProgram(model, tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(output), tea.WithAltScreen()).Run()
		},
	}
}

type invocation struct {
	command     string
	socket      localprofile.PathFlag
	credentials localprofile.PathFlag
	agentID     string
	diagnostic  bool
}

func Execute(args []string, deps Dependencies) int {
	deps = completeDependencies(deps)
	if len(args) == 1 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
		usage(deps.Out)
		return 0
	}
	invocation, err := parseInvocation(args, deps.Err)
	if errors.Is(err, flag.ErrHelp) {
		usage(deps.Out)
		return 0
	}
	if err != nil {
		usage(deps.Err)
		return 2
	}
	if invocation.command != "logout" && !deps.IsInteractive() {
		fmt.Fprintf(deps.Err, "openagentx console %s requires an interactive TTY; automation must use the Observe API\n", displayCommand(invocation.command))
		return 2
	}

	resolver := localprofile.DefaultResolver()
	socket, err := resolver.Resolve(localprofile.SocketPath, invocation.socket.Override())
	if err != nil {
		fmt.Fprintf(deps.Err, "resolve Console socket: %v\n", err)
		return 2
	}
	credentials, err := resolver.Resolve(localprofile.CredentialsPath, invocation.credentials.Override())
	if err != nil {
		fmt.Fprintf(deps.Err, "resolve Console credentials: %v\n", err)
		return 2
	}
	store, err := deps.NewCredentialStore(credentials.Path)
	if err != nil {
		fmt.Fprintf(deps.Err, "open Console credential store: %v\n", err)
		return 1
	}
	baseContext := deps.Context
	if baseContext == nil {
		baseContext = context.Background()
	}
	ctx, cancel := signal.NotifyContext(baseContext, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	application := newConsoleApplication(ctx, socket.Path, store, deps)

	switch invocation.command {
	case "login":
		username, readErr := deps.ReadUsername("Console username: ")
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			fmt.Fprintf(deps.Err, "read Console username: %v\n", readErr)
			return 1
		}
		password, readErr := deps.ReadPassword("Console password: ")
		if readErr != nil {
			fmt.Fprintf(deps.Err, "read Console password: %v\n", readErr)
			return 1
		}
		status, loginErr := application.login(strings.TrimSpace(username), password)
		password = ""
		if loginErr != nil {
			fmt.Fprintf(deps.Err, "Console login failed: %s\n", safeErrorSummary(loginErr))
			return 1
		}
		fmt.Fprintf(deps.Out, "CLI session established for %s; expires %s\n", status.Username, status.ExpiresAt.UTC().Format(time.RFC3339))
		return 0
	case "logout":
		message, logoutErr := application.logout()
		if logoutErr != nil {
			fmt.Fprintf(deps.Err, "Console logout failed: %s\n", safeErrorSummary(logoutErr))
			return 1
		}
		if strings.Contains(message, "unknown") || strings.Contains(message, "not sent") || strings.Contains(message, "invalid") {
			fmt.Fprintln(deps.Err, "warning: "+message)
		} else {
			fmt.Fprintln(deps.Out, message)
		}
		return 0
	}

	var direct *initialAttach
	if invocation.command == "attach" {
		mode := consoleapi.ModeNormal
		if invocation.diagnostic {
			mode = consoleapi.ModeDiagnostic
		}
		direct = &initialAttach{AgentID: invocation.agentID, Mode: mode}
	}
	model := newTUIModel(application, deps.Now, direct)
	finalModel, err := deps.RunProgram(ctx, model, deps.In, deps.Out)
	if err != nil {
		if errors.Is(err, tea.ErrInterrupted) || errors.Is(err, context.Canceled) {
			return 0
		}
		fmt.Fprintf(deps.Err, "Console TUI failed: %s\n", safeErrorSummary(err))
		return 1
	}
	if final, ok := finalModel.(tuiModel); ok && final.fatal {
		fmt.Fprintln(deps.Err, boundedSafeText(final.notice, 1024))
		return 1
	}
	return 0
}

func completeDependencies(deps Dependencies) Dependencies {
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
	if deps.ReadUsername == nil {
		deps.ReadUsername = defaults.ReadUsername
	}
	if deps.ReadPassword == nil {
		deps.ReadPassword = defaults.ReadPassword
	}
	if deps.IsInteractive == nil {
		deps.IsInteractive = defaults.IsInteractive
	}
	if deps.Now == nil {
		deps.Now = defaults.Now
	}
	if deps.NewClient == nil {
		deps.NewClient = defaults.NewClient
	}
	if deps.NewCredentialStore == nil {
		deps.NewCredentialStore = defaults.NewCredentialStore
	}
	if deps.RunProgram == nil {
		deps.RunProgram = defaults.RunProgram
	}
	if deps.Tmux == nil {
		deps.Tmux = defaults.Tmux
	}
	return deps
}

func parseInvocation(args []string, output io.Writer) (invocation, error) {
	result := invocation{command: "menu"}
	remaining := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		result.command = args[0]
		remaining = args[1:]
	}
	if result.command != "menu" && result.command != "login" && result.command != "logout" && result.command != "attach" {
		return invocation{}, fmt.Errorf("unknown Console command")
	}
	flags := flag.NewFlagSet("console "+result.command, flag.ContinueOnError)
	flags.SetOutput(output)
	flags.Var(&result.socket, "socket", localprofile.PathUsage(localprofile.SocketPath, "OpenAgentX Unix socket"))
	flags.Var(&result.credentials, "credentials", localprofile.PathUsage(localprofile.CredentialsPath, "CLI credential file"))
	if result.command == "attach" {
		flags.StringVar(&result.agentID, "agent", "", "Agent ID")
		flags.BoolVar(&result.diagnostic, "diagnostic", false, "Enable authorized diagnostic projection")
	}
	if err := flags.Parse(remaining); err != nil {
		return invocation{}, err
	}
	if flags.NArg() != 0 {
		return invocation{}, fmt.Errorf("unexpected Console arguments")
	}
	return result, nil
}

func displayCommand(command string) string {
	if command == "menu" {
		return "menu"
	}
	return command
}

func validateCredentialSession(credential credentialstore.Credential, session openapi.CLISessionResponse, now time.Time) error {
	if session.InstallationID != credential.InstallationID || session.Principal.TokenID != credential.TokenID ||
		session.Principal.Username != credential.Username || !session.AbsoluteExpiresAt.Equal(credential.AbsoluteExpires) {
		return fmt.Errorf("server session does not match the stored credential")
	}
	if !now.UTC().Before(session.AbsoluteExpiresAt.UTC()) {
		return fmt.Errorf("server session is expired")
	}
	return nil
}

func usage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage: openagentx console [--socket <path>] [--credentials <path>]")
	fmt.Fprintln(writer, "       openagentx console login [--socket <path>] [--credentials <path>]")
	fmt.Fprintln(writer, "       openagentx console logout [--socket <path>] [--credentials <path>]")
	fmt.Fprintln(writer, "       openagentx console attach [--socket <path>] [--credentials <path>] [--agent <agent-id>] [--diagnostic]")
	fmt.Fprintln(writer, "Attach requires an interactive TTY in pane 0 of the exact OAX session; automation must use the Observe API")
	fmt.Fprintf(writer, "Default socket source: $%s > $%s > ~/.openagentx/run/openagentx.sock\n", localprofile.EnvSocketPath, localprofile.EnvHome)
	fmt.Fprintf(writer, "Default credential source: $%s > $%s > ~/.openagentx/credentials.json\n", localprofile.EnvCredentialsPath, localprofile.EnvHome)
}
