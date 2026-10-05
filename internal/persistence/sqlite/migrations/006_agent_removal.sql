-- Scope rows are transaction-local authorizations: the deferred FK cannot be
-- satisfied (guard only accepts 0), so an authorization can never be committed.
CREATE TABLE agent_removal_guard (id INTEGER PRIMARY KEY CHECK(id=0));
CREATE TABLE agent_removal_scope (
 table_name TEXT NOT NULL,
 entity_key TEXT NOT NULL,
 transaction_key INTEGER NOT NULL DEFAULT 1 CHECK(transaction_key=1),
 PRIMARY KEY(table_name,entity_key),
 FOREIGN KEY(transaction_key) REFERENCES agent_removal_guard(id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE agent_removal_receipts (
 actor_principal_id TEXT NOT NULL REFERENCES principals(principal_id),
 digest TEXT NOT NULL,
 agent_ids_json TEXT NOT NULL,
 counts_json TEXT NOT NULL,
 created_at TEXT NOT NULL,
 PRIMARY KEY(actor_principal_id,digest)
);
DROP TRIGGER event_journal_reject_delete;
CREATE TRIGGER event_journal_reject_delete BEFORE DELETE ON event_journal
WHEN NOT EXISTS(SELECT 1 FROM agent_removal_scope WHERE table_name='event_journal' AND entity_key=json_array(OLD.aggregate_type,OLD.aggregate_id))
BEGIN SELECT RAISE(ABORT, 'event_journal is append-only'); END;
DROP TRIGGER network_mode_policies_reject_delete;
CREATE TRIGGER network_mode_policies_reject_delete BEFORE DELETE ON network_mode_policies
WHEN NOT EXISTS(SELECT 1 FROM agent_removal_scope WHERE table_name='network_mode_policies' AND entity_key=json_array(OLD.agent_id,OLD.backend_id,OLD.policy_version))
BEGIN SELECT RAISE(ABORT, 'network mode policy is immutable'); END;
UPDATE schema_meta SET version=6,applied_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE singleton=1;
