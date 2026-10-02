CREATE TABLE external_role_catalogs (
 organization_id TEXT PRIMARY KEY REFERENCES organizations(organization_id),
 revision INTEGER NOT NULL CHECK(revision > 0)
);
CREATE TABLE external_role_scopes (
 organization_id TEXT NOT NULL REFERENCES external_role_catalogs(organization_id),
 scope TEXT NOT NULL,
 owner_agent_id TEXT NOT NULL REFERENCES agents(agent_id),
 description TEXT NOT NULL,
 PRIMARY KEY(organization_id,scope)
);
CREATE INDEX idx_external_role_owner ON external_role_scopes(organization_id,owner_agent_id);
ALTER TABLE external_messages ADD COLUMN scope TEXT NOT NULL DEFAULT '';
ALTER TABLE external_messages ADD COLUMN forwarded_from_message_id TEXT REFERENCES external_messages(message_id);
ALTER TABLE external_messages ADD COLUMN processing_state TEXT NOT NULL DEFAULT 'pending' CHECK(processing_state IN ('pending','accepted','needs_clarification','out_of_scope','completed'));
ALTER TABLE external_messages ADD COLUMN processing_note TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX uq_external_single_forward ON external_messages(forwarded_from_message_id) WHERE forwarded_from_message_id IS NOT NULL;
UPDATE external_messages SET processing_state='completed' WHERE kind='result' OR message_id IN (SELECT reply_to_message_id FROM external_messages WHERE kind='result');
UPDATE schema_meta SET version=4,applied_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE singleton=1;
