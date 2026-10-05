package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"openagentx/internal/domain"
	"openagentx/internal/persistence/sqlite/migrations"
)

// OpenMaintenance never creates a database or migrates it implicitly. Plans on
// v5 take a deferred snapshot, leaving the live daemon's schema untouched.
func OpenMaintenance(ctx context.Context, path string, readOnly bool, options Options) (*Repository, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("maintenance database must be an existing regular file")
	}
	query := url.Values{"mode": {"rw"}, "_foreign_keys": {"ON"}, "_busy_timeout": {"750"}, "_txlock": {"immediate"}}
	if readOnly {
		query.Set("mode", "ro")
		query.Set("_txlock", "deferred")
	}
	db, err := sql.Open(openAgentXSQLiteDriver, (&url.URL{Scheme: "file", Path: abs, RawQuery: query.Encode()}).String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err = migrations.ValidateAgentRemovalCompatible(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return newRepository(db, options), nil
}

func (r *Repository) UpgradeAgentRemovalSchema(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return migrations.Apply(ctx, r.db)
}

type removalQuery interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type removalSelection struct {
	plan       *domain.AgentRemovalPlan
	predicates map[string]string
	rows       map[string][]int64
}

func normalizedRemovalIDs(ids []string) ([]string, error) {
	protected := map[string]bool{"openagentx": true, "rhythm": true, "pay-service": true, "quote-service": true, "identity-service": true, "oneaxe-voice": true, "orchestrator": true}
	seen := map[string]bool{}
	result := []string{}
	for _, id := range ids {
		if strings.TrimSpace(id) != id || id == "" || len(id) > 128 {
			return nil, domain.ErrInvalidInput("invalid agent ID")
		}
		if protected[id] {
			return nil, domain.ErrForbidden("protected agent: " + id)
		}
		if !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	if len(result) == 0 {
		return nil, domain.ErrInvalidInput("explicit agent IDs are required")
	}
	sort.Strings(result)
	return result, nil
}
func removalLiteral(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
func removalIn(ids []string) string {
	p := make([]string, len(ids))
	for i, id := range ids {
		p[i] = removalLiteral(id)
	}
	return "(" + strings.Join(p, ",") + ")"
}
func removalOwner(ctx context.Context, q removalQuery, actor string) error {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM web_users u JOIN principals p ON p.principal_id=u.principal_id WHERE u.principal_id=? AND u.status='active' AND p.status='active' AND EXISTS(SELECT 1 FROM json_each(u.roles_json) WHERE value='owner')`, actor).Scan(&n)
	if err != nil {
		return err
	}
	if n != 1 {
		return domain.ErrUnauthorized
	}
	return nil
}

func (r *Repository) PlanAgentRemoval(ctx context.Context, actor string, ids []string) (*domain.AgentRemovalPlan, error) {
	ids, err := normalizedRemovalIDs(ids)
	if err != nil {
		return nil, err
	}
	// Read-only handles use deferred BEGIN, so hashing never reserves a writer.
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "BEGIN DEFERRED"); err != nil {
		return nil, err
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")
	selected, err := r.planAgentRemoval(ctx, conn, actor, ids)
	if err != nil {
		return nil, err
	}
	return selected.plan, nil
}

func removalPredicates(ids []string) map[string]string {
	agents := removalIn(ids)
	a := "agent_id IN " + agents
	principal := "SELECT principal_id FROM agents WHERE " + a
	worker := "SELECT worker_instance_id FROM worker_instances WHERE " + a
	task := "SELECT task_id FROM tasks WHERE target_agent_id IN " + agents
	run := "SELECT run_id FROM run_attempts WHERE task_id IN (" + task + ") AND " + a
	approval := "SELECT approval_request_id FROM approval_requests WHERE task_id IN (" + task + ")"
	binding := "SELECT binding_id FROM external_session_bindings WHERE " + a
	message := "SELECT message_id FROM external_messages WHERE sender_agent_id IN " + agents + " AND target_agent_id IN " + agents
	p := map[string]string{
		"agents": a, "principals": "principal_id IN (" + principal + ")", "position_assignments": a, "agent_profiles": a,
		"authority_policies": "subject_kind='principal' AND subject_id IN (" + principal + ")",
		"worker_instances":   a, "runtime_backend_registrations": "worker_instance_id IN (" + worker + ")", "worker_commands": "worker_instance_id IN (" + worker + ")",
		"tasks": "target_agent_id IN " + agents, "messages": "task_id IN (" + task + ")", "run_attempts": "run_id IN (" + run + ")",
		"session_bindings": a, "workspace_leases": "run_id IN (" + run + ")",
		"approval_requests": "approval_request_id IN (" + approval + ")", "approval_decisions": "approval_request_id IN (" + approval + ")",
		"mailbox_items": "target_agent_id IN " + agents, "artifacts": "task_id IN (" + task + ")",
		"external_session_bindings": a, "external_messages": "message_id IN (" + message + ")",
		"managed_message_tasks":    "binding_id IN (" + binding + ") AND task_id IN (" + task + ") AND message_id IN (" + message + ")",
		"external_role_scopes":     "owner_agent_id IN " + agents,
		"network_profile_bindings": a, "network_mode_policies": a, "network_mode_tests": a,
		"network_tests": "worker_instance_id IN (" + worker + ")", "network_work_items": a,
		"network_imports": "worker_instance_id IN (" + worker + ")",
	}
	// These command receipts are owned by an explicit typed result, not by
	// the human actor who issued them. Profile-changing operations stay shared.
	p["network_workflow_commands"] = "((operation='mode_test' AND json_extract(result_json,'$.agent_id') IN " + agents + " AND json_extract(result_json,'$.test_id') IN (SELECT test_id FROM network_mode_tests WHERE " + a + ")) OR (operation IN ('mode_publish','bind') AND json_extract(result_json,'$.agent_id') IN " + agents + " AND json_extract(result_json,'$.work_id') IN (SELECT work_id FROM network_work_items WHERE " + a + ")) OR (operation='test' AND json_extract(result_json,'$.test_id') IN (SELECT test_id FROM network_tests WHERE " + p["network_tests"] + ")) OR (operation='import' AND json_extract(result_json,'$.import_id') IN (SELECT work_id FROM network_imports WHERE " + p["network_imports"] + ")))"
	p["network_workflow_commands"] += " AND NOT EXISTS(SELECT 1 FROM json_each(result_json) WHERE key NOT IN ('test_id','agent_id','backend_id','mode','policy_version','manifest_digest','state','binding_revision','work_id','state_revision','import_id','source_identity') OR type IN ('object','array'))"
	aggregates := map[string][2]string{
		"agent": {"agents", "agent_id"}, "principal": {"principals", "principal_id"}, "worker_instance": {"worker_instances", "worker_instance_id"}, "worker_command": {"worker_commands", "worker_command_id"},
		"task": {"tasks", "task_id"}, "run_attempt": {"run_attempts", "run_id"}, "runtime": {"run_attempts", "run_id"}, "session_binding": {"session_bindings", "session_binding_id"}, "mailbox_item": {"mailbox_items", "mailbox_item_id"}, "approval_request": {"approval_requests", "approval_request_id"}, "external_session": {"external_session_bindings", "binding_id"}, "external_message": {"external_messages", "message_id"},
		"network_binding": {"network_profile_bindings", "agent_id || ':' || backend_id"},
		"network_work":    {"network_work_items", "work_id"}, "network_import": {"network_imports", "work_id"}, "network_test": {"network_tests", "test_id"}, "network_mode_test": {"network_mode_tests", "test_id"},
	}
	aggregates["network_work"] = [2]string{"network_work_items", "work_id"}
	aggregates["network_import"] = [2]string{"network_imports", "work_id"}
	aggregates["network_test"] = [2]string{"network_tests", "test_id"}
	aggregates["network_mode_test"] = [2]string{"network_mode_tests", "test_id"}
	aggregates["message"] = [2]string{"messages", "message_id"}
	aggregates["approval_decision"] = [2]string{"approval_decisions", "approval_decision_id"}
	aggregates["artifact"] = [2]string{"artifacts", "artifact_id"}
	var journal []string
	keys := make([]string, 0, len(aggregates))
	for k := range aggregates {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, kind := range keys {
		v := aggregates[kind]
		journal = append(journal, "(aggregate_type="+removalLiteral(kind)+" AND aggregate_id IN (SELECT "+v[1]+" FROM "+v[0]+" WHERE "+p[v[0]]+"))")
	}
	p["event_journal"] = strings.Join(journal, " OR ")
	return p
}

func (r *Repository) planAgentRemoval(ctx context.Context, q removalQuery, actor string, ids []string) (*removalSelection, error) {
	if err := removalOwner(ctx, q, actor); err != nil {
		return nil, err
	}
	s := &removalSelection{plan: &domain.AgentRemovalPlan{AgentIDs: ids, Counts: map[string]int64{}, Blockers: []string{}}, predicates: removalPredicates(ids), rows: map[string][]int64{}}
	if err := q.QueryRowContext(ctx, "SELECT installation_id FROM installation_metadata WHERE singleton=1").Scan(&s.plan.InstallationID); err != nil {
		return nil, err
	}
	if err := q.QueryRowContext(ctx, "SELECT version FROM schema_meta WHERE singleton=1").Scan(&s.plan.SchemaVersion); err != nil {
		return nil, err
	}
	if s.plan.SchemaVersion != 5 && s.plan.SchemaVersion != 6 {
		return nil, migrations.ErrUnsupportedSchemaVersion
	}
	h := sha256.New()
	encoder := json.NewEncoder(h)
	// v5 -> v6 is additive and must not invalidate a plan made before upgrade.
	_ = encoder.Encode([]any{"agent-removal-v1", s.plan.InstallationID, actor, ids})
	tables := make([]string, 0, len(s.predicates))
	for table := range s.predicates {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	for _, table := range tables {
		if table == "event_journal" {
			// Immutable Journal rows cannot change in place. Per-entity sequence
			// boundaries and count bind this snapshot to every append/removal without
			// copying runtime payloads into the maintenance process. Unrelated events
			// do not invalidate the digest; table trigger shape is validated on open.
			rows, err := q.QueryContext(ctx, "SELECT aggregate_type,aggregate_id,count(*),min(sequence),max(sequence) FROM event_journal WHERE "+s.predicates[table]+" GROUP BY aggregate_type,aggregate_id ORDER BY aggregate_type,aggregate_id")
			if err != nil {
				return nil, err
			}
			_ = encoder.Encode(table)
			s.plan.Counts[table] = 0
			for rows.Next() {
				var kind, id string
				var count, first, last int64
				if err = rows.Scan(&kind, &id, &count, &first, &last); err != nil {
					rows.Close()
					return nil, err
				}
				_ = encoder.Encode([]any{kind, id, count, first, last})
				s.plan.Counts[table] += count
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, err
			}
			continue
		}
		rows, err := q.QueryContext(ctx, "SELECT rowid,* FROM "+table+" WHERE "+s.predicates[table]+" ORDER BY rowid")
		if err != nil {
			return nil, fmt.Errorf("inspect removal %s: %w", table, err)
		}
		cols, err := rows.Columns()
		if err != nil {
			rows.Close()
			return nil, err
		}
		values := make([]any, len(cols))
		dest := make([]any, len(cols))
		for i := range values {
			dest[i] = &values[i]
		}
		_ = encoder.Encode(table)
		for rows.Next() {
			if err = rows.Scan(dest...); err != nil {
				rows.Close()
				return nil, err
			}
			if err = encoder.Encode(values); err != nil {
				rows.Close()
				return nil, err
			}
			s.plan.Counts[table]++
			if table != "event_journal" {
				s.rows[table] = append(s.rows[table], values[0].(int64))
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		if _, ok := s.plan.Counts[table]; !ok {
			s.plan.Counts[table] = 0
		}
	}
	if s.plan.Counts["agents"] == 0 && s.plan.SchemaVersion == 6 {
		encoded, _ := json.Marshal(ids)
		var savedDigest, savedCounts string
		err := q.QueryRowContext(ctx, "SELECT digest,counts_json FROM agent_removal_receipts WHERE actor_principal_id=? AND agent_ids_json=? ORDER BY created_at DESC LIMIT 1", actor, string(encoded)).Scan(&savedDigest, &savedCounts)
		if err == nil {
			s.plan.Counts, s.plan.PreservedReferences, err = decodeRemovalReceiptCounts(savedCounts)
			if err != nil {
				return nil, err
			}
			s.plan.Digest = savedDigest
			s.plan.Replayed = true
			return s, nil
		}
		if err != sql.ErrNoRows {
			return nil, err
		}
	}
	if s.plan.Counts["agents"] != int64(len(ids)) {
		s.plan.Blockers = append(s.plan.Blockers, "one or more selected agents do not exist")
	}
	if err := r.removalBlockers(ctx, q, s); err != nil {
		return nil, err
	}
	sort.Strings(s.plan.Blockers)
	_ = encoder.Encode(s.plan.Blockers)
	_ = encoder.Encode(s.plan.PreservedReferences)
	s.plan.Digest = hex.EncodeToString(h.Sum(nil))
	return s, nil
}

func (r *Repository) removalBlockers(ctx context.Context, q removalQuery, s *removalSelection) error {
	p := s.predicates
	ids := removalIn(s.plan.AgentIDs)
	workerRows, err := q.QueryContext(ctx, "SELECT worker_instance_id FROM worker_instances WHERE ("+p["worker_instances"]+") AND (status<>'offline' OR julianday(lease_until)>julianday(?)) ORDER BY worker_instance_id", formatTime(r.now()))
	if err != nil {
		return err
	}
	for workerRows.Next() {
		var id string
		if err = workerRows.Scan(&id); err != nil {
			workerRows.Close()
			return err
		}
		s.plan.WorkersToStop = append(s.plan.WorkersToStop, id)
	}
	err = workerRows.Err()
	workerRows.Close()
	if err != nil {
		return err
	}
	// Shared organization/profile snapshots retain their original payload.
	// A historical mention is reported, not treated as a live ownership edge.
	preservedRows, err := q.QueryContext(ctx, "SELECT e.aggregate_type,count(DISTINCT e.event_id) FROM event_journal e,json_tree(e.payload_json) j WHERE e.aggregate_type IN ('external_roles','network_profile') AND j.type='text' AND j.key IN ('owner_agent_id','agent_id','worker_instance_id','test_id','work_id','import_id') AND (j.value IN "+ids+" OR j.value IN (SELECT worker_instance_id FROM worker_instances WHERE "+p["worker_instances"]+") OR j.value IN (SELECT test_id FROM network_tests WHERE "+p["network_tests"]+") OR j.value IN (SELECT test_id FROM network_mode_tests WHERE "+p["network_mode_tests"]+") OR j.value IN (SELECT work_id FROM network_work_items WHERE "+p["network_work_items"]+")) GROUP BY e.aggregate_type")
	if err != nil {
		return err
	}
	s.plan.PreservedReferences = map[string]int64{}
	for preservedRows.Next() {
		var aggregate string
		var count int64
		if err = preservedRows.Scan(&aggregate, &count); err != nil {
			preservedRows.Close()
			return err
		}
		s.plan.PreservedReferences["event_journal."+aggregate] = count
	}
	err = preservedRows.Err()
	preservedRows.Close()
	if err != nil {
		return err
	}
	checks := map[string]string{
		"shared sender principal": "SELECT 1 FROM tasks WHERE (" + p["tasks"] + ") AND sender_principal_id IN (SELECT principal_id FROM agents WHERE agent_id NOT IN " + ids + ") UNION ALL SELECT 1 FROM messages WHERE (" + p["messages"] + ") AND sender_principal_id IN (SELECT principal_id FROM agents WHERE agent_id NOT IN " + ids + ") LIMIT 1",

		"active task":                    "SELECT 1 FROM tasks WHERE (" + p["tasks"] + ") AND status IN ('dispatching','running','waiting_approval','cancel_requested') LIMIT 1",
		"live mailbox claim":             "SELECT 1 FROM mailbox_items WHERE (" + p["mailbox_items"] + ") AND state='claimed' AND julianday(lease_until)>julianday(?) LIMIT 1",
		"active run":                     "SELECT 1 FROM run_attempts WHERE (" + p["run_attempts"] + ") AND status IN ('starting','running','waiting_approval','finishing') LIMIT 1",
		"live workspace lease":           "SELECT 1 FROM workspace_leases WHERE (" + p["workspace_leases"] + ") AND julianday(lease_until)>julianday(?) LIMIT 1",
		"active external writer":         "SELECT 1 FROM external_session_bindings WHERE (" + p["external_session_bindings"] + ") AND state='active' AND mode='external' LIMIT 1",
		"shared peer allowlist":          "SELECT 1 FROM external_session_bindings b,json_each(b.allowed_peer_agent_ids_json) j WHERE NOT (" + p["external_session_bindings"] + ") AND j.value IN " + ids + " LIMIT 1",
		"shared provider session":        "SELECT 1 FROM session_bindings WHERE NOT (" + p["session_bindings"] + ") AND provider_session_id IN (SELECT provider_session_id FROM session_bindings WHERE " + p["session_bindings"] + ") LIMIT 1",
		"shared external thread":         "SELECT 1 FROM external_session_bindings WHERE NOT (" + p["external_session_bindings"] + ") AND thread_id IN (SELECT thread_id FROM external_session_bindings WHERE " + p["external_session_bindings"] + " UNION SELECT provider_session_id FROM session_bindings WHERE " + p["session_bindings"] + ") LIMIT 1",
		"shared managed thread":          "SELECT 1 FROM session_bindings WHERE NOT (" + p["session_bindings"] + ") AND provider_session_id IN (SELECT thread_id FROM external_session_bindings WHERE " + p["external_session_bindings"] + ") LIMIT 1",
		"shared role catalog":            "SELECT 1 FROM external_role_scopes WHERE " + p["external_role_scopes"] + " LIMIT 1",
		"shared network profile head":    "SELECT 1 FROM network_profile_heads WHERE ready_test_id IN (SELECT test_id FROM network_tests WHERE " + p["network_tests"] + ") LIMIT 1",
		"shared network binding worker":  "SELECT 1 FROM network_profile_bindings WHERE NOT (" + p["network_profile_bindings"] + ") AND applied_worker_id IN (SELECT worker_instance_id FROM worker_instances WHERE " + p["worker_instances"] + ") LIMIT 1",
		"shared session context":         "SELECT 1 FROM session_bindings WHERE NOT (" + p["session_bindings"] + ") AND context_id IN (SELECT task_id FROM tasks WHERE " + p["tasks"] + ") LIMIT 1",
		"shared authority scope":         "SELECT 1 FROM authority_policies a,json_tree(a.resource_scope_json) j WHERE NOT (" + p["authority_policies"] + ") AND j.type='text' AND j.value IN " + ids + " LIMIT 1",
		"shared network command receipt": "SELECT 1 FROM network_workflow_commands n,json_tree(n.result_json) j WHERE n.rowid NOT IN (SELECT rowid FROM network_workflow_commands WHERE " + p["network_workflow_commands"] + ") AND j.type='text' AND (j.value IN " + ids + " OR j.value IN (SELECT worker_instance_id FROM worker_instances WHERE " + p["worker_instances"] + ") OR j.value IN (SELECT test_id FROM network_tests WHERE " + p["network_tests"] + ") OR j.value IN (SELECT test_id FROM network_mode_tests WHERE " + p["network_mode_tests"] + ") OR j.value IN (SELECT work_id FROM network_work_items WHERE " + p["network_work_items"] + ")) LIMIT 1",
	}
	for label, query := range checks {
		var n int
		var args []any
		if label == "live workspace lease" || label == "live mailbox claim" {
			args = []any{formatTime(r.now())}
		}
		err := q.QueryRowContext(ctx, query, args...).Scan(&n)
		if err == nil {
			s.plan.Blockers = append(s.plan.Blockers, label)
		} else if err != sql.ErrNoRows {
			return fmt.Errorf("inspect %s: %w", label, err)
		}
	}
	return removalForeignKeyBlockers(ctx, q, s)
}

// Every retained FK child is checked, including composite and self references.
// Shared rows are blockers, never recursively absorbed into the deletion set.
func removalForeignKeyBlockers(ctx context.Context, q removalQuery, s *removalSelection) error {
	rows, err := q.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		return err
	}
	var tables []string
	for rows.Next() {
		var t string
		if err = rows.Scan(&t); err != nil {
			rows.Close()
			return err
		}
		tables = append(tables, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	type fkPart struct{ parent, from, to string }
	for _, child := range tables {
		fkrows, err := q.QueryContext(ctx, "PRAGMA foreign_key_list("+removalLiteral(child)+")")
		if err != nil {
			return err
		}
		groups := map[int][]fkPart{}
		for fkrows.Next() {
			var id, seq int
			var parent, from, to, onUpdate, onDelete, match string
			if err = fkrows.Scan(&id, &seq, &parent, &from, &to, &onUpdate, &onDelete, &match); err != nil {
				fkrows.Close()
				return err
			}
			groups[id] = append(groups[id], fkPart{parent, from, to})
		}
		err = fkrows.Err()
		fkrows.Close()
		if err != nil {
			return err
		}
		for _, parts := range groups {
			parent := parts[0].parent
			parentPredicate, ok := s.predicates[parent]
			if !ok {
				continue
			}
			var joins []string
			for _, part := range parts {
				joins = append(joins, "c.\""+part.from+"\"=p.\""+part.to+"\"")
			}
			// Agent/task/run/work references must stay within the selection in both
			// directions. Human/system actors are shared intentionally; actor identity
			// never defines Journal ownership.
			if childPred, owned := s.predicates[child]; owned && parent != "principals" && child != "event_journal" {
				reverse := "SELECT 1 FROM \"" + child + "\" c JOIN \"" + parent + "\" p ON " + strings.Join(joins, " AND ") + " WHERE c.rowid IN (SELECT rowid FROM \"" + child + "\" WHERE " + childPred + ") AND p.rowid NOT IN (SELECT rowid FROM \"" + parent + "\" WHERE " + parentPredicate + ") LIMIT 1"
				var n int
				err = q.QueryRowContext(ctx, reverse).Scan(&n)
				if err == nil {
					s.plan.Blockers = append(s.plan.Blockers, "shared outgoing reference: "+child+" -> "+parent)
				} else if err != sql.ErrNoRows {
					return fmt.Errorf("inspect outgoing reference %s: %w", child, err)
				}
			}
			if s.plan.Counts[parent] == 0 {
				continue
			}
			outside := "1"
			if pred, ok := s.predicates[child]; ok {
				outside = "c.rowid NOT IN (SELECT rowid FROM \"" + child + "\" WHERE " + pred + ")"
			}
			query := "SELECT 1 FROM \"" + child + "\" c JOIN (SELECT * FROM \"" + parent + "\" WHERE " + parentPredicate + ") p ON " + strings.Join(joins, " AND ") + " WHERE " + outside + " LIMIT 1"
			var n int
			err = q.QueryRowContext(ctx, query).Scan(&n)
			if err == nil {
				s.plan.Blockers = append(s.plan.Blockers, "shared reference: "+child+" -> "+parent)
			} else if err != sql.ErrNoRows {
				return fmt.Errorf("inspect removal reference %s: %w", child, err)
			}
		}
	}
	return nil
}

func (r *Repository) ApplyAgentRemoval(ctx context.Context, actor string, ids []string, digest string) (*domain.AgentRemovalResult, error) {
	ids, err := normalizedRemovalIDs(ids)
	if err != nil {
		return nil, err
	}
	if len(digest) != 64 {
		return nil, domain.ErrInvalidInput("plan digest is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = removalOwner(ctx, tx, actor); err != nil {
		return nil, err
	}
	var version int
	if err = tx.QueryRowContext(ctx, "SELECT version FROM schema_meta WHERE singleton=1").Scan(&version); err != nil {
		return nil, err
	}
	if version != 6 {
		return nil, migrations.ErrUnsupportedSchemaVersion
	}
	var savedIDs, savedCounts string
	err = tx.QueryRowContext(ctx, "SELECT agent_ids_json,counts_json FROM agent_removal_receipts WHERE actor_principal_id=? AND digest=?", actor, digest).Scan(&savedIDs, &savedCounts)
	encodedIDs, _ := json.Marshal(ids)
	if err == nil {
		if savedIDs != string(encodedIDs) {
			return nil, domain.ErrIdempotencyConflict
		}
		var n int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM agents WHERE agent_id IN "+removalIn(ids)).Scan(&n); err != nil {
			return nil, err
		}
		if n != 0 {
			return nil, domain.ErrConflict("selected agent identity was recreated")
		}
		result := &domain.AgentRemovalResult{AgentIDs: ids, Digest: digest, Replayed: true}
		result.Counts, result.PreservedReferences, err = decodeRemovalReceiptCounts(savedCounts)
		if err != nil {
			return nil, err
		}
		return result, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	selected, err := r.planAgentRemoval(ctx, tx, actor, ids)
	if err != nil {
		return nil, err
	}
	if selected.plan.Digest != digest {
		return nil, domain.ErrStaleVersion
	}
	if len(selected.plan.WorkersToStop) != 0 {
		return nil, domain.ErrConflict("selected workers must stop before removal")
	}
	if len(selected.plan.Blockers) != 0 {
		return nil, domain.ErrConflict("removal plan has blockers: " + strings.Join(selected.plan.Blockers, ", "))
	}
	// Deferral handles legitimate cycles entirely inside the exclusive closure.
	// Foreign keys stay enabled and commit still validates every retained row.
	if _, err = tx.ExecContext(ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO agent_removal_scope(table_name,entity_key) SELECT DISTINCT 'event_journal',json_array(aggregate_type,aggregate_id) FROM event_journal WHERE "+selected.predicates["event_journal"]); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO agent_removal_scope(table_name,entity_key) SELECT 'network_mode_policies',json_array(agent_id,backend_id,policy_version) FROM network_mode_policies WHERE "+selected.predicates["network_mode_policies"]); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM event_journal WHERE "+selected.predicates["event_journal"]); err != nil {
		return nil, err
	}
	if err = r.inject(FaultAfterDelivery); err != nil {
		return nil, err
	}
	order := []string{"managed_message_tasks", "external_messages", "external_session_bindings", "mailbox_items", "artifacts", "approval_decisions", "approval_requests", "workspace_leases", "run_attempts", "messages", "session_bindings", "tasks", "network_workflow_commands", "network_imports", "network_work_items", "network_mode_tests", "network_tests", "network_profile_bindings", "network_mode_policies", "worker_commands", "runtime_backend_registrations", "worker_instances", "external_role_scopes", "authority_policies", "position_assignments", "agent_profiles", "agents", "principals"}
	for _, table := range order {
		rowIDs := selected.rows[table]
		for len(rowIDs) > 0 {
			n := len(rowIDs)
			if n > 400 {
				n = 400
			}
			args := make([]any, n)
			marks := make([]string, n)
			for i, id := range rowIDs[:n] {
				args[i] = id
				marks[i] = "?"
			}
			if _, err = tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE rowid IN ("+strings.Join(marks, ",")+")", args...); err != nil {
				return nil, fmt.Errorf("remove %s: %w", table, err)
			}
			rowIDs = rowIDs[n:]
		}
	}
	if err = r.inject(FaultAfterStateWrite); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM agent_removal_scope"); err != nil {
		return nil, err
	}
	counts, _ := json.Marshal(removalReceiptCounts{Counts: selected.plan.Counts, PreservedReferences: selected.plan.PreservedReferences})
	if _, err = tx.ExecContext(ctx, "INSERT INTO agent_removal_receipts(actor_principal_id,digest,agent_ids_json,counts_json,created_at) VALUES(?,?,?,?,?)", actor, digest, string(encodedIDs), string(counts), formatTime(r.now())); err != nil {
		return nil, err
	}
	if err = r.inject(FaultBeforeCommit); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &domain.AgentRemovalResult{AgentIDs: ids, Digest: digest, Counts: selected.plan.Counts, PreservedReferences: selected.plan.PreservedReferences}, nil
}

// counts_json predates shared-history reporting. Decode both that flat map and
// the additive envelope, so already-committed v6 receipts remain replayable.
type removalReceiptCounts struct {
	Counts              map[string]int64 `json:"counts"`
	PreservedReferences map[string]int64 `json:"preserved_references"`
}

func decodeRemovalReceiptCounts(raw string) (map[string]int64, map[string]int64, error) {
	var shape map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &shape); err != nil {
		return nil, nil, err
	}
	if _, envelope := shape["counts"]; envelope {
		var v removalReceiptCounts
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, nil, err
		}
		if v.Counts == nil {
			return nil, nil, fmt.Errorf("invalid removal receipt counts")
		}
		if v.PreservedReferences == nil {
			v.PreservedReferences = map[string]int64{}
		}
		return v.Counts, v.PreservedReferences, nil
	}
	var counts map[string]int64
	if err := json.Unmarshal([]byte(raw), &counts); err != nil {
		return nil, nil, err
	}
	return counts, map[string]int64{}, nil
}
