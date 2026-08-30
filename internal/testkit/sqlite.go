package testkit

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"agentbus/internal/persistence/sqlite/migrations"
	_ "github.com/mattn/go-sqlite3"
)

func OpenTargetSQLite(tb testing.TB) *sql.DB {
	tb.Helper()
	dbPath := filepath.Join(tb.TempDir(), "openagentx.db")
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON")
	if err != nil {
		tb.Fatalf("open target sqlite: %v", err)
	}
	db.SetMaxOpenConns(4)
	if err := migrations.Apply(context.Background(), db); err != nil {
		_ = db.Close()
		tb.Fatalf("apply target migrations: %v", err)
	}
	tb.Cleanup(func() { _ = db.Close() })
	return db
}
