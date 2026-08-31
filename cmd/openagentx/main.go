package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	openapi "openagentx/internal/api"
	adminapi "openagentx/internal/api/admin"
	apiauth "openagentx/internal/api/auth"
	"openagentx/internal/api/panel"
	"openagentx/internal/api/workerapi"
	webAuth "openagentx/internal/auth/web"
	admincli "openagentx/internal/cli/admin"
	workercli "openagentx/internal/cli/worker"
	"openagentx/internal/controlplane"
	"openagentx/internal/domain"
	openagentsqlite "openagentx/internal/persistence/sqlite"
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
		return admincli.ExecuteAgent(args[1:], admincli.DefaultDependencies())
	case "worker":
		return workercli.ExecuteOpenAgentX(args, workercli.RunWorkerProcess)
	case "serve":
		return runDaemon(args[1:])
	case "schema":
		return runSchema(args[1:])
	case "help", "--help", "-h":
		fmt.Fprintln(os.Stderr, "OpenAgentX - Agent Organization Control Plane")
		fmt.Fprintln(os.Stderr, "Usage: openagentx init --db <path>")
		fmt.Fprintln(os.Stderr, "       openagentx agent apply --db <path> --file <identity.yaml>")
		fmt.Fprintln(os.Stderr, "Usage: openagentx serve --db <path> --socket <path> [--http-addr :18100] [--web-dir web/dist]")
		fmt.Fprintln(os.Stderr, "       openagentx worker run --config <agent.yaml>")
		fmt.Fprintln(os.Stderr, "       openagentx schema verify --db <path>")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", args[0])
		return 1
	}
}

func runDaemon(args []string) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	dbPath := flags.String("db", "", "Target SQLite database path")
	socketPath := flags.String("socket", "", "Unix Socket path")
	httpAddr := flags.String("http-addr", ":18100", "Private HTTP listen address for Web Panel")
	webDir := flags.String("web-dir", "web/dist", "Built Web Panel directory")
	if err := flags.Parse(args); err != nil || *dbPath == "" || *socketPath == "" {
		fmt.Fprintln(os.Stderr, "Usage: openagentx serve --db <path> --socket <path> [--http-addr :18100] [--web-dir web/dist]")
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	repository, err := openagentsqlite.Open(ctx, *dbPath, openagentsqlite.Options{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "open database: %v\n", err)
		return 1
	}
	defer repository.Close()
	broker := controlplane.NewMemoryWakeupBroker()
	service, err := controlplane.NewWorkerService(repository, broker, controlplane.WorkerServiceOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "create Worker service: %v\n", err)
		return 1
	}
	if err := service.Reconcile(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "reconcile database: %v\n", err)
		return 1
	}
	handler, err := workerapi.NewHandler(service, workerapi.StaticPrincipal("daemon"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "create Worker API: %v\n", err)
		return 1
	}
	server, err := unixhttp.NewServer(*socketPath, handler, slog.Default())
	if err != nil {
		fmt.Fprintf(os.Stderr, "create Unix server: %v\n", err)
		return 1
	}
	authManager, err := loadAuthManager(ctx, repository)
	if err != nil {
		fmt.Fprintf(os.Stderr, "configure web users: %v\n", err)
		return 1
	}
	commands, err := controlplane.NewCommandService(repository, broker, time.Now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create command service: %v\n", err)
		return 1
	}
	panelHandler, err := panel.NewHandler(repository, commands, authManager)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create panel handler: %v\n", err)
		return 1
	}
	workerAdminService, err := controlplane.NewWorkerAdminService(repository, broker, time.Now, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create Worker admin service: %v\n", err)
		return 1
	}
	adminHandler, err := adminapi.NewHandler(workerAdminService, authManager)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create Worker admin handler: %v\n", err)
		return 1
	}
	authHandler := apiauth.NewHandler(authManager)
	webMux := http.NewServeMux()
	webMux.Handle(openapi.AuthLoginPath, authHandler)
	webMux.Handle(openapi.AuthLogoutPath, authHandler)
	webMux.Handle(openapi.AuthSessionPath, authHandler)
	webMux.Handle("/api/admin/", adminHandler)
	webMux.Handle("/api/", panelHandler)
	webFS, err := os.Open(filepath.Clean(*webDir))
	if err != nil {
		fmt.Fprintf(os.Stderr, "open web directory: %v\n", err)
		return 1
	}
	defer webFS.Close()
	webMux.Handle("/", staticHandler(filepath.Clean(*webDir)))
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
	flags := flag.NewFlagSet("schema verify", flag.ContinueOnError)
	dbPath := flags.String("db", "", "Target SQLite database path")
	if len(args) == 0 || args[0] != "verify" || flags.Parse(args[1:]) != nil || *dbPath == "" {
		fmt.Fprintln(os.Stderr, "Usage: openagentx schema verify --db <path>")
		return 2
	}
	db, err := openagentsqlite.Open(context.Background(), *dbPath, openagentsqlite.Options{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "schema verification failed: %v\n", err)
		return 1
	}
	defer db.Close()
	fmt.Printf("OpenAgentX schema v%d verified\n", 1)
	return 0
}
