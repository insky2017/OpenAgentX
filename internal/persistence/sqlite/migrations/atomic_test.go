package migrations

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestTargetSchemaTransactionRollsBackOnMigrationFailure(t *testing.T) {
	db, err := sql.Open("sqlite3", t.TempDir()+"/atomic.db?_foreign_keys=ON")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(context.Background(), targetSchema+"\nTHIS IS NOT SQL;"); err == nil {
		t.Fatal("invalid migration suffix must fail")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var tableCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_meta'").Scan(&tableCount); err != nil {
		t.Fatal(err)
	}
	if tableCount != 0 {
		t.Fatalf("failed migration left schema_meta table count=%d", tableCount)
	}
}

func TestDamagedInstallationObjectsAreRejectedWithoutRepair(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "upgrade.db")+"?_foreign_keys=ON")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{`DROP TABLE cli_tokens`, `DROP TABLE installation_metadata`} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	injected := errors.New("stop before CLI schema commit")
	err = apply(ctx, db, migrationOptions{newInstallationID: func() (string, error) { return "installation-test", nil }, beforeCLICommit: func() error { return injected }})
	if !errors.Is(err, ErrIncompleteSchema) {
		t.Fatalf("migration error=%v", err)
	}
	for _, table := range []string{"installation_metadata", "cli_tokens"} {
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("partial table %s count=%d err=%v", table, count, err)
		}
	}
}
