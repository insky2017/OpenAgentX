package migrations

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
)

//go:embed 007_agent_sessions.sql
var agentSessionSchema string

func applyAgentSessions(ctx context.Context, tx *sql.Tx, options migrationOptions) error {
	if _, err := tx.ExecContext(ctx, agentSessionSchema); err != nil {
		return fmt.Errorf("agent session schema: %w", err)
	}
	if options.beforeV7Commit != nil {
		return options.beforeV7Commit()
	}
	return nil
}
func migrateV6ToV7(ctx context.Context, db *sql.DB, options migrationOptions) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRowContext(ctx, "SELECT version FROM schema_meta WHERE singleton=1").Scan(&version); err != nil {
		return err
	}
	if version != 6 {
		return ErrUnsupportedSchemaVersion
	}
	if err = validateBaseObjects(ctx, tx); err != nil {
		return err
	}
	if err = validateExternalObjects(ctx, tx); err != nil {
		return err
	}
	if err = validateAgentRemoval(ctx, tx); err != nil {
		return err
	}
	if err = applyAgentSessions(ctx, tx, options); err != nil {
		return err
	}
	if err = validateObjects(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}
func validateAgentSessions(ctx context.Context, q schemaQueryer) error {
	for _, name := range []string{"agent_sessions", "uq_agent_session_pending"} {
		var actual string
		if err := q.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE name=?", name).Scan(&actual); err != nil {
			return fmt.Errorf("%w: missing %s", ErrIncompleteSchema, name)
		}
		found := false
		for _, statement := range strings.Split(agentSessionSchema, ";") {
			if compactSQL(statement) == compactSQL(actual) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%w: invalid %s", ErrIncompleteSchema, name)
		}
	}
	return nil
}
