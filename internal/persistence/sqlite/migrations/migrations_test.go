package migrations_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"openagentx/internal/persistence/sqlite/migrations"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "schema.db")
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=ON")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestApplyCreatesTargetSchemaAndIsRepeatable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openDB(t)
	if err := migrations.Apply(ctx, db); err != nil {
		t.Fatalf("first migration apply failed: %v", err)
	}
	if err := migrations.Apply(ctx, db); err != nil {
		t.Fatalf("second migration apply failed: %v", err)
	}
	version, err := migrations.Version(ctx, db)
	if err != nil || version != migrations.CurrentVersion {
		t.Fatalf("schema version = %d, err=%v", version, err)
	}

	requiredTables := []string{
		"agents", "agent_profiles", "principals", "organizations", "org_units", "positions", "roles",
		"position_assignments", "reporting_lines", "authority_policies", "worker_instances", "execution_profiles",
		"runtime_backend_registrations", "tasks", "messages", "event_journal", "mailbox_items", "worker_commands",
		"run_attempts", "session_bindings", "workspace_leases", "artifacts", "approval_requests", "approval_decisions",
		"web_users", "web_sessions",
	}
	for _, table := range requiredTables {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("required table %s is missing", table)
		}
	}
}

func TestApplyUpgradesN1WorkerNetworkColumns(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	if err := migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "ALTER TABLE network_profile_bindings DROP COLUMN diagnostic"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "ALTER TABLE network_profile_bindings DROP COLUMN applied_binding_revision"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "ALTER TABLE runtime_backend_registrations DROP COLUMN network_json"); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('network_profile_bindings') WHERE name='diagnostic'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("diagnostic column count=%d", count)
	}
	for table, column := range map[string]string{
		"network_profile_bindings":      "applied_binding_revision",
		"runtime_backend_registrations": "network_json",
	} {
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?", table, column).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("%s.%s column count=%d", table, column, count)
		}
	}
}

func TestApplyRejectsLegacyAndUnsupportedSchemas(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	legacy := openDB(t)
	if _, err := legacy.ExecContext(ctx, "CREATE TABLE agents (id TEXT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Apply(ctx, legacy); !errors.Is(err, migrations.ErrIncompatibleLegacySchema) {
		t.Fatalf("legacy schema error = %v", err)
	}

	unsupported := openDB(t)
	if _, err := unsupported.ExecContext(ctx, "CREATE TABLE schema_meta (singleton INTEGER PRIMARY KEY, version INTEGER NOT NULL, applied_at TEXT NOT NULL); INSERT INTO schema_meta VALUES (1, 99, 'now')"); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Apply(ctx, unsupported); !errors.Is(err, migrations.ErrUnsupportedSchemaVersion) {
		t.Fatalf("unsupported schema error = %v", err)
	}
}

func TestApplyRejectsPartialCurrentSchema(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openDB(t)
	if _, err := db.ExecContext(ctx, `CREATE TABLE schema_meta (
		singleton INTEGER PRIMARY KEY, version INTEGER NOT NULL, applied_at TEXT NOT NULL
	); INSERT INTO schema_meta VALUES (1, 1, 'now')`); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Apply(ctx, db); !errors.Is(err, migrations.ErrIncompleteSchema) {
		t.Fatalf("partial current schema error = %v", err)
	}
}

func TestTargetSchemaAllowsQueuedTasksButRejectsTwoActiveRuns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openDB(t)
	if err := migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	statements := []string{
		fmt.Sprintf("INSERT INTO principals VALUES ('human-1','human','Human','active','%s','%s')", now, now),
		fmt.Sprintf("INSERT INTO principals VALUES ('agent-principal','agent','Agent','active','%s','%s')", now, now),
		fmt.Sprintf("INSERT INTO organizations VALUES ('org-1','Org','active','%s','%s')", now, now),
		fmt.Sprintf("INSERT INTO agents VALUES ('quote','agent-principal','org-1','Quote','active',1,'%s','%s')", now, now),
		fmt.Sprintf("INSERT INTO worker_instances VALUES ('worker-1','quote',1,'unix','worker-principal','[]','online',NULL,NULL,'%s','%s',1,'%s','%s')", now, now, now, now),
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed schema: %v\n%s", err, statement)
		}
	}
	for index := 1; index <= 2; index++ {
		_, err := db.ExecContext(ctx, `INSERT INTO tasks (
			task_id, version, status, sender_principal_id, target_agent_id, dispatch_mode,
			organization_id, idempotency_key, content, created_at, updated_at
		) VALUES (?,1,'queued','human-1','quote','direct','org-1',?,?,?,?)`,
			fmt.Sprintf("task-%d", index), fmt.Sprintf("idem-%d", index), "work", now, now)
		if err != nil {
			t.Fatalf("queued task %d rejected: %v", index, err)
		}
	}
	insertRun := `INSERT INTO run_attempts (
		run_id, task_id, agent_id, version, status, worker_instance_id, fencing_token, lease_until,
		execution_spec_version, requested_execution_json, resolved_execution_json,
		adapter_id, backend_id, model, reasoning_mode, reasoning_value, started_at, created_at, updated_at
	) VALUES (?,?,'quote',1,'running','worker-1',1,?,1,'{}','{}','fake','local','model','effort','high',?,?,?)`
	if _, err := db.ExecContext(ctx, insertRun, "run-1", "task-1", now, now, now, now); err != nil {
		t.Fatalf("first active run rejected: %v", err)
	}
	if _, err := db.ExecContext(ctx, insertRun, "run-2", "task-2", now, now, now, now); err == nil {
		t.Fatal("second active run for same agent must violate unique index")
	}
}
