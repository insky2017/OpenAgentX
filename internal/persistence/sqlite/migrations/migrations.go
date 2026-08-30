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
)

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
		return nil
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
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit target schema v%d: %w", CurrentVersion, err)
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
