package fleet

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"golang.org/x/term"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"
	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	admincli "openagentx/internal/cli/admin"
	consolecli "openagentx/internal/cli/console"
	consoleclient "openagentx/internal/client/console"
	"openagentx/internal/domain"
	fleetmodel "openagentx/internal/fleet"
	"openagentx/internal/localprofile"
	workerconfig "openagentx/internal/worker"
)

type agentOptions struct {
	id, name, workspace, role, roleText, identity, workerSource, environmentSource, passwordFile, username, organization, webURL, binary, model string
	timeout, wait                                                                                                                               time.Duration
	console, noOpen, watch, configureOnly, jsonOutput                                                                                           bool
	paths                                                                                                                                       fleetPaths
}

func ExecuteAgent(args []string, dependencies Dependencies) int {
	deps := withDefaults(dependencies)
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		agentUsage(deps.Out)
		return 0
	}
	command := args[0]
	switch command {
	case "add", "open", "status", "pause", "resume", "start":
	default:
		agentUsage(deps.Err)
		return 2
	}
	o, err := parseAgentOptions(command, args[1:], deps)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(deps.Err, err)
		return 2
	}
	ctx := deps.Context
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if command == "add" {
		if err = addAgent(ctx, &o, deps); err != nil {
			fmt.Fprintf(deps.Err, "Agent add: %v\n", err)
			return 1
		}
		if o.configureOnly {
			fmt.Fprintf(deps.Out, "Agent %s 已纳管；运行 agent resume %s 启动并准备网络。\n", o.id, o.id)
			return 0
		}
		command = "resume"
	}
	role, scope := domain.WebRoleViewer, domain.CLIScopeConsoleRead
	if command == "pause" || command == "resume" || command == "start" {
		role, scope = domain.WebRoleOwner, domain.CLIScopeFleetLifecycle
	}
	client, session, err := authenticateFleetClient(ctx, o.paths, role, scope, deps)
	if err != nil {
		fmt.Fprintf(deps.Err, "Agent 登录不可用: %v；运行 openagentx console login --socket %s --credentials %s\n", safeAuthError(err), o.paths.socket, o.paths.credentials)
		return 1
	}
	api, err := newAgentAPI(o.paths, session, deps)
	if err != nil {
		fmt.Fprintln(deps.Err, err)
		return 1
	}
	defer api.http.CloseIdleConnections()
	if command == "status" {
		for {
			if err = printAgentStatus(ctx, api, client, o, deps); err != nil {
				fmt.Fprintln(deps.Err, err)
				return 1
			}
			if !o.watch {
				return 0
			}
			if err = deps.Wait(ctx, 5*time.Second); err != nil {
				return 0
			}
		}
	}
	if err = domain.ValidateIdentifier("agent_id", o.id); err != nil {
		fmt.Fprintln(deps.Err, err)
		return 2
	}
	if command == "pause" {
		m := fleetmodel.Manifest{Agents: []fleetmodel.Agent{{AgentID: o.id}}}
		_, targets, err := queueStops(ctx, client, m, false, deps)
		if err == nil {
			err = waitForOffline(ctx, client, targets, deps)
		}
		if err != nil {
			fmt.Fprintf(deps.Err, "暂停观察结束（已提交的停止请求保持有效）: %v\n", safeAuthError(err))
			return 1
		}
		fmt.Fprintf(deps.Out, "Agent %s 已暂停；恢复: openagentx agent resume %s\n", o.id, o.id)
		return 0
	}
	if command == "resume" || command == "start" {
		readyCtx, done := context.WithTimeout(ctx, o.wait)
		defer done()
		if err = startAgent(readyCtx, client, api, o, deps); err != nil {
			fmt.Fprintf(deps.Err, "Agent 未就绪: %v\n", err)
			return 1
		}
	}
	if err = printAgentStatus(ctx, api, client, o, deps); err != nil {
		fmt.Fprintln(deps.Err, err)
		return 1
	}
	if o.noOpen {
		return 0
	}
	if o.console {
		if !deps.IsInteractive() {
			fmt.Fprintln(deps.Err, "--console requires an interactive TTY")
			return 2
		}
		options, e := client.ListAgentOptions(ctx)
		if e != nil {
			fmt.Fprintln(deps.Err, e)
			return 1
		}
		optionMap, e := validateAgentOptions(options)
		if e != nil {
			return 1
		}
		manifest, _, e := prepare(o.paths.manifest, o.paths.workerDir, o.paths.socket, optionMap, o.id)
		if e != nil {
			fmt.Fprintln(deps.Err, e)
			return 1
		}
		workspace, e := newWorkspace(o.paths, true, deps)
		if e == nil {
			_, e = workspace.Reconcile(ctx, manifest)
		}
		if e != nil {
			fmt.Fprintln(deps.Err, e)
			return 1
		}
		action := "attach-session"
		if os.Getenv("TMUX") != "" {
			action = "switch-client"
		}
		cmd := exec.CommandContext(ctx, "tmux", action, "-t", fleetmodel.SessionName+":"+o.id)
		cmd.Stdin = deps.In
		cmd.Stdout = deps.Out
		cmd.Stderr = deps.Err
		if e = cmd.Run(); e != nil {
			fmt.Fprintln(deps.Err, e)
			return 1
		}
		return 0
	}
	link, err := agentLink(ctx, o, deps)
	if err != nil {
		fmt.Fprintln(deps.Err, err)
		return 1
	}
	fmt.Fprintf(deps.Out, "工作台: %s\n", link)
	if deps.IsInteractive() {
		if err = exec.CommandContext(ctx, "xdg-open", link).Run(); err != nil {
			fmt.Fprintln(deps.Err, "浏览器未自动打开，请使用上方工作台链接。")
		}
	}
	return 0
}

func agentUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: openagentx agent add [--name NAME --workspace DIR --role FILE | --identity FILE] [--id ID] [--worker-config FILE] [--password-file FILE] [--configure-only]\n       openagentx agent <open|pause|resume|status> [AGENT] [--console] [--no-open] [--watch] [--json] [--web-url URL]\n       Path overrides: --db --socket --file (Fleet manifest) --worker-dir --credentials")
}
func parseAgentOptions(command string, args []string, deps Dependencies) (agentOptions, error) {
	var o agentOptions
	// Accept the documented positional Agent before or after flags.
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		o.id = args[0]
		args = args[1:]
	}
	f := flag.NewFlagSet("agent "+command, flag.ContinueOnError)
	f.SetOutput(deps.Err)
	var manifest, database, socket, workers, credentials localprofile.PathFlag
	f.Var(&manifest, "file", "Fleet manifest")
	f.Var(&database, "db", "Database path")
	f.Var(&socket, "socket", "Daemon socket")
	f.Var(&workers, "worker-dir", "Worker config directory")
	f.Var(&credentials, "credentials", "Credential store")
	f.StringVar(&o.id, "id", o.id, "Stable Agent ID (default derived from name)")
	f.StringVar(&o.name, "name", "", "Display name")
	f.StringVar(&o.workspace, "workspace", "", "Working directory")
	f.StringVar(&o.role, "role", "", "Role Markdown file")
	f.StringVar(&o.roleText, "role-text", "", "Role text to save as Markdown")
	f.StringVar(&o.identity, "identity", "", "Existing identity YAML to import")
	f.StringVar(&o.workerSource, "worker-config", "", "Existing Worker YAML to import")
	f.StringVar(&o.environmentSource, "environment-file", "", "Existing private Worker environment file to import")
	f.StringVar(&o.passwordFile, "password-file", "", "Private password file; content is never printed")
	f.StringVar(&o.username, "owner-username", "owner", "Owner username")
	f.StringVar(&o.organization, "organization-id", "default", "Organization ID")
	f.StringVar(&o.webURL, "web-url", os.Getenv("OPENAGENTX_WEB_URL"), "Web origin (otherwise discover from user service)")
	f.StringVar(&o.binary, "runtime-binary", "agy-graft", "AGY wrapper")
	f.StringVar(&o.model, "model", "", "AGY model")
	f.DurationVar(&o.timeout, "timeout", 30*time.Minute, "Per-run timeout")
	f.DurationVar(&o.wait, "wait", 3*time.Minute, "Startup/network readiness deadline")
	f.BoolVar(&o.console, "console", false, "Open terminal Console")
	f.BoolVar(&o.noOpen, "no-open", false, "Print status without opening UI")
	f.BoolVar(&o.watch, "watch", false, "Refresh read-only status every five seconds")
	f.BoolVar(&o.jsonOutput, "json", false, "Full diagnostic JSON instead of concise navigation")
	f.BoolVar(&o.configureOnly, "configure-only", false, "Register/configure without starting Worker")
	if err := f.Parse(args); err != nil {
		return o, err
	}
	if f.NArg() == 1 && o.id == "" {
		o.id = f.Arg(0)
	} else if f.NArg() != 0 {
		return o, fmt.Errorf("unexpected positional arguments")
	}
	if o.wait <= 0 || o.timeout <= 0 {
		return o, fmt.Errorf("wait and timeout must be positive")
	}
	var err error
	o.paths, err = resolveFleetPaths(manifest, database, socket, workers, credentials)
	return o, err
}

func addAgent(ctx context.Context, o *agentOptions, deps Dependencies) error {
	reader := bufio.NewReader(deps.In)
	ask := func(label string, target *string) error {
		if *target != "" {
			return nil
		}
		if !deps.IsInteractive() {
			return fmt.Errorf("%s is required in non-interactive mode", label)
		}
		fmt.Fprintf(deps.Err, "%s: ", label)
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		*target = strings.TrimSpace(line)
		if *target == "" {
			return fmt.Errorf("%s cannot be empty", label)
		}
		return nil
	}
	var definition *admincli.AgentDefinition
	var err error
	if o.identity != "" {
		o.identity, err = filepath.Abs(o.identity)
		if err != nil {
			return err
		}
		definition, err = admincli.LoadAgentDefinition(o.identity)
		if err != nil {
			return err
		}
		if o.id != "" && o.id != definition.AgentID {
			return fmt.Errorf("--id conflicts with imported identity")
		}
		o.id = definition.AgentID
		o.name = definition.DisplayName
		o.workspace = definition.Profile.WorkspaceRoot
		o.role = definition.Profile.InstructionsPath
	} else {
		if err = ask("Agent 名称", &o.name); err != nil {
			return err
		}
		if err = ask("工作目录", &o.workspace); err != nil {
			return err
		}
		if o.roleText == "" {
			if err = ask("职责文档路径或职责描述", &o.role); err != nil {
				return err
			}
		}
		if o.id == "" {
			o.id = o.name
			if domain.ValidateIdentifier("agent_id", o.id) != nil {
				o.id = fmt.Sprintf("agent-%x", sha256.Sum256([]byte(o.name)))[:18]
			}
		}
		if err = domain.ValidateIdentifier("agent_id", o.id); err != nil {
			return err
		}
		o.workspace, err = filepath.Abs(o.workspace)
		if err != nil {
			return err
		}
		info, statErr := os.Stat(o.workspace)
		if statErr != nil || !info.IsDir() {
			return fmt.Errorf("工作目录必须已存在")
		}
		if o.roleText == "" {
			if info, err := os.Stat(o.role); err != nil || !info.Mode().IsRegular() {
				if !deps.IsInteractive() {
					return fmt.Errorf("role file missing; use --role-text for text")
				}
				o.roleText = o.role
			}
		}
		if o.roleText != "" {
			o.role = filepath.Join(o.paths.workerDir, "identities", o.id+".md")
			if _, err = fleetmodel.WriteExactFileAtomic(o.role, []byte(o.roleText+"\n"), fleetmodel.AtomicFileOptions{}); err != nil {
				return err
			}
		} else {
			o.role, err = filepath.Abs(o.role)
			if err != nil {
				return err
			}
		}
		definition = &admincli.AgentDefinition{Version: 1, AgentID: o.id, PrincipalID: "agent-" + o.id, OrganizationID: o.organization, DisplayName: o.name}
		definition.Profile.InstructionsPath = o.role
		definition.Profile.WorkspaceRoot = o.workspace
		o.identity = filepath.Join(o.paths.workerDir, "identities", o.id+".yaml")
		content, err := yaml.Marshal(definition)
		if err != nil {
			return err
		}
		if _, err = fleetmodel.WriteExactFileAtomic(o.identity, content, fleetmodel.AtomicFileOptions{}); err != nil {
			return err
		}
	}
	if o.id == fleetmodel.OverviewWindow {
		return fmt.Errorf("Agent ID overview is reserved")
	}
	canonical, err := localprofile.DefaultResolver().WorkerConfig(localprofile.Override{Set: true, Value: o.paths.workerDir}, o.id)
	if err != nil {
		return err
	}
	var content []byte
	if o.workerSource != "" {
		o.workerSource, err = filepath.Abs(o.workerSource)
		if err != nil {
			return err
		}
		content, err = readWorkerSource(o.workerSource)
		if err != nil {
			return err
		}
	} else if existing, readErr := readWorkerSource(canonical.Path); readErr == nil {
		content = existing
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	} else {
		options := map[string]any{"binary": o.binary, "working_dir": o.workspace, "timeout": o.timeout.String()}
		if o.model != "" {
			options["models"] = []string{o.model}
		}
		config := workerconfig.ProcessConfig{Version: 1, AgentID: o.id, Transport: domain.WorkerTransportUnix, UnixSocket: o.paths.socket, RuntimeBackendConfig: []workerconfig.RuntimeBackendConfig{{BackendID: "agy", AdapterID: "agy-batch", Options: options, Network: domain.NetworkPolicy{Mode: domain.NetworkInherit}}}}
		content, err = yaml.Marshal(config)
		if err != nil {
			return err
		}
	}
	config, err := validateWorkerConfig(content, o.id, o.paths.socket)
	if err != nil {
		return err
	}
	if err = validateRelocatableWorkerConfig(config); err != nil {
		return err
	}
	if _, err = fleetmodel.CheckExactFile(canonical.Path, content); err != nil {
		return err
	}
	password := ""
	adminDeps := admincli.DefaultDependencies()
	adminDeps.Out = deps.Out
	adminDeps.Err = deps.Err
	originalRead := adminDeps.ReadPassword
	adminDeps.ReadPassword = func(prompt string) (string, error) {
		if password != "" {
			return password, nil
		}
		if o.passwordFile != "" {
			p, e := filepath.Abs(o.passwordFile)
			if e != nil {
				return "", e
			}
			b, e := fleetmodel.ReadSecureFile(p, fleetmodel.SecureFileOptions{MaximumBytes: 8192, RequirePrivate: true})
			if e != nil {
				return "", e
			}
			password = strings.TrimRight(string(b), "\r\n")
			return password, nil
		}
		var e error
		password, e = originalRead(prompt)
		return password, e
	}
	if admincli.ExecuteInit([]string{"--db", o.paths.database, "--owner-username", o.username, "--organization-id", definition.OrganizationID}, adminDeps) != 0 {
		return fmt.Errorf("初始化未完成")
	}
	if admincli.ExecuteAgent([]string{"apply", "--db", o.paths.database, "--file", o.identity, "--owner-username", o.username}, adminDeps) != 0 {
		return fmt.Errorf("Agent identity未应用；已有冲突不会覆盖")
	}
	if _, err = fleetmodel.WriteExactFileAtomic(canonical.Path, content, fleetmodel.AtomicFileOptions{}); err != nil {
		return err
	}
	if o.environmentSource != "" {
		source, err := filepath.Abs(o.environmentSource)
		if err != nil {
			return err
		}
		env, err := fleetmodel.ReadSecureFile(source, fleetmodel.SecureFileOptions{MaximumBytes: 1 << 20, RequirePrivate: true})
		if err != nil {
			return err
		}
		if _, err = fleetmodel.WriteExactFileAtomic(strings.TrimSuffix(canonical.Path, ".yaml")+".env", env, fleetmodel.AtomicFileOptions{}); err != nil {
			return err
		}
	}
	if o.environmentSource == "" {
		envPath := strings.TrimSuffix(canonical.Path, ".yaml") + ".env"
		if _, readErr := os.Lstat(envPath); errors.Is(readErr, os.ErrNotExist) {
			var env strings.Builder
			for _, name := range []string{"AGY_GRAFT_NATIVE_PROXY", "AGY_GRAFT_REAL_BIN", "AGY_GRAFT_MGRAFTCP_BIN", "AGY_GRAFT_GOMAXPROCS", "AGY_GRAFT_IPV4_ONLY", "AGY_GRAFT_IPV4_ONLY_FILE", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "all_proxy", "no_proxy"} {
				value := os.Getenv(name)
				if value == "" {
					continue
				}
				if strings.ContainsAny(value, "\r\n\x00") {
					return fmt.Errorf("environment %s must be a single line", name)
				}
				value = strings.ReplaceAll(strings.ReplaceAll(value, "\\", "\\\\"), "\"", "\\\"")
				fmt.Fprintf(&env, "%s=\"%s\"\n", name, value)
			}
			if _, err = fleetmodel.WriteExactFileAtomic(envPath, []byte(env.String()), fleetmodel.AtomicFileOptions{}); err != nil {
				return err
			}
		} else if readErr != nil {
			return readErr
		}
	}
	if err = fleetmodel.AddAgent(o.paths.manifest, fleetmodel.Agent{AgentID: o.id, IdentityFile: o.identity, WorkerConfig: canonical.Path, Enabled: true}); err != nil {
		return err
	}
	if _, _, err = authenticateFleetClient(ctx, o.paths, domain.WebRoleOwner, domain.CLIScopeFleetLifecycle, deps); err != nil {
		if _, startErr := deps.RunSystemctl(ctx, "--user", "start", "openagentx.service"); startErr != nil {
			return fmt.Errorf("daemon 未就绪；启动用户 openagentx.service 后重试")
		}
		connectCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		for {
			probeClient, probeErr := deps.NewConsole(o.paths.socket)
			if probeErr == nil {
				_, probeErr = probeClient.ProbeInstallation(connectCtx)
			}
			if probeErr == nil {
				break
			}
			if err = deps.Wait(connectCtx, time.Second); err != nil {
				return fmt.Errorf("daemon连接尚未就绪；检查 %s 对应的用户服务", o.paths.socket)
			}
		}
		loginDeps := consolecli.DefaultDependencies()
		loginDeps.Out = deps.Out
		loginDeps.Err = deps.Err
		loginDeps.ReadPassword = adminDeps.ReadPassword
		loginDeps.ReadUsername = func(string) (string, error) { return o.username, nil }
		loginDeps.IsInteractive = func() bool { return true }
		if consolecli.Execute([]string{"login", "--socket", o.paths.socket, "--credentials", o.paths.credentials}, loginDeps) != 0 {
			return fmt.Errorf("CLI登录未完成")
		}
	}
	fmt.Fprintf(deps.Out, "Agent %s 已纳管，工作目录 %s，职责 %s\n", o.id, o.workspace, o.role)
	return nil
}

type agentAPI struct {
	http  *http.Client
	token string
}

func newAgentAPI(paths fleetPaths, session openapi.CLISessionResponse, deps Dependencies) (*agentAPI, error) {
	store, err := deps.NewCredentialStore(paths.credentials)
	if err != nil {
		return nil, err
	}
	credential, err := store.Load(paths.socket, session.InstallationID, session.Principal.Username)
	if err != nil {
		return nil, err
	}
	if credential.TokenID != session.Principal.TokenID {
		return nil, fmt.Errorf("CLI credential changed; retry")
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", paths.socket)
	}}
	return &agentAPI{http: &http.Client{Transport: transport, Timeout: 35 * time.Second}, token: credential.Token}, nil
}
func (a *agentAPI) call(ctx context.Context, method, path string, body, dst any) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://unix"+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Content-Type", "application/json")
	if body != nil {
		var meta struct {
			Meta openapi.CommandMeta `json:"meta"`
		}
		_ = json.Unmarshal(data, &meta)
		req.Header.Set("Idempotency-Key", meta.Meta.IdempotencyKey)
	}
	res, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("API %s returned HTTP %d (request was not assumed successful)", path, res.StatusCode)
	}
	if dst == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(dst)
}

func startAgent(ctx context.Context, client consoleClient, api *agentAPI, o agentOptions, deps Dependencies) error {
	options, err := client.ListAgentOptions(ctx)
	if err != nil {
		return err
	}
	optionMap, err := validateAgentOptions(options)
	if err != nil {
		return err
	}
	_, prepared, err := prepare(o.paths.manifest, o.paths.workerDir, o.paths.socket, optionMap, o.id)
	if err != nil {
		return err
	}
	var selected *preparedAgent
	for i := range prepared {
		if prepared[i].entry.AgentID == o.id {
			selected = &prepared[i]
		}
	}
	if selected == nil {
		return fmt.Errorf("Agent %s 尚未纳管；运行 agent add --identity <file>", o.id)
	}
	selected.entry.Enabled = true // explicit resume targets this Agent even if Fleet bulk start is disabled
	before, err := client.Attach(ctx, o.id, consoleapi.ModeNormal)
	if err != nil {
		return err
	}
	if before.WorkerStatus == domain.WorkerStatusDraining {
		return fmt.Errorf("当前代正在结束工作；请待暂停完成后恢复")
	}
	if effectivelyOffline(before, deps.Now()) {
		if err = ensureAgentUnit(ctx, *selected, deps); err != nil {
			return err
		}
		if err = verifyUserUnits(ctx, []preparedAgent{*selected}, deps); err != nil {
			return err
		}
		if _, err = deps.RunSystemctl(ctx, "--user", "start", workerUnit(o.id)); err != nil {
			return fmt.Errorf("启动服务失败: %w", err)
		}
	}
	var attached consoleapi.AttachResponse
	for {
		attached, err = client.Attach(ctx, o.id, consoleapi.ModeNormal)
		if err != nil {
			return err
		}
		if !effectivelyOffline(attached, deps.Now()) {
			break
		}
		if err = deps.Wait(ctx, time.Second); err != nil {
			return fmt.Errorf("等待 Worker 上线超时；查看 journalctl --user -u %s", workerUnit(o.id))
		}
	}
	config, err := workerconfig.LoadProcessConfig(selected.workerPath)
	if err != nil {
		return err
	}
	agyCount := 0
	for _, backend := range config.RuntimeBackendConfig {
		if backend.AdapterID != "agy-batch" {
			continue
		}
		agyCount++
		if err = prepareNetwork(ctx, api, attached, backend, deps); err != nil {
			return err
		}
	}
	if agyCount == 0 {
		return fmt.Errorf("Agent configuration has no AGY backend")
	}
	confirmed, err := client.Attach(ctx, o.id, consoleapi.ModeNormal)
	if err != nil {
		return err
	}
	if confirmed.WorkerInstanceID != attached.WorkerInstanceID || confirmed.Generation != attached.Generation || effectivelyOffline(confirmed, deps.Now()) {
		return fmt.Errorf("Worker generation changed while preparing network; retry resume")
	}
	for {
		var overview struct {
			Agents []struct {
				ID        string `json:"agent_id"`
				Readiness struct {
					Ready  bool   `json:"ready"`
					Reason string `json:"reason"`
				} `json:"readiness"`
			} `json:"agents"`
		}
		if err = api.call(ctx, http.MethodGet, openapi.ObserveOverviewPath, nil, &overview); err != nil {
			return err
		}
		ready := false
		reason := "后台尚未就绪"
		for _, agent := range overview.Agents {
			if agent.ID == o.id {
				ready = agent.Readiness.Ready
				reason = agent.Readiness.Reason
			}
		}
		if ready {
			break
		}
		if err = deps.Wait(ctx, time.Second); err != nil {
			return fmt.Errorf("%s: %w", reason, err)
		}
	}
	fmt.Fprintf(deps.Out, "Agent %s 服务在线，当前代 %d 网络已应用。\n", o.id, attached.Generation)
	return nil
}
func ensureAgentUnit(ctx context.Context, agent preparedAgent, deps Dependencies) error {
	binary, err := canonicalUserBinary(deps)
	if err != nil {
		return err
	}
	home, err := deps.UserHomeDir()
	if err != nil {
		return err
	}
	for _, s := range []string{binary, home, agent.workerPath} {
		if strings.ContainsAny(s, "\n\r\"%\\") {
			return fmt.Errorf("unsupported systemd path characters")
		}
	}
	// An existing unit must pass the same actual ExecStart verification; never replace it.
	loaded, _ := deps.RunSystemctl(ctx, "--user", "show", workerUnit(agent.entry.AgentID), "--property=LoadState", "--value")
	if strings.TrimSpace(loaded) == "loaded" {
		if err = verifyUserUnits(ctx, []preparedAgent{agent}, deps); err == nil {
			return nil
		}
		fragment, fragmentErr := deps.RunSystemctl(ctx, "--user", "show", workerUnit(agent.entry.AgentID), "--property=FragmentPath", "--value")
		active, activeErr := deps.RunSystemctl(ctx, "--user", "show", workerUnit(agent.entry.AgentID), "--property=ActiveState", "--value")
		if fragmentErr != nil || activeErr != nil || !strings.HasSuffix(strings.TrimSpace(fragment), "openagentx-worker@.service") || strings.TrimSpace(active) != "inactive" {
			return fmt.Errorf("existing user service configuration conflicts; no unit was changed: %w", err)
		}
		// A fresh Agent may inherit an unrelated default template path. Create
		// its own instance unit, preserving the template and all other Agents.
	}
	directory := filepath.Join(home, ".config", "systemd", "user")
	if err = os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	path := filepath.Join(directory, workerUnit(agent.entry.AgentID))
	env := strings.TrimSuffix(agent.workerPath, filepath.Ext(agent.workerPath)) + ".env"
	content := []byte(fmt.Sprintf("[Unit]\nDescription=OpenAgentX Agent %s\nAfter=openagentx.service\nWants=openagentx.service\n\n[Service]\nType=simple\nWorkingDirectory=\"%s\"\nEnvironment=\"PATH=%s/.local/bin:/usr/local/bin:/usr/bin:/bin\"\nEnvironmentFile=-\"%s\"\nExecStart=\"%s\" worker run --config \"%s\"\nRestart=on-failure\nRestartSec=3s\nUMask=0077\n\n[Install]\nWantedBy=default.target\n", agent.entry.AgentID, home, home, env, binary, agent.workerPath))
	identical, err := fleetmodel.CheckExactFile(path, content)
	if err != nil {
		return err
	}
	if !identical {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = f.Write(content)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	_, err = deps.RunSystemctl(ctx, "--user", "daemon-reload")
	return err
}

func prepareNetwork(ctx context.Context, api *agentAPI, attached consoleapi.AttachResponse, backend workerconfig.RuntimeBackendConfig, deps Dependencies) error {
	read := func() (openapi.NetworkOverviewResponse, error) {
		var v openapi.NetworkOverviewResponse
		err := api.call(ctx, http.MethodGet, openapi.ObserveNetworkProfilesPath, nil, &v)
		return v, err
	}
	current, err := read()
	if err != nil {
		return err
	}
	mode := backend.Network.Mode
	if mode == "" {
		mode = domain.NetworkInherit
	}
	var expectedVersion int64
	for _, b := range current.Bindings {
		if b.AgentID == attached.AgentID && b.BackendID == backend.BackendID {
			if networkApplied(b, attached) {
				return nil
			}
			// Preserve the published choice. A new generation needs a new
			// formal test and publish, guarded by the existing binding revision.
			mode, expectedVersion = b.Mode, b.Version
			break
		}
	}
	if mode != domain.NetworkInherit && mode != domain.NetworkDirect {
		return fmt.Errorf("命名网络配置尚不支持自动恢复；请在工作台为当前 Worker 重新测试并发布原绑定，现有绑定未修改")
	}
	{
		key := consoleclient.IdempotencyKey("agent-network")
		req := openapi.TestNetworkModeRequest{Meta: openapi.CommandMeta{IdempotencyKey: key + "-test", ExpectedVersion: expectedVersion}, AgentID: attached.AgentID, BackendID: backend.BackendID, Mode: mode, WorkerInstanceID: attached.WorkerInstanceID, Generation: attached.Generation}
		var receipt openapi.NetworkCommandResponse
		if err = api.call(ctx, http.MethodPost, openapi.ControlNetworkModeTestPath, req, &receipt); err != nil {
			return err
		}
		var test struct {
			TestID string `json:"test_id"`
		}
		if err = json.Unmarshal(receipt.Receipt, &test); err != nil {
			return err
		}
		if test.TestID == "" {
			return fmt.Errorf("network test returned no test_id")
		}
		for {
			current, err = read()
			if err != nil {
				return err
			}
			state := ""
			for _, t := range current.ModeTests {
				if t.ID == test.TestID {
					state = t.State
				}
			}
			if state == "succeeded" {
				break
			}
			if state == "failed" || state == "stale" {
				return fmt.Errorf("网络测试 %s %s；请在工作台检查网络", test.TestID, state)
			}
			if err = deps.Wait(ctx, time.Second); err != nil {
				return fmt.Errorf("等待网络测试: %w", err)
			}
		}
		publish := openapi.PublishNetworkModeRequest{Meta: openapi.CommandMeta{IdempotencyKey: key + "-publish", ExpectedVersion: expectedVersion}, TestID: test.TestID, WorkerInstanceID: attached.WorkerInstanceID, Generation: attached.Generation}
		if err = api.call(ctx, http.MethodPost, openapi.ControlNetworkModePublishPath, publish, &receipt); err != nil {
			return err
		}
	}
	for {
		current, err = read()
		if err != nil {
			return err
		}
		for _, b := range current.Bindings {
			if b.AgentID == attached.AgentID && b.BackendID == backend.BackendID {
				if networkApplied(b, attached) {
					return nil
				}
			}
		}
		if err = deps.Wait(ctx, time.Second); err != nil {
			return fmt.Errorf("等待当前代网络应用: %w", err)
		}
	}
}
func networkApplied(b domain.NetworkBinding, a consoleapi.AttachResponse) bool {
	return b.DesiredStatus == "applied" && b.AppliedWorkerID == a.WorkerInstanceID && b.AppliedGeneration == a.Generation && b.AppliedBindingRevision == b.Version && b.AppliedMode == b.Mode && b.AppliedProfileID == b.ProfileID && b.AppliedProfileVersion == b.ProfileVersion && b.AppliedPolicyVersion == b.PolicyVersion
}

func printAgentStatus(ctx context.Context, api *agentAPI, client consoleClient, o agentOptions, deps Dependencies) error {
	var overview map[string]json.RawMessage
	if err := api.call(ctx, http.MethodGet, openapi.ObserveOverviewPath, nil, &overview); err != nil {
		return err
	}
	var agents []map[string]any
	if err := json.Unmarshal(overview["agents"], &agents); err != nil {
		return err
	}
	var frame bytes.Buffer
	frameDeps := deps
	frameDeps.Out = &frame
	found := false
	for _, a := range agents {
		id, _ := a["agent_id"].(string)
		if o.id != "" && o.id != id {
			continue
		}
		found = true
		attached, err := client.Attach(ctx, id, consoleapi.ModeNormal)
		if err != nil {
			return err
		}
		a["pending_task"] = attached.SuggestedTask
		path := strings.Replace(consoleapi.AgentTasksPath, "{agentID}", url.PathEscape(id), 1) + "?limit=1"
		var page openapi.ConsoleTaskPage
		if err = api.call(ctx, http.MethodGet, path, nil, &page); err != nil {
			return err
		}
		var latest openapi.ConsoleTaskSnapshot
		if len(page.Tasks) > 0 {
			a["latest_task"] = page.Tasks[0]
			taskPath := strings.Replace(strings.Replace(consoleapi.AgentTaskPath, "{agentID}", url.PathEscape(id), 1), "{taskID}", url.PathEscape(page.Tasks[0].TaskID), 1)
			var task openapi.ConsoleTaskSnapshot
			if err = api.call(ctx, http.MethodGet, taskPath, nil, &task); err != nil {
				return err
			}
			a["latest_result"] = task
			latest = task
		}
		a["active_run"] = attached.ActiveRun
		a["open_command"] = "openagentx agent open " + id
		a["resume_command"] = "openagentx agent resume " + id
		if o.jsonOutput {
			if printJSON(frameDeps, a) != 0 {
				return fmt.Errorf("write Agent status")
			}
		} else {
			name, _ := a["display_name"].(string)
			if name == "" {
				name = id
			}
			reason := "状态未知"
			if readiness, ok := a["readiness"].(map[string]any); ok {
				if value, ok := readiness["reason"].(string); ok && value != "" {
					reason = value
				}
			}
			current := "无"
			if attached.ActiveRun != nil {
				current = attached.ActiveRun.TaskID + " · " + string(attached.ActiveRun.Status)
			} else if attached.SuggestedTask != nil {
				current = attached.SuggestedTask.TaskID + " · " + string(attached.SuggestedTask.Status)
			}
			result := "暂无结果"
			if latest.Task.Result != nil && strings.TrimSpace(*latest.Task.Result) != "" {
				result = *latest.Task.Result
			} else if latest.LatestRun != nil && latest.LatestRun.TurnResult != nil && latest.LatestRun.TurnResult.Body != "" {
				result = latest.LatestRun.TurnResult.Body
			} else if len(page.Tasks) > 0 {
				result = page.Tasks[0].TaskID + " · " + string(page.Tasks[0].Status)
			}
			fmt.Fprintf(&frame, "%s (%s) · %s\n  当前：%s\n  最近：%s\n  打开：openagentx agent open %s\n  恢复：openagentx agent resume %s\n\n", statusSummary(name, 48), id, statusSummary(reason, 72), statusSummary(current, 100), statusSummary(result, 120), id, id)
		}
	}
	if o.id != "" && !found {
		return fmt.Errorf("Agent %s 不存在", o.id)
	}
	if !found && !o.jsonOutput {
		fmt.Fprintln(&frame, "暂无 Agent；运行 openagentx agent add 添加。")
	}
	outputTTY := false
	if file, ok := deps.Out.(*os.File); ok {
		outputTTY = term.IsTerminal(int(file.Fd()))
	}
	return writeAgentStatusFrame(deps.Out, frame.Bytes(), o.watch && !o.jsonOutput && outputTTY)
}

func statusSummary(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	runes := []rune(strings.Join(strings.Fields(value), " "))
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return string(runes)
}
func writeAgentStatusFrame(out io.Writer, frame []byte, refresh bool) error {
	if refresh {
		if _, err := io.WriteString(out, "\x1b[H\x1b[2J"); err != nil {
			return err
		}
	}
	_, err := out.Write(frame)
	return err
}
func agentLink(ctx context.Context, o agentOptions, deps Dependencies) (string, error) {
	origin := o.webURL
	if origin == "" {
		output, err := deps.RunSystemctl(ctx, "--user", "show", "openagentx.service", "--property=ExecStart", "--value")
		if err != nil {
			return "", fmt.Errorf("无法确定工作台地址；使用 --web-url 或 OPENAGENTX_WEB_URL")
		}
		if !strings.Contains(output, "argv[]=") {
			return "", fmt.Errorf("无法确定工作台地址；使用 --web-url 或 OPENAGENTX_WEB_URL")
		}
		fields := strings.Fields(output)
		address := ":18100"
		for i, s := range fields {
			if strings.HasPrefix(s, "--http-addr=") {
				address = strings.TrimPrefix(s, "--http-addr=")
			}
			if s == "--http-addr" && i+1 < len(fields) {
				address = fields[i+1]
			}
		}
		address = strings.Trim(address, "\"'")
		if address == "${OPENAGENTX_HTTP_ADDR}" || address == "$OPENAGENTX_HTTP_ADDR" {
			pidText, pidErr := deps.RunSystemctl(ctx, "--user", "show", "openagentx.service", "--property=MainPID", "--value")
			pid, parseErr := strconv.Atoi(strings.TrimSpace(pidText))
			if pidErr != nil || parseErr != nil || pid <= 0 {
				return "", fmt.Errorf("daemon未运行，无法读取实际工作台地址；使用 --web-url")
			}
			address, err = serviceHTTPAddress(pid)
			if err != nil {
				return "", err
			}
		}
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return "", fmt.Errorf("无法解析工作台地址；使用 --web-url")
		}
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "127.0.0.1"
		}
		origin = "http://" + net.JoinHostPort(host, port)
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return "", fmt.Errorf("--web-url must be an HTTP(S) origin without credentials")
	}
	q := u.Query()
	q.Set("agent", o.id)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Read only the named, non-secret address from the actual service process.
// Never evaluate shell input or expose the surrounding environment.
func serviceHTTPAddress(pid int) (string, error) {
	file, err := os.Open(fmt.Sprintf("/proc/%d/environ", pid))
	if err != nil {
		return "", fmt.Errorf("无法读取daemon实际HTTP地址；使用 --web-url")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return "", fmt.Errorf("无法读取daemon实际HTTP地址；使用 --web-url")
	}
	for _, entry := range bytes.Split(data, []byte{0}) {
		if bytes.HasPrefix(entry, []byte("OPENAGENTX_HTTP_ADDR=")) {
			value := string(bytes.TrimPrefix(entry, []byte("OPENAGENTX_HTTP_ADDR=")))
			if value != "" && !strings.ContainsAny(value, "\r\n") {
				return value, nil
			}
			break
		}
	}
	return "", fmt.Errorf("daemon没有有效的OPENAGENTX_HTTP_ADDR；使用 --web-url")
}
