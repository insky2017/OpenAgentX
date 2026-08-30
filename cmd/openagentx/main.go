package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"openagentx/internal/api/workerapi"
	workercli "openagentx/internal/cli/worker"
	"openagentx/internal/controlplane"
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
	case "worker":
		return workercli.ExecuteOpenAgentX(args, workercli.RunWorkerProcess)
	case "daemon":
		return runDaemon(args[1:])
	case "schema":
		return runSchema(args[1:])
	case "help", "--help", "-h":
		fmt.Fprintln(os.Stderr, "OpenAgentX - Agent Organization Control Plane")
		fmt.Fprintln(os.Stderr, "Usage: openagentx daemon --db <path> --socket <path>")
		fmt.Fprintln(os.Stderr, "       openagentx worker run --config <agent.yaml>")
		fmt.Fprintln(os.Stderr, "       openagentx schema verify --db <path>")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", args[0])
		return 1
	}
}

func runDaemon(args []string) int {
	flags := flag.NewFlagSet("daemon", flag.ContinueOnError)
	dbPath := flags.String("db", "", "Target SQLite database path")
	socketPath := flags.String("socket", "", "Unix Socket path")
	if err := flags.Parse(args); err != nil || *dbPath == "" || *socketPath == "" {
		fmt.Fprintln(os.Stderr, "Usage: openagentx daemon --db <path> --socket <path>")
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
	service, err := controlplane.NewWorkerService(repository, controlplane.NewMemoryWakeupBroker(), controlplane.WorkerServiceOptions{})
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
	if err := server.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "daemon stopped: %v\n", err)
		return 1
	}
	return 0
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
