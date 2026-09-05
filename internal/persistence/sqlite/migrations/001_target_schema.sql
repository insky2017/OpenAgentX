CREATE TABLE schema_meta (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    version INTEGER NOT NULL CHECK (version > 0),
    applied_at TEXT NOT NULL
);

INSERT INTO schema_meta (singleton, version, applied_at)
VALUES (1, 1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));

CREATE TABLE principals (
    principal_id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('human', 'agent', 'worker', 'system')),
    display_name TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active', 'disabled')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE organizations (
    organization_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active', 'disabled')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE org_units (
    org_unit_id TEXT PRIMARY KEY,
    organization_id TEXT NOT NULL REFERENCES organizations(organization_id),
    parent_org_unit_id TEXT REFERENCES org_units(org_unit_id),
    name TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX idx_org_units_organization ON org_units(organization_id);

CREATE TABLE roles (
    role_id TEXT PRIMARY KEY,
    organization_id TEXT NOT NULL REFERENCES organizations(organization_id),
    name TEXT NOT NULL,
    capabilities_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (organization_id, name)
);

CREATE TABLE positions (
    position_id TEXT PRIMARY KEY,
    organization_id TEXT NOT NULL REFERENCES organizations(organization_id),
    org_unit_id TEXT REFERENCES org_units(org_unit_id),
    role_id TEXT NOT NULL REFERENCES roles(role_id),
    name TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX idx_positions_organization ON positions(organization_id);

CREATE TABLE agents (
    agent_id TEXT PRIMARY KEY,
    principal_id TEXT NOT NULL UNIQUE REFERENCES principals(principal_id),
    organization_id TEXT NOT NULL REFERENCES organizations(organization_id),
    display_name TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active', 'disabled')),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX idx_agents_organization ON agents(organization_id);

CREATE TABLE position_assignments (
    position_assignment_id TEXT PRIMARY KEY,
    position_id TEXT NOT NULL REFERENCES positions(position_id),
    agent_id TEXT NOT NULL REFERENCES agents(agent_id),
    active INTEGER NOT NULL CHECK (active IN (0, 1)),
    starts_at TEXT NOT NULL,
    ends_at TEXT,
    created_at TEXT NOT NULL
);

CREATE UNIQUE INDEX uq_position_assignments_active_position
ON position_assignments(position_id) WHERE active = 1;

CREATE TABLE reporting_lines (
    reporting_line_id TEXT PRIMARY KEY,
    organization_id TEXT NOT NULL REFERENCES organizations(organization_id),
    manager_position_id TEXT NOT NULL REFERENCES positions(position_id),
    report_position_id TEXT NOT NULL REFERENCES positions(position_id),
    created_at TEXT NOT NULL,
    CHECK (manager_position_id <> report_position_id),
    UNIQUE (organization_id, manager_position_id, report_position_id)
);

CREATE TABLE authority_policies (
    authority_policy_id TEXT PRIMARY KEY,
    organization_id TEXT NOT NULL REFERENCES organizations(organization_id),
    subject_kind TEXT NOT NULL CHECK (subject_kind IN ('principal', 'position', 'role')),
    subject_id TEXT NOT NULL,
    action TEXT NOT NULL,
    resource_scope_json TEXT NOT NULL,
    effect TEXT NOT NULL CHECK (effect IN ('allow', 'deny')),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX idx_authority_policy_lookup
ON authority_policies(organization_id, subject_kind, subject_id, action);

CREATE TABLE agent_profiles (
    agent_id TEXT PRIMARY KEY REFERENCES agents(agent_id) ON DELETE CASCADE,
    profile_version INTEGER NOT NULL CHECK (profile_version > 0),
    instructions_path TEXT NOT NULL,
    workspace_root TEXT NOT NULL,
    default_execution_profile_id TEXT REFERENCES execution_profiles(execution_profile_id),
    capabilities_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE execution_profiles (
    execution_profile_id TEXT PRIMARY KEY,
    organization_id TEXT NOT NULL REFERENCES organizations(organization_id),
    name TEXT NOT NULL,
    execution_json TEXT NOT NULL,
    version INTEGER NOT NULL CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (organization_id, name)
);

CREATE TABLE worker_instances (
    worker_instance_id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL REFERENCES agents(agent_id),
    generation INTEGER NOT NULL CHECK (generation > 0),
    transport TEXT NOT NULL CHECK (transport IN ('unix', 'https')),
    authenticated_principal TEXT NOT NULL,
    capabilities_json TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('bootstrapping', 'online', 'degraded', 'draining', 'offline')),
    session_token_digest TEXT,
    session_token_expires_at TEXT,
    last_heartbeat_at TEXT NOT NULL,
    lease_until TEXT NOT NULL,
    fencing_token INTEGER NOT NULL CHECK (fencing_token > 0),
    started_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (agent_id, generation)
);

CREATE UNIQUE INDEX uq_worker_instances_agent_active
ON worker_instances(agent_id)
WHERE status IN ('bootstrapping', 'online', 'degraded', 'draining');

CREATE INDEX idx_worker_instances_lease ON worker_instances(status, lease_until);

CREATE TABLE runtime_backend_registrations (
    runtime_backend_registration_id TEXT PRIMARY KEY,
    worker_instance_id TEXT NOT NULL REFERENCES worker_instances(worker_instance_id) ON DELETE CASCADE,
    adapter_id TEXT NOT NULL,
    backend_id TEXT NOT NULL,
    descriptor_json TEXT NOT NULL,
    health TEXT NOT NULL CHECK (health IN ('healthy', 'degraded', 'unavailable')),
    observed_at TEXT NOT NULL,
    UNIQUE (worker_instance_id, adapter_id, backend_id)
);

CREATE TABLE tasks (
    task_id TEXT PRIMARY KEY,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    status TEXT NOT NULL CHECK (status IN (
        'queued', 'dispatching', 'running', 'waiting_input', 'waiting_approval',
        'cancel_requested', 'succeeded', 'failed', 'canceled', 'uncertain'
    )),
    sender_principal_id TEXT NOT NULL REFERENCES principals(principal_id),
    target_agent_id TEXT NOT NULL REFERENCES agents(agent_id),
    dispatch_mode TEXT NOT NULL CHECK (dispatch_mode IN ('coordinated', 'direct')),
    parent_task_id TEXT REFERENCES tasks(task_id),
    organization_id TEXT NOT NULL REFERENCES organizations(organization_id),
    idempotency_key TEXT NOT NULL,
    content TEXT NOT NULL,
    result TEXT,
    error TEXT,
    cancel_requested_by TEXT REFERENCES principals(principal_id),
    cancel_requested_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (sender_principal_id, idempotency_key)
);

CREATE INDEX idx_tasks_target_status ON tasks(target_agent_id, status, created_at);
CREATE INDEX idx_tasks_organization ON tasks(organization_id, created_at);

CREATE TABLE messages (
    message_id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(task_id) ON DELETE CASCADE,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    sequence INTEGER NOT NULL CHECK (sequence > 0),
    sender_principal_id TEXT NOT NULL REFERENCES principals(principal_id),
    target_agent_id TEXT NOT NULL REFERENCES agents(agent_id),
    kind TEXT NOT NULL CHECK (kind IN ('instruction', 'supplement', 'status_update')),
    content TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (task_id, sequence)
);

CREATE TABLE run_attempts (
    run_id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(task_id) ON DELETE CASCADE,
    agent_id TEXT NOT NULL REFERENCES agents(agent_id),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    status TEXT NOT NULL CHECK (status IN (
        'starting', 'running', 'waiting_approval', 'finishing',
        'succeeded', 'failed', 'canceled', 'uncertain'
    )),
    worker_instance_id TEXT NOT NULL REFERENCES worker_instances(worker_instance_id),
    fencing_token INTEGER NOT NULL CHECK (fencing_token > 0),
    lease_until TEXT NOT NULL,
    execution_spec_version INTEGER NOT NULL CHECK (execution_spec_version > 0),
    requested_execution_json TEXT NOT NULL,
    resolved_execution_json TEXT NOT NULL,
    adapter_id TEXT NOT NULL,
    backend_id TEXT NOT NULL,
    model TEXT NOT NULL,
    reasoning_mode TEXT NOT NULL,
    reasoning_value TEXT NOT NULL,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    result_json TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX uq_run_attempts_agent_active
ON run_attempts(agent_id)
WHERE status IN ('starting', 'running', 'waiting_approval', 'finishing');

CREATE INDEX idx_run_attempts_task ON run_attempts(task_id, created_at);
CREATE INDEX idx_run_attempts_lease ON run_attempts(status, lease_until);

CREATE TABLE session_bindings (
    session_binding_id TEXT PRIMARY KEY,
    context_id TEXT NOT NULL,
    agent_id TEXT NOT NULL REFERENCES agents(agent_id),
    backend_id TEXT NOT NULL,
    provider_session_id TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('active', 'invalid')),
    version INTEGER NOT NULL CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (context_id, agent_id, backend_id)
);

CREATE TABLE workspace_leases (
    workspace_lease_id TEXT PRIMARY KEY,
    workspace_key TEXT NOT NULL,
    run_id TEXT NOT NULL UNIQUE REFERENCES run_attempts(run_id) ON DELETE CASCADE,
    worker_instance_id TEXT NOT NULL REFERENCES worker_instances(worker_instance_id),
    fencing_token INTEGER NOT NULL CHECK (fencing_token > 0),
    lease_until TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX uq_workspace_leases_key ON workspace_leases(workspace_key);

CREATE TABLE approval_requests (
    approval_request_id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(task_id) ON DELETE CASCADE,
    mode TEXT NOT NULL CHECK (mode IN ('native', 'preflight')),
    target_run_id TEXT REFERENCES run_attempts(run_id),
    expected_run_version INTEGER,
    scope_digest TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('pending', 'approved', 'rejected', 'stale', 'consumed', 'expired')),
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    CHECK (
        (mode = 'native' AND target_run_id IS NOT NULL AND expected_run_version > 0)
        OR
        (mode = 'preflight' AND target_run_id IS NULL AND expected_run_version IS NULL)
    )
);

CREATE TABLE approval_decisions (
    approval_decision_id TEXT PRIMARY KEY,
    approval_request_id TEXT NOT NULL REFERENCES approval_requests(approval_request_id) ON DELETE CASCADE,
    decided_by TEXT NOT NULL REFERENCES principals(principal_id),
    decision TEXT NOT NULL CHECK (decision IN ('approve', 'reject')),
    state TEXT NOT NULL CHECK (state IN ('persisted', 'applied', 'superseded')),
    idempotency_key TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (decided_by, idempotency_key)
);

CREATE TABLE mailbox_items (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    mailbox_item_id TEXT NOT NULL UNIQUE,
    target_agent_id TEXT NOT NULL REFERENCES agents(agent_id),
    kind TEXT NOT NULL CHECK (kind IN ('task', 'message', 'approval', 'cancel')),
    lane TEXT NOT NULL CHECK (lane IN ('work', 'control')),
    task_id TEXT REFERENCES tasks(task_id) ON DELETE CASCADE,
    message_id TEXT REFERENCES messages(message_id) ON DELETE CASCADE,
    approval_request_id TEXT REFERENCES approval_requests(approval_request_id) ON DELETE CASCADE,
    approval_decision_id TEXT REFERENCES approval_decisions(approval_decision_id) ON DELETE CASCADE,
    target_run_id TEXT REFERENCES run_attempts(run_id),
    expected_run_version INTEGER,
    state TEXT NOT NULL CHECK (state IN ('pending', 'claimed', 'accepted', 'superseded', 'failed')),
    worker_instance_id TEXT REFERENCES worker_instances(worker_instance_id),
    fencing_token INTEGER,
    lease_until TEXT,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    created_at TEXT NOT NULL,
    accepted_at TEXT,
    CHECK (kind <> 'task' OR lane = 'work'),
    CHECK (kind <> 'cancel' OR lane = 'control'),
    CHECK (
        (lane = 'control' AND target_run_id IS NOT NULL AND expected_run_version > 0)
        OR lane = 'work'
    )
);

CREATE INDEX idx_mailbox_claim
ON mailbox_items(target_agent_id, state, lane, sequence);

CREATE TABLE worker_commands (
    worker_command_id TEXT PRIMARY KEY,
    worker_instance_id TEXT NOT NULL REFERENCES worker_instances(worker_instance_id) ON DELETE CASCADE,
    generation INTEGER NOT NULL CHECK (generation > 0),
    kind TEXT NOT NULL CHECK (kind IN ('drain', 'stop', 'health_check')),
    state TEXT NOT NULL CHECK (state IN ('pending', 'claimed', 'applied', 'failed')),
    requested_by TEXT NOT NULL REFERENCES principals(principal_id),
    idempotency_key TEXT NOT NULL,
    lease_until TEXT,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    created_at TEXT NOT NULL,
    claimed_at TEXT,
    applied_at TEXT,
    result TEXT,
    UNIQUE (requested_by, idempotency_key)
);

CREATE INDEX idx_worker_commands_claim
ON worker_commands(worker_instance_id, generation, state, created_at);

CREATE TABLE artifacts (
    artifact_id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(task_id) ON DELETE CASCADE,
    run_id TEXT REFERENCES run_attempts(run_id),
    kind TEXT NOT NULL,
    uri TEXT NOT NULL,
    digest TEXT NOT NULL,
    metadata_json TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE event_journal (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id TEXT NOT NULL UNIQUE,
    organization_id TEXT REFERENCES organizations(organization_id),
    aggregate_type TEXT NOT NULL,
    aggregate_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    actor_principal_id TEXT NOT NULL REFERENCES principals(principal_id),
    payload_json TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX idx_event_journal_aggregate
ON event_journal(aggregate_type, aggregate_id, sequence);

CREATE TRIGGER event_journal_reject_update
BEFORE UPDATE ON event_journal
BEGIN
    SELECT RAISE(ABORT, 'event_journal is append-only');
END;

CREATE TRIGGER event_journal_reject_delete
BEFORE DELETE ON event_journal
BEGIN
    SELECT RAISE(ABORT, 'event_journal is append-only');
END;

CREATE TABLE web_users (
    web_user_id TEXT PRIMARY KEY,
    principal_id TEXT NOT NULL UNIQUE REFERENCES principals(principal_id),
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    roles_json TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active', 'disabled')),
    password_changed_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE web_sessions (
    web_session_id TEXT PRIMARY KEY,
    web_user_id TEXT NOT NULL REFERENCES web_users(web_user_id) ON DELETE CASCADE,
    session_digest TEXT NOT NULL UNIQUE,
    csrf_digest TEXT NOT NULL,
    created_at TEXT NOT NULL,
    last_activity_at TEXT NOT NULL,
    idle_expires_at TEXT NOT NULL,
    absolute_expires_at TEXT NOT NULL,
    revoked_at TEXT
);

CREATE INDEX idx_web_sessions_expiry
ON web_sessions(idle_expires_at, absolute_expires_at);

CREATE TABLE network_profiles (
    profile_id TEXT NOT NULL,
    version INTEGER NOT NULL CHECK (version > 0),
    status TEXT NOT NULL CHECK (status IN ('draft', 'published')),
    mode TEXT NOT NULL CHECK (mode IN ('only_http_proxy', 'only_socks5')),
    host TEXT NOT NULL,
    port INTEGER NOT NULL CHECK (port BETWEEN 1 AND 65535),
    config_file TEXT,
    secret_ref TEXT,
    created_by TEXT NOT NULL REFERENCES principals(principal_id),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (profile_id, version)
);

CREATE INDEX idx_network_profiles_latest
ON network_profiles(profile_id, version DESC);

CREATE TABLE network_profile_bindings (
    agent_id TEXT NOT NULL REFERENCES agents(agent_id) ON DELETE CASCADE,
    backend_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    profile_version INTEGER NOT NULL,
    version INTEGER NOT NULL CHECK (version > 0),
    desired_status TEXT NOT NULL CHECK (desired_status IN ('pending', 'applied', 'failed')),
    applied_worker_id TEXT,
    applied_generation INTEGER,
    applied_profile_version INTEGER,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (agent_id, backend_id),
    FOREIGN KEY (profile_id, profile_version) REFERENCES network_profiles(profile_id, version)
);
