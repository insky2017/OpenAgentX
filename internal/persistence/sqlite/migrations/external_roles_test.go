package migrations

import (
	"context"
	"errors"
	"testing"
)

func TestExternalV3ToV4AtomicMigrationAndShapeValidation(t *testing.T) {
	ctx := context.Background()
	db := openIntentMigrationDB(t)
	if _, err := db.Exec(targetSchema + "\n" + externalSessionSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO installation_metadata VALUES(1,'preserved-installation','2026-10-03T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	// Seed genuine v3 rows: one acknowledged unfinished request and one historical result.
	for _, sql := range []string{
		`INSERT INTO organizations VALUES('org','Org','active','2026-10-03T00:00:00Z','2026-10-03T00:00:00Z')`,
		`INSERT INTO principals VALUES('p-a','agent','A','active','2026-10-03T00:00:00Z','2026-10-03T00:00:00Z'),('p-b','agent','B','active','2026-10-03T00:00:00Z','2026-10-03T00:00:00Z')`,
		`INSERT INTO agents VALUES('a','p-a','org','A','active',1,'2026-10-03T00:00:00Z','2026-10-03T00:00:00Z'),('b','p-b','org','B','active',1,'2026-10-03T00:00:00Z','2026-10-03T00:00:00Z')`,
		`INSERT INTO external_session_bindings VALUES('binding-a','a','desktop','thread-a',1,'active','digest-a','2026-11-03T00:00:00Z','["b"]','2026-10-03T00:00:00Z','2026-10-03T00:00:00Z'),('binding-b','b','desktop','thread-b',1,'active','digest-b','2026-11-03T00:00:00Z','["a"]','2026-10-03T00:00:00Z','2026-10-03T00:00:00Z')`,
		`INSERT INTO external_messages(message_id,sender_agent_id,target_agent_id,sender_binding_id,sender_generation,kind,reply_to_message_id,content,idempotency_key,payload_digest,delivery_state,created_at,acknowledged_at) VALUES('unfinished','a','b','binding-a',1,'request',NULL,'original unresolved','key-1','digest-1','acknowledged','2026-10-03T00:00:00Z','2026-10-03T00:01:00Z'),('finished','a','b','binding-a',1,'request',NULL,'original finished','key-2','digest-2','pending','2026-10-03T00:00:00Z',NULL),('reply','b','a','binding-b',1,'result','finished','historical reply','key-3','digest-3','pending','2026-10-03T00:00:00Z',NULL)`,
	} {
		if _, err := db.Exec(sql); err != nil {
			t.Fatal(err)
		}
	}
	if err := apply(ctx, db, migrationOptions{beforeV4Commit: func() error { return errors.New("v4 fail") }}); err == nil {
		t.Fatal("faulted migration succeeded")
	}
	if v, err := Version(ctx, db); err != nil || v != 3 {
		t.Fatalf("source version not preserved %d %v", v, err)
	}
	if ok, err := tableExists(ctx, db, "external_role_scopes"); err != nil || ok {
		t.Fatal("partial roles table")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('external_messages') WHERE name='processing_state'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial state column")
	}
	if err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCurrent(ctx, db); err != nil {
		t.Fatal(err)
	}
	if v, err := Version(ctx, db); err != nil || v != CurrentVersion {
		t.Fatalf("final version %d %v", v, err)
	}
	var state, delivery, body string
	if err := db.QueryRow(`SELECT processing_state,delivery_state,content FROM external_messages WHERE message_id='unfinished'`).Scan(&state, &delivery, &body); err != nil || state != "pending" || delivery != "acknowledged" || body != "original unresolved" {
		t.Fatalf("legacy unfinished changed: %s %s %s %v", state, delivery, body, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM external_messages WHERE processing_state='completed'`).Scan(&count); err != nil || count != 2 {
		t.Fatal("historical result and parent not completed")
	}
	var digest string
	if err := db.QueryRow(`SELECT token_digest FROM external_session_bindings WHERE agent_id='a'`).Scan(&digest); err != nil || digest != "digest-a" {
		t.Fatal("binding credential changed")
	}
	if _, err := db.Exec(`DROP INDEX uq_external_single_forward`); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCurrent(ctx, db); !errors.Is(err, ErrIncompleteSchema) {
		t.Fatalf("missing forward uniqueness accepted: %v", err)
	}
}
