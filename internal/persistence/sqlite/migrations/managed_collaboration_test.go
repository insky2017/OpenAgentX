package migrations

import (
	"context"
	"errors"
	"testing"
)

func TestManagedV4MigrationPreservesExternalRowsAndRollsBackAtomically(t *testing.T) {
	ctx := context.Background()
	db := openIntentMigrationDB(t)
	if _, err := db.Exec(targetSchema + "\n" + externalSessionSchema + "\n" + externalRolesSchema); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO installation_metadata VALUES(1,'original-installation','2026-10-03T00:00:00Z')`,
		`INSERT INTO organizations VALUES('org','Org','active','2026-10-03T00:00:00Z','2026-10-03T00:00:00Z')`,
		`INSERT INTO principals VALUES('p-a','agent','A','active','2026-10-03T00:00:00Z','2026-10-03T00:00:00Z'),('p-b','agent','B','active','2026-10-03T00:00:00Z','2026-10-03T00:00:00Z')`,
		`INSERT INTO agents VALUES('a','p-a','org','A','active',1,'2026-10-03T00:00:00Z','2026-10-03T00:00:00Z'),('b','p-b','org','B','active',1,'2026-10-03T00:00:00Z','2026-10-03T00:00:00Z')`,
		`INSERT INTO external_session_bindings VALUES('binding-a','a','original-desktop','original-thread',7,'active','original-digest','2026-11-03T00:00:00Z','["b"]','2026-10-03T00:00:00Z','2026-10-03T00:00:00Z')`,
		`INSERT INTO external_messages(message_id,sender_agent_id,target_agent_id,sender_binding_id,sender_generation,kind,content,idempotency_key,payload_digest,delivery_state,created_at,processing_state,processing_note) VALUES('historical-request','a','b','binding-a',7,'consultation','original-body','original-key','original-payload','pending','2026-10-03T00:00:00Z','accepted','original-note')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := apply(ctx, db, migrationOptions{beforeV5Commit: func() error { return errors.New("v5 rollback") }}); err == nil {
		t.Fatal("injected failure committed")
	}
	if v, err := Version(ctx, db); err != nil || v != 4 {
		t.Fatalf("partial migration %d %v", v, err)
	}
	if exists, err := tableExists(ctx, db, "managed_message_tasks"); err != nil || exists {
		t.Fatal("partial managed table")
	}
	if err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := Apply(ctx, db); err != nil {
		t.Fatal("migration not replay-safe", err)
	}
	var host, thread, digest, mode, body, note string
	var generation int
	if err := db.QueryRow(`SELECT host_id,thread_id,token_digest,generation,mode FROM external_session_bindings WHERE binding_id='binding-a'`).Scan(&host, &thread, &digest, &generation, &mode); err != nil || host != "original-desktop" || thread != "original-thread" || digest != "original-digest" || generation != 7 || mode != "external" {
		t.Fatal("legacy binding changed", err)
	}
	if err := db.QueryRow(`SELECT content,processing_note FROM external_messages WHERE message_id='historical-request'`).Scan(&body, &note); err != nil || body != "original-body" || note != "original-note" {
		t.Fatal("legacy message changed", err)
	}
	if _, err := db.Exec(`DROP TABLE managed_message_tasks`); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCurrent(ctx, db); !errors.Is(err, ErrIncompleteSchema) {
		t.Fatal("missing managed table accepted", err)
	}
}
