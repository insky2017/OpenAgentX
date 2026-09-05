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
	"network_profiles", "network_profile_bindings",
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
		// ADR-002 adds these tables without invalidating existing v1 databases.
		// Apply them transactionally before validating the current schema so an
		// upgrade from an already deployed v1 remains usable.
		if err := ensureNetworkTables(ctx, db); err != nil {
			return err
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

func ensureNetworkTables(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin network schema upgrade: %w", err)
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS network_profiles (
			profile_id TEXT NOT NULL, version INTEGER NOT NULL CHECK (version > 0),
			status TEXT NOT NULL CHECK (status IN ('draft', 'published')),
			mode TEXT NOT NULL CHECK (mode IN ('only_http_proxy', 'only_socks5')),
			host TEXT NOT NULL, port INTEGER NOT NULL CHECK (port BETWEEN 1 AND 65535),
			config_file TEXT, secret_ref TEXT, created_by TEXT NOT NULL REFERENCES principals(principal_id),
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL, PRIMARY KEY (profile_id, version)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_network_profiles_latest ON network_profiles(profile_id, version DESC)`,
		`CREATE TABLE IF NOT EXISTS network_profile_bindings (
			agent_id TEXT NOT NULL REFERENCES agents(agent_id) ON DELETE CASCADE, backend_id TEXT NOT NULL,
			profile_id TEXT NOT NULL, profile_version INTEGER NOT NULL, version INTEGER NOT NULL CHECK (version > 0),
			desired_status TEXT NOT NULL CHECK (desired_status IN ('pending', 'applied', 'failed')),
			applied_worker_id TEXT, applied_generation INTEGER, applied_profile_version INTEGER, applied_binding_revision INTEGER, diagnostic TEXT, updated_at TEXT NOT NULL,
			PRIMARY KEY (agent_id, backend_id), FOREIGN KEY (profile_id, profile_version) REFERENCES network_profiles(profile_id, version)
		)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply network schema upgrade: %w", err)
		}
	}
	// v1 databases may already contain network_profile_bindings created before
	// the diagnostic field was introduced. CREATE TABLE IF NOT EXISTS does not
	// alter that table, so add the nullable column explicitly when absent.
	columns := map[string]bool{}
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(network_profile_bindings)`)
	if err != nil {
		return fmt.Errorf("inspect network binding schema: %w", err)
	}
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return fmt.Errorf("scan network binding schema: %w", err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read network binding schema: %w", err)
	}
	rows.Close()
	if !columns["diagnostic"] {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE network_profile_bindings ADD COLUMN diagnostic TEXT`); err != nil {
			return fmt.Errorf("add network binding diagnostic column: %w", err)
		}
	}
	if !columns["applied_binding_revision"] {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE network_profile_bindings ADD COLUMN applied_binding_revision INTEGER`); err != nil {
			return fmt.Errorf("add applied binding revision column: %w", err)
		}
	}
	var hasBackendTable, hasBackendNetwork bool
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) > 0 FROM sqlite_master WHERE type='table' AND name='runtime_backend_registrations'`).Scan(&hasBackendTable); err != nil {
		return fmt.Errorf("inspect Backend registration table: %w", err)
	}
	backendRows, err := tx.QueryContext(ctx, `PRAGMA table_info(runtime_backend_registrations)`)
	if err != nil {
		return fmt.Errorf("inspect Backend registration schema: %w", err)
	}
	for backendRows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := backendRows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			backendRows.Close()
			return fmt.Errorf("scan Backend registration schema: %w", err)
		}
		if name == "network_json" {
			hasBackendNetwork = true
		}
	}
	if err := backendRows.Err(); err != nil {
		backendRows.Close()
		return fmt.Errorf("read Backend registration schema: %w", err)
	}
	backendRows.Close()
	if hasBackendTable && !hasBackendNetwork {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE runtime_backend_registrations ADD COLUMN network_json TEXT NOT NULL DEFAULT '{}'`); err != nil {
			return fmt.Errorf("add Backend network policy column: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit network schema upgrade: %w", err)
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
