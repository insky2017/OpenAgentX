package migrations

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strings"
)

const CurrentVersion = 1

var (
	ErrIncompatibleLegacySchema = errors.New("database contains a legacy schema without OpenAgentX schema metadata")
	ErrUnsupportedSchemaVersion = errors.New("unsupported OpenAgentX schema version")
	ErrIncompleteSchema         = errors.New("OpenAgentX schema is incomplete or corrupted")
)

var requiredTables = []string{
	"schema_meta", "principals", "organizations", "org_units", "roles", "positions", "agents",
	"position_assignments", "reporting_lines", "authority_policies", "agent_profiles", "execution_profiles",
	"worker_instances", "runtime_backend_registrations", "tasks", "messages", "run_attempts", "session_bindings",
	"workspace_leases", "approval_requests", "approval_decisions", "mailbox_items", "worker_commands", "artifacts",
	"event_journal", "web_users", "web_sessions",
	"network_profiles", "network_profile_bindings",
	"network_profile_heads", "network_tests", "network_work_items", "network_workflow_commands", "network_imports",
	"network_profile_publications", "network_mode_policies", "network_mode_tests",
}

var requiredTriggers = []string{"event_journal_reject_update", "event_journal_reject_delete", "network_profiles_reject_update", "network_profiles_reject_delete", "network_mode_policies_reject_update", "network_mode_policies_reject_delete"}

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
		// ADR-002 adds these tables without invalidating existing v1 databases.
		// Apply them transactionally before validating the current schema so an
		// upgrade from an already deployed v1 remains usable.
		if err := ensureNetworkTables(ctx, db); err != nil {
			return err
		}
		if err := ensureWorkerCommandKinds(ctx, db); err != nil {
			return err
		}
		return ValidateCurrent(ctx, db)
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
	if err := validateObjects(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit target schema v%d: %w", CurrentVersion, err)
	}
	return nil
}

func ensureWorkerCommandKinds(ctx context.Context, db *sql.DB) error {
	var definition string
	if err := db.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='table' AND name='worker_commands'`).Scan(&definition); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect Worker command schema: %w", err)
	}
	if strings.Contains(definition, "'force_stop'") {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Worker command schema upgrade: %w", err)
	}
	defer tx.Rollback()
	statements := []string{
		`ALTER TABLE worker_commands RENAME TO worker_commands_legacy`,
		`CREATE TABLE worker_commands (
			worker_command_id TEXT PRIMARY KEY,
			worker_instance_id TEXT NOT NULL REFERENCES worker_instances(worker_instance_id) ON DELETE CASCADE,
			generation INTEGER NOT NULL CHECK (generation > 0),
			kind TEXT NOT NULL CHECK (kind IN ('drain', 'stop', 'force_stop', 'health_check')),
			state TEXT NOT NULL CHECK (state IN ('pending', 'claimed', 'applied', 'failed')),
			requested_by TEXT NOT NULL REFERENCES principals(principal_id),
			idempotency_key TEXT NOT NULL, lease_until TEXT,
			attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
			created_at TEXT NOT NULL, claimed_at TEXT, applied_at TEXT, result TEXT,
			UNIQUE (requested_by, idempotency_key)
		)`,
		`INSERT INTO worker_commands SELECT worker_command_id,worker_instance_id,generation,kind,state,requested_by,idempotency_key,lease_until,attempts,created_at,claimed_at,applied_at,result FROM worker_commands_legacy`,
		`DROP TABLE worker_commands_legacy`,
		`CREATE INDEX idx_worker_commands_claim ON worker_commands(worker_instance_id, generation, state, created_at)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("upgrade Worker command schema: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Worker command schema upgrade: %w", err)
	}
	return nil
}

func ensureNetworkTables(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin network schema upgrade: %w", err)
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS network_profiles (
			profile_id TEXT NOT NULL, version INTEGER NOT NULL CHECK (version > 0),
			status TEXT NOT NULL CHECK (status IN ('draft', 'published')),
			mode TEXT NOT NULL CHECK (mode IN ('only_http_proxy', 'only_socks5')),
			host TEXT NOT NULL, port INTEGER NOT NULL CHECK (port BETWEEN 1 AND 65535),
			config_file TEXT, secret_ref TEXT, created_by TEXT NOT NULL REFERENCES principals(principal_id),
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL, PRIMARY KEY (profile_id, version)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_network_profiles_latest ON network_profiles(profile_id, version DESC)`,
		`CREATE TABLE IF NOT EXISTS network_profile_bindings (
			agent_id TEXT NOT NULL REFERENCES agents(agent_id) ON DELETE CASCADE, backend_id TEXT NOT NULL,
			mode TEXT NOT NULL CHECK(mode IN ('inherit','direct','named_profile')), profile_id TEXT, profile_version INTEGER, policy_version INTEGER, test_id TEXT,
			version INTEGER NOT NULL CHECK (version > 0), desired_status TEXT NOT NULL CHECK (desired_status IN ('pending', 'applied', 'failed')),
			applied_worker_id TEXT, applied_generation INTEGER, applied_mode TEXT, applied_profile_id TEXT, applied_profile_version INTEGER, applied_policy_version INTEGER,
			applied_binding_revision INTEGER, diagnostic TEXT, manifest_digest TEXT, secret_version TEXT, runtime_identity_json TEXT, updated_at TEXT NOT NULL,
			PRIMARY KEY (agent_id, backend_id), FOREIGN KEY (profile_id, profile_version) REFERENCES network_profiles(profile_id, version),
			CHECK((mode='named_profile' AND profile_id IS NOT NULL AND profile_version IS NOT NULL AND policy_version IS NULL) OR (mode IN ('inherit','direct') AND profile_id IS NULL AND profile_version IS NULL AND policy_version IS NOT NULL))
		)`,
		`CREATE TABLE IF NOT EXISTS network_profile_heads (profile_id TEXT PRIMARY KEY, current_content_version INTEGER NOT NULL CHECK(current_content_version>0), state TEXT NOT NULL CHECK(state IN ('draft','testing','ready','published','stale')), state_revision INTEGER NOT NULL CHECK(state_revision>0), ready_test_id TEXT, published_content_version INTEGER, updated_at TEXT NOT NULL, FOREIGN KEY(profile_id,current_content_version) REFERENCES network_profiles(profile_id,version))`,
		`CREATE TABLE IF NOT EXISTS network_tests (test_id TEXT PRIMARY KEY, profile_id TEXT NOT NULL, content_version INTEGER NOT NULL, secret_version TEXT, worker_instance_id TEXT NOT NULL REFERENCES worker_instances(worker_instance_id), generation INTEGER NOT NULL CHECK(generation>0), backend_id TEXT NOT NULL, runtime_identity_json TEXT NOT NULL, state TEXT NOT NULL CHECK(state IN ('pending','claimed','succeeded','failed','stale')), diagnostic_code TEXT, duration_ms INTEGER, probe_results_json TEXT NOT NULL DEFAULT '[]', created_by TEXT NOT NULL REFERENCES principals(principal_id), created_at TEXT NOT NULL, finished_at TEXT, FOREIGN KEY(profile_id,content_version) REFERENCES network_profiles(profile_id,version))`,
		`CREATE TABLE IF NOT EXISTS network_work_items (work_id TEXT PRIMARY KEY, kind TEXT NOT NULL CHECK(kind IN ('test','apply','import')), profile_id TEXT, content_version INTEGER, secret_version TEXT, agent_id TEXT NOT NULL REFERENCES agents(agent_id), backend_id TEXT NOT NULL, worker_instance_id TEXT NOT NULL REFERENCES worker_instances(worker_instance_id), generation INTEGER NOT NULL CHECK(generation>0), binding_revision INTEGER, network_mode TEXT, policy_version INTEGER, manifest_digest TEXT, runtime_identity_json TEXT NOT NULL, state TEXT NOT NULL CHECK(state IN ('pending','claimed','succeeded','failed','stale')), diagnostic_code TEXT, created_at TEXT NOT NULL, finished_at TEXT)`,
		`CREATE INDEX IF NOT EXISTS idx_network_work_claim ON network_work_items(worker_instance_id,generation,state,created_at)`,
		`CREATE TABLE IF NOT EXISTS network_workflow_commands (actor_principal_id TEXT NOT NULL REFERENCES principals(principal_id), operation TEXT NOT NULL, idempotency_key TEXT NOT NULL, request_digest TEXT NOT NULL, result_json TEXT NOT NULL, created_at TEXT NOT NULL, PRIMARY KEY(actor_principal_id,operation,idempotency_key))`,
		`CREATE TABLE IF NOT EXISTS network_imports (worker_instance_id TEXT NOT NULL REFERENCES worker_instances(worker_instance_id), generation INTEGER NOT NULL, backend_id TEXT NOT NULL, source_identity TEXT NOT NULL, work_id TEXT NOT NULL UNIQUE REFERENCES network_work_items(work_id), profile_id TEXT, content_version INTEGER, state TEXT NOT NULL CHECK(state IN ('pending','succeeded','failed')), created_at TEXT NOT NULL, finished_at TEXT, PRIMARY KEY(worker_instance_id,generation,backend_id,source_identity))`,
		`CREATE TABLE IF NOT EXISTS network_profile_publications (profile_id TEXT NOT NULL, content_version INTEGER NOT NULL, test_id TEXT NOT NULL REFERENCES network_tests(test_id), runtime_identity_json TEXT NOT NULL, published_by TEXT NOT NULL REFERENCES principals(principal_id), published_at TEXT NOT NULL, PRIMARY KEY(profile_id,content_version), FOREIGN KEY(profile_id,content_version) REFERENCES network_profiles(profile_id,version))`,
		`CREATE TABLE IF NOT EXISTS network_mode_policies (agent_id TEXT NOT NULL REFERENCES agents(agent_id) ON DELETE CASCADE, backend_id TEXT NOT NULL, policy_version INTEGER NOT NULL CHECK(policy_version>0), mode TEXT NOT NULL CHECK(mode IN ('inherit','direct')), manifest_digest TEXT NOT NULL, created_by TEXT NOT NULL REFERENCES principals(principal_id), created_at TEXT NOT NULL, PRIMARY KEY(agent_id,backend_id,policy_version))`,
		`CREATE TABLE IF NOT EXISTS network_mode_tests (test_id TEXT PRIMARY KEY, agent_id TEXT NOT NULL, backend_id TEXT NOT NULL, policy_version INTEGER NOT NULL, mode TEXT NOT NULL CHECK(mode IN ('inherit','direct')), manifest_digest TEXT NOT NULL, worker_instance_id TEXT NOT NULL REFERENCES worker_instances(worker_instance_id), generation INTEGER NOT NULL CHECK(generation>0), runtime_identity_json TEXT NOT NULL, binding_revision INTEGER NOT NULL CHECK(binding_revision>=0), state TEXT NOT NULL CHECK(state IN ('pending','claimed','succeeded','failed','stale')), diagnostic_code TEXT, duration_ms INTEGER, probe_results_json TEXT NOT NULL DEFAULT '[]', created_by TEXT NOT NULL REFERENCES principals(principal_id), created_at TEXT NOT NULL, finished_at TEXT, FOREIGN KEY(agent_id,backend_id,policy_version) REFERENCES network_mode_policies(agent_id,backend_id,policy_version))`,
		`CREATE TRIGGER IF NOT EXISTS network_profiles_reject_update BEFORE UPDATE ON network_profiles BEGIN SELECT RAISE(ABORT, 'network profile content is immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS network_profiles_reject_delete BEFORE DELETE ON network_profiles BEGIN SELECT RAISE(ABORT, 'network profile content is immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS network_mode_policies_reject_update BEFORE UPDATE ON network_mode_policies BEGIN SELECT RAISE(ABORT, 'network mode policy is immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS network_mode_policies_reject_delete BEFORE DELETE ON network_mode_policies BEGIN SELECT RAISE(ABORT, 'network mode policy is immutable'); END`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply network schema upgrade: %w", err)
		}
	}
	// v1 databases may already contain network_profile_bindings created before
	// the diagnostic field was introduced. CREATE TABLE IF NOT EXISTS does not
	// alter that table, so add the nullable column explicitly when absent.
	columns := map[string]bool{}
	columnNotNull := map[string]bool{}
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(network_profile_bindings)`)
	if err != nil {
		return fmt.Errorf("inspect network binding schema: %w", err)
	}
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return fmt.Errorf("scan network binding schema: %w", err)
		}
		columns[name] = true
		columnNotNull[name] = notNull != 0
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read network binding schema: %w", err)
	}
	rows.Close()
	requiredBindingColumns := []string{"mode", "policy_version", "test_id", "applied_mode", "applied_profile_id", "applied_policy_version", "manifest_digest", "secret_version", "runtime_identity_json", "diagnostic", "applied_binding_revision"}
	needsBindingRebuild := columnNotNull["profile_id"] || columnNotNull["profile_version"]
	for _, name := range requiredBindingColumns {
		needsBindingRebuild = needsBindingRebuild || !columns[name]
	}
	if needsBindingRebuild {
		if err := rebuildNetworkBindings(ctx, tx, columns); err != nil {
			return err
		}
	}
	workColumns, err := tableColumns(ctx, tx, "network_work_items")
	if err != nil {
		return err
	}
	for name, definition := range map[string]string{"network_mode": "TEXT", "policy_version": "INTEGER", "manifest_digest": "TEXT"} {
		if !workColumns[name] {
			if _, err := tx.ExecContext(ctx, `ALTER TABLE network_work_items ADD COLUMN `+name+` `+definition); err != nil {
				return fmt.Errorf("add network work %s column: %w", name, err)
			}
		}
	}
	for _, table := range []string{"network_tests", "network_mode_tests"} {
		testColumns, err := tableColumns(ctx, tx, table)
		if err != nil {
			return err
		}
		if !testColumns["probe_results_json"] {
			if _, err := tx.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN probe_results_json TEXT NOT NULL DEFAULT '[]'`); err != nil {
				return fmt.Errorf("add %s probe results column: %w", table, err)
			}
		}
	}
	profileColumns := map[string]bool{}
	profileRows, err := tx.QueryContext(ctx, `PRAGMA table_info(network_profiles)`)
	if err != nil {
		return fmt.Errorf("inspect network profile schema: %w", err)
	}
	for profileRows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := profileRows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			profileRows.Close()
			return err
		}
		profileColumns[name] = true
	}
	if err := profileRows.Close(); err != nil {
		return err
	}
	for name, definition := range map[string]string{"direct_ips_json": "TEXT NOT NULL DEFAULT '[]'", "manifest_digest": "TEXT"} {
		if !profileColumns[name] {
			if _, err := tx.ExecContext(ctx, `ALTER TABLE network_profiles ADD COLUMN `+name+` `+definition); err != nil {
				return fmt.Errorf("add network profile %s column: %w", name, err)
			}
		}
	}
	// Existing path-based rows are visible only as stale migration candidates.
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO network_profile_heads(profile_id,current_content_version,state,state_revision,published_content_version,updated_at)
		SELECT p.profile_id,p.version,'stale',1,CASE WHEN p.status='published' THEN p.version END,p.updated_at FROM network_profiles p
		JOIN (SELECT profile_id,MAX(version) version FROM network_profiles GROUP BY profile_id) latest ON latest.profile_id=p.profile_id AND latest.version=p.version`); err != nil {
		return fmt.Errorf("mark legacy network profiles stale: %w", err)
	}
	var hasBackendTable, hasBackendNetwork bool
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) > 0 FROM sqlite_master WHERE type='table' AND name='runtime_backend_registrations'`).Scan(&hasBackendTable); err != nil {
		return fmt.Errorf("inspect Backend registration table: %w", err)
	}
	backendRows, err := tx.QueryContext(ctx, `PRAGMA table_info(runtime_backend_registrations)`)
	if err != nil {
		return fmt.Errorf("inspect Backend registration schema: %w", err)
	}
	for backendRows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := backendRows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			backendRows.Close()
			return fmt.Errorf("scan Backend registration schema: %w", err)
		}
		if name == "network_json" {
			hasBackendNetwork = true
		}
	}
	if err := backendRows.Err(); err != nil {
		backendRows.Close()
		return fmt.Errorf("read Backend registration schema: %w", err)
	}
	backendRows.Close()
	if hasBackendTable && !hasBackendNetwork {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE runtime_backend_registrations ADD COLUMN network_json TEXT NOT NULL DEFAULT '{}'`); err != nil {
			return fmt.Errorf("add Backend network policy column: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit network schema upgrade: %w", err)
	}
	return nil
}

func tableColumns(ctx context.Context, tx *sql.Tx, table string) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return nil, fmt.Errorf("inspect %s schema: %w", table, err)
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

func rebuildNetworkBindings(ctx context.Context, tx *sql.Tx, columns map[string]bool) error {
	column := func(name, fallback string) string {
		if columns[name] {
			return name
		}
		return fallback
	}
	legacy := !columns["mode"]
	mode := column("mode", "'named_profile'")
	appliedMode := column("applied_mode", "NULL")
	appliedProfileID := column("applied_profile_id", "NULL")
	appliedProfileVersion := column("applied_profile_version", "NULL")
	if legacy {
		match := "applied_binding_revision=version AND applied_profile_version IS NOT NULL"
		if !columns["applied_binding_revision"] {
			match = "0"
		}
		appliedMode = "CASE WHEN " + match + " THEN 'named_profile' END"
		appliedProfileID = "CASE WHEN " + match + " THEN profile_id END"
		appliedProfileVersion = "CASE WHEN " + match + " THEN applied_profile_version END"
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE network_profile_bindings_next (
		agent_id TEXT NOT NULL REFERENCES agents(agent_id) ON DELETE CASCADE, backend_id TEXT NOT NULL,
		mode TEXT NOT NULL CHECK(mode IN ('inherit','direct','named_profile')), profile_id TEXT, profile_version INTEGER, policy_version INTEGER, test_id TEXT,
		version INTEGER NOT NULL CHECK(version>0), desired_status TEXT NOT NULL CHECK(desired_status IN ('pending','applied','failed')),
		applied_worker_id TEXT, applied_generation INTEGER, applied_mode TEXT CHECK(applied_mode IS NULL OR applied_mode IN ('inherit','direct','named_profile')),
		applied_profile_id TEXT, applied_profile_version INTEGER, applied_policy_version INTEGER, applied_binding_revision INTEGER,
		diagnostic TEXT, manifest_digest TEXT, secret_version TEXT, runtime_identity_json TEXT, updated_at TEXT NOT NULL,
		PRIMARY KEY(agent_id,backend_id), FOREIGN KEY(profile_id,profile_version) REFERENCES network_profiles(profile_id,version),
		CHECK((mode='named_profile' AND profile_id IS NOT NULL AND profile_version IS NOT NULL AND policy_version IS NULL) OR
			(mode IN ('inherit','direct') AND profile_id IS NULL AND profile_version IS NULL AND policy_version IS NOT NULL))
	)`); err != nil {
		return fmt.Errorf("create upgraded network bindings: %w", err)
	}
	query := fmt.Sprintf(`INSERT INTO network_profile_bindings_next(
		agent_id,backend_id,mode,profile_id,profile_version,policy_version,test_id,version,desired_status,
		applied_worker_id,applied_generation,applied_mode,applied_profile_id,applied_profile_version,applied_policy_version,
		applied_binding_revision,diagnostic,manifest_digest,secret_version,runtime_identity_json,updated_at)
		SELECT agent_id,backend_id,%s,profile_id,profile_version,%s,%s,version,desired_status,
		%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,updated_at FROM network_profile_bindings`,
		mode, column("policy_version", "NULL"), column("test_id", "NULL"),
		column("applied_worker_id", "NULL"), column("applied_generation", "NULL"), appliedMode, appliedProfileID,
		appliedProfileVersion, column("applied_policy_version", "NULL"), column("applied_binding_revision", "NULL"),
		column("diagnostic", "NULL"), column("manifest_digest", "NULL"), column("secret_version", "NULL"), column("runtime_identity_json", "NULL"))
	if _, err := tx.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("copy upgraded network bindings: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE network_profile_bindings`); err != nil {
		return fmt.Errorf("drop legacy network bindings: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE network_profile_bindings_next RENAME TO network_profile_bindings`); err != nil {
		return fmt.Errorf("install upgraded network bindings: %w", err)
	}
	return nil
}

type schemaQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func ValidateCurrent(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	version, err := Version(ctx, db)
	if err != nil {
		return err
	}
	if version != CurrentVersion {
		return fmt.Errorf("%w: got %d, want %d", ErrUnsupportedSchemaVersion, version, CurrentVersion)
	}
	return validateObjects(ctx, db)
}

func validateObjects(ctx context.Context, queryer schemaQueryer) error {
	for _, table := range requiredTables {
		if err := requireSchemaObject(ctx, queryer, "table", table); err != nil {
			return err
		}
	}
	for _, trigger := range requiredTriggers {
		if err := requireSchemaObject(ctx, queryer, "trigger", trigger); err != nil {
			return err
		}
	}
	return nil
}

func requireSchemaObject(ctx context.Context, queryer schemaQueryer, objectType string, name string) error {
	var count int
	if err := queryer.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type = ? AND name = ?", objectType, name,
	).Scan(&count); err != nil {
		return fmt.Errorf("inspect schema object %s %s: %w", objectType, name, err)
	}
	if count != 1 {
		return fmt.Errorf("%w: missing %s %s", ErrIncompleteSchema, objectType, name)
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
