ALTER TABLE external_session_bindings ADD COLUMN mode TEXT NOT NULL DEFAULT 'external' CHECK(mode IN ('external','managed'));
ALTER TABLE external_session_bindings ADD COLUMN managed_context_task_id TEXT REFERENCES tasks(task_id);
ALTER TABLE external_session_bindings ADD COLUMN managed_backend_id TEXT NOT NULL DEFAULT '';
CREATE TABLE managed_message_tasks (
 message_id TEXT PRIMARY KEY REFERENCES external_messages(message_id),
 task_id TEXT NOT NULL UNIQUE REFERENCES tasks(task_id),
 role TEXT NOT NULL CHECK(role IN ('consultation','result_consumption')),
 binding_id TEXT NOT NULL REFERENCES external_session_bindings(binding_id),
 binding_generation INTEGER NOT NULL CHECK(binding_generation>0),
 state TEXT NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','completed','needs_review')),
 result_message_id TEXT REFERENCES external_messages(message_id),
 completed_run_id TEXT REFERENCES run_attempts(run_id)
);
DROP TRIGGER external_binding_insert_guard;
DROP TRIGGER external_binding_update_guard;
DROP TRIGGER worker_insert_external_guard;
DROP TRIGGER task_insert_external_guard;
DROP TRIGGER worker_update_external_guard;
DROP TRIGGER session_insert_external_guard;
DROP TRIGGER session_update_external_guard;
CREATE TRIGGER external_binding_insert_guard BEFORE INSERT ON external_session_bindings
WHEN NEW.state='active' AND NEW.mode='external' AND (
    EXISTS(SELECT 1 FROM tasks WHERE target_agent_id=NEW.agent_id AND status NOT IN ('succeeded','failed','canceled','uncertain')) OR
    EXISTS(SELECT 1 FROM worker_instances WHERE agent_id=NEW.agent_id AND status<>'offline') OR
    EXISTS(SELECT 1 FROM session_bindings WHERE state='active' AND (agent_id=NEW.agent_id OR provider_session_id=NEW.thread_id))
)
BEGIN SELECT RAISE(ABORT,'external session conflicts with managed execution'); END;
CREATE TRIGGER external_binding_update_guard BEFORE UPDATE ON external_session_bindings
WHEN NEW.state='active' AND NEW.mode='external' AND (
    EXISTS(SELECT 1 FROM tasks WHERE target_agent_id=NEW.agent_id AND status NOT IN ('succeeded','failed','canceled','uncertain')) OR
    EXISTS(SELECT 1 FROM worker_instances WHERE agent_id=NEW.agent_id AND status<>'offline') OR
    EXISTS(SELECT 1 FROM session_bindings WHERE state='active' AND (agent_id=NEW.agent_id OR provider_session_id=NEW.thread_id))
)
BEGIN SELECT RAISE(ABORT,'external session conflicts with managed execution'); END;
CREATE TRIGGER worker_insert_external_guard BEFORE INSERT ON worker_instances
WHEN NEW.status<>'offline' AND EXISTS(SELECT 1 FROM external_session_bindings WHERE mode='external' AND agent_id=NEW.agent_id AND state='active')
BEGIN SELECT RAISE(ABORT,'managed Worker conflicts with external session'); END;
CREATE TRIGGER task_insert_external_guard BEFORE INSERT ON tasks
WHEN EXISTS(SELECT 1 FROM external_session_bindings WHERE mode='external' AND agent_id=NEW.target_agent_id AND state='active')
BEGIN SELECT RAISE(ABORT,'external session uses messages, not managed tasks'); END;
CREATE TRIGGER worker_update_external_guard BEFORE UPDATE ON worker_instances
WHEN NEW.status<>'offline' AND EXISTS(SELECT 1 FROM external_session_bindings WHERE mode='external' AND agent_id=NEW.agent_id AND state='active')
BEGIN SELECT RAISE(ABORT,'managed Worker conflicts with external session'); END;
CREATE TRIGGER session_insert_external_guard BEFORE INSERT ON session_bindings
WHEN NEW.state='active' AND EXISTS(SELECT 1 FROM external_session_bindings WHERE mode='external' AND state='active' AND (agent_id=NEW.agent_id OR thread_id=NEW.provider_session_id))
BEGIN SELECT RAISE(ABORT,'managed session conflicts with external session'); END;
CREATE TRIGGER session_update_external_guard BEFORE UPDATE ON session_bindings
WHEN NEW.state='active' AND EXISTS(SELECT 1 FROM external_session_bindings WHERE mode='external' AND state='active' AND (agent_id=NEW.agent_id OR thread_id=NEW.provider_session_id))
BEGIN SELECT RAISE(ABORT,'managed session conflicts with external session'); END;

UPDATE schema_meta SET version=5,applied_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE singleton=1;
