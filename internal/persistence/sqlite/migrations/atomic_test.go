package migrations

import (
	"context"
	"database/sql"
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
