package migrations

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
)

const CurrentVersion = 1

var (
	ErrIncompatibleLegacySchema = errors.New("database contains a legacy schema without OpenAgentX schema metadata")
	ErrUnsupportedSchemaVersion = errors.New("unsupported OpenAgentX schema version")
	ErrIncompleteSchema         = errors.New("OpenAgentX schema is incomplete or corrupted")
)

var requiredTables = []string{
	"schema_meta", "principals", "organizations", "org_units", "roles", "positions", "agents",
	"position_assignments", "reporting_lines", "authority_policies", "agent_profiles", "execution_profiles",
	"worker_instances", "runtime_backend_registrations", "tasks", "messages", "run_attempts", "session_bindings",
	"workspace_leases", "approval_requests", "approval_decisions", "mailbox_items", "worker_commands", "artifacts",
	"event_journal", "web_users", "web_sessions",
}

var requiredTriggers = []string{"event_journal_reject_update", "event_journal_reject_delete"}

//go:embed 001_target_schema.sql
var targetSchema string

func Apply(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("enable foreign keys: %w", err)
	}

	hasMeta, err := tableExists(ctx, db, "schema_meta")
	if err != nil {
		return err
	}
	if hasMeta {
		version, err := Version(ctx, db)
		if err != nil {
			return err
		}
		if version != CurrentVersion {
			return fmt.Errorf("%w: got %d, want %d", ErrUnsupportedSchemaVersion, version, CurrentVersion)
		}
		return ValidateCurrent(ctx, db)
	}

	nonSystemTables, err := countNonSystemTables(ctx, db)
	if err != nil {
		return err
	}
	if nonSystemTables != 0 {
		return ErrIncompatibleLegacySchema
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema migration: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, targetSchema); err != nil {
		return fmt.Errorf("apply target schema v%d: %w", CurrentVersion, err)
	}
	if err := validateObjects(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit target schema v%d: %w", CurrentVersion, err)
	}
	return nil
}

type schemaQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func ValidateCurrent(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	version, err := Version(ctx, db)
	if err != nil {
		return err
	}
	if version != CurrentVersion {
		return fmt.Errorf("%w: got %d, want %d", ErrUnsupportedSchemaVersion, version, CurrentVersion)
	}
	return validateObjects(ctx, db)
}

func validateObjects(ctx context.Context, queryer schemaQueryer) error {
	for _, table := range requiredTables {
		if err := requireSchemaObject(ctx, queryer, "table", table); err != nil {
			return err
		}
	}
	for _, trigger := range requiredTriggers {
		if err := requireSchemaObject(ctx, queryer, "trigger", trigger); err != nil {
			return err
		}
	}
	return nil
}

func requireSchemaObject(ctx context.Context, queryer schemaQueryer, objectType string, name string) error {
	var count int
	if err := queryer.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type = ? AND name = ?", objectType, name,
	).Scan(&count); err != nil {
		return fmt.Errorf("inspect schema object %s %s: %w", objectType, name, err)
	}
	if count != 1 {
		return fmt.Errorf("%w: missing %s %s", ErrIncompleteSchema, objectType, name)
	}
	return nil
}

func Version(ctx context.Context, db *sql.DB) (int, error) {
	var version int
	if err := db.QueryRowContext(ctx, "SELECT version FROM schema_meta WHERE singleton = 1").Scan(&version); err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return version, nil
}

func tableExists(ctx context.Context, db *sql.DB, table string) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table,
	).Scan(&count); err != nil {
		return false, fmt.Errorf("inspect sqlite schema: %w", err)
	}
	return count != 0, nil
}

func countNonSystemTables(ctx context.Context, db *sql.DB) (int, error) {
	var count int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'",
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count existing tables: %w", err)
	}
	return count, nil
}
