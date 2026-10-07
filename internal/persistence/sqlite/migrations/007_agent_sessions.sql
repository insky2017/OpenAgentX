CREATE TABLE agent_sessions (
 agent_id TEXT NOT NULL REFERENCES agents(agent_id) ON DELETE CASCADE,
 backend_id TEXT NOT NULL,
 provider_session_id TEXT NOT NULL,
 context_task_id TEXT REFERENCES tasks(task_id) ON DELETE CASCADE,
 version INTEGER NOT NULL CHECK(version>=0),
 pending_task_id TEXT UNIQUE REFERENCES tasks(task_id) ON DELETE CASCADE,
 updated_at TEXT NOT NULL,
 PRIMARY KEY(agent_id,backend_id)
);
CREATE UNIQUE INDEX uq_agent_session_pending ON agent_sessions(agent_id) WHERE pending_task_id IS NOT NULL;
UPDATE schema_meta SET version=7,applied_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE singleton=1;
