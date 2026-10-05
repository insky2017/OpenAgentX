package migrations

import (
	"context"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"

	_ "github.com/mattn/go-sqlite3"
	"time"
)

const CurrentVersion = 6

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
	"installation_metadata", "cli_tokens",
	"network_profiles", "network_profile_bindings",
	"network_profile_heads", "network_tests", "network_work_items", "network_workflow_commands", "network_imports",
	"network_profile_publications", "network_mode_policies", "network_mode_tests",
}

var requiredTriggers = []string{"event_journal_reject_update", "event_journal_reject_delete", "tasks_reject_intent_update", "network_profiles_reject_update", "network_profiles_reject_delete", "network_mode_policies_reject_update", "network_mode_policies_reject_delete"}

var requiredIndexes = []string{"idx_cli_tokens_expiry", "idx_cli_tokens_user"}

var requiredColumns = map[string][]string{
	"installation_metadata": {"singleton", "installation_id", "created_at"},
	"cli_tokens":            {"token_id", "token_digest", "web_user_id", "principal_id", "scopes_json", "installation_id", "created_at", "last_used_at", "absolute_expires_at", "revoked_at"},
}

var requiredDefinitionFragments = map[string][]string{
	"installation_metadata": {"singleton integer primary key", "check (singleton = 1)", "installation_id text not null unique"},
	"cli_tokens":            {"token_id text primary key", "token_digest text not null unique", "references web_users(web_user_id)", "references principals(principal_id)", "references installation_metadata(installation_id)"},
}

var requiredTaskIntentColumns = []string{"intent", "completion_basis"}
var requiredTaskIntentDefinitionFragments = []string{"intent text not null default 'mutation'", "check (intent in ('mutation', 'query'))", taskCompletionColumnSQL}

//go:embed 001_target_schema.sql
var targetSchema string

//go:embed 003_external_sessions.sql
var externalSessionSchema string

//go:embed 004_external_roles.sql
var externalRolesSchema string

//go:embed 005_managed_collaboration.sql
var managedCollaborationSchema string

func Apply(ctx context.Context, db *sql.DB) error {
	return apply(ctx, db, migrationOptions{})
}

type migrationOptions struct {
	newInstallationID func() (string, error)
	beforeCLICommit   func() error
	beforeV2Commit    func() error
	beforeV3Commit    func() error
	beforeV4Commit    func() error
	beforeV5Commit    func() error
	beforeV6Commit    func() error
}

func apply(ctx context.Context, db *sql.DB, options migrationOptions) error {
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
		switch version {
		case CurrentVersion:
			return ValidateCurrent(ctx, db)
		case 1:
			return migrateV1ToV2(ctx, db, options)
		case 2:
			return migrateV2ToV3(ctx, db, options)
		case 3:
			return migrateV3ToV4(ctx, db, options)
		case 4:
			return migrateV4ToV5(ctx, db, options)
		case 5:
			return migrateV5ToV6(ctx, db, options)
		default:
			return fmt.Errorf("%w: got %d, want 1 or %d", ErrUnsupportedSchemaVersion, version, CurrentVersion)
		}
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
	if err := initializeInstallation(ctx, tx, options); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, externalSessionSchema); err != nil {
		return fmt.Errorf("apply external sessions: %w", err)
	}
	if err := applyExternalRoles(ctx, tx, options); err != nil {
		return err
	}
	if err := validateObjects(ctx, tx); err != nil {
		return err
	}
	if options.beforeCLICommit != nil {
		if err := options.beforeCLICommit(); err != nil {
			return fmt.Errorf("CLI Token schema pre-commit: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit target schema v%d: %w", CurrentVersion, err)
	}
	return nil
}

// migrateV1ToV2 accepts only the complete deployed v1 schema, before ADR-006.
// All validation, DDL, and metadata changes share the same transaction.
func migrateV1ToV2(ctx context.Context, db *sql.DB, options migrationOptions) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin v1 to v2 migration: %w", err)
	}
	defer tx.Rollback()
	var sourceVersion int
	if err := tx.QueryRowContext(ctx, "SELECT version FROM schema_meta WHERE singleton=1").Scan(&sourceVersion); err != nil {
		return fmt.Errorf("read migration source version: %w", err)
	}
	if sourceVersion != 1 {
		return fmt.Errorf("%w: migration source version %d", ErrUnsupportedSchemaVersion, sourceVersion)
	}
	if err := validateSchemaShape(ctx, tx, 1); err != nil {
		return err
	}
	if err := validateObjectsWithoutTaskIntent(ctx, tx); err != nil {
		return err
	}
	for _, statement := range []string{
		"ALTER TABLE tasks ADD COLUMN intent TEXT NOT NULL DEFAULT 'mutation' CHECK (intent IN ('mutation', 'query'))",
		"ALTER TABLE tasks ADD COLUMN " + taskCompletionColumnSQL,
		taskIntentTriggerSQL,
		"UPDATE schema_meta SET version=2, applied_at=strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE singleton=1 AND version=1",
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate v1 to v2: %w", err)
		}
	}
	if err := validateBaseObjects(ctx, tx); err != nil {
		return err
	}
	if options.beforeV2Commit != nil {
		if err := options.beforeV2Commit(); err != nil {
			return fmt.Errorf("v2 pre-commit: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, externalSessionSchema); err != nil {
		return err
	}
	if err := applyExternalRoles(ctx, tx, options); err != nil {
		return err
	}
	if err := validateObjects(ctx, tx); err != nil {
		return err
	}
	if options.beforeV3Commit != nil {
		if err := options.beforeV3Commit(); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func migrateV2ToV3(ctx context.Context, db *sql.DB, options migrationOptions) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRowContext(ctx, "SELECT version FROM schema_meta WHERE singleton=1").Scan(&version); err != nil {
		return err
	}
	if version != 2 {
		return ErrUnsupportedSchemaVersion
	}
	if err = validateBaseObjects(ctx, tx); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, externalSessionSchema); err != nil {
		return err
	}
	if err = applyExternalRoles(ctx, tx, options); err != nil {
		return err
	}
	if err = validateObjects(ctx, tx); err != nil {
		return err
	}
	if options.beforeV3Commit != nil {
		if err = options.beforeV3Commit(); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const taskCompletionColumnSQL = "completion_basis TEXT NOT NULL DEFAULT '' CHECK (completion_basis IN ('', 'query_result_delivered', 'mutation_effects_known')) CHECK (completion_basis = '' OR (status = 'succeeded' AND ((intent = 'query' AND completion_basis = 'query_result_delivered') OR (intent = 'mutation' AND completion_basis = 'mutation_effects_known'))))"

func initializeInstallation(ctx context.Context, tx *sql.Tx, options migrationOptions) error {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM installation_metadata WHERE singleton=1`).Scan(&count); err != nil {
		return fmt.Errorf("inspect installation identity: %w", err)
	}
	if count == 0 {
		generator := options.newInstallationID
		if generator == nil {
			generator = randomInstallationID
		}
		installationID, err := generator()
		if err != nil {
			return fmt.Errorf("generate installation identity: %w", err)
		}
		if strings.TrimSpace(installationID) == "" {
			return fmt.Errorf("generate installation identity: empty value")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO installation_metadata(singleton,installation_id,created_at) VALUES(1,?,?)`, installationID, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("persist installation identity: %w", err)
		}
	}
	return nil
}

func randomInstallationID() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

type schemaQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
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
	if err := validateBaseObjects(ctx, queryer); err != nil {
		return err
	}
	if err := validateExternalObjects(ctx, queryer); err != nil {
		return err
	}
	return validateAgentRemoval(ctx, queryer)
}

func validateBaseObjects(ctx context.Context, queryer schemaQueryer) error {
	var version int
	if err := queryer.QueryRowContext(ctx, "SELECT version FROM schema_meta WHERE singleton=1").Scan(&version); err != nil {
		return err
	}
	if err := validateSchemaShape(ctx, queryer, version); err != nil {
		return err
	}
	if err := validateObjectsWithoutTaskIntent(ctx, queryer); err != nil {
		return err
	}
	return validateTaskIntentObjects(ctx, queryer)
}

func validateObjectsWithoutTaskIntent(ctx context.Context, queryer schemaQueryer) error {
	for _, table := range requiredTables {
		if err := requireSchemaObject(ctx, queryer, "table", table); err != nil {
			return err
		}
	}
	for _, trigger := range requiredTriggers {
		if trigger == "tasks_reject_intent_update" {
			continue
		}
		if err := requireSchemaObject(ctx, queryer, "trigger", trigger); err != nil {
			return err
		}
	}
	return validateCLIObjects(ctx, queryer)
}

const taskIntentTriggerSQL = `CREATE TRIGGER tasks_reject_intent_update BEFORE UPDATE OF intent ON tasks WHEN OLD.intent <> NEW.intent BEGIN SELECT RAISE(ABORT, 'task intent is immutable'); END`

func validateTaskIntentObjects(ctx context.Context, queryer schemaQueryer) error {
	for _, column := range requiredTaskIntentColumns {
		var count int
		if err := queryer.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('tasks') WHERE name=?`, column).Scan(&count); err != nil {
			return fmt.Errorf("inspect Task intent column: %w", err)
		}
		if count != 1 {
			return fmt.Errorf("%w: missing column tasks.%s", ErrIncompleteSchema, column)
		}
	}
	var definition string
	if err := queryer.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='table' AND name='tasks'`).Scan(&definition); err != nil {
		return fmt.Errorf("inspect Task intent schema definition: %w", err)
	}
	normalized := strings.ToLower(strings.Join(strings.Fields(definition), " "))
	for _, fragment := range requiredTaskIntentDefinitionFragments {
		if !strings.Contains(normalized, strings.ToLower(fragment)) {
			return fmt.Errorf("%w: invalid definition for table tasks", ErrIncompleteSchema)
		}
	}
	if err := requireSchemaObject(ctx, queryer, "trigger", "tasks_reject_intent_update"); err != nil {
		return err
	}
	if err := queryer.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='trigger' AND name='tasks_reject_intent_update'`).Scan(&definition); err != nil {
		return fmt.Errorf("inspect Task intent immutability definition: %w", err)
	}
	normalizeSQL := func(sql string) string {
		return strings.ToLower(strings.Join(strings.Fields(strings.TrimSuffix(strings.TrimSpace(sql), ";")), " "))
	}
	if normalizeSQL(definition) != normalizeSQL(taskIntentTriggerSQL) {
		return fmt.Errorf("%w: invalid Task intent immutability trigger", ErrIncompleteSchema)
	}
	return nil
}

func validateCLIObjects(ctx context.Context, queryer schemaQueryer) error {
	for _, index := range requiredIndexes {
		if err := requireSchemaObject(ctx, queryer, "index", index); err != nil {
			return err
		}
	}
	for table, columns := range requiredColumns {
		for _, column := range columns {
			var count int
			query := fmt.Sprintf("SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name=?", table)
			if err := queryer.QueryRowContext(ctx, query, column).Scan(&count); err != nil {
				return fmt.Errorf("inspect schema column %s.%s: %w", table, column, err)
			}
			if count != 1 {
				return fmt.Errorf("%w: missing column %s.%s", ErrIncompleteSchema, table, column)
			}
		}
	}
	for table, fragments := range requiredDefinitionFragments {
		var definition string
		if err := queryer.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&definition); err != nil {
			return fmt.Errorf("inspect schema definition %s: %w", table, err)
		}
		normalized := strings.ToLower(strings.Join(strings.Fields(definition), " "))
		for _, fragment := range fragments {
			if !strings.Contains(normalized, fragment) {
				return fmt.Errorf("%w: invalid definition for table %s", ErrIncompleteSchema, table)
			}
		}
	}
	var installationRows int
	if err := queryer.QueryRowContext(ctx, `SELECT COUNT(*) FROM installation_metadata WHERE singleton=1 AND length(trim(installation_id))>0`).Scan(&installationRows); err != nil {
		return fmt.Errorf("inspect installation identity row: %w", err)
	}
	if installationRows != 1 {
		return fmt.Errorf("%w: installation identity row is missing or invalid", ErrIncompleteSchema)
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

type schemaColumn struct {
	Name, Type, Default string
	NotNull, PrimaryKey int
}
type schemaObject struct{ Kind, Name, SQL string }

var schemaShapeOnce sync.Once
var schemaShapeErr error
var schemaShapeColumns map[string][]schemaColumn
var schemaShapeObjects []schemaObject

func readSchemaColumns(ctx context.Context, q schemaQueryer, table string) ([]schemaColumn, error) {
	rows, err := q.QueryContext(ctx, "SELECT name,type,COALESCE(dflt_value,''),\"notnull\",pk FROM pragma_table_info(?) ORDER BY name", table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var columns []schemaColumn
	for rows.Next() {
		var column schemaColumn
		if err := rows.Scan(&column.Name, &column.Type, &column.Default, &column.NotNull, &column.PrimaryKey); err != nil {
			return nil, err
		}
		columns = append(columns, column)
	}
	return columns, rows.Err()
}

// The target DDL supplies the complete required column/index/trigger inventory.
// Column order may differ after ALTER TABLE, but missing or extra columns,
// weakened types/defaults, and missing or ineffective triggers are rejected.
func validateSchemaShape(ctx context.Context, q schemaQueryer, version int) error {
	schemaShapeOnce.Do(func() {
		db, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			schemaShapeErr = err
			return
		}
		defer db.Close()
		if _, err := db.Exec(targetSchema); err != nil {
			schemaShapeErr = err
			return
		}
		schemaShapeColumns = make(map[string][]schemaColumn)
		for _, table := range requiredTables {
			columns, err := readSchemaColumns(context.Background(), db, table)
			if err != nil {
				schemaShapeErr = err
				return
			}
			schemaShapeColumns[table] = columns
		}
		rows, err := db.Query("SELECT type,name,sql FROM sqlite_master WHERE type IN ('index','trigger') AND sql IS NOT NULL ORDER BY name")
		if err != nil {
			schemaShapeErr = err
			return
		}
		defer rows.Close()
		for rows.Next() {
			var object schemaObject
			if err := rows.Scan(&object.Kind, &object.Name, &object.SQL); err != nil {
				schemaShapeErr = err
				return
			}
			schemaShapeObjects = append(schemaShapeObjects, object)
		}
		schemaShapeErr = rows.Err()
	})
	if schemaShapeErr != nil {
		return fmt.Errorf("load target schema contract: %w", schemaShapeErr)
	}
	for table, expected := range schemaShapeColumns {
		actual, err := readSchemaColumns(ctx, q, table)
		if err != nil {
			return fmt.Errorf("%w: inspect %s: %v", ErrIncompleteSchema, table, err)
		}
		want := make([]schemaColumn, 0, len(expected))
		for _, column := range expected {
			if version == 1 && table == "tasks" && (column.Name == "intent" || column.Name == "completion_basis") {
				continue
			}
			want = append(want, column)
		}
		if fmt.Sprint(actual) != fmt.Sprint(want) {
			return fmt.Errorf("%w: unsupported column contract for %s", ErrIncompleteSchema, table)
		}
	}
	for _, object := range schemaShapeObjects {
		if version == 1 && object.Name == "tasks_reject_intent_update" {
			continue
		}
		var definition string
		if err := q.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type=? AND name=?", object.Kind, object.Name).Scan(&definition); err != nil {
			return fmt.Errorf("%w: missing %s %s", ErrIncompleteSchema, object.Kind, object.Name)
		}
		expectedSQL := object.SQL
		if version >= 6 {
			if replacement, ok := removalTriggerDefinitions()[object.Name]; ok {
				expectedSQL = replacement
			}
		}
		if compactSQL(definition) != compactSQL(expectedSQL) {
			return fmt.Errorf("%w: invalid %s %s", ErrIncompleteSchema, object.Kind, object.Name)
		}
	}
	if version == 1 {
		var count int
		if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE name='tasks_reject_intent_update'").Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("%w: partial ADR-006 objects on v1", ErrIncompleteSchema)
		}
	}
	// Deployed v1 already supports these domain constraints; old pre-force-stop
	// and proxy-only network schemas are intentionally not upgraded here.
	var commands string
	if err := q.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE name='worker_commands'").Scan(&commands); err != nil {
		return err
	}
	if !strings.Contains(commands, "'force_stop'") {
		return fmt.Errorf("%w: unsupported worker command schema", ErrIncompleteSchema)
	}
	return nil
}

func compactSQL(value string) string {
	return strings.ToLower(strings.ReplaceAll(strings.Join(strings.Fields(strings.TrimSuffix(strings.TrimSpace(value), ";")), ""), "\"", ""))
}

func applyExternalRoles(ctx context.Context, tx *sql.Tx, options migrationOptions) error {
	if _, err := tx.ExecContext(ctx, externalRolesSchema); err != nil {
		return fmt.Errorf("migrate external roles v4: %w", err)
	}
	if options.beforeV4Commit != nil {
		if err := options.beforeV4Commit(); err != nil {
			return err
		}
	}
	return applyManagedCollaboration(ctx, tx, options)
}
func migrateV3ToV4(ctx context.Context, db *sql.DB, options migrationOptions) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRowContext(ctx, "SELECT version FROM schema_meta WHERE singleton=1").Scan(&version); err != nil {
		return err
	}
	if version != 3 {
		return ErrUnsupportedSchemaVersion
	}
	if err = validateBaseObjects(ctx, tx); err != nil {
		return err
	}
	if err = validateExternalSchema(ctx, tx, 3); err != nil {
		return err
	}
	if err = applyExternalRoles(ctx, tx, options); err != nil {
		return err
	}
	if err = validateObjects(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func applyManagedCollaboration(ctx context.Context, tx *sql.Tx, options migrationOptions) error {
	if _, err := tx.ExecContext(ctx, managedCollaborationSchema); err != nil {
		return fmt.Errorf("migrate managed collaboration v5: %w", err)
	}
	if options.beforeV5Commit != nil {
		if err := options.beforeV5Commit(); err != nil {
			return err
		}
	}
	return applyAgentRemoval(ctx, tx, options)
}
func migrateV4ToV5(ctx context.Context, db *sql.DB, options migrationOptions) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRowContext(ctx, "SELECT version FROM schema_meta WHERE singleton=1").Scan(&version); err != nil {
		return err
	}
	if version != 4 {
		return ErrUnsupportedSchemaVersion
	}
	if err = validateBaseObjects(ctx, tx); err != nil {
		return err
	}
	if err = validateExternalSchema(ctx, tx, 4); err != nil {
		return err
	}
	if err = applyManagedCollaboration(ctx, tx, options); err != nil {
		return err
	}
	if err = validateObjects(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}
