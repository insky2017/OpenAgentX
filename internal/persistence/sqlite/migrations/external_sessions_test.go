package migrations

import (
	"context"
	"errors"
	"testing"
)

func TestExternalV2MigrationIsAtomicPreservesIdentityAndValidatesGuards(t *testing.T) {
	ctx := context.Background()
	db := openIntentMigrationDB(t)
	if _, err := db.Exec(targetSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO installation_metadata VALUES(1,'existing-installation','2026-10-03T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := apply(ctx, db, migrationOptions{beforeV3Commit: func() error { return errors.New("stop external migration") }}); err == nil {
		t.Fatal("faulted migration succeeded")
	}
	version, err := Version(ctx, db)
	if err != nil || version != 2 {
		t.Fatalf("rollback version=%d err=%v", version, err)
	}
	if exists, err := tableExists(ctx, db, "external_messages"); err != nil || exists {
		t.Fatalf("partial external schema exists=%v err=%v", exists, err)
	}
	if err = Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err = ValidateCurrent(ctx, db); err != nil {
		t.Fatal(err)
	}
	var installation string
	if err = db.QueryRow(`SELECT installation_id FROM installation_metadata`).Scan(&installation); err != nil || installation != "existing-installation" {
		t.Fatal("installation identity changed")
	}
	if _, err = db.Exec(`DROP TRIGGER session_update_external_guard`); err != nil {
		t.Fatal(err)
	}
	if err = ValidateCurrent(ctx, db); !errors.Is(err, ErrIncompleteSchema) {
		t.Fatalf("missing external guard accepted: %v", err)
	}
}
