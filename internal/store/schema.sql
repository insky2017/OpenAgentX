CREATE TABLE IF NOT EXISTS agents (
    id TEXT PRIMARY KEY,
    role TEXT NOT NULL,
    connector TEXT NOT NULL,
    address TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS agent_profiles (
    agent_id TEXT PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
    manifest_version INTEGER NOT NULL,
    runtime TEXT NOT NULL,
    workspace TEXT NOT NULL,
    config_path TEXT NOT NULL,
    instructions_path TEXT NOT NULL,
    capabilities_json TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS agent_sessions (
    agent_id TEXT PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
    generation INTEGER NOT NULL,
    status TEXT NOT NULL,
    resolved_pane_id TEXT NOT NULL,
    delivery_error TEXT,
    started_at TEXT NOT NULL,
    ready_at TEXT,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS tasks (
    id TEXT PRIMARY KEY,
    sender_agent_id TEXT NOT NULL REFERENCES agents(id),
    target_agent_id TEXT NOT NULL REFERENCES agents(id),
    idempotency_key TEXT NOT NULL,
    content TEXT NOT NULL,
    status TEXT NOT NULL,
    result TEXT,
    error TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tasks_sender ON tasks(sender_agent_id);
CREATE INDEX IF NOT EXISTS idx_tasks_target ON tasks(target_agent_id);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE UNIQUE INDEX IF NOT EXISTS uq_tasks_sender_idempotency ON tasks(sender_agent_id, idempotency_key);
CREATE UNIQUE INDEX IF NOT EXISTS uq_tasks_target_active ON tasks(target_agent_id) WHERE status IN ('queued', 'running');

CREATE TABLE IF NOT EXISTS task_messages (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    sender_agent_id TEXT NOT NULL REFERENCES agents(id),
    kind TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_messages_task_id ON task_messages(task_id);

CREATE TABLE IF NOT EXISTS task_events (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT NOT NULL UNIQUE,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    actor_agent_id TEXT NOT NULL,
    type TEXT NOT NULL,
    payload TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events_task_id ON task_events(task_id);
