package migrations

import (
	"context"
	"errors"
	"testing"
)

func TestAgentSessionMigrationPreservesV6AndRollsBack(t *testing.T) {
	db := openIntentMigrationDB(t)
	ctx := context.Background()
	if _, err := db.Exec(targetSchema + externalSessionSchema + externalRolesSchema + managedCollaborationSchema + agentRemovalSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO installation_metadata VALUES(1,'preserved-installation','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("v7 commit failure")
	if err := apply(ctx, db, migrationOptions{beforeV7Commit: func() error { return injected }}); !errors.Is(err, injected) {
		t.Fatal(err)
	}
	if v, err := Version(ctx, db); err != nil || v != 6 {
		t.Fatal("migration escaped rollback", v, err)
	}
	if exists, err := tableExists(ctx, db, "agent_sessions"); err != nil || exists {
		t.Fatal("new table escaped rollback", err)
	}
	if err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCurrent(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP INDEX uq_agent_session_pending`); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCurrent(ctx, db); !errors.Is(err, ErrIncompleteSchema) {
		t.Fatal("missing pending exclusion accepted", err)
	}
}
