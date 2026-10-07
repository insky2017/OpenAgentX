package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openagentx/internal/domain"
	"openagentx/internal/persistence/sqlite/migrations"
)

func removalFixture(t *testing.T) (*Repository, string) {
	t.Helper()
	r, path := openTestRepository(t, nil)
	removalExec(t, r.db, `
 INSERT INTO principals VALUES('owner','human','Owner','active','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z'),('p-test','agent','Test','active','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z'),('p-kept','agent','Kept','active','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');
 INSERT INTO organizations VALUES('org','Org','active','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');
 INSERT INTO web_users(web_user_id,principal_id,username,password_hash,roles_json,status,password_changed_at,created_at,updated_at) VALUES('user','owner','owner','not-a-credential','["owner"]','active','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');
 INSERT INTO agents VALUES('test','p-test','org','Test','active',1,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z'),('kept','p-kept','org','Kept','active',1,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');
 INSERT INTO worker_instances(worker_instance_id,agent_id,generation,transport,authenticated_principal,capabilities_json,status,last_heartbeat_at,lease_until,fencing_token,started_at,updated_at) VALUES('worker-test','test',1,'unix','p-test','[]','offline','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z',1,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');
 INSERT INTO tasks(task_id,status,sender_principal_id,target_agent_id,dispatch_mode,organization_id,idempotency_key,content,created_at,updated_at) VALUES('task-test','uncertain','owner','test','direct','org','key-test','fixture body','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z'),('task-kept','queued','owner','kept','direct','org','key-kept','keep body','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');
 INSERT INTO run_attempts(run_id,task_id,agent_id,status,worker_instance_id,fencing_token,lease_until,execution_spec_version,requested_execution_json,resolved_execution_json,adapter_id,backend_id,model,reasoning_mode,reasoning_value,started_at,created_at,updated_at) VALUES('run-test','task-test','test','uncertain','worker-test',1,'2026-01-01T00:00:00Z',1,'{}','{}','test','test','test','none','','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');
 INSERT INTO mailbox_items(mailbox_item_id,target_agent_id,kind,lane,task_id,state,created_at) VALUES('mail-test','test','task','work','task-test','pending','2026-01-01T00:00:00Z');
 INSERT INTO session_bindings VALUES('session-test','task-test','test','test','thread-test','active',1,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');
 INSERT INTO network_mode_policies VALUES('test','test',1,'direct','manifest','owner','2026-01-01T00:00:00Z');
 INSERT INTO event_journal(event_id,organization_id,aggregate_type,aggregate_id,event_type,actor_principal_id,payload_json,created_at) VALUES('event-test','org','runtime','run-test','runtime.output','owner','{"body":"fixture private data"}','2026-01-01T00:00:00Z'),('event-kept','org','task','task-kept','task.created','owner','{}','2026-01-01T00:00:00Z');
 `)
	return r, path
}
func removalExec(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.Exec(query); err != nil {
		t.Fatal(err)
	}
}
func removalCount(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func removalPlan(t *testing.T, r *Repository) *domain.AgentRemovalPlan {
	t.Helper()
	p, err := r.PlanAgentRemoval(context.Background(), "owner", []string{"test"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAgentRemovalExclusiveHistoryAtomicReplayAndRecreation(t *testing.T) {
	r, _ := removalFixture(t)
	ctx := context.Background()
	p := removalPlan(t, r)
	if len(p.Blockers) != 0 || p.Counts["event_journal"] != 1 {
		t.Fatalf("unexpected plan: %+v", p)
	}
	for _, point := range []FaultPoint{FaultAfterDelivery, FaultAfterStateWrite, FaultBeforeCommit} {
		failing := newRepository(r.db, Options{FaultInjector: func(got FaultPoint) error {
			if got == point {
				return errors.New("injected")
			}
			return nil
		}})
		if _, err := failing.ApplyAgentRemoval(ctx, "owner", p.AgentIDs, p.Digest); err == nil {
			t.Fatal("injected operation committed")
		}
		if removalCount(t, r.db, "agents") != 2 || removalCount(t, r.db, "event_journal") != 2 || removalCount(t, r.db, "agent_removal_scope") != 0 || removalCount(t, r.db, "agent_removal_receipts") != 0 {
			t.Fatal("partial purge survived rollback")
		}
	}
	result, err := r.ApplyAgentRemoval(ctx, "owner", p.AgentIDs, p.Digest)
	if err != nil {
		t.Fatal(err)
	}
	if result.Replayed {
		t.Fatal("first apply replayed")
	}
	for _, table := range []string{"worker_instances", "run_attempts", "mailbox_items", "session_bindings", "network_mode_policies", "agent_removal_scope"} {
		if removalCount(t, r.db, table) != 0 {
			t.Fatalf("history retained in %s", table)
		}
	}
	if removalCount(t, r.db, "agents") != 1 || removalCount(t, r.db, "tasks") != 1 || removalCount(t, r.db, "event_journal") != 1 {
		t.Fatal("preserved closure altered")
	}
	var seq int
	if err = r.db.QueryRow("SELECT seq FROM sqlite_sequence WHERE name='event_journal'").Scan(&seq); err != nil || seq != 2 {
		t.Fatal("sequence reset", seq, err)
	}
	result, err = r.ApplyAgentRemoval(ctx, "owner", p.AgentIDs, p.Digest)
	if err != nil || !result.Replayed {
		t.Fatal("replay", err)
	}
	removalExec(t, r.db, `INSERT INTO principals VALUES('p-test-new','agent','Test','active','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z'); INSERT INTO agents VALUES('test','p-test-new','org','New','active',1,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`)
	if _, err = r.ApplyAgentRemoval(ctx, "owner", p.AgentIDs, p.Digest); err == nil {
		t.Fatal("re-created agent accepted by old receipt")
	}
}

func TestAgentRemovalBlockersDigestAndAuthorization(t *testing.T) {
	cases := []struct{ name, sql, blocker string }{
		{"incoming task", `UPDATE tasks SET parent_task_id='task-test' WHERE task_id='task-kept'`, "shared reference: tasks -> tasks"},
		{"outgoing task", `UPDATE tasks SET parent_task_id='task-kept' WHERE task_id='task-test'`, "shared outgoing reference: tasks -> tasks"},
		{"shared thread", `INSERT INTO session_bindings VALUES('session-kept','task-kept','kept','test','thread-test','invalid',1,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, "shared provider session"},
		{"active run", `UPDATE run_attempts SET status='running'`, "active run"},
		{"live lease", `INSERT INTO workspace_leases VALUES('lease','workspace','run-test','worker-test',1,'2099-01-01T00:00:00Z','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, "live workspace lease"},
		{"shared journal actor", `INSERT INTO event_journal(event_id,aggregate_type,aggregate_id,event_type,actor_principal_id,payload_json,created_at) VALUES('shared','task','task-kept','task.changed','p-test','{}','2026-01-01T00:00:00Z')`, "shared reference: event_journal -> principals"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, _ := removalFixture(t)
			removalExec(t, r.db, c.sql)
			p := removalPlan(t, r)
			if !strings.Contains(strings.Join(p.Blockers, ";"), c.blocker) {
				t.Fatalf("missing blocker %s in %v", c.blocker, p.Blockers)
			}
			if _, err := r.ApplyAgentRemoval(context.Background(), "owner", p.AgentIDs, p.Digest); err == nil {
				t.Fatal("blocked plan applied")
			}
		})
	}
	t.Run("digest and owner", func(t *testing.T) {
		r, _ := removalFixture(t)
		p := removalPlan(t, r)
		removalExec(t, r.db, `UPDATE tasks SET content='changed but same count' WHERE task_id='task-test'`)
		if _, err := r.ApplyAgentRemoval(context.Background(), "owner", p.AgentIDs, p.Digest); !errors.Is(err, domain.ErrStaleVersion) {
			t.Fatal("stale digest", err)
		}
		if _, err := r.PlanAgentRemoval(context.Background(), "p-kept", []string{"test"}); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatal("non-owner", err)
		}
		for _, id := range []string{"openagentx", "rhythm", "pay-service", "quote-service", "identity-service", "oneaxe-voice", "orchestrator"} {
			if _, err := r.PlanAgentRemoval(context.Background(), "owner", []string{id}); err == nil {
				t.Fatal("protected ID accepted", id)
			}
		}
	})
	t.Run("idle worker", func(t *testing.T) {
		r, _ := removalFixture(t)
		removalExec(t, r.db, `UPDATE worker_instances SET status='online'`)
		p := removalPlan(t, r)
		if len(p.Blockers) != 0 || len(p.WorkersToStop) != 1 {
			t.Fatalf("idle worker plan %+v", p)
		}
		if _, err := r.ApplyAgentRemoval(context.Background(), "owner", p.AgentIDs, p.Digest); err == nil {
			t.Fatal("live worker removed")
		}
	})
}

func TestAgentRemovalGuardsCannotCommitAndRemainExact(t *testing.T) {
	r, _ := removalFixture(t)
	ctx := context.Background()
	if _, err := r.db.Exec("DELETE FROM event_journal"); err == nil {
		t.Fatal("ordinary journal deletion allowed")
	}
	if _, err := r.db.Exec("DELETE FROM network_mode_policies"); err == nil {
		t.Fatal("ordinary immutable policy deletion allowed")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO agent_removal_scope(table_name,entity_key) VALUES('event_journal','["runtime","run-test"]')`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`DELETE FROM event_journal WHERE event_id='event-kept'`); err == nil {
		t.Fatal("scope permitted wrong entity")
	}
	if _, err = tx.Exec(`DELETE FROM event_journal WHERE event_id='event-test'`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err == nil {
		t.Fatal("transaction-scoped authorization survived commit")
	}
	if removalCount(t, r.db, "event_journal") != 2 || removalCount(t, r.db, "agent_removal_scope") != 0 {
		t.Fatal("failed guard commit leaked state")
	}
}

func TestAgentRemovalV5ReadOnlyAndAdditiveUpgrade(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open(openAgentXSQLiteDriver, "file:"+path+"?_foreign_keys=ON&_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, file := range []string{"001_target_schema.sql", "003_external_sessions.sql", "004_external_roles.sql", "005_managed_collaboration.sql"} {
		b, err := os.ReadFile(filepath.Join("migrations", file))
		if err != nil {
			t.Fatal(err)
		}
		removalExec(t, db, string(b))
	}
	removalExec(t, db, `INSERT INTO installation_metadata VALUES(1,'old-installation','2026-01-01T00:00:00Z'); INSERT INTO principals VALUES('owner','human','Owner','active','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z'); INSERT INTO web_users VALUES('u','owner','owner','not-a-credential','["owner"]','active','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`)
	ro, err := OpenMaintenance(ctx, path, true, Options{})
	if err != nil {
		t.Fatal(err)
	}
	p, err := ro.PlanAgentRemoval(ctx, "owner", []string{"missing-test"})
	if err != nil || p.SchemaVersion != 5 {
		t.Fatal("read-only v5 plan", err)
	}
	ro.Close()
	var v int
	if err = db.QueryRow("SELECT version FROM schema_meta").Scan(&v); err != nil || v != 5 {
		t.Fatal("plan mutated v5", v, err)
	}
	rw, err := OpenMaintenance(ctx, path, false, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()
	if err = rw.UpgradeAgentRemovalSchema(ctx); err != nil {
		t.Fatal(err)
	}
	// An already-open v5-style connection still performs normal writes.
	removalExec(t, db, `INSERT INTO event_journal(event_id,aggregate_type,aggregate_id,event_type,actor_principal_id,payload_json,created_at) VALUES('after-upgrade','principal','owner','owner.checked','owner','{}','2026-01-01T00:00:00Z')`)
	if err = db.QueryRow("SELECT version FROM schema_meta").Scan(&v); err != nil || v != migrations.CurrentVersion {
		t.Fatal("upgrade", v, err)
	}
	var installation string
	if err = db.QueryRow("SELECT installation_id FROM installation_metadata").Scan(&installation); err != nil || installation != "old-installation" {
		t.Fatal("installation changed", err)
	}
	if _, err = OpenMaintenance(ctx, filepath.Join(t.TempDir(), "missing.db"), true, Options{}); !os.IsNotExist(err) {
		t.Fatal("maintenance created missing database", err)
	}
}

func TestAgentRemovalProductionScaleBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("production-scale SQLite integration")
	}
	r, _ := removalFixture(t)
	// Rough deployed proportions: 350k total events, 66k exclusively selected.
	removalExec(t, r.db, `WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<350000) INSERT INTO event_journal(event_id,aggregate_type,aggregate_id,event_type,actor_principal_id,payload_json,created_at) SELECT 'scale-'||x,CASE WHEN x<=66000 THEN 'runtime' ELSE 'task' END,CASE WHEN x<=66000 THEN 'run-test' ELSE 'task-kept' END,'runtime.output','owner',json_object('body',printf('%0300d',x)),'2026-01-01T00:00:00Z' FROM n`)
	start := time.Now()
	p := removalPlan(t, r)
	planDuration := time.Since(start)
	if len(p.Blockers) != 0 {
		t.Fatal(p.Blockers)
	}
	start = time.Now()
	result, err := r.ApplyAgentRemoval(context.Background(), "owner", p.AgentIDs, p.Digest)
	elapsed := time.Since(start)
	t.Logf("350002 events; plan=%s; apply=%s; err=%v", planDuration, elapsed, err)
	if err != nil {
		if removalCount(t, r.db, "agents") != 2 || removalCount(t, r.db, "event_journal") != 350002 {
			t.Fatal("timeout partially committed")
		}
		t.Fatal("bounded production-scale purge failed", err)
	}
	if result.Counts["event_journal"] != 66001 || removalCount(t, r.db, "event_journal") != 284001 {
		t.Fatal("scale closure mismatch")
	}
	if elapsed > 2*time.Second {
		t.Fatal(fmt.Sprintf("apply exceeded deadline: %s", elapsed))
	}
}

func TestAgentRemovalNetworkReceiptOwnership(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprint("mixed=", mixed), func(t *testing.T) {
			r, _ := removalFixture(t)
			removalExec(t, r.db, `INSERT INTO network_mode_tests(test_id,agent_id,backend_id,policy_version,mode,manifest_digest,worker_instance_id,generation,runtime_identity_json,binding_revision,state,created_by,created_at) VALUES('mode-test','test','test',1,'direct','manifest','worker-test',1,'{}',0,'succeeded','owner','2026-01-01T00:00:00Z');
 INSERT INTO network_work_items(work_id,kind,agent_id,backend_id,worker_instance_id,generation,runtime_identity_json,state,created_at) VALUES('apply-test','apply','test','test','worker-test',1,'{}','succeeded','2026-01-01T00:00:00Z');
 INSERT INTO network_workflow_commands VALUES('owner','mode_test','mode-key','digest','{"agent_id":"test","test_id":"mode-test","state":"pending"}','2026-01-01T00:00:00Z'),('owner','mode_publish','publish-key','digest','{"agent_id":"test","work_id":"apply-test","state":"pending"}','2026-01-01T00:00:00Z'),('owner','create','shared-key','digest','{"profile_id":"shared-profile"}','2026-01-01T00:00:00Z');
 INSERT INTO event_journal(event_id,aggregate_type,aggregate_id,event_type,actor_principal_id,payload_json,created_at) VALUES('mode-event','network_mode_test','mode-test','network.mode_test_requested','owner','{}','2026-01-01T00:00:00Z'),('work-event','network_work','apply-test','network.work_applied','owner','{}','2026-01-01T00:00:00Z')`)
			if mixed {
				removalExec(t, r.db, `UPDATE network_workflow_commands SET result_json=json_set(result_json,'$.other_agent_id','kept') WHERE operation='mode_test'`)
			}
			p := removalPlan(t, r)
			if mixed {
				if !strings.Contains(strings.Join(p.Blockers, ";"), "shared network command receipt") {
					t.Fatal("mixed receipt not blocked", p.Blockers)
				}
				return
			}
			if len(p.Blockers) != 0 || p.Counts["network_workflow_commands"] != 2 || p.Counts["event_journal"] != 3 {
				t.Fatalf("network closure %+v", p)
			}
			if _, err := r.ApplyAgentRemoval(context.Background(), "owner", p.AgentIDs, p.Digest); err != nil {
				t.Fatal(err)
			}
			if removalCount(t, r.db, "network_workflow_commands") != 1 {
				t.Fatal("shared receipt changed")
			}
		})
	}
}

func TestAgentRemovalCommittedReceiptPlansFilesystemRetry(t *testing.T) {
	r, _ := removalFixture(t)
	ctx := context.Background()
	p := removalPlan(t, r)
	if _, err := r.ApplyAgentRemoval(ctx, "owner", p.AgentIDs, p.Digest); err != nil {
		t.Fatal(err)
	}
	replay := removalPlan(t, r)
	if !replay.Replayed || replay.Digest != p.Digest || len(replay.Blockers) != 0 || replay.Counts["agents"] != 1 {
		t.Fatalf("retry plan %+v", replay)
	}
	if got, err := r.ApplyAgentRemoval(ctx, "owner", replay.AgentIDs, replay.Digest); err != nil || !got.Replayed {
		t.Fatal("receipt retry", err)
	}
	missing, err := r.PlanAgentRemoval(ctx, "owner", []string{"missing"})
	if err != nil || missing.Replayed || len(missing.Blockers) == 0 {
		t.Fatal("missing identity incorrectly accepted", err)
	}
}

func TestAgentRemovalContextCancellationRollsBack(t *testing.T) {
	r, _ := removalFixture(t)
	p := removalPlan(t, r)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopping := newRepository(r.db, Options{FaultInjector: func(point FaultPoint) error {
		if point == FaultAfterDelivery {
			cancel()
		}
		return nil
	}})
	if _, err := stopping.ApplyAgentRemoval(ctx, "owner", p.AgentIDs, p.Digest); err == nil {
		t.Fatal("canceled purge succeeded")
	}
	if removalCount(t, r.db, "agents") != 2 || removalCount(t, r.db, "event_journal") != 2 || removalCount(t, r.db, "agent_removal_scope") != 0 {
		t.Fatal("canceled purge partially committed")
	}
}

func TestAgentRemovalExactMultiAgentClosureAndImmutableJournalDigest(t *testing.T) {
	r, _ := removalFixture(t)
	ctx := context.Background()
	removalExec(t, r.db, `INSERT INTO principals VALUES('p-test2','agent','Test2','active','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');
 INSERT INTO agents VALUES('test2','p-test2','org','Test2','active',1,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');
 INSERT INTO tasks(task_id,status,sender_principal_id,target_agent_id,dispatch_mode,organization_id,idempotency_key,content,created_at,updated_at,parent_task_id) VALUES('task-test2','queued','owner','test2','direct','org','key-test2','fixture','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','task-test');
 UPDATE tasks SET parent_task_id='task-test2' WHERE task_id='task-test';
 INSERT INTO external_session_bindings(binding_id,agent_id,host_id,thread_id,generation,state,token_digest,token_expires_at,allowed_peer_agent_ids_json,created_at,updated_at,mode,managed_context_task_id) VALUES('ext-test','test','host','thread-test',1,'revoked','fixture-digest-1','2026-01-01T00:00:00Z','["test2"]','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','managed','task-test'),('ext-test2','test2','host','thread-test2',1,'revoked','fixture-digest-2','2026-01-01T00:00:00Z','["test"]','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','managed','task-test2');
 INSERT INTO external_messages(message_id,sender_agent_id,target_agent_id,sender_binding_id,sender_generation,kind,content,idempotency_key,payload_digest,origin_task_id,delivery_state,created_at) VALUES('ext-message','test','test2','ext-test',1,'consultation','fixture','ext-key','fixture-digest','task-test','pending','2026-01-01T00:00:00Z');
 INSERT INTO managed_message_tasks(message_id,task_id,role,binding_id,binding_generation) VALUES('ext-message','task-test2','consultation','ext-test2',1)`)
	single := removalPlan(t, r)
	if len(single.Blockers) == 0 {
		t.Fatal("single selection expanded across Agents")
	}
	p, err := r.PlanAgentRemoval(ctx, "owner", []string{"test2", "test", "test"})
	if err != nil || len(p.Blockers) != 0 || len(p.AgentIDs) != 2 {
		t.Fatalf("multi plan %+v %v", p, err)
	}
	removalExec(t, r.db, `INSERT INTO event_journal(event_id,aggregate_type,aggregate_id,event_type,actor_principal_id,payload_json,created_at) VALUES('unrelated','task','task-kept','task.changed','owner','{}','2026-01-01T00:00:00Z')`)
	after, err := r.PlanAgentRemoval(ctx, "owner", p.AgentIDs)
	if err != nil || after.Digest != p.Digest {
		t.Fatal("unrelated append invalidated plan", err)
	}
	removalExec(t, r.db, `INSERT INTO event_journal(event_id,aggregate_type,aggregate_id,event_type,actor_principal_id,payload_json,created_at) VALUES('selected-append','external_message','ext-message','message.changed','owner','{}','2026-01-01T00:00:00Z')`)
	if _, err = r.ApplyAgentRemoval(ctx, "owner", p.AgentIDs, p.Digest); !errors.Is(err, domain.ErrStaleVersion) {
		t.Fatal("selected append did not invalidate plan", err)
	}
	after, err = r.PlanAgentRemoval(ctx, "owner", p.AgentIDs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.ApplyAgentRemoval(ctx, "owner", after.AgentIDs, after.Digest); err != nil {
		t.Fatal("exclusive cyclic multi selection", err)
	}
	if removalCount(t, r.db, "agents") != 1 || removalCount(t, r.db, "tasks") != 1 || removalCount(t, r.db, "external_messages") != 0 || removalCount(t, r.db, "managed_message_tasks") != 0 {
		t.Fatal("multi closure incomplete")
	}
}

func TestAgentRemovalPreservesMixedCatalogHistoryAndReplaysWarnings(t *testing.T) {
	r, _ := removalFixture(t)
	ctx := context.Background()
	const payload = `{"organization_id":"org","revision":1,"rules":[{"scope":"test.scope","owner_agent_id":"test","description":"historical test"},{"scope":"business.scope","owner_agent_id":"kept","description":"business preserved"}]}`
	if _, err := r.db.Exec(`INSERT INTO event_journal(event_id,aggregate_type,aggregate_id,event_type,actor_principal_id,payload_json,created_at) VALUES('catalog-history','external_roles','org','external_roles.applied','owner',?,'2026-01-01T00:00:00Z'),('profile-history','network_profile','profile-shared','network.profile_checked','owner','{"worker_instance_id":"worker-test","test_id":"historical","profile_id":"shared"}','2026-01-01T00:00:00Z')`, payload); err != nil {
		t.Fatal(err)
	}
	var beforeSeq int64
	if err := r.db.QueryRow(`SELECT sequence FROM event_journal WHERE event_id='catalog-history'`).Scan(&beforeSeq); err != nil {
		t.Fatal(err)
	}
	p := removalPlan(t, r)
	if len(p.Blockers) != 0 || p.PreservedReferences["event_journal.external_roles"] != 1 || p.PreservedReferences["event_journal.network_profile"] != 1 || p.Counts["event_journal"] != 1 {
		t.Fatalf("mixed history plan %+v", p)
	}
	result, err := r.ApplyAgentRemoval(ctx, "owner", p.AgentIDs, p.Digest)
	if err != nil {
		t.Fatal(err)
	}
	if result.PreservedReferences["event_journal.external_roles"] != 1 || result.PreservedReferences["event_journal.network_profile"] != 1 {
		t.Fatal("preservation report missing", result)
	}
	var afterSeq int64
	var afterPayload string
	if err = r.db.QueryRow(`SELECT sequence,payload_json FROM event_journal WHERE event_id='catalog-history'`).Scan(&afterSeq, &afterPayload); err != nil || afterSeq != beforeSeq || afterPayload != payload {
		t.Fatal("mixed organization history altered", err)
	}
	if removalCount(t, r.db, "event_journal") != 3 || removalCount(t, r.db, "agents") != 1 {
		t.Fatal("exclusive purge or shared retention failed")
	}
	retry := removalPlan(t, r)
	if !retry.Replayed || retry.PreservedReferences["event_journal.external_roles"] != 1 {
		t.Fatal("retry plan lost preservation report", retry)
	}
	replay, err := r.ApplyAgentRemoval(ctx, "owner", p.AgentIDs, p.Digest)
	if err != nil || !replay.Replayed || replay.PreservedReferences["event_journal.network_profile"] != 1 {
		t.Fatal("replay lost preservation report", err)
	}
	// Compatibility with receipts already committed by the initial v6 candidate.
	removalExec(t, r.db, `UPDATE agent_removal_receipts SET counts_json=json_extract(counts_json,'$.counts')`)
	old, err := r.ApplyAgentRemoval(ctx, "owner", p.AgentIDs, p.Digest)
	if err != nil || !old.Replayed || old.Counts["agents"] != 1 {
		t.Fatal("initial v6 receipt incompatible", err)
	}
	oldPlan := removalPlan(t, r)
	if !oldPlan.Replayed || oldPlan.Counts["agents"] != 1 {
		t.Fatal("initial v6 retry plan incompatible")
	}
}

func TestAgentRemovalCurrentRoleAssignmentStillBlocks(t *testing.T) {
	r, _ := removalFixture(t)
	removalExec(t, r.db, `INSERT INTO external_role_catalogs VALUES('org',1); INSERT INTO external_role_scopes VALUES('org','active.test.scope','test','current role')`)
	p := removalPlan(t, r)
	if !strings.Contains(strings.Join(p.Blockers, ";"), "shared role catalog") {
		t.Fatal("live role became mere history warning", p.Blockers)
	}
}
