package cli

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"agentbus/internal/client"
	"agentbus/internal/connector"
	"agentbus/internal/server"
	"agentbus/internal/service"
	"agentbus/internal/store"
)

func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	dbPath := fs.String("db", client.DefaultDBPath(), "SQLite database file path")
	socketPath := fs.String("socket", client.ResolveSocketPath(""), "Unix domain socket path")
	logLevelStr := fs.String("log-level", "info", "Log level (debug, info, warn, error)")

	if err := fs.Parse(ReorderArgs(args)); err != nil {
		return 1
	}

	var level slog.Level
	switch *logLevelStr {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	}))
	slog.SetDefault(logger)

	logger.Info("starting agentbus daemon",
		slog.String("db", *dbPath),
		slog.String("socket", *socketPath),
	)

	st, err := store.OpenSQLite(*dbPath)
	if err != nil {
		logger.Error("failed to open sqlite store", slog.String("error", err.Error()))
		return 1
	}
	defer st.Close()

	conn := connector.NewTmuxConnector(nil)
	svc := service.NewService(st, conn, logger)
	srv := server.NewServer(svc, *socketPath, logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := srv.Start(ctx); err != nil {
		logger.Error("server fatal error", slog.String("error", err.Error()))
		return 1
	}

	fmt.Fprintln(os.Stderr, "Daemon stopped cleanly.")
	return 0
}
