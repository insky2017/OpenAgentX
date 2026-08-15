CREATE TABLE IF NOT EXISTS agents (
    id TEXT PRIMARY KEY,
    role TEXT NOT NULL,
    connector TEXT NOT NULL,
    address TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
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
    updated_at TEXT NOT NULL,
    UNIQUE(sender_agent_id, idempotency_key)
);

CREATE TABLE IF NOT EXISTS messages (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    sender_agent_id TEXT NOT NULL REFERENCES agents(id),
    kind TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT NOT NULL UNIQUE,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    actor_agent_id TEXT NOT NULL,
    type TEXT NOT NULL,
    payload TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_tasks_target_active ON tasks(target_agent_id) WHERE status IN ('queued', 'running');
CREATE INDEX IF NOT EXISTS idx_tasks_target_status ON tasks(target_agent_id, status);
CREATE INDEX IF NOT EXISTS idx_tasks_sender ON tasks(sender_agent_id);
CREATE INDEX IF NOT EXISTS idx_messages_task ON messages(task_id);
CREATE INDEX IF NOT EXISTS idx_events_task_seq ON events(task_id, sequence);
