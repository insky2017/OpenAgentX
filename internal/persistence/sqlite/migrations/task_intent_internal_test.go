package migrations

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func openIntentMigrationDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "intent.db")+"?_foreign_keys=ON")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func applyLegacyV1WithoutTaskIntent(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	legacy := strings.Replace(targetSchema, "    intent TEXT NOT NULL DEFAULT 'mutation' CHECK (intent IN ('mutation', 'query')),\n", "", 1)
	legacy = strings.Replace(legacy, "CREATE TRIGGER tasks_reject_intent_update BEFORE UPDATE OF intent ON tasks\nWHEN OLD.intent <> NEW.intent\nBEGIN\n    SELECT RAISE(ABORT, 'task intent is immutable');\nEND;\n\n", "", 1)
	if _, err := db.ExecContext(ctx, legacy); err != nil {
		t.Fatalf("apply old complete v1 schema: %v", err)
	}
}

func hasTaskIntentColumn(t *testing.T, ctx context.Context, db *sql.DB) bool {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('tasks') WHERE name='intent'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count == 1
}

func TestApplyUpgradesCompleteV1TaskIntentAtomically(t *testing.T) {
	ctx := context.Background()
	db := openIntentMigrationDB(t)
	applyLegacyV1WithoutTaskIntent(t, ctx, db)
	if hasTaskIntentColumn(t, ctx, db) {
		t.Fatal("legacy schema unexpectedly has intent")
	}
	// Insert the historical row before Apply so the test proves migration of
	// existing data, including the conservative terminal state and evidence.
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, statement := range []string{
		`INSERT INTO principals VALUES ('human-1','human','Human','active','` + now + `','` + now + `')`,
		`INSERT INTO principals VALUES ('agent-principal','agent','Agent','active','` + now + `','` + now + `')`,
		`INSERT INTO organizations VALUES ('org-1','Org','active','` + now + `','` + now + `')`,
		`INSERT INTO agents VALUES ('quote','agent-principal','org-1','Quote','active',1,'` + now + `','` + now + `')`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO tasks(task_id,version,status,sender_principal_id,target_agent_id,dispatch_mode,organization_id,idempotency_key,content,result,error,created_at,updated_at) VALUES ('task-old',7,'uncertain','human-1','quote','direct','org-1','old','work','historical reply','business_effect_unverified',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if err := apply(ctx, db, migrationOptions{beforeTaskIntentCommit: func() error { return errors.New("stop intent migration") }}); err == nil {
		t.Fatal("faulted Task intent migration succeeded")
	}
	if hasTaskIntentColumn(t, ctx, db) {
		t.Fatal("faulted Task intent migration left a column")
	}
	if err := Apply(ctx, db); err != nil {
		t.Fatalf("upgrade complete v1 schema: %v", err)
	}
	if err := Apply(ctx, db); err != nil {
		t.Fatalf("repeat upgraded schema apply: %v", err)
	}
	if !hasTaskIntentColumn(t, ctx, db) {
		t.Fatal("upgraded schema has no intent column")
	}
	var intent, status, result, taskError, createdAt, updatedAt string
	var version int64
	if err := db.QueryRowContext(ctx, `SELECT intent,version,status,result,error,created_at,updated_at FROM tasks WHERE task_id='task-old'`).Scan(&intent, &version, &status, &result, &taskError, &createdAt, &updatedAt); err != nil {
		t.Fatal(err)
	}
	if intent != "mutation" || version != 7 || status != "uncertain" || result != "historical reply" || taskError != "business_effect_unverified" || createdAt != now || updatedAt != now {
		t.Fatal("migration changed historical Task state or lost the mutation default")
	}
}

func TestApplyRejectsPartialTaskIntentObjects(t *testing.T) {
	ctx := context.Background()
	db := openIntentMigrationDB(t)
	if err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DROP TRIGGER tasks_reject_intent_update`); err != nil {
		t.Fatal(err)
	}
	if err := Apply(ctx, db); !errors.Is(err, ErrIncompleteSchema) {
		t.Fatalf("partial Task intent schema error=%v", err)
	}
}

func TestApplyRejectsDamagedV1BeforeTaskIntentUpgrade(t *testing.T) {
	ctx := context.Background()
	db := openIntentMigrationDB(t)
	applyLegacyV1WithoutTaskIntent(t, ctx, db)
	if _, err := db.ExecContext(ctx, `DROP TRIGGER event_journal_reject_update`); err != nil {
		t.Fatal(err)
	}
	if err := Apply(ctx, db); !errors.Is(err, ErrIncompleteSchema) {
		t.Fatalf("damaged v1 error=%v", err)
	}
	var triggerCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name='tasks_reject_intent_update'`).Scan(&triggerCount); err != nil {
		t.Fatal(err)
	}
	if hasTaskIntentColumn(t, ctx, db) || triggerCount != 0 {
		t.Fatal("rejected v1 was partially upgraded")
	}
}

func TestApplyRejectsIneffectiveTaskIntentTrigger(t *testing.T) {
	ctx := context.Background()
	db := openIntentMigrationDB(t)
	if err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`DROP TRIGGER tasks_reject_intent_update`,
		`CREATE TRIGGER tasks_reject_intent_update BEFORE UPDATE OF intent ON tasks WHEN OLD.intent <> NEW.intent AND 0 BEGIN SELECT RAISE(ABORT, 'task intent is immutable'); END`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := Apply(ctx, db); !errors.Is(err, ErrIncompleteSchema) {
		t.Fatalf("ineffective immutability trigger error=%v", err)
	}
}
