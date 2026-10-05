package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	openapi "openagentx/internal/api"
	adminapi "openagentx/internal/api/admin"
	apiauth "openagentx/internal/api/auth"
	consoleapi "openagentx/internal/api/console"
	externalapi "openagentx/internal/api/external"
	"openagentx/internal/api/panel"
	"openagentx/internal/api/workerapi"
	cliAuth "openagentx/internal/auth/cli"
	webAuth "openagentx/internal/auth/web"
	admincli "openagentx/internal/cli/admin"
	consolecli "openagentx/internal/cli/console"
	externalcli "openagentx/internal/cli/external"
	fleetcli "openagentx/internal/cli/fleet"
	overviewcli "openagentx/internal/cli/overview"
	workercli "openagentx/internal/cli/worker"
	"openagentx/internal/controlplane"
	"openagentx/internal/domain"
	"openagentx/internal/localprofile"
	"openagentx/internal/network/secretstore"
	openagentsqlite "openagentx/internal/persistence/sqlite"
	"openagentx/internal/persistence/sqlite/migrations"
	"openagentx/internal/transport/remotehttps"
	"openagentx/internal/transport/unixhttp"
)

func main() {
	os.Exit(execute(os.Args[1:]))
}

func execute(args []string) int {
	if len(args) == 0 {
		return workercli.ExecuteOpenAgentX(args, workercli.RunWorkerProcess)
	}
	switch args[0] {
	case "init":
		return admincli.ExecuteInit(args[1:], admincli.DefaultDependencies())
	case "agent":
		if len(args) > 1 && args[1] == "apply" {
			return admincli.ExecuteAgent(args[1:], admincli.DefaultDependencies())
		}
		return fleetcli.ExecuteAgent(args[1:], fleetcli.DefaultDependencies())
	case "worker":
		return workercli.ExecuteOpenAgentX(args, workercli.RunWorkerProcess)
	case "overview":
		return overviewcli.Execute(args[1:], overviewcli.DefaultDependencies())
	case "console":
		return consolecli.Execute(args[1:], consolecli.DefaultDependencies())
	case "external":
		return externalcli.Execute(args[1:], os.Stdout, os.Stderr)
	case "collaborate":
		return externalcli.ExecuteCollaboration(args[1:], os.Stdout, os.Stderr)
	case "fleet":
		return fleetcli.Execute(args[1:], fleetcli.DefaultDependencies())
	case "serve":
		return runDaemon(args[1:])
	case "schema":
		return runSchema(args[1:])
	case "help", "--help", "-h":
		fmt.Fprintln(os.Stderr, "OpenAgentX - Agent Organization Control Plane")
		fmt.Fprintln(os.Stderr, "Usage: openagentx init [--db <path>]")
		fmt.Fprintln(os.Stderr, "       openagentx agent <add|join|open|status|pause|resume> [agent] [flags]")
		fmt.Fprintln(os.Stderr, "       openagentx agent apply [--db <path>] --file <identity.yaml>")
		fmt.Fprintln(os.Stderr, "       openagentx serve [--db <path>] [--socket <path>] [--http-addr :18100] [--web-dir web/dist]")
		fmt.Fprintln(os.Stderr, "       optional remote Worker HTTPS: --worker-https-addr :18101 --worker-mtls-ca <ca.pem> --worker-mtls-cert <server.pem> --worker-mtls-key <server.key> --worker-mtls-binding <principal=agent[,agent...]>")
		fmt.Fprintln(os.Stderr, "       openagentx worker run --config <agent.yaml>")
		fmt.Fprintln(os.Stderr, "       openagentx console [login|logout|attach]")
		fmt.Fprintln(os.Stderr, "       openagentx overview [--socket PATH] [--credentials PATH] [--worker-dir PATH]")
		fmt.Fprintln(os.Stderr, "       openagentx external <bind|status|send|reply|inbox|ack|revoke> [flags] (existing host, messages only)")
		fmt.Fprintln(os.Stderr, "       openagentx collaborate <enable|ask|status|instructions|inbox|roles|disable> [flags] (managed consultation and continuation)")
		fmt.Fprintln(os.Stderr, "       openagentx fleet <init|workspace|up|status|down|force-stop> [--file <fleet.yaml>] [--db <path>] [--socket <path>] [--worker-dir <dir>] [--credentials <path>]")
		fmt.Fprintln(os.Stderr, "       openagentx schema verify [--db <path>]")
		fmt.Fprintln(os.Stderr, "Local path precedence: explicit flag > resource environment > OPENAGENTX_HOME > ~/.openagentx")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", args[0])
		return 1
	}
}

func runDaemon(args []string) int {
	options, err := parseServeOptions(args, os.Stderr)
	if err == flag.ErrHelp {
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "serve configuration: %v\n", err)
		serveUsage(os.Stderr)
		return 2
	}
	dbPath := &options.dbPath
	networkSecretDir := &options.networkSecretDir
	socketPath := &options.socketPath
	httpAddr := &options.httpAddr
	webDir := &options.webDir
	workerHTTPSAddr := &options.workerHTTPSAddr
	workerMTLSCA := &options.workerMTLSCA
	workerMTLSCert := &options.workerMTLSCert
	workerMTLSKey := &options.workerMTLSKey
	workerBindings := options.workerBindings

	if *workerHTTPSAddr == "" && (*workerMTLSCA != "" || *workerMTLSCert != "" || *workerMTLSKey != "" || len(workerBindings) != 0) {
		fmt.Fprintln(os.Stderr, "remote Worker mTLS options require --worker-https-addr")
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	absoluteDBPath, err := filepath.Abs(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve database path: %v\n", err)
		return 1
	}
	repository, err := openagentsqlite.Open(ctx, absoluteDBPath, openagentsqlite.Options{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "open database: %v\n", err)
		return 1
	}
	defer repository.Close()
	broker := controlplane.NewMemoryWakeupBroker()
	secretDirectory := strings.TrimSpace(*networkSecretDir)
	if secretDirectory == "" {
		secretDirectory = absoluteDBPath + ".network-secrets"
	}
	secrets, err := secretstore.Open(secretDirectory)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open network secret store: %v\n", err)
		return 1
	}
	networkWorkflow, err := controlplane.NewNetworkWorkflowService(repository, secrets, broker, time.Now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create network workflow service: %v\n", err)
		return 1
	}
	service, err := controlplane.NewWorkerService(repository, broker, controlplane.WorkerServiceOptions{NetworkWorkflow: networkWorkflow})
	if err != nil {
		fmt.Fprintf(os.Stderr, "create Worker service: %v\n", err)
		return 1
	}
	if err := service.Reconcile(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "reconcile database: %v\n", err)
		return 1
	}
	// Lease expiry also happens while the daemon remains alive. Reuse the
	// same conservative transaction and stop it before closing the repository.
	recoveryContext, stopRecovery := context.WithCancel(ctx)
	recoveryDone := make(chan struct{})
	go func() {
		defer close(recoveryDone)
		reconcilePeriodically(recoveryContext, 10*time.Second, service.Reconcile, slog.Default())
	}()
	defer func() {
		stopRecovery()
		<-recoveryDone
	}()
	handler, err := workerapi.NewHandler(service, workerapi.StaticPrincipal("daemon"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "create Worker API: %v\n", err)
		return 1
	}
	if *workerHTTPSAddr != "" {
		if *workerMTLSCA == "" || *workerMTLSCert == "" || *workerMTLSKey == "" || len(workerBindings) == 0 {
			fmt.Fprintln(os.Stderr, "remote Worker HTTPS requires mTLS files and at least one --worker-mtls-binding")
			return 2
		}
		principalResolver, err := remotehttps.NewCertificatePrincipalResolver(workerBindings)
		if err != nil {
			fmt.Fprintf(os.Stderr, "configure remote Worker mTLS bindings: %v\n", err)
			return 1
		}
		remoteHandler, err := workerapi.NewHandler(service, principalResolver)
		if err != nil {
			fmt.Fprintf(os.Stderr, "create remote Worker API: %v\n", err)
			return 1
		}
		remoteServer, err := remotehttps.NewConfiguredServer(remotehttps.ServerConfig{
			ListenAddr: *workerHTTPSAddr, CAFile: *workerMTLSCA, CertFile: *workerMTLSCert, KeyFile: *workerMTLSKey,
		}, remoteHandler, slog.Default())
		if err != nil {
			fmt.Fprintf(os.Stderr, "configure remote Worker HTTPS: %v\n", err)
			return 1
		}
		go func() {
			if err := remoteServer.Start(ctx); err != nil {
				slog.Error("remote Worker HTTPS stopped", "error", err)
			}
		}()
		if err := remoteServer.WaitReady(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "start remote Worker HTTPS: %v\n", err)
			return 1
		}
	}
	authManager, err := loadAuthManager(ctx, repository)
	if err != nil {
		fmt.Fprintf(os.Stderr, "configure web users: %v\n", err)
		return 1
	}
	cliAuthService, err := cliAuth.NewService(repository, cliAuth.Config{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "configure CLI authentication: %v\n", err)
		return 1
	}
	commands, err := controlplane.NewCommandService(repository, broker, time.Now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create command service: %v\n", err)
		return 1
	}
	webPanelHandler, err := panel.NewHandler(repository, commands, authManager, networkWorkflow)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create panel handler: %v\n", err)
		return 1
	}
	workerAdminService, err := controlplane.NewWorkerAdminService(repository, broker, time.Now, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create Worker admin service: %v\n", err)
		return 1
	}
	webAdminHandler, err := adminapi.NewHandler(workerAdminService, authManager)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create Worker admin handler: %v\n", err)
		return 1
	}
	webConsoleHandler, err := consoleapi.NewHandler(repository, authManager)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create Console attach handler: %v\n", err)
		return 1
	}
	cliPanelHandler, err := panel.NewCLIHandler(repository, commands, cliAuthService, networkWorkflow)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create CLI panel handler: %v\n", err)
		return 1
	}
	cliAdminHandler, err := adminapi.NewCLIHandler(workerAdminService, cliAuthService)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create CLI Worker admin handler: %v\n", err)
		return 1
	}
	cliConsoleHandler, err := consoleapi.NewCLIHandler(repository, cliAuthService)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create CLI Console attach handler: %v\n", err)
		return 1
	}
	webAuthHandler := apiauth.NewHandler(authManager)
	cliAuthHandler, err := apiauth.NewCLIHandler(cliAuthService)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create CLI auth handler: %v\n", err)
		return 1
	}
	unixMux := newUnixMux(handler, cliAuthHandler, cliConsoleHandler, cliAdminHandler, cliPanelHandler)
	externalService, err := controlplane.NewExternalSessionService(repository, broker, time.Now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create external session service: %v\n", err)
		return 1
	}
	externalHandler, err := externalapi.NewHandler(externalService, cliAuthService)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create external session handler: %v\n", err)
		return 1
	}
	// Agent communication is local UDS only; no additional public listener.
	unixMux.Handle(externalapi.Prefix, externalHandler)
	server, err := unixhttp.NewServer(*socketPath, unixMux, slog.Default())
	if err != nil {
		fmt.Fprintf(os.Stderr, "create Unix server: %v\n", err)
		return 1
	}
	webFS, err := os.Open(filepath.Clean(*webDir))
	if err != nil {
		fmt.Fprintf(os.Stderr, "open web directory: %v\n", err)
		return 1
	}
	defer webFS.Close()
	webMux := newWebMux(webAuthHandler, webAdminHandler, webConsoleHandler, webPanelHandler, staticHandler(filepath.Clean(*webDir)))
	httpServer := &http.Server{
		Addr:              *httpAddr,
		Handler:           webMux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       40 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("web panel stopped", "error", err)
		}
	}()
	if err := server.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "daemon stopped: %v\n", err)
		return 1
	}
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	_ = httpServer.Shutdown(shutdownCtx)
	return 0
}

func newUnixMux(worker, cliAuthentication, console, admin, panelHandler http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/api/v1/", worker)
	mux.Handle("/api/auth/v1/cli/", cliAuthentication)
	mux.Handle("/api/console/", console)
	mux.Handle("/api/admin/", admin)
	mux.Handle("/api/observe/", panelHandler)
	mux.Handle("/api/control/", panelHandler)
	return mux
}

func newWebMux(authentication, admin, console, panelHandler, static http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/api/auth/v1/cli/", http.NotFoundHandler())
	mux.Handle(openapi.AuthLoginPath, authentication)
	mux.Handle(openapi.AuthLogoutPath, authentication)
	mux.Handle(openapi.AuthSessionPath, authentication)
	mux.Handle("/api/admin/", admin)
	mux.Handle("/api/console/", console)
	mux.Handle("/api/", panelHandler)
	mux.Handle("/", static)
	return mux
}

type serveOptions struct {
	dbPath, networkSecretDir, socketPath, httpAddr, webDir       string
	workerHTTPSAddr, workerMTLSCA, workerMTLSCert, workerMTLSKey string
	workerBindings                                               bindingFlags
}

func parseServeOptions(args []string, output io.Writer) (serveOptions, error) {
	var options serveOptions
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(output)
	var databaseFlag localprofile.PathFlag
	var socketFlag localprofile.PathFlag
	flags.Var(&databaseFlag, "db", localprofile.PathUsage(localprofile.DatabasePath, "Target SQLite database path"))
	flags.StringVar(&options.networkSecretDir, "network-secret-dir", "", "Network secret directory (default: <absolute-db-path>.network-secrets)")
	flags.Var(&socketFlag, "socket", localprofile.PathUsage(localprofile.SocketPath, "Unix Socket path"))
	flags.StringVar(&options.httpAddr, "http-addr", ":18100", "Private HTTP listen address for Web Panel")
	flags.StringVar(&options.webDir, "web-dir", "web/dist", "Built Web Panel directory")
	flags.StringVar(&options.workerHTTPSAddr, "worker-https-addr", "", "Optional HTTPS listen address for remote Workers")
	flags.StringVar(&options.workerMTLSCA, "worker-mtls-ca", "", "Remote Worker mTLS client CA PEM")
	flags.StringVar(&options.workerMTLSCert, "worker-mtls-cert", "", "Remote Worker mTLS server certificate PEM")
	flags.StringVar(&options.workerMTLSKey, "worker-mtls-key", "", "Remote Worker mTLS server private key")
	flags.Var(&options.workerBindings, "worker-mtls-binding", "mTLS principal to Agent binding (principal=agent[,agent...]); repeatable")
	if err := flags.Parse(args); err != nil {
		return serveOptions{}, err
	}
	if flags.NArg() != 0 {
		return serveOptions{}, fmt.Errorf("serve does not accept positional arguments")
	}
	resolver := localprofile.DefaultResolver()
	databasePath, err := resolver.Resolve(localprofile.DatabasePath, databaseFlag.Override())
	if err != nil {
		return serveOptions{}, fmt.Errorf("resolve database: %w", err)
	}
	socketPath, err := resolver.Resolve(localprofile.SocketPath, socketFlag.Override())
	if err != nil {
		return serveOptions{}, fmt.Errorf("resolve socket: %w", err)
	}
	if err := localprofile.EnsureDistinct(map[string]string{"database": databasePath.Path, "socket": socketPath.Path}); err != nil {
		return serveOptions{}, err
	}
	options.dbPath = databasePath.Path
	options.socketPath = socketPath.Path
	return options, nil
}

func serveUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage: openagentx serve [--db <path>] [--socket <path>] [--http-addr :18100] [--web-dir web/dist]")
	fmt.Fprintf(writer, "Default database source: $%s > $%s > ~/.openagentx/data/openagentx.db\n", localprofile.EnvDatabasePath, localprofile.EnvHome)
	fmt.Fprintf(writer, "Default socket source: $%s > $%s > ~/.openagentx/run/openagentx.sock\n", localprofile.EnvSocketPath, localprofile.EnvHome)
}

type bindingFlags map[string][]string

func (f *bindingFlags) String() string { return fmt.Sprint(map[string][]string(*f)) }

func (f *bindingFlags) Set(value string) error {
	parts := strings.SplitN(value, "=", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return fmt.Errorf("binding must be principal=agent[,agent...]")
	}
	if *f == nil {
		*f = make(bindingFlags)
	}
	principal := strings.TrimSpace(parts[0])
	for _, rawAgent := range strings.Split(parts[1], ",") {
		agent := strings.TrimSpace(rawAgent)
		if agent == "" {
			return fmt.Errorf("binding contains empty Agent")
		}
		(*f)[principal] = append((*f)[principal], agent)
	}
	return nil
}

type webUserSource interface {
	ListWebUsers(context.Context) ([]domain.WebUserRecord, error)
}

func loadAuthManager(ctx context.Context, source webUserSource) (*webAuth.Manager, error) {
	users, err := source.ListWebUsers(ctx)
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, fmt.Errorf("OpenAgentX is not initialized; run openagentx init --db <path>")
	}
	store, ok := source.(webAuth.SessionStore)
	if !ok {
		return nil, fmt.Errorf("web user source does not provide persistent Web Sessions")
	}
	manager := webAuth.NewManager(webAuth.Config{Store: store})
	for _, record := range users {
		roles := make([]webAuth.Role, 0, len(record.Roles))
		for _, role := range record.Roles {
			switch role {
			case domain.WebRoleOwner:
				roles = append(roles, webAuth.RoleOwner)
			case domain.WebRoleOperator:
				roles = append(roles, webAuth.RoleOperator)
			case domain.WebRoleViewer:
				roles = append(roles, webAuth.RoleViewer)
			default:
				return nil, fmt.Errorf("web user %q has unsupported role %q", record.Username, role)
			}
		}
		if err := manager.AddUser(webAuth.User{ID: record.PrincipalID, WebUserID: record.ID, Username: record.Username, Roles: roles, PasswordDigest: record.PasswordDigest}); err != nil {
			return nil, fmt.Errorf("load web user %q: %w", record.Username, err)
		}
	}
	return manager, nil
}

func staticHandler(directory string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Clean(r.URL.Path)
		if path == "." || path == "/" {
			path = "/index.html"
		}
		if _, err := os.Stat(filepath.Join(directory, filepath.FromSlash(path))); err != nil {
			path = "/index.html"
		}
		r.URL.Path = path
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self'; manifest-src 'self'; object-src 'none'; script-src 'self'; style-src 'self'; worker-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		content, err := os.ReadFile(filepath.Join(directory, filepath.FromSlash(path)))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if filepath.Ext(path) == ".html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		} else if filepath.Ext(path) == ".js" {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		} else if filepath.Ext(path) == ".css" {
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		} else if filepath.Ext(path) == ".webmanifest" {
			w.Header().Set("Content-Type", "application/manifest+json")
		}
		_, _ = w.Write(content)
	})
}

func runSchema(args []string) int {
	dbPath, err := parseSchemaDatabase(args, os.Stderr)
	if err == flag.ErrHelp {
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "schema configuration: %v\n", err)
		schemaUsage(os.Stderr)
		return 2
	}
	db, err := openagentsqlite.Open(context.Background(), dbPath, openagentsqlite.Options{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "schema verification failed: %v\n", err)
		return 1
	}
	defer db.Close()
	fmt.Printf("OpenAgentX schema v%d verified\n", migrations.CurrentVersion)
	return 0
}

func parseSchemaDatabase(args []string, output io.Writer) (string, error) {
	flags := flag.NewFlagSet("schema verify", flag.ContinueOnError)
	flags.SetOutput(output)
	var databaseFlag localprofile.PathFlag
	flags.Var(&databaseFlag, "db", localprofile.PathUsage(localprofile.DatabasePath, "Target SQLite database path"))
	if len(args) == 0 || args[0] != "verify" {
		return "", fmt.Errorf("schema subcommand must be verify")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return "", err
	}
	if flags.NArg() != 0 {
		return "", fmt.Errorf("schema verify does not accept positional arguments")
	}
	resolved, err := localprofile.DefaultResolver().Resolve(localprofile.DatabasePath, databaseFlag.Override())
	if err != nil {
		return "", fmt.Errorf("resolve database: %w", err)
	}
	return resolved.Path, nil
}

func schemaUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage: openagentx schema verify [--db <path>]")
	fmt.Fprintf(writer, "Default database source: $%s > $%s > ~/.openagentx/data/openagentx.db\n", localprofile.EnvDatabasePath, localprofile.EnvHome)
}
