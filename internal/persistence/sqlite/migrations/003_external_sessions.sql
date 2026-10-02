CREATE TABLE external_session_bindings (
    binding_id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL UNIQUE REFERENCES agents(agent_id),
    host_id TEXT NOT NULL,
    thread_id TEXT NOT NULL,
    generation INTEGER NOT NULL CHECK (generation > 0),
    state TEXT NOT NULL CHECK (state IN ('active','revoked')),
    token_digest TEXT NOT NULL UNIQUE,
    token_expires_at TEXT NOT NULL,
    allowed_peer_agent_ids_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX uq_external_active_thread ON external_session_bindings(host_id,thread_id) WHERE state='active';

CREATE TABLE external_messages (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id TEXT NOT NULL UNIQUE,
    sender_agent_id TEXT NOT NULL REFERENCES agents(agent_id),
    target_agent_id TEXT NOT NULL REFERENCES agents(agent_id),
    sender_binding_id TEXT NOT NULL REFERENCES external_session_bindings(binding_id),
    sender_generation INTEGER NOT NULL CHECK (sender_generation > 0),
    kind TEXT NOT NULL CHECK (kind IN ('consultation','request','result')),
    reply_to_message_id TEXT REFERENCES external_messages(message_id),
    content TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    payload_digest TEXT NOT NULL,
    origin_task_id TEXT REFERENCES tasks(task_id),
    delivery_state TEXT NOT NULL CHECK (delivery_state IN ('pending','acknowledged')),
    created_at TEXT NOT NULL,
    acknowledged_at TEXT,
    CHECK (sender_agent_id <> target_agent_id),
    CHECK ((kind='result' AND reply_to_message_id IS NOT NULL) OR (kind<>'result' AND reply_to_message_id IS NULL)),
    CHECK ((delivery_state='pending' AND acknowledged_at IS NULL) OR (delivery_state='acknowledged' AND acknowledged_at IS NOT NULL)),
    UNIQUE(sender_agent_id,idempotency_key)
);
CREATE UNIQUE INDEX uq_external_single_result ON external_messages(reply_to_message_id) WHERE kind='result';
CREATE INDEX idx_external_inbox ON external_messages(target_agent_id,sequence);

CREATE TRIGGER external_binding_insert_guard BEFORE INSERT ON external_session_bindings
WHEN NEW.state='active' AND (
    EXISTS(SELECT 1 FROM tasks WHERE target_agent_id=NEW.agent_id AND status NOT IN ('succeeded','failed','canceled','uncertain')) OR
    EXISTS(SELECT 1 FROM worker_instances WHERE agent_id=NEW.agent_id AND status<>'offline') OR
    EXISTS(SELECT 1 FROM session_bindings WHERE state='active' AND (agent_id=NEW.agent_id OR provider_session_id=NEW.thread_id))
)
BEGIN SELECT RAISE(ABORT,'external session conflicts with managed execution'); END;
CREATE TRIGGER external_binding_update_guard BEFORE UPDATE ON external_session_bindings
WHEN NEW.state='active' AND (
    EXISTS(SELECT 1 FROM tasks WHERE target_agent_id=NEW.agent_id AND status NOT IN ('succeeded','failed','canceled','uncertain')) OR
    EXISTS(SELECT 1 FROM worker_instances WHERE agent_id=NEW.agent_id AND status<>'offline') OR
    EXISTS(SELECT 1 FROM session_bindings WHERE state='active' AND (agent_id=NEW.agent_id OR provider_session_id=NEW.thread_id))
)
BEGIN SELECT RAISE(ABORT,'external session conflicts with managed execution'); END;
CREATE TRIGGER worker_insert_external_guard BEFORE INSERT ON worker_instances
WHEN NEW.status<>'offline' AND EXISTS(SELECT 1 FROM external_session_bindings WHERE agent_id=NEW.agent_id AND state='active')
BEGIN SELECT RAISE(ABORT,'managed Worker conflicts with external session'); END;
CREATE TRIGGER task_insert_external_guard BEFORE INSERT ON tasks
WHEN EXISTS(SELECT 1 FROM external_session_bindings WHERE agent_id=NEW.target_agent_id AND state='active')
BEGIN SELECT RAISE(ABORT,'external session uses messages, not managed tasks'); END;
CREATE TRIGGER worker_update_external_guard BEFORE UPDATE ON worker_instances
WHEN NEW.status<>'offline' AND EXISTS(SELECT 1 FROM external_session_bindings WHERE agent_id=NEW.agent_id AND state='active')
BEGIN SELECT RAISE(ABORT,'managed Worker conflicts with external session'); END;
CREATE TRIGGER session_insert_external_guard BEFORE INSERT ON session_bindings
WHEN NEW.state='active' AND EXISTS(SELECT 1 FROM external_session_bindings WHERE state='active' AND (agent_id=NEW.agent_id OR thread_id=NEW.provider_session_id))
BEGIN SELECT RAISE(ABORT,'managed session conflicts with external session'); END;
CREATE TRIGGER session_update_external_guard BEFORE UPDATE ON session_bindings
WHEN NEW.state='active' AND EXISTS(SELECT 1 FROM external_session_bindings WHERE state='active' AND (agent_id=NEW.agent_id OR thread_id=NEW.provider_session_id))
BEGIN SELECT RAISE(ABORT,'managed session conflicts with external session'); END;

UPDATE schema_meta SET version=3,applied_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE singleton=1;
