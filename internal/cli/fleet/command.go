package fleet

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	admincli "openagentx/internal/cli/admin"
	consoleclient "openagentx/internal/client/console"
	"openagentx/internal/credentialstore"
	"openagentx/internal/domain"
	fleetmodel "openagentx/internal/fleet"
	"openagentx/internal/localprofile"
	workerconfig "openagentx/internal/worker"
)

const maxWorkerConfigBytes = 1 << 20

type Dependencies struct {
	Out                io.Writer
	Err                io.Writer
	In                 io.Reader
	IsInteractive      func() bool
	SelectAgents       func([]domain.ConsoleAgentOption) ([]string, error)
	Tmux               fleetmodel.CommandRunner
	RunSystemctl       func(context.Context, ...string) (string, error)
	RunLoginctl        func(context.Context, ...string) (string, error)
	NewConsole         func(string) (consoleClient, error)
	NewCredentialStore func(string) (credentialStore, error)
	Now                func() time.Time
	Context            context.Context
	Wait               func(context.Context, time.Duration) error
	UserHomeDir        func() (string, error)
	BeforeAtomicRename func(string) error
}

type consoleClient interface {
	ProbeInstallation(context.Context) (openapi.CLIInstallationResponse, error)
	UseCredential(context.Context, string, string) error
	Session(context.Context) (openapi.CLISessionResponse, error)
	ListAgentOptions(context.Context) ([]domain.ConsoleAgentOption, error)
	Attach(context.Context, string, string) (consoleapi.AttachResponse, error)
	WorkerCommand(context.Context, string, int64, domain.WorkerCommandKind, string, bool) (openapi.WorkerCommandResponse, error)
}

type credentialStore interface {
	Load(string, string, string) (credentialstore.Credential, error)
	Delete(credentialstore.Credential) (bool, error)
}

type stringListFlag []string

func (f *stringListFlag) String() string { return strings.Join(*f, ",") }
func (f *stringListFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

type preparedAgent struct {
	entry      fleetmodel.Agent
	workerPath string
}

type fleetPaths struct {
	manifest    string
	database    string
	socket      string
	workerDir   string
	credentials string
}

func DefaultDependencies() Dependencies {
	run := func(ctx context.Context, name string, args ...string) (string, error) {
		output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		return strings.TrimSpace(string(output)), err
	}
	return Dependencies{
		Out: os.Stdout, Err: os.Stderr, In: os.Stdin,
		IsInteractive: func() bool { return term.IsTerminal(int(os.Stdin.Fd())) },
		Tmux:          fleetmodel.ExecRunner{}, Now: time.Now, UserHomeDir: os.UserHomeDir,
		RunSystemctl: func(ctx context.Context, args ...string) (string, error) { return run(ctx, "systemctl", args...) },
		RunLoginctl:  func(ctx context.Context, args ...string) (string, error) { return run(ctx, "loginctl", args...) },
		NewConsole:   func(socketPath string) (consoleClient, error) { return consoleclient.NewUnixClient(socketPath) },
		NewCredentialStore: func(path string) (credentialStore, error) {
			return credentialstore.New(path, credentialstore.Options{})
		},
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
	}
}

func Execute(args []string, deps Dependencies) int {
	deps = withDefaults(deps)
	if len(args) == 0 {
		usage(deps.Err)
		return 2
	}
	command := args[0]
	if command == "help" || command == "--help" || command == "-h" {
		usage(deps.Out)
		return 0
	}
	switch command {
	case "init", "workspace", "up", "status", "down", "force-stop":
	default:
		usage(deps.Err)
		return 2
	}

	flags := flag.NewFlagSet("fleet "+command, flag.ContinueOnError)
	flags.SetOutput(deps.Err)
	var manifestFlag, databaseFlag, socketFlag, workerDirFlag, credentialsFlag localprofile.PathFlag
	var agents, workerSources stringListFlag
	flags.Var(&manifestFlag, "file", localprofile.PathUsage(localprofile.FleetManifest, "Fleet manifest"))
	flags.Var(&databaseFlag, "db", localprofile.PathUsage(localprofile.DatabasePath, "OpenAgentX SQLite database"))
	flags.Var(&socketFlag, "socket", localprofile.PathUsage(localprofile.SocketPath, "OpenAgentX Unix socket"))
	flags.Var(&workerDirFlag, "worker-dir", localprofile.PathUsage(localprofile.WorkerConfigDir, "Worker config directory"))
	flags.Var(&credentialsFlag, "credentials", localprofile.PathUsage(localprofile.CredentialsPath, "CLI credential file"))
	flags.Var(&agents, "agent", "Agent ID to include when initializing a missing manifest (repeatable)")
	flags.Var(&workerSources, "worker-config", "agent-id=/absolute/source.yaml to import atomically during init (repeatable)")
	respawnDead := flags.Bool("respawn-dead", false, "Respawn only compatible managed dead pane 0 consoles")
	confirmForce := flags.Bool("confirm-force-stop", false, "Acknowledge that force-stop is destructive")
	confirmUncertain := flags.Bool("confirm-active-run-uncertain", false, "Acknowledge active RunAttempt may become uncertain")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		usage(deps.Err)
		return 2
	}
	if flags.NArg() != 0 || (command != "init" && (len(agents) != 0 || len(workerSources) != 0)) {
		usage(deps.Err)
		return 2
	}
	if command == "force-stop" && (!*confirmForce || !*confirmUncertain) {
		fmt.Fprintln(deps.Err, "force-stop requires both --confirm-force-stop and --confirm-active-run-uncertain")
		return 2
	}

	paths, err := resolveFleetPaths(manifestFlag, databaseFlag, socketFlag, workerDirFlag, credentialsFlag)
	if err != nil {
		fmt.Fprintf(deps.Err, "resolve Fleet paths: %v\n", err)
		return 2
	}
	baseContext := deps.Context
	if baseContext == nil {
		baseContext = context.Background()
	}
	ctx, cancel := signal.NotifyContext(baseContext, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var workspace fleetmodel.Workspace
	if command == "init" || command == "workspace" || command == "up" {
		workspace, err = newWorkspace(paths, *respawnDead, deps)
		if err != nil {
			fmt.Fprintf(deps.Err, "Fleet workspace preflight failed: %v\n", err)
			return 1
		}
	}

	requiredRole, requiredScope := domain.WebRoleOwner, domain.CLIScopeFleetLifecycle
	if command == "status" || command == "workspace" {
		requiredRole, requiredScope = domain.WebRoleViewer, domain.CLIScopeConsoleRead
	}
	client, session, err := authenticateFleetClient(ctx, paths, requiredRole, requiredScope, deps)
	if err != nil {
		fmt.Fprintf(deps.Err, "Fleet CLI session failed: %v; run openagentx console login\n", safeAuthError(err))
		return 1
	}
	options, err := client.ListAgentOptions(ctx)
	if err != nil {
		fmt.Fprintf(deps.Err, "Fleet Agent list failed: %v\n", safeAuthError(err))
		return 1
	}
	optionMap, err := validateAgentOptions(options)
	if err != nil {
		fmt.Fprintf(deps.Err, "Fleet Agent list failed: %v\n", err)
		return 1
	}

	manifestMissing := false
	if _, err := os.Lstat(paths.manifest); os.IsNotExist(err) {
		manifestMissing = true
	} else if err != nil {
		fmt.Fprintf(deps.Err, "Fleet manifest preflight failed: %v\n", err)
		return 1
	}
	if manifestMissing && command != "init" {
		fmt.Fprintf(deps.Err, "Fleet manifest is missing at %s; run fleet init with explicit Agent selection first\n", paths.manifest)
		return 1
	}
	if manifestMissing {
		if _, err := initializeManifest(paths, agents, workerSources, options, deps); err != nil {
			fmt.Fprintf(deps.Err, "Fleet init failed: %v\n", err)
			return 1
		}
	} else if command == "init" && (len(agents) != 0 || len(workerSources) != 0) {
		if _, err := initializeManifest(paths, agents, workerSources, options, deps); err != nil {
			fmt.Fprintf(deps.Err, "Fleet init conflict: %v\n", err)
			return 1
		}
	}

	manifest, prepared, err := prepare(paths.manifest, paths.workerDir, paths.socket, optionMap)
	if err != nil {
		fmt.Fprintf(deps.Err, "Fleet preflight failed: %v\n", err)
		return 1
	}
	switch command {
	case "init", "workspace":
		if err := workspace.Preflight(ctx, manifest); err != nil {
			fmt.Fprintf(deps.Err, "Fleet workspace preflight failed: %v\n", err)
			return 1
		}
		report, err := workspace.Reconcile(ctx, manifest)
		if err != nil {
			fmt.Fprintf(deps.Err, "Fleet workspace failed: %v\n", err)
			return 1
		}
		return printJSON(deps, map[string]any{"cli_username": session.Principal.Username, "workspace": report})
	case "up":
		if err := workspace.Preflight(ctx, manifest); err != nil {
			fmt.Fprintf(deps.Err, "Fleet workspace preflight failed: %v\n", err)
			return 1
		}
		if err := verifyUserUnits(ctx, prepared, deps); err != nil {
			fmt.Fprintf(deps.Err, "Fleet user-systemd preflight failed: %v\n", err)
			return 1
		}
		if _, err := workspace.Reconcile(ctx, manifest); err != nil {
			fmt.Fprintf(deps.Err, "Fleet workspace failed: %v\n", err)
			return 1
		}
		checkLinger(ctx, deps)
		for _, agent := range prepared {
			if !agent.entry.Enabled {
				continue
			}
			unit := workerUnit(agent.entry.AgentID)
			if output, err := deps.RunSystemctl(ctx, "--user", "start", unit); err != nil {
				fmt.Fprintf(deps.Err, "start %s: %v %s\n", unit, err, bounded(output, 512))
				return 1
			}
			fmt.Fprintf(deps.Out, "started user unit %s\n", unit)
		}
		return 0
	case "status":
		return fleetStatus(ctx, manifest, prepared, options, deps)
	case "down", "force-stop":
		force := command == "force-stop"
		client, targets, err := queueStops(ctx, client, manifest, force, deps)
		if err != nil {
			fmt.Fprintf(deps.Err, "Fleet stop failed: %v\n", safeAuthError(err))
			return 1
		}
		if !force {
			if err := waitForOffline(ctx, client, targets, deps); err != nil {
				if errors.Is(err, context.Canceled) {
					fmt.Fprintln(deps.Out, "Fleet down observation canceled; persisted graceful-stop intents remain active")
					return 0
				}
				fmt.Fprintf(deps.Err, "Fleet down observation failed: %v\n", safeAuthError(err))
				return 1
			}
		}
		return 0
	default:
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
	if deps.In == nil {
		deps.In = defaults.In
	}
	if deps.IsInteractive == nil {
		deps.IsInteractive = defaults.IsInteractive
	}
	if deps.Tmux == nil {
		deps.Tmux = defaults.Tmux
	}
	if deps.RunSystemctl == nil {
		deps.RunSystemctl = defaults.RunSystemctl
	}
	if deps.RunLoginctl == nil {
		deps.RunLoginctl = defaults.RunLoginctl
	}
	if deps.NewConsole == nil {
		deps.NewConsole = defaults.NewConsole
	}
	if deps.NewCredentialStore == nil {
		deps.NewCredentialStore = defaults.NewCredentialStore
	}
	if deps.Now == nil {
		deps.Now = defaults.Now
	}
	if deps.Wait == nil {
		deps.Wait = defaults.Wait
	}
	if deps.UserHomeDir == nil {
		deps.UserHomeDir = defaults.UserHomeDir
	}
	return deps
}

func newWorkspace(paths fleetPaths, respawnDead bool, deps Dependencies) (fleetmodel.Workspace, error) {
	binary, err := canonicalUserBinary(deps)
	if err != nil {
		return fleetmodel.Workspace{}, err
	}
	return fleetmodel.Workspace{
		Runner: deps.Tmux, RespawnDead: respawnDead,
		OverviewCommand: []string{binary, "agent", "status", "--watch", "--socket", paths.socket, "--credentials", paths.credentials, "--file", paths.manifest, "--worker-dir", paths.workerDir, "--db", paths.database},
		ConsoleCommand: func(agentID string) []string {
			return []string{binary, "console", "attach", "--socket", paths.socket, "--credentials", paths.credentials, "--agent", agentID}
		},
	}, nil
}

func resolveFleetPaths(manifest, database, socket, workerDir, credentials localprofile.PathFlag) (fleetPaths, error) {
	resolver := localprofile.DefaultResolver()
	var result fleetPaths
	resources := []struct {
		resource localprofile.Resource
		override localprofile.Override
		target   *string
	}{
		{localprofile.FleetManifest, manifest.Override(), &result.manifest},
		{localprofile.DatabasePath, database.Override(), &result.database},
		{localprofile.SocketPath, socket.Override(), &result.socket},
		{localprofile.WorkerConfigDir, workerDir.Override(), &result.workerDir},
		{localprofile.CredentialsPath, credentials.Override(), &result.credentials},
	}
	resolved := make(map[string]string, len(resources))
	for _, item := range resources {
		path, err := resolver.Resolve(item.resource, item.override)
		if err != nil {
			return fleetPaths{}, err
		}
		*item.target = path.Path
		resolved[string(item.resource)] = path.Path
	}
	if err := localprofile.EnsureDistinct(resolved); err != nil {
		return fleetPaths{}, err
	}
	return result, nil
}

func authenticateFleetClient(ctx context.Context, paths fleetPaths, role domain.WebRole, scope domain.CLIScope, deps Dependencies) (consoleClient, openapi.CLISessionResponse, error) {
	client, err := deps.NewConsole(paths.socket)
	if err != nil {
		return nil, openapi.CLISessionResponse{}, err
	}
	probe, err := client.ProbeInstallation(ctx)
	if err != nil {
		return nil, openapi.CLISessionResponse{}, err
	}
	store, err := deps.NewCredentialStore(paths.credentials)
	if err != nil {
		return nil, openapi.CLISessionResponse{}, err
	}
	credential, err := store.Load(paths.socket, probe.InstallationID, "")
	if err != nil {
		return nil, openapi.CLISessionResponse{}, err
	}
	if !deps.Now().UTC().Before(credential.AbsoluteExpires.UTC()) {
		_, _ = store.Delete(credential)
		return nil, openapi.CLISessionResponse{}, fmt.Errorf("stored CLI credential is expired")
	}
	if err := client.UseCredential(ctx, credential.InstallationID, credential.Token); err != nil {
		return nil, openapi.CLISessionResponse{}, err
	}
	session, err := client.Session(ctx)
	if err != nil {
		var apiErr *consoleclient.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 401 && apiErr.Code == openapi.ErrorCLIUnauthenticated {
			_, _ = store.Delete(credential)
		}
		return nil, openapi.CLISessionResponse{}, err
	}
	if session.InstallationID != credential.InstallationID || session.Principal.TokenID != credential.TokenID ||
		session.Principal.Username != credential.Username || !session.AbsoluteExpiresAt.Equal(credential.AbsoluteExpires) ||
		!deps.Now().UTC().Before(session.AbsoluteExpiresAt.UTC()) {
		return nil, openapi.CLISessionResponse{}, fmt.Errorf("server CLI session does not match stored credential")
	}
	if !roleAllowed(session.Principal.Roles, role) || !contains(session.Principal.Scopes, string(scope)) {
		return nil, openapi.CLISessionResponse{}, fmt.Errorf("CLI session lacks required role %s and scope %s", role, scope)
	}
	return client, session, nil
}

func roleAllowed(roles []string, required domain.WebRole) bool {
	rank := map[string]int{string(domain.WebRoleViewer): 1, string(domain.WebRoleOperator): 2, string(domain.WebRoleOwner): 3}
	for _, role := range roles {
		if rank[role] >= rank[string(required)] {
			return true
		}
	}
	return false
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func validateAgentOptions(options []domain.ConsoleAgentOption) (map[string]domain.ConsoleAgentOption, error) {
	result := make(map[string]domain.ConsoleAgentOption, len(options))
	for _, option := range options {
		if err := option.Validate(); err != nil {
			return nil, fmt.Errorf("invalid safe Agent projection")
		}
		if _, exists := result[option.AgentID]; exists {
			return nil, fmt.Errorf("duplicate Agent %q", option.AgentID)
		}
		result[option.AgentID] = option
	}
	return result, nil
}

func initializeManifest(paths fleetPaths, requested, sourceFlags []string, options []domain.ConsoleAgentOption, deps Dependencies) (fleetmodel.Manifest, error) {
	selected := append([]string(nil), requested...)
	if len(selected) == 0 {
		if !deps.IsInteractive() {
			return fleetmodel.Manifest{}, fmt.Errorf("non-interactive init requires at least one explicit --agent")
		}
		var err error
		if deps.SelectAgents != nil {
			selected, err = deps.SelectAgents(options)
		} else {
			selected, err = selectAgents(deps, options)
		}
		if err != nil {
			return fleetmodel.Manifest{}, err
		}
	}
	if len(selected) == 0 {
		return fleetmodel.Manifest{}, fmt.Errorf("at least one Agent must be selected")
	}
	optionMap, err := validateAgentOptions(options)
	if err != nil {
		return fleetmodel.Manifest{}, err
	}
	sources, err := parseWorkerSources(sourceFlags)
	if err != nil {
		return fleetmodel.Manifest{}, err
	}
	seen := make(map[string]struct{}, len(selected))
	sort.Strings(selected)
	manifest := fleetmodel.Manifest{Version: fleetmodel.ManifestVersion, Session: fleetmodel.SessionName}
	type install struct {
		path    string
		content []byte
	}
	installs := make([]install, 0, len(selected))
	resolver := localprofile.DefaultResolver()
	workerOverride := localprofile.Override{Set: true, Value: paths.workerDir}
	for _, agentID := range selected {
		if _, duplicate := seen[agentID]; duplicate {
			return fleetmodel.Manifest{}, fmt.Errorf("Agent %q selected more than once", agentID)
		}
		seen[agentID] = struct{}{}
		if _, ok := optionMap[agentID]; !ok {
			return fleetmodel.Manifest{}, fmt.Errorf("Agent %q is not in the authenticated control-plane list", agentID)
		}
		canonical, err := resolver.WorkerConfig(workerOverride, agentID)
		if err != nil {
			return fleetmodel.Manifest{}, err
		}
		source := sources[agentID]
		if source == "" {
			source = canonical.Path
		}
		content, err := readWorkerSource(source)
		if err != nil {
			return fleetmodel.Manifest{}, fmt.Errorf("Agent %q worker config: %w", agentID, err)
		}
		config, err := validateWorkerConfig(content, agentID, paths.socket)
		if err != nil {
			return fleetmodel.Manifest{}, err
		}
		if source != canonical.Path {
			if err := validateRelocatableWorkerConfig(config); err != nil {
				return fleetmodel.Manifest{}, fmt.Errorf("Agent %q imported worker config: %w", agentID, err)
			}
		}
		if _, err := fleetmodel.CheckExactFile(canonical.Path, content); err != nil {
			return fleetmodel.Manifest{}, err
		}
		if source != canonical.Path {
			installs = append(installs, install{path: canonical.Path, content: content})
		}
		manifest.Agents = append(manifest.Agents, fleetmodel.Agent{AgentID: agentID, WorkerConfig: canonical.Path, Enabled: true})
	}
	for agentID := range sources {
		if _, ok := seen[agentID]; !ok {
			return fleetmodel.Manifest{}, fmt.Errorf("worker config source supplied for unselected Agent %q", agentID)
		}
	}
	encoded, err := fleetmodel.Encode(manifest)
	if err != nil {
		return fleetmodel.Manifest{}, err
	}
	if _, err := fleetmodel.CheckExactFile(paths.manifest, encoded); err != nil {
		return fleetmodel.Manifest{}, err
	}
	for _, item := range installs {
		if _, err := fleetmodel.WriteExactFileAtomic(item.path, item.content, fleetmodel.AtomicFileOptions{BeforeRename: atomicHook(deps, item.path)}); err != nil {
			return fleetmodel.Manifest{}, err
		}
	}
	if _, err := fleetmodel.WriteExactFileAtomic(paths.manifest, encoded, fleetmodel.AtomicFileOptions{BeforeRename: atomicHook(deps, paths.manifest)}); err != nil {
		return fleetmodel.Manifest{}, err
	}
	return manifest, nil
}

func atomicHook(deps Dependencies, path string) func() error {
	if deps.BeforeAtomicRename == nil {
		return nil
	}
	return func() error { return deps.BeforeAtomicRename(path) }
}

func selectAgents(deps Dependencies, options []domain.ConsoleAgentOption) ([]string, error) {
	if len(options) == 0 {
		return nil, fmt.Errorf("authenticated Agent list is empty")
	}
	fmt.Fprintln(deps.Err, "Select one or more Agents by comma-separated Agent ID:")
	for _, option := range options {
		fmt.Fprintf(deps.Err, "  %s\t%s\t%s\tgeneration=%d\n", option.AgentID, option.DisplayName, option.WorkerStatus, option.Generation)
	}
	fmt.Fprint(deps.Err, "Agents: ")
	line, err := bufio.NewReader(deps.In).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	parts := strings.Split(strings.TrimSpace(line), ",")
	selected := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			selected = append(selected, value)
		}
	}
	return selected, nil
}

func parseWorkerSources(values []string) (map[string]string, error) {
	result := make(map[string]string, len(values))
	for _, value := range values {
		agentID, path, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(agentID) != agentID || !filepath.IsAbs(path) {
			return nil, fmt.Errorf("--worker-config must be agent-id=/absolute/source.yaml")
		}
		if err := domain.ValidateIdentifier("agent_id", agentID); err != nil {
			return nil, err
		}
		if _, duplicate := result[agentID]; duplicate {
			return nil, fmt.Errorf("duplicate worker config source for Agent %q", agentID)
		}
		result[agentID] = filepath.Clean(path)
	}
	return result, nil
}

func readWorkerSource(path string) ([]byte, error) {
	return fleetmodel.ReadSecureFile(path, fleetmodel.SecureFileOptions{MaximumBytes: maxWorkerConfigBytes})
}

func validateWorkerConfig(content []byte, agentID, socketPath string) (*workerconfig.ProcessConfig, error) {
	config, err := workerconfig.DecodeProcessConfig(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("load Agent %q worker config: %w", agentID, err)
	}
	if config.AgentID != agentID {
		return nil, fmt.Errorf("Agent %q worker config declares %q", agentID, config.AgentID)
	}
	if config.Transport == domain.WorkerTransportUnix && filepath.Clean(config.UnixSocket) != socketPath {
		return nil, fmt.Errorf("Agent %q worker config socket %q does not match canonical Fleet socket %q", agentID, config.UnixSocket, socketPath)
	}
	return config, nil
}

func validateRelocatableWorkerConfig(config *workerconfig.ProcessConfig) error {
	paths := map[string]string{
		"ca_file": config.CAFile, "client_cert_file": config.ClientCertFile,
		"client_key_file": config.ClientKeyFile, "network_materialization_dir": config.NetworkMaterializationDir,
	}
	for name, value := range paths {
		if value != "" && !filepath.IsAbs(value) {
			return fmt.Errorf("%s must be absolute before importing to the canonical worker directory", name)
		}
	}
	for _, backend := range config.RuntimeBackendConfig {
		if value := backend.Network.ConfigFile; value != "" && !filepath.IsAbs(value) {
			return fmt.Errorf("runtime backend %q network config_file must be absolute before importing to the canonical worker directory", backend.BackendID)
		}
	}
	return nil
}

func prepare(manifestPath, workerDir, socketPath string, options map[string]domain.ConsoleAgentOption, selectedIDs ...string) (fleetmodel.Manifest, []preparedAgent, error) {
	manifestContent, err := fleetmodel.ReadSecureFile(manifestPath, fleetmodel.SecureFileOptions{MaximumBytes: maxWorkerConfigBytes, RequirePrivate: true})
	if err != nil {
		return fleetmodel.Manifest{}, nil, fmt.Errorf("read Fleet manifest: %w", err)
	}
	manifest, err := fleetmodel.Decode(bytes.NewReader(manifestContent))
	if err != nil {
		return fleetmodel.Manifest{}, nil, err
	}
	if len(selectedIDs) > 0 {
		selected := make(map[string]bool, len(selectedIDs))
		for _, id := range selectedIDs {
			selected[id] = true
		}
		entries := make([]fleetmodel.Agent, 0, len(selectedIDs))
		for _, entry := range manifest.Agents {
			if selected[entry.AgentID] {
				entries = append(entries, entry)
			}
		}
		if len(entries) != len(selected) {
			return fleetmodel.Manifest{}, nil, fmt.Errorf("selected Agent is not managed by this Fleet")
		}
		manifest.Agents = entries
	}
	resolver := localprofile.DefaultResolver()
	workerOverride := localprofile.Override{Set: true, Value: workerDir}
	prepared := make([]preparedAgent, 0, len(manifest.Agents))
	for _, entry := range manifest.Agents {
		option, ok := options[entry.AgentID]
		if !ok {
			return fleetmodel.Manifest{}, nil, fmt.Errorf("Agent %q is not in the authenticated control-plane list", entry.AgentID)
		}
		canonical, err := resolver.WorkerConfig(workerOverride, entry.AgentID)
		if err != nil {
			return fleetmodel.Manifest{}, nil, err
		}
		if !filepath.IsAbs(entry.WorkerConfig) || filepath.Clean(entry.WorkerConfig) != canonical.Path {
			return fleetmodel.Manifest{}, nil, fmt.Errorf("Agent %q worker_config must be canonical path %q", entry.AgentID, canonical.Path)
		}
		workerContent, err := fleetmodel.ReadSecureFile(canonical.Path, fleetmodel.SecureFileOptions{MaximumBytes: maxWorkerConfigBytes, RequirePrivate: true})
		if err != nil {
			return fleetmodel.Manifest{}, nil, fmt.Errorf("read Worker config %q: %w", canonical.Path, err)
		}
		environmentPath := strings.TrimSuffix(canonical.Path, filepath.Ext(canonical.Path)) + ".env"
		if _, err := fleetmodel.ReadSecureFile(environmentPath, fleetmodel.SecureFileOptions{MaximumBytes: maxWorkerConfigBytes, RequirePrivate: true}); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fleetmodel.Manifest{}, nil, fmt.Errorf("read Worker environment file: %w", err)
		}
		if _, err := validateWorkerConfig(workerContent, entry.AgentID, socketPath); err != nil {
			return fleetmodel.Manifest{}, nil, err
		}
		if entry.IdentityFile != "" {
			if err := validateIdentityCompatibility(manifestPath, entry.IdentityFile, option); err != nil {
				return fleetmodel.Manifest{}, nil, err
			}
		}
		prepared = append(prepared, preparedAgent{entry: entry, workerPath: canonical.Path})
	}
	return manifest, prepared, nil
}

func validateIdentityCompatibility(manifestPath, value string, option domain.ConsoleAgentOption) error {
	path := value
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(manifestPath), path)
	}
	definition, err := admincli.LoadAgentDefinition(path)
	if err != nil {
		return fmt.Errorf("Agent %q identity_file: %w", option.AgentID, err)
	}
	if definition.AgentID != option.AgentID || definition.OrganizationID != option.OrganizationID || definition.DisplayName != option.DisplayName {
		return fmt.Errorf("Agent %q identity_file does not match the existing authenticated Agent; use the formal Agent management entrypoint", option.AgentID)
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

func queueStops(ctx context.Context, client consoleClient, manifest fleetmodel.Manifest, force bool, deps Dependencies) (consoleClient, []stopTarget, error) {
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
				if printJSON(deps, stopObservation{AgentID: agentID, Elapsed: elapsedString(now.Sub(target.QueuedAt)), Message: "observation temporarily unavailable; persisted graceful-stop intent remains active"}) != 0 {
					return fmt.Errorf("write graceful-stop observation")
				}
				continue
			}
			if effectivelyOffline(attached, now) {
				if printJSON(deps, stopObservation{AgentID: agentID, WorkerStatus: domain.WorkerStatusOffline, Elapsed: elapsedString(now.Sub(target.QueuedAt)), Message: "offline; graceful stop completed"}) != 0 {
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
			if printJSON(deps, stopObservation{AgentID: agentID, Generation: attached.Generation, WorkerStatus: attached.WorkerStatus,
				RunAttempt: attached.ActiveRun, Elapsed: elapsedString(now.Sub(target.QueuedAt)), RecentStatus: recent,
				NoNewTasks: draining, Message: message}) != 0 {
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

func fleetStatus(ctx context.Context, manifest fleetmodel.Manifest, prepared []preparedAgent, options []domain.ConsoleAgentOption, deps Dependencies) int {
	windows, workspaceErr := (fleetmodel.Workspace{Runner: deps.Tmux}).Inspect(ctx)
	windowStatus := make([]map[string]any, 0, len(windows))
	for _, window := range windows {
		windowStatus = append(windowStatus, map[string]any{
			"name": window.Name, "pane_indices": window.PaneIndices,
			"managed":  window.Managed.Set && window.Managed.Value == "1",
			"agent_id": window.AgentID.Value, "pane_zero_dead": window.PaneZeroSeen && window.PaneZeroDead,
		})
	}
	status := map[string]any{"session": fleetmodel.SessionName, "windows": windowStatus, "agents": options,
		"foreground_takeover": "Foreground Takeover（规划中，暂不可用）"}
	if workspaceErr != nil {
		status["workspace_status"] = "missing_or_unavailable"
	} else {
		status["workspace_status"] = "available"
	}
	units := make(map[string]string, len(prepared))
	for _, agent := range prepared {
		output, err := deps.RunSystemctl(ctx, "--user", "is-active", workerUnit(agent.entry.AgentID))
		if err != nil && output == "" {
			output = "unknown"
		}
		units[agent.entry.AgentID] = bounded(output, 128)
	}
	status["workers"] = units
	status["fleet_agents"] = manifest.Agents
	return printJSON(deps, status)
}

func checkLinger(ctx context.Context, deps Dependencies) {
	output, err := deps.RunLoginctl(ctx, "show-user", strconv.Itoa(os.Getuid()), "--property=Linger", "--value")
	if err != nil || strings.TrimSpace(output) != "yes" {
		fmt.Fprintln(deps.Err, "warning: user linger is not confirmed; Fleet will not enable it automatically")
	}
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

func bounded(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len(value) > maximum {
		return value[:maximum] + "..."
	}
	return value
}

func safeAuthError(err error) string {
	var apiErr *consoleclient.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code + ": " + apiErr.Message
	}
	return bounded(err.Error(), 512)
}

func usage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage: openagentx fleet <init|workspace|up|status|down|force-stop> [--file <fleet.yaml>] [--db <path>] [--socket <path>] [--worker-dir <dir>] [--credentials <path>]")
	fmt.Fprintln(writer, "       fleet init [--agent <agent-id> ...] [--worker-config <agent-id=/absolute/source.yaml> ...] [--respawn-dead]")
	fmt.Fprintln(writer, "       fleet workspace [--respawn-dead]")
	fmt.Fprintln(writer, "       fleet force-stop --confirm-force-stop --confirm-active-run-uncertain")
	fmt.Fprintln(writer, "Fleet uses the stored CLI session from openagentx console login; it never reads an Owner password")
	fmt.Fprintln(writer, "Managed tmux workspace: exact session OAX with stable pane 0; existing agentx sessions are not migrated")
	fmt.Fprintf(writer, "Defaults: $%s/$%s/$%s/$%s/$%s > $%s > ~/.openagentx\n", localprofile.EnvFleetManifest, localprofile.EnvDatabasePath, localprofile.EnvSocketPath, localprofile.EnvWorkerConfigDir, localprofile.EnvCredentialsPath, localprofile.EnvHome)
}
