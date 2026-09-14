package fleet

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"golang.org/x/term"
	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	webAuth "openagentx/internal/auth/web"
	admincli "openagentx/internal/cli/admin"
	consoleclient "openagentx/internal/client/console"
	"openagentx/internal/domain"
	fleetmodel "openagentx/internal/fleet"
	openagentsqlite "openagentx/internal/persistence/sqlite"
	workerconfig "openagentx/internal/worker"
)

type Dependencies struct {
	Out               io.Writer
	Err               io.Writer
	ReadPassword      func(string) (string, error)
	Tmux              fleetmodel.CommandRunner
	RunSystemctl      func(context.Context, ...string) (string, error)
	NewConsole        func(string) (consoleClient, error)
	Now               func() time.Time
	NewID             func(string) string
	Context           context.Context
	Wait              func(context.Context, time.Duration) error
	SystemdConfigPath func(string) string
}

type consoleClient interface {
	Login(context.Context, string, string) error
	Attach(context.Context, string, string) (consoleapi.AttachResponse, error)
	WorkerCommand(context.Context, string, int64, domain.WorkerCommandKind, string, bool) (openapi.WorkerCommandResponse, error)
}

func DefaultDependencies() Dependencies {
	return Dependencies{
		Out: os.Stdout, Err: os.Stderr, Tmux: fleetmodel.ExecRunner{}, Now: time.Now,
		NewID: func(prefix string) string { return prefix + "-" + uuid.NewString() },
		ReadPassword: func(prompt string) (string, error) {
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return "", fmt.Errorf("interactive terminal is required for password input")
			}
			fmt.Fprint(os.Stderr, prompt)
			value, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			return string(value), err
		},
		RunSystemctl: func(ctx context.Context, args ...string) (string, error) {
			output, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput()
			return strings.TrimSpace(string(output)), err
		},
		NewConsole: func(socketPath string) (consoleClient, error) { return consoleclient.NewUnixClient(socketPath) },
		Wait: func(ctx context.Context, duration time.Duration) error {
			timer := time.NewTimer(duration)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
		SystemdConfigPath: func(agentID string) string { return filepath.Join("/etc/openagentx/workers", agentID+".yaml") },
	}
}

type preparedAgent struct {
	entry      fleetmodel.Agent
	definition *admincli.AgentDefinition
	worker     *workerconfig.ProcessConfig
	workerPath string
}

func Execute(args []string, deps Dependencies) int {
	deps = withDefaults(deps)
	if len(args) == 0 {
		usage(deps.Err)
		return 2
	}
	flags := flag.NewFlagSet("fleet "+args[0], flag.ContinueOnError)
	flags.SetOutput(deps.Err)
	manifestPath := flags.String("file", "", "Fleet manifest")
	databasePath := flags.String("db", "", "OpenAgentX SQLite database")
	socketPath := flags.String("socket", "", "OpenAgentX Unix socket")
	ownerUsername := flags.String("owner-username", "owner", "Owner username")
	confirmForce := flags.Bool("confirm-force-stop", false, "Acknowledge force-stop risk")
	if err := flags.Parse(args[1:]); err != nil || *manifestPath == "" {
		usage(deps.Err)
		return 2
	}
	manifest, prepared, err := prepare(*manifestPath)
	if err != nil {
		fmt.Fprintf(deps.Err, "Fleet preflight failed: %v\n", err)
		return 1
	}
	baseContext := deps.Context
	if baseContext == nil {
		baseContext = context.Background()
	}
	ctx, cancel := signal.NotifyContext(baseContext, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	workspace := fleetmodel.Workspace{Runner: deps.Tmux}
	if args[0] == "init" || args[0] == "up" {
		consoleSocket, socketErr := resolveConsoleSocket(*socketPath, prepared)
		if socketErr != nil {
			fmt.Fprintf(deps.Err, "Fleet Console preflight failed: %v\n", socketErr)
			return 1
		}
		binary, executableErr := os.Executable()
		if executableErr != nil {
			fmt.Fprintf(deps.Err, "resolve OpenAgentX executable: %v\n", executableErr)
			return 1
		}
		workspace.ConsoleCommand = func(agentID string) []string {
			return []string{binary, "console", "attach", "--socket", consoleSocket, "--username", strings.TrimSpace(*ownerUsername), "--agent", agentID}
		}
		if preflightErr := workspace.Preflight(ctx, manifest); preflightErr != nil {
			fmt.Fprintf(deps.Err, "Fleet workspace preflight failed: %v\n", preflightErr)
			return 1
		}
	}
	switch args[0] {
	case "init":
		if *databasePath == "" {
			usage(deps.Err)
			return 2
		}
		if err := applyIdentities(ctx, *databasePath, *ownerUsername, prepared, deps); err != nil {
			fmt.Fprintf(deps.Err, "Fleet identity apply failed: %v\n", err)
			return 1
		}
		report, err := workspace.Reconcile(ctx, manifest)
		if err != nil {
			fmt.Fprintf(deps.Err, "Fleet workspace failed: %v\n", err)
			return 1
		}
		return printJSON(deps, report)
	case "up":
		if err := verifySystemdConfigs(prepared, deps.SystemdConfigPath); err != nil {
			fmt.Fprintf(deps.Err, "Fleet systemd config preflight failed: %v\n", err)
			return 1
		}
		if _, err := workspace.Reconcile(ctx, manifest); err != nil {
			fmt.Fprintf(deps.Err, "Fleet workspace failed: %v\n", err)
			return 1
		}
		for _, agent := range prepared {
			if !agent.entry.Enabled {
				continue
			}
			unit := "openagentx-worker@" + agent.entry.AgentID + ".service"
			if output, err := deps.RunSystemctl(ctx, "start", unit); err != nil {
				fmt.Fprintf(deps.Err, "start %s: %v %s\n", unit, err, output)
				return 1
			}
			fmt.Fprintf(deps.Out, "started %s\n", unit)
		}
		return 0
	case "status":
		return fleetStatus(ctx, manifest, prepared, deps)
	case "down", "force-stop":
		if *socketPath == "" {
			usage(deps.Err)
			return 2
		}
		force := args[0] == "force-stop"
		if force && !*confirmForce {
			fmt.Fprintln(deps.Err, "force-stop requires --confirm-force-stop")
			return 2
		}
		client, targets, err := queueStops(ctx, *socketPath, *ownerUsername, manifest, force, deps)
		if err != nil {
			fmt.Fprintf(deps.Err, "Fleet stop failed: %v\n", err)
			return 1
		}
		if !force {
			if err := waitForOffline(ctx, client, targets, deps); err != nil {
				if errors.Is(err, context.Canceled) {
					fmt.Fprintln(deps.Out, "Fleet down observation canceled; persisted graceful-stop intents remain active")
					return 0
				}
				fmt.Fprintf(deps.Err, "Fleet down observation failed: %v\n", err)
				return 1
			}
		}
		return 0
	default:
		usage(deps.Err)
		return 2
	}
}

func withDefaults(deps Dependencies) Dependencies {
	defaults := DefaultDependencies()
	if deps.Out == nil {
		deps.Out = defaults.Out
	}
	if deps.Err == nil {
		deps.Err = defaults.Err
	}
	if deps.ReadPassword == nil {
		deps.ReadPassword = defaults.ReadPassword
	}
	if deps.Tmux == nil {
		deps.Tmux = defaults.Tmux
	}
	if deps.RunSystemctl == nil {
		deps.RunSystemctl = defaults.RunSystemctl
	}
	if deps.NewConsole == nil {
		deps.NewConsole = defaults.NewConsole
	}
	if deps.Now == nil {
		deps.Now = defaults.Now
	}
	if deps.NewID == nil {
		deps.NewID = defaults.NewID
	}
	if deps.Wait == nil {
		deps.Wait = defaults.Wait
	}
	if deps.SystemdConfigPath == nil {
		deps.SystemdConfigPath = defaults.SystemdConfigPath
	}
	return deps
}

func verifySystemdConfigs(prepared []preparedAgent, canonicalPath func(string) string) error {
	if canonicalPath == nil {
		return fmt.Errorf("systemd config path resolver is required")
	}
	for _, agent := range prepared {
		if !agent.entry.Enabled {
			continue
		}
		canonical := filepath.Clean(strings.TrimSpace(canonicalPath(agent.entry.AgentID)))
		if !filepath.IsAbs(canonical) {
			return fmt.Errorf("Agent %q canonical systemd config must be an absolute path", agent.entry.AgentID)
		}
		validatedInfo, err := os.Stat(agent.workerPath)
		if err != nil {
			return fmt.Errorf("stat validated config for Agent %q: %w", agent.entry.AgentID, err)
		}
		canonicalInfo, err := os.Stat(canonical)
		if err != nil {
			return fmt.Errorf("stat canonical systemd config %q for Agent %q: %w", canonical, agent.entry.AgentID, err)
		}
		if os.SameFile(validatedInfo, canonicalInfo) {
			continue
		}
		validated, err := os.ReadFile(agent.workerPath)
		if err != nil {
			return fmt.Errorf("read validated config for Agent %q: %w", agent.entry.AgentID, err)
		}
		deployed, err := os.ReadFile(canonical)
		if err != nil {
			return fmt.Errorf("read canonical systemd config for Agent %q: %w", agent.entry.AgentID, err)
		}
		if sha256.Sum256(validated) != sha256.Sum256(deployed) {
			return fmt.Errorf("Agent %q worker_config %q does not match canonical systemd config %q", agent.entry.AgentID, agent.workerPath, canonical)
		}
	}
	return nil
}

func prepare(path string) (fleetmodel.Manifest, []preparedAgent, error) {
	manifest, err := fleetmodel.LoadFile(path)
	if err != nil {
		return fleetmodel.Manifest{}, nil, err
	}
	base, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return fleetmodel.Manifest{}, nil, err
	}
	prepared := make([]preparedAgent, 0, len(manifest.Agents))
	for _, entry := range manifest.Agents {
		identityPath := resolve(base, entry.IdentityFile)
		workerPath := resolve(base, entry.WorkerConfig)
		definition, err := admincli.LoadAgentDefinition(identityPath)
		if err != nil {
			return fleetmodel.Manifest{}, nil, fmt.Errorf("Agent %q identity: %w", entry.AgentID, err)
		}
		if definition.AgentID != entry.AgentID {
			return fleetmodel.Manifest{}, nil, fmt.Errorf("Agent %q identity file declares %q", entry.AgentID, definition.AgentID)
		}
		worker, err := workerconfig.LoadProcessConfig(workerPath)
		if err != nil {
			return fleetmodel.Manifest{}, nil, fmt.Errorf("Agent %q worker config: %w", entry.AgentID, err)
		}
		if worker.AgentID != entry.AgentID {
			return fleetmodel.Manifest{}, nil, fmt.Errorf("Agent %q worker config declares %q", entry.AgentID, worker.AgentID)
		}
		prepared = append(prepared, preparedAgent{entry: entry, definition: definition, worker: worker, workerPath: workerPath})
	}
	return manifest, prepared, nil
}

func resolveConsoleSocket(explicit string, prepared []preparedAgent) (string, error) {
	if value := strings.TrimSpace(explicit); value != "" {
		return value, nil
	}
	var socket string
	for _, item := range prepared {
		if item.worker.Transport != domain.WorkerTransportUnix || strings.TrimSpace(item.worker.UnixSocket) == "" {
			return "", fmt.Errorf("--socket is required when Fleet contains a non-Unix Worker")
		}
		if socket == "" {
			socket = item.worker.UnixSocket
			continue
		}
		if socket != item.worker.UnixSocket {
			return "", fmt.Errorf("--socket is required when Worker configs use different Unix sockets")
		}
	}
	if socket == "" {
		return "", fmt.Errorf("Console socket could not be resolved")
	}
	return socket, nil
}

func resolve(base, value string) string {
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(base, filepath.Clean(value))
}

func authenticateOwner(repository *openagentsqlite.Repository, username string, deps Dependencies) (*domain.WebUserRecord, error) {
	owner, err := repository.GetWebUserByUsername(context.Background(), strings.TrimSpace(username))
	if err != nil || owner.Status != domain.IdentityActive || !owner.HasRole(domain.WebRoleOwner) {
		return nil, fmt.Errorf("active owner is required")
	}
	password, err := deps.ReadPassword("Owner password: ")
	if err != nil {
		return nil, err
	}
	if !webAuth.VerifyPassword(owner.PasswordDigest, password) {
		return nil, fmt.Errorf("owner authentication failed")
	}
	return owner, nil
}

func applyIdentities(ctx context.Context, databasePath, username string, prepared []preparedAgent, deps Dependencies) error {
	repository, err := openagentsqlite.Open(ctx, databasePath, openagentsqlite.Options{Now: deps.Now})
	if err != nil {
		return err
	}
	defer repository.Close()
	owner, err := authenticateOwner(repository, username, deps)
	if err != nil {
		return err
	}
	for _, item := range prepared {
		definition := item.definition
		principal := &domain.Principal{ID: definition.PrincipalID, Kind: domain.PrincipalAgent, DisplayName: definition.DisplayName, Status: domain.IdentityActive}
		agent := &domain.AgentIdentity{ID: definition.AgentID, PrincipalID: definition.PrincipalID, OrganizationID: definition.OrganizationID, DisplayName: definition.DisplayName, Status: domain.AgentIdentityActive, Version: 1}
		profile := &domain.AgentProfileRecord{AgentID: definition.AgentID, Version: 1, InstructionsPath: definition.Profile.InstructionsPath, WorkspaceRoot: definition.Profile.WorkspaceRoot, DefaultExecutionProfileID: definition.Profile.DefaultExecutionProfileID, Capabilities: definition.Profile.Capabilities}
		events := []*domain.JournalEvent{
			fleetEvent(deps, owner.PrincipalID, definition.OrganizationID, "principal", principal.ID, "principal.created"),
			fleetEvent(deps, owner.PrincipalID, definition.OrganizationID, "agent", agent.ID, "agent.created"),
		}
		if _, err := repository.ApplyAgent(ctx, principal, agent, profile, events); err != nil {
			return fmt.Errorf("apply Agent %q: %w", agent.ID, err)
		}
	}
	return nil
}

type stopTarget struct {
	AgentID  string
	QueuedAt time.Time
}

type stopObservation struct {
	AgentID      string                  `json:"agent_id"`
	Generation   int64                   `json:"generation,omitempty"`
	WorkerStatus domain.WorkerStatus     `json:"worker_status,omitempty"`
	RunAttempt   *consoleapi.RunSnapshot `json:"run_attempt,omitempty"`
	Elapsed      string                  `json:"elapsed"`
	RecentStatus time.Time               `json:"recent_status,omitempty"`
	NoNewTasks   bool                    `json:"no_new_tasks"`
	Message      string                  `json:"message"`
}

func queueStops(ctx context.Context, socketPath, username string, manifest fleetmodel.Manifest, force bool, deps Dependencies) (consoleClient, []stopTarget, error) {
	password, err := deps.ReadPassword("Owner password: ")
	if err != nil {
		return nil, nil, err
	}
	client, err := deps.NewConsole(socketPath)
	if err != nil {
		return nil, nil, err
	}
	if err := client.Login(ctx, strings.TrimSpace(username), password); err != nil {
		return nil, nil, err
	}
	kind := domain.WorkerCommandStop
	if force {
		kind = domain.WorkerCommandForceStop
	}
	attachedAgents := make([]consoleapi.AttachResponse, 0, len(manifest.Agents))
	for _, entry := range manifest.Agents {
		attached, err := client.Attach(ctx, entry.AgentID, consoleapi.ModeNormal)
		if err != nil {
			return nil, nil, fmt.Errorf("attach Agent %q: %w", entry.AgentID, err)
		}
		if attached.AgentID != entry.AgentID {
			return nil, nil, fmt.Errorf("attach Agent %q returned identity %q", entry.AgentID, attached.AgentID)
		}
		if effectivelyOffline(attached, deps.Now().UTC()) {
			fmt.Fprintf(deps.Out, "%s already offline\n", entry.AgentID)
			continue
		}
		attachedAgents = append(attachedAgents, attached)
	}
	targets := make([]stopTarget, 0, len(attachedAgents))
	for _, attached := range attachedAgents {
		key := fmt.Sprintf("fleet-%s-%s-g%d", kind, attached.AgentID, attached.Generation)
		response, err := client.WorkerCommand(ctx, attached.WorkerInstanceID, attached.Generation, kind, key, force)
		if err != nil {
			return nil, nil, fmt.Errorf("queue %s for %q: %w", kind, attached.AgentID, err)
		}
		queuedAt := deps.Now().UTC()
		targets = append(targets, stopTarget{AgentID: attached.AgentID, QueuedAt: queuedAt})
		fmt.Fprintf(deps.Out, "%s queued for %s generation %d (%s); intent is persistent\n", kind, attached.AgentID, attached.Generation, response.Command.State)
	}
	return client, targets, nil
}

func waitForOffline(ctx context.Context, client consoleClient, targets []stopTarget, deps Dependencies) error {
	pending := make(map[string]stopTarget, len(targets))
	for _, target := range targets {
		pending[target.AgentID] = target
	}
	for len(pending) > 0 {
		now := deps.Now().UTC()
		for agentID, target := range pending {
			attached, err := client.Attach(ctx, agentID, consoleapi.ModeNormal)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if code := printJSON(deps, stopObservation{AgentID: agentID,
					Elapsed: elapsedString(now.Sub(target.QueuedAt)), Message: "observation temporarily unavailable; persisted graceful-stop intent remains active"}); code != 0 {
					return fmt.Errorf("write graceful-stop observation failure")
				}
				continue
			}
			if effectivelyOffline(attached, now) {
				if code := printJSON(deps, stopObservation{AgentID: agentID, WorkerStatus: domain.WorkerStatusOffline,
					Elapsed: elapsedString(now.Sub(target.QueuedAt)), Message: "offline; graceful stop completed"}); code != 0 {
					return fmt.Errorf("write graceful-stop completion")
				}
				delete(pending, agentID)
				continue
			}
			recent := attached.LastHeartbeatAt
			if attached.ActiveRun != nil && attached.ActiveRun.UpdatedAt.After(recent) {
				recent = attached.ActiveRun.UpdatedAt
			}
			draining := attached.WorkerStatus == domain.WorkerStatusDraining
			message := "graceful stop pending; waiting for Worker to enter draining"
			if draining {
				message = "draining; current RunAttempt may finish naturally; Worker will not claim new tasks"
			}
			if code := printJSON(deps, stopObservation{AgentID: agentID, Generation: attached.Generation, WorkerStatus: attached.WorkerStatus,
				RunAttempt: attached.ActiveRun, Elapsed: elapsedString(now.Sub(target.QueuedAt)), RecentStatus: recent,
				NoNewTasks: draining, Message: message}); code != 0 {
				return fmt.Errorf("write graceful-stop observation")
			}
		}
		if len(pending) == 0 {
			return nil
		}
		if err := deps.Wait(ctx, time.Second); err != nil {
			return err
		}
	}
	return nil
}

func effectivelyOffline(attached consoleapi.AttachResponse, now time.Time) bool {
	return attached.WorkerInstanceID == "" || attached.WorkerStatus == domain.WorkerStatusOffline ||
		(!attached.LeaseUntil.IsZero() && !attached.LeaseUntil.After(now))
}

func elapsedString(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	return duration.Round(time.Second).String()
}

func fleetStatus(ctx context.Context, manifest fleetmodel.Manifest, prepared []preparedAgent, deps Dependencies) int {
	windows, workspaceErr := (fleetmodel.Workspace{Runner: deps.Tmux}).Inspect(ctx)
	status := map[string]any{"session": fleetmodel.SessionName, "windows": windows, "foreground_takeover": "Foreground Takeover（规划中，暂不可用）"}
	if workspaceErr != nil {
		status["workspace_status"] = "missing_or_unavailable"
	} else {
		status["workspace_status"] = "available"
	}
	units := make(map[string]string, len(prepared))
	for _, agent := range prepared {
		unit := "openagentx-worker@" + agent.entry.AgentID + ".service"
		output, err := deps.RunSystemctl(ctx, "is-active", unit)
		if err != nil && output == "" {
			output = "unknown"
		}
		units[agent.entry.AgentID] = output
	}
	status["workers"] = units
	status["fleet_agents"] = manifest.Agents
	return printJSON(deps, status)
}

func fleetEvent(deps Dependencies, actor, organization, aggregate, aggregateID, eventType string) *domain.JournalEvent {
	payload, _ := json.Marshal(map[string]string{"source": "fleet"})
	return &domain.JournalEvent{ID: deps.NewID("event"), OrganizationID: organization, AggregateType: aggregate, AggregateID: aggregateID, EventType: eventType, ActorPrincipalID: actor, Payload: payload, CreatedAt: deps.Now().UTC()}
}

func printJSON(deps Dependencies, value any) int {
	encoder := json.NewEncoder(deps.Out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintln(deps.Err, err)
		return 1
	}
	return 0
}

func usage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage: openagentx fleet <init|up|status|down|force-stop> --file <fleet.yaml> [--db <path>] [--socket <path>] [--confirm-force-stop]")
}
