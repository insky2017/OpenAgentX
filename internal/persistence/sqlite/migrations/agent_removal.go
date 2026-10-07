package migrations

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
)

//go:embed 006_agent_removal.sql
var agentRemovalSchema string

func removalTriggerDefinitions() map[string]string {
	result := map[string]string{}
	for _, name := range []string{"event_journal_reject_delete", "network_mode_policies_reject_delete"} {
		start := strings.Index(agentRemovalSchema, "CREATE TRIGGER "+name)
		end := strings.Index(agentRemovalSchema[start:], "END;") + start + 3
		result[name] = agentRemovalSchema[start:end]
	}
	return result
}

func applyAgentRemoval(ctx context.Context, tx *sql.Tx, options migrationOptions) error {
	if _, err := tx.ExecContext(ctx, agentRemovalSchema); err != nil {
		return fmt.Errorf("agent removal schema: %w", err)
	}
	if options.beforeV6Commit != nil {
		if err := options.beforeV6Commit(); err != nil {
			return err
		}
	}
	return applyAgentSessions(ctx, tx, options)
}

func migrateV5ToV6(ctx context.Context, db *sql.DB, options migrationOptions) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRowContext(ctx, "SELECT version FROM schema_meta WHERE singleton=1").Scan(&version); err != nil {
		return err
	}
	if version != 5 {
		return ErrUnsupportedSchemaVersion
	}
	if err = validateBaseObjects(ctx, tx); err != nil {
		return err
	}
	if err = validateExternalObjects(ctx, tx); err != nil {
		return err
	}
	if err = applyAgentRemoval(ctx, tx, options); err != nil {
		return err
	}
	if err = validateObjects(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// ValidateAgentRemovalCompatible is read-only and accepts the deployed v5 schema.
func ValidateAgentRemovalCompatible(ctx context.Context, db *sql.DB) error {
	v, err := Version(ctx, db)
	if err != nil {
		return err
	}
	if v != 5 && v != 6 && v != 7 {
		return ErrUnsupportedSchemaVersion
	}
	if err = validateBaseObjects(ctx, db); err != nil {
		return err
	}
	if err = validateExternalObjects(ctx, db); err != nil {
		return err
	}
	if v >= 6 {
		return validateAgentRemoval(ctx, db)
	}
	return nil
}

func validateAgentRemoval(ctx context.Context, q schemaQueryer) error {
	for _, name := range []string{"agent_removal_guard", "agent_removal_scope", "agent_removal_receipts"} {
		start := strings.Index(agentRemovalSchema, "CREATE TABLE "+name)
		end := strings.Index(agentRemovalSchema[start:], ";") + start
		var actual string
		if err := q.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&actual); err != nil {
			return fmt.Errorf("%w: missing %s", ErrIncompleteSchema, name)
		}
		if compactSQL(actual) != compactSQL(agentRemovalSchema[start:end]) {
			return fmt.Errorf("%w: invalid %s", ErrIncompleteSchema, name)
		}
	}
	var n int
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM agent_removal_scope").Scan(&n); err != nil {
		return err
	}
	if n != 0 {
		return fmt.Errorf("%w: removal scope is not empty", ErrIncompleteSchema)
	}
	return nil
}
