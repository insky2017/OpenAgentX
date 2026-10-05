package migrations

import (
	"context"
	"errors"
	"testing"
)

func TestAgentRemovalV6MigrationRollbackAndTriggerValidation(t *testing.T) {
	db := openIntentMigrationDB(t)
	ctx := context.Background()
	if _, err := db.Exec(targetSchema + externalSessionSchema + externalRolesSchema + managedCollaborationSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO installation_metadata VALUES(1,'stable-installation','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	failed := errors.New("v6 fail before commit")
	if err := apply(ctx, db, migrationOptions{beforeV6Commit: func() error { return failed }}); !errors.Is(err, failed) {
		t.Fatal("missing failure", err)
	}
	if v, err := Version(ctx, db); err != nil || v != 5 {
		t.Fatal("migration partially committed", v, err)
	}
	if exists, err := tableExists(ctx, db, "agent_removal_scope"); err != nil || exists {
		t.Fatal("scope table survived rollback", err)
	}
	if err := ValidateAgentRemovalCompatible(ctx, db); err != nil {
		t.Fatal("v5 protections changed", err)
	}
	if err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TRIGGER event_journal_reject_delete; CREATE TRIGGER event_journal_reject_delete BEFORE DELETE ON event_journal BEGIN SELECT 1; END`); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCurrent(ctx, db); !errors.Is(err, ErrIncompleteSchema) {
		t.Fatal("weakened trigger accepted", err)
	}
}
