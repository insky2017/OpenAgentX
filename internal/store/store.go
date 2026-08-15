package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agentbus/internal/domain"
	_ "github.com/mattn/go-sqlite3"
)

type Store interface {
	Close() error
	RegisterAgent(ctx context.Context, agent *domain.Agent) error
	GetAgent(ctx context.Context, id string) (*domain.Agent, error)
	ListAgents(ctx context.Context) ([]*domain.Agent, error)

	// V0.1 Profile & Session methods
	AttachAgent(ctx context.Context, agent *domain.Agent, profile *domain.AgentProfile, sessionStatus string, resolvedPane string, delivErr *string) (*domain.AgentSession, error)
	BootstrapAgent(ctx context.Context, agentID string, sessionStatus string, resolvedPane string, delivErr *string) (*domain.Agent, *domain.AgentProfile, *domain.AgentSession, error)
	ReadySession(ctx context.Context, agentID string, generation int64) (*domain.AgentSession, error)
	UpdateSessionDelivery(ctx context.Context, agentID string, generation int64, status string, resolvedPane string, delivErr *string) (*domain.AgentSession, error)
	GetAgentSession(ctx context.Context, agentID string) (*domain.Agent, *domain.AgentProfile, *domain.AgentSession, error)
	IsAgentReady(ctx context.Context, agentID string) (bool, error)

	SubmitTask(ctx context.Context, task *domain.Task, initialMsg *domain.Message, initialEvt *domain.Event) (*domain.Task, bool, error)
	GetTask(ctx context.Context, id string) (*domain.Task, error)
	ListTasks(ctx context.Context, agentID string, status string) ([]*domain.Task, error)
	AckTask(ctx context.Context, id string, actorAgentID string, evt *domain.Event) (*domain.Task, error)
	UpdateTaskStatus(ctx context.Context, id string, actorAgentID string, msg *domain.Message, evt *domain.Event) error
	SendMessage(ctx context.Context, taskID string, senderAgentID string, msg *domain.Message, evt *domain.Event) error
	CompleteTask(ctx context.Context, id string, actorAgentID string, result string, evt *domain.Event) (*domain.Task, error)
	FailTask(ctx context.Context, id string, actorAgentID string, errStr string, evt *domain.Event) (*domain.Task, error)
	CancelTask(ctx context.Context, id string, actorAgentID string, evt *domain.Event) (*domain.Task, error)

	AddEvent(ctx context.Context, evt *domain.Event) (int64, error)
	GetEvents(ctx context.Context, taskID string, afterSeq int64) ([]*domain.Event, error)
	GetTaskMessages(ctx context.Context, taskID string) ([]*domain.Message, error)
}

type SQLiteStore struct {
	db *sql.DB
}

func OpenSQLite(dbPath string) (*SQLiteStore, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON", dbPath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	db.SetMaxOpenConns(1)

	if err := initSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

func initSchema(db *sql.DB) error {
	schema := `
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
`
	_, err := db.Exec(schema)
	return err
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) RegisterAgent(ctx context.Context, agent *domain.Agent) error {
	if err := agent.Validate(); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if agent.CreatedAt == "" {
		agent.CreatedAt = now
	}
	agent.UpdatedAt = now

	query := `INSERT INTO agents (id, role, connector, address, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			role = excluded.role,
			connector = excluded.connector,
			address = excluded.address,
			status = excluded.status,
			updated_at = excluded.updated_at`
	_, err := s.db.ExecContext(ctx, query,
		agent.ID, agent.Role, agent.Connector, agent.Address, agent.Status, agent.CreatedAt, agent.UpdatedAt,
	)
	return err
}

func (s *SQLiteStore) GetAgent(ctx context.Context, id string) (*domain.Agent, error) {
	query := `SELECT id, role, connector, address, status, created_at, updated_at FROM agents WHERE id = ?`
	var a domain.Agent
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&a.ID, &a.Role, &a.Connector, &a.Address, &a.Status, &a.CreatedAt, &a.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrAgentNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *SQLiteStore) ListAgents(ctx context.Context) ([]*domain.Agent, error) {
	query := `SELECT id, role, connector, address, status, created_at, updated_at FROM agents ORDER BY id ASC`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []*domain.Agent
	for rows.Next() {
		var a domain.Agent
		if err := rows.Scan(&a.ID, &a.Role, &a.Connector, &a.Address, &a.Status, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		agents = append(agents, &a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return agents, nil
}

// V0.1 Attach, Bootstrap, Session methods

func (s *SQLiteStore) AttachAgent(ctx context.Context, agent *domain.Agent, profile *domain.AgentProfile, sessionStatus string, resolvedPane string, delivErr *string) (*domain.AgentSession, error) {
	if err := agent.Validate(); err != nil {
		return nil, err
	}
	if err := profile.Validate(); err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if agent.CreatedAt == "" {
		agent.CreatedAt = now
	}
	agent.UpdatedAt = now
	profile.UpdatedAt = now

	// 1. Upsert Agent
	agentQuery := `INSERT INTO agents (id, role, connector, address, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			role = excluded.role,
			connector = excluded.connector,
			address = excluded.address,
			status = excluded.status,
			updated_at = excluded.updated_at`
	if _, err := tx.ExecContext(ctx, agentQuery, agent.ID, agent.Role, agent.Connector, agent.Address, agent.Status, agent.CreatedAt, agent.UpdatedAt); err != nil {
		return nil, fmt.Errorf("failed to upsert agent: %w", err)
	}

	// 2. Upsert Profile
	profQuery := `INSERT INTO agent_profiles (agent_id, manifest_version, runtime, workspace, config_path, instructions_path, capabilities_json, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(agent_id) DO UPDATE SET
			manifest_version = excluded.manifest_version,
			runtime = excluded.runtime,
			workspace = excluded.workspace,
			config_path = excluded.config_path,
			instructions_path = excluded.instructions_path,
			capabilities_json = excluded.capabilities_json,
			updated_at = excluded.updated_at`
	if _, err := tx.ExecContext(ctx, profQuery, profile.AgentID, profile.ManifestVersion, profile.Runtime, profile.Workspace, profile.ConfigPath, profile.InstructionsPath, profile.CapabilitiesJSON(), profile.UpdatedAt); err != nil {
		return nil, fmt.Errorf("failed to upsert agent profile: %w", err)
	}

	// 3. Query existing session generation
	var currentGen int64
	err = tx.QueryRowContext(ctx, `SELECT generation FROM agent_sessions WHERE agent_id = ?`, agent.ID).Scan(&currentGen)
	var newGen int64 = 1
	if err == nil {
		newGen = currentGen + 1
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("failed to query agent session generation: %w", err)
	}

	// 4. Upsert Session
	session := &domain.AgentSession{
		AgentID:        agent.ID,
		Generation:     newGen,
		Status:         sessionStatus,
		ResolvedPaneID: resolvedPane,
		DeliveryError:  delivErr,
		StartedAt:      now,
		ReadyAt:        nil,
		UpdatedAt:      now,
	}

	sessQuery := `INSERT INTO agent_sessions (agent_id, generation, status, resolved_pane_id, delivery_error, started_at, ready_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, NULL, ?)
		ON CONFLICT(agent_id) DO UPDATE SET
			generation = excluded.generation,
			status = excluded.status,
			resolved_pane_id = excluded.resolved_pane_id,
			delivery_error = excluded.delivery_error,
			started_at = excluded.started_at,
			ready_at = NULL,
			updated_at = excluded.updated_at`
	if _, err := tx.ExecContext(ctx, sessQuery, session.AgentID, session.Generation, session.Status, session.ResolvedPaneID, session.DeliveryError, session.StartedAt, session.UpdatedAt); err != nil {
		return nil, fmt.Errorf("failed to upsert agent session: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return session, nil
}

func (s *SQLiteStore) BootstrapAgent(ctx context.Context, agentID string, sessionStatus string, resolvedPane string, delivErr *string) (*domain.Agent, *domain.AgentProfile, *domain.AgentSession, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	defer tx.Rollback()

	// 1. Get Agent
	var a domain.Agent
	err = tx.QueryRowContext(ctx, `SELECT id, role, connector, address, status, created_at, updated_at FROM agents WHERE id = ?`, agentID).Scan(
		&a.ID, &a.Role, &a.Connector, &a.Address, &a.Status, &a.CreatedAt, &a.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil, domain.ErrAgentNotFound
	}
	if err != nil {
		return nil, nil, nil, err
	}

	// 2. Get Profile
	var p domain.AgentProfile
	var capsJSON string
	err = tx.QueryRowContext(ctx, `SELECT agent_id, manifest_version, runtime, workspace, config_path, instructions_path, capabilities_json, updated_at FROM agent_profiles WHERE agent_id = ?`, agentID).Scan(
		&p.AgentID, &p.ManifestVersion, &p.Runtime, &p.Workspace, &p.ConfigPath, &p.InstructionsPath, &capsJSON, &p.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil, domain.ErrAgentProfileNotFound
	}
	if err != nil {
		return nil, nil, nil, err
	}
	_ = json.Unmarshal([]byte(capsJSON), &p.Capabilities)

	// 3. Increment generation
	var currentGen int64
	err = tx.QueryRowContext(ctx, `SELECT generation FROM agent_sessions WHERE agent_id = ?`, agentID).Scan(&currentGen)
	var newGen int64 = 1
	if err == nil {
		newGen = currentGen + 1
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	session := &domain.AgentSession{
		AgentID:        agentID,
		Generation:     newGen,
		Status:         sessionStatus,
		ResolvedPaneID: resolvedPane,
		DeliveryError:  delivErr,
		StartedAt:      now,
		ReadyAt:        nil,
		UpdatedAt:      now,
	}

	sessQuery := `INSERT INTO agent_sessions (agent_id, generation, status, resolved_pane_id, delivery_error, started_at, ready_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, NULL, ?)
		ON CONFLICT(agent_id) DO UPDATE SET
			generation = excluded.generation,
			status = excluded.status,
			resolved_pane_id = excluded.resolved_pane_id,
			delivery_error = excluded.delivery_error,
			started_at = excluded.started_at,
			ready_at = NULL,
			updated_at = excluded.updated_at`
	if _, err := tx.ExecContext(ctx, sessQuery, session.AgentID, session.Generation, session.Status, session.ResolvedPaneID, session.DeliveryError, session.StartedAt, session.UpdatedAt); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to update agent session on bootstrap: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, nil, err
	}

	return &a, &p, session, nil
}

func (s *SQLiteStore) ReadySession(ctx context.Context, agentID string, generation int64) (*domain.AgentSession, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var sess domain.AgentSession
	var delivErr, readyAt sql.NullString
	query := `SELECT agent_id, generation, status, resolved_pane_id, delivery_error, started_at, ready_at, updated_at
		FROM agent_sessions WHERE agent_id = ?`
	err = tx.QueryRowContext(ctx, query, agentID).Scan(
		&sess.AgentID, &sess.Generation, &sess.Status, &sess.ResolvedPaneID, &delivErr, &sess.StartedAt, &readyAt, &sess.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrAgentSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	if delivErr.Valid {
		sess.DeliveryError = &delivErr.String
	}
	if readyAt.Valid {
		sess.ReadyAt = &readyAt.String
	}

	if sess.Generation != generation {
		return nil, fmt.Errorf("%w: requested generation %d does not match active generation %d", domain.ErrSessionGenerationConflict, generation, sess.Generation)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	updateQuery := `UPDATE agent_sessions SET status = ?, ready_at = ?, delivery_error = NULL, updated_at = ? WHERE agent_id = ? AND generation = ?`
	res, err := tx.ExecContext(ctx, updateQuery, domain.SessionStatusReady, now, now, agentID, generation)
	if err != nil {
		return nil, err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil || rowsAffected == 0 {
		return nil, fmt.Errorf("%w: failed to update session ready state", domain.ErrSessionGenerationConflict)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	sess.Status = domain.SessionStatusReady
	sess.ReadyAt = &now
	sess.DeliveryError = nil
	sess.UpdatedAt = now
	return &sess, nil
}

func (s *SQLiteStore) UpdateSessionDelivery(ctx context.Context, agentID string, generation int64, status string, resolvedPane string, delivErr *string) (*domain.AgentSession, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var sess domain.AgentSession
	var existingDelivErr, readyAt sql.NullString
	query := `SELECT agent_id, generation, status, resolved_pane_id, delivery_error, started_at, ready_at, updated_at
		FROM agent_sessions WHERE agent_id = ?`
	err = tx.QueryRowContext(ctx, query, agentID).Scan(
		&sess.AgentID, &sess.Generation, &sess.Status, &sess.ResolvedPaneID, &existingDelivErr, &sess.StartedAt, &readyAt, &sess.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrAgentSessionNotFound
	}
	if err != nil {
		return nil, err
	}

	if sess.Generation != generation {
		return nil, fmt.Errorf("%w: requested generation %d does not match active generation %d", domain.ErrSessionGenerationConflict, generation, sess.Generation)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	updateQuery := `UPDATE agent_sessions SET status = ?, resolved_pane_id = ?, delivery_error = ?, ready_at = NULL, updated_at = ? WHERE agent_id = ? AND generation = ?`
	res, err := tx.ExecContext(ctx, updateQuery, status, resolvedPane, delivErr, now, agentID, generation)
	if err != nil {
		return nil, err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil || rowsAffected == 0 {
		return nil, fmt.Errorf("%w: failed to update session delivery state", domain.ErrSessionGenerationConflict)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	sess.Status = status
	sess.ResolvedPaneID = resolvedPane
	sess.DeliveryError = delivErr
	sess.ReadyAt = nil
	sess.UpdatedAt = now
	return &sess, nil
}

func (s *SQLiteStore) GetAgentSession(ctx context.Context, agentID string) (*domain.Agent, *domain.AgentProfile, *domain.AgentSession, error) {
	a, err := s.GetAgent(ctx, agentID)
	if err != nil {
		return nil, nil, nil, err
	}

	var p domain.AgentProfile
	var capsJSON string
	profQuery := `SELECT agent_id, manifest_version, runtime, workspace, config_path, instructions_path, capabilities_json, updated_at FROM agent_profiles WHERE agent_id = ?`
	err = s.db.QueryRowContext(ctx, profQuery, agentID).Scan(
		&p.AgentID, &p.ManifestVersion, &p.Runtime, &p.Workspace, &p.ConfigPath, &p.InstructionsPath, &capsJSON, &p.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil, domain.ErrAgentProfileNotFound
	}
	if err != nil {
		return nil, nil, nil, err
	}
	_ = json.Unmarshal([]byte(capsJSON), &p.Capabilities)

	var sess domain.AgentSession
	var delivErr, readyAt sql.NullString
	sessQuery := `SELECT agent_id, generation, status, resolved_pane_id, delivery_error, started_at, ready_at, updated_at FROM agent_sessions WHERE agent_id = ?`
	err = s.db.QueryRowContext(ctx, sessQuery, agentID).Scan(
		&sess.AgentID, &sess.Generation, &sess.Status, &sess.ResolvedPaneID, &delivErr, &sess.StartedAt, &readyAt, &sess.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil, domain.ErrAgentSessionNotFound
	}
	if err != nil {
		return nil, nil, nil, err
	}
	if delivErr.Valid {
		sess.DeliveryError = &delivErr.String
	}
	if readyAt.Valid {
		sess.ReadyAt = &readyAt.String
	}

	return a, &p, &sess, nil
}

func (s *SQLiteStore) IsAgentReady(ctx context.Context, agentID string) (bool, error) {
	var status string
	err := s.db.QueryRowContext(ctx, `SELECT status FROM agent_sessions WHERE agent_id = ?`, agentID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return status == domain.SessionStatusReady, nil
}

func (s *SQLiteStore) verifyAgentReadyInTx(ctx context.Context, tx *sql.Tx, agentID string, roleDescription string) error {
	var status string
	err := tx.QueryRowContext(ctx, `SELECT status FROM agent_sessions WHERE agent_id = ?`, agentID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) || status != domain.SessionStatusReady {
		return fmt.Errorf("%w: %s '%s' is not ready (status=%s)", domain.ErrAgentNotReady, roleDescription, agentID, status)
	}
	if err != nil {
		return err
	}
	return nil
}

// Tasks

func (s *SQLiteStore) SubmitTask(ctx context.Context, task *domain.Task, initialMsg *domain.Message, initialEvt *domain.Event) (*domain.Task, bool, error) {
	if err := task.Validate(); err != nil {
		return nil, false, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()

	// Atomic Ready Gate verification inside the transaction
	if err := s.verifyAgentReadyInTx(ctx, tx, task.SenderAgentID, "sender agent"); err != nil {
		return nil, false, err
	}
	if err := s.verifyAgentReadyInTx(ctx, tx, task.TargetAgentID, "target agent"); err != nil {
		return nil, false, err
	}

	// 1. Check idempotency: sender_agent_id + idempotency_key
	var existingTask domain.Task
	var res, errStr sql.NullString
	checkQuery := `SELECT id, sender_agent_id, target_agent_id, idempotency_key, content, status, result, error, created_at, updated_at
		FROM tasks WHERE sender_agent_id = ? AND idempotency_key = ?`
	err = tx.QueryRowContext(ctx, checkQuery, task.SenderAgentID, task.IdempotencyKey).Scan(
		&existingTask.ID,
		&existingTask.SenderAgentID,
		&existingTask.TargetAgentID,
		&existingTask.IdempotencyKey,
		&existingTask.Content,
		&existingTask.Status,
		&res,
		&errStr,
		&existingTask.CreatedAt,
		&existingTask.UpdatedAt,
	)
	if err == nil {
		if res.Valid {
			existingTask.Result = &res.String
		}
		if errStr.Valid {
			existingTask.Error = &errStr.String
		}
		if existingTask.TargetAgentID != task.TargetAgentID || existingTask.Content != task.Content {
			return nil, false, domain.ErrIdempotencyConflict
		}
		return &existingTask, true, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}

	// 2. Insert Task
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if task.CreatedAt == "" {
		task.CreatedAt = now
	}
	task.UpdatedAt = now

	insertTask := `INSERT INTO tasks (id, sender_agent_id, target_agent_id, idempotency_key, content, status, result, error, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err = tx.ExecContext(ctx, insertTask,
		task.ID, task.SenderAgentID, task.TargetAgentID, task.IdempotencyKey, task.Content, task.Status, task.Result, task.Error, task.CreatedAt, task.UpdatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "uq_tasks_target_active") || (strings.Contains(err.Error(), "UNIQUE constraint failed") && strings.Contains(err.Error(), "tasks.target_agent_id")) {
			return nil, false, domain.ErrWorkerBusy
		}
		return nil, false, err
	}

	// 3. Insert Initial Message if present
	if initialMsg != nil {
		if initialMsg.CreatedAt == "" {
			initialMsg.CreatedAt = now
		}
		insertMsg := `INSERT INTO task_messages (id, task_id, sender_agent_id, kind, content, created_at)
			VALUES (?, ?, ?, ?, ?, ?)`
		_, err = tx.ExecContext(ctx, insertMsg,
			initialMsg.ID, initialMsg.TaskID, initialMsg.SenderAgentID, initialMsg.Kind, initialMsg.Content, initialMsg.CreatedAt,
		)
		if err != nil {
			return nil, false, err
		}
	}

	// 4. Insert Initial Event if present
	if initialEvt != nil {
		if initialEvt.CreatedAt == "" {
			initialEvt.CreatedAt = now
		}
		insertEvt := `INSERT INTO task_events (id, task_id, actor_agent_id, type, payload, created_at)
			VALUES (?, ?, ?, ?, ?, ?)`
		res, err := tx.ExecContext(ctx, insertEvt,
			initialEvt.ID, initialEvt.TaskID, initialEvt.ActorAgentID, initialEvt.Type, initialEvt.Payload, initialEvt.CreatedAt,
		)
		if err != nil {
			return nil, false, err
		}
		seq, err := res.LastInsertId()
		if err == nil {
			initialEvt.Sequence = seq
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, false, err
	}

	return task, false, nil
}

func (s *SQLiteStore) GetTask(ctx context.Context, id string) (*domain.Task, error) {
	query := `SELECT id, sender_agent_id, target_agent_id, idempotency_key, content, status, result, error, created_at, updated_at
		FROM tasks WHERE id = ?`
	var t domain.Task
	var res, errStr sql.NullString
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&t.ID, &t.SenderAgentID, &t.TargetAgentID, &t.IdempotencyKey, &t.Content, &t.Status, &res, &errStr, &t.CreatedAt, &t.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	if res.Valid {
		t.Result = &res.String
	}
	if errStr.Valid {
		t.Error = &errStr.String
	}
	return &t, nil
}

func (s *SQLiteStore) ListTasks(ctx context.Context, agentID string, status string) ([]*domain.Task, error) {
	baseQuery := `SELECT id, sender_agent_id, target_agent_id, idempotency_key, content, status, result, error, created_at, updated_at FROM tasks`
	var conditions []string
	var args []any

	if agentID != "" {
		conditions = append(conditions, "(sender_agent_id = ? OR target_agent_id = ?)")
		args = append(args, agentID, agentID)
	}
	if status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, status)
	}

	if len(conditions) > 0 {
		baseQuery += " WHERE " + strings.Join(conditions, " AND ")
	}
	baseQuery += " ORDER BY created_at ASC"

	rows, err := s.db.QueryContext(ctx, baseQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*domain.Task
	for rows.Next() {
		var t domain.Task
		var res, errStr sql.NullString
		if err := rows.Scan(&t.ID, &t.SenderAgentID, &t.TargetAgentID, &t.IdempotencyKey, &t.Content, &t.Status, &res, &errStr, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		if res.Valid {
			t.Result = &res.String
		}
		if errStr.Valid {
			t.Error = &errStr.String
		}
		tasks = append(tasks, &t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tasks, nil
}

func (s *SQLiteStore) AckTask(ctx context.Context, id string, actorAgentID string, evt *domain.Event) (*domain.Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Atomic Ready Gate verification inside the transaction
	if err := s.verifyAgentReadyInTx(ctx, tx, actorAgentID, "actor agent"); err != nil {
		return nil, err
	}

	task, err := s.getTaskForUpdate(ctx, tx, id)
	if err != nil {
		return nil, err
	}

	if task.TargetAgentID != actorAgentID {
		return nil, fmt.Errorf("%w: actor '%s' is not target agent '%s'", domain.ErrUnauthorized, actorAgentID, task.TargetAgentID)
	}

	if task.Status != domain.TaskStatusQueued {
		return nil, fmt.Errorf("%w: cannot ACK task in status '%s' (expected 'queued')", domain.ErrInvalidTransition, task.Status)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	task.Status = domain.TaskStatusRunning
	task.UpdatedAt = now

	updateQ := `UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?`
	if _, err := tx.ExecContext(ctx, updateQ, task.Status, task.UpdatedAt, task.ID); err != nil {
		return nil, err
	}

	if evt != nil {
		if err := s.insertEventInTx(ctx, tx, evt); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *SQLiteStore) UpdateTaskStatus(ctx context.Context, id string, actorAgentID string, msg *domain.Message, evt *domain.Event) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Atomic Ready Gate verification inside the transaction
	if err := s.verifyAgentReadyInTx(ctx, tx, actorAgentID, "actor agent"); err != nil {
		return err
	}

	task, err := s.getTaskForUpdate(ctx, tx, id)
	if err != nil {
		return err
	}

	if task.TargetAgentID != actorAgentID {
		return fmt.Errorf("%w: actor '%s' is not target agent '%s'", domain.ErrUnauthorized, actorAgentID, task.TargetAgentID)
	}

	if task.Status != domain.TaskStatusRunning {
		return fmt.Errorf("%w: cannot update status for task in state '%s' (must be 'running')", domain.ErrInvalidTransition, task.Status)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	task.UpdatedAt = now
	updateQ := `UPDATE tasks SET updated_at = ? WHERE id = ?`
	if _, err := tx.ExecContext(ctx, updateQ, task.UpdatedAt, task.ID); err != nil {
		return err
	}

	if msg != nil {
		if err := s.insertMessageInTx(ctx, tx, msg); err != nil {
			return err
		}
	}
	if evt != nil {
		if err := s.insertEventInTx(ctx, tx, evt); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) SendMessage(ctx context.Context, taskID string, senderAgentID string, msg *domain.Message, evt *domain.Event) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Atomic Ready Gate verification inside the transaction
	if err := s.verifyAgentReadyInTx(ctx, tx, senderAgentID, "sender agent"); err != nil {
		return err
	}

	task, err := s.getTaskForUpdate(ctx, tx, taskID)
	if err != nil {
		return err
	}

	if task.SenderAgentID != senderAgentID {
		return fmt.Errorf("%w: sender '%s' is not task initiator '%s'", domain.ErrUnauthorized, senderAgentID, task.SenderAgentID)
	}

	if task.IsTerminal() {
		return fmt.Errorf("%w: cannot send message to task in terminal state '%s'", domain.ErrTerminalState, task.Status)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	task.UpdatedAt = now
	updateQ := `UPDATE tasks SET updated_at = ? WHERE id = ?`
	if _, err := tx.ExecContext(ctx, updateQ, task.UpdatedAt, task.ID); err != nil {
		return err
	}

	if msg != nil {
		if err := s.insertMessageInTx(ctx, tx, msg); err != nil {
			return err
		}
	}
	if evt != nil {
		if err := s.insertEventInTx(ctx, tx, evt); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) CompleteTask(ctx context.Context, id string, actorAgentID string, result string, evt *domain.Event) (*domain.Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Atomic Ready Gate verification inside the transaction
	if err := s.verifyAgentReadyInTx(ctx, tx, actorAgentID, "actor agent"); err != nil {
		return nil, err
	}

	task, err := s.getTaskForUpdate(ctx, tx, id)
	if err != nil {
		return nil, err
	}

	if task.TargetAgentID != actorAgentID {
		return nil, fmt.Errorf("%w: actor '%s' is not target agent '%s'", domain.ErrUnauthorized, actorAgentID, task.TargetAgentID)
	}

	if task.Status != domain.TaskStatusRunning {
		return nil, fmt.Errorf("%w: cannot complete task in state '%s' (must be 'running')", domain.ErrInvalidTransition, task.Status)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	task.Status = domain.TaskStatusSucceeded
	task.Result = &result
	task.UpdatedAt = now

	updateQ := `UPDATE tasks SET status = ?, result = ?, updated_at = ? WHERE id = ?`
	if _, err := tx.ExecContext(ctx, updateQ, task.Status, task.Result, task.UpdatedAt, task.ID); err != nil {
		return nil, err
	}

	if evt != nil {
		if err := s.insertEventInTx(ctx, tx, evt); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *SQLiteStore) FailTask(ctx context.Context, id string, actorAgentID string, errStr string, evt *domain.Event) (*domain.Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Atomic Ready Gate verification inside the transaction
	if err := s.verifyAgentReadyInTx(ctx, tx, actorAgentID, "actor agent"); err != nil {
		return nil, err
	}

	task, err := s.getTaskForUpdate(ctx, tx, id)
	if err != nil {
		return nil, err
	}

	if task.TargetAgentID != actorAgentID {
		return nil, fmt.Errorf("%w: actor '%s' is not target agent '%s'", domain.ErrUnauthorized, actorAgentID, task.TargetAgentID)
	}

	if task.Status != domain.TaskStatusRunning && task.Status != domain.TaskStatusQueued {
		return nil, fmt.Errorf("%w: cannot fail task in state '%s'", domain.ErrInvalidTransition, task.Status)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	task.Status = domain.TaskStatusFailed
	task.Error = &errStr
	task.UpdatedAt = now

	updateQ := `UPDATE tasks SET status = ?, error = ?, updated_at = ? WHERE id = ?`
	if _, err := tx.ExecContext(ctx, updateQ, task.Status, task.Error, task.UpdatedAt, task.ID); err != nil {
		return nil, err
	}

	if evt != nil {
		if err := s.insertEventInTx(ctx, tx, evt); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *SQLiteStore) CancelTask(ctx context.Context, id string, actorAgentID string, evt *domain.Event) (*domain.Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Atomic Ready Gate verification inside the transaction
	if err := s.verifyAgentReadyInTx(ctx, tx, actorAgentID, "actor agent"); err != nil {
		return nil, err
	}

	task, err := s.getTaskForUpdate(ctx, tx, id)
	if err != nil {
		return nil, err
	}

	if task.SenderAgentID != actorAgentID {
		return nil, fmt.Errorf("%w: only sender agent '%s' can cancel task, got actor '%s'", domain.ErrUnauthorized, task.SenderAgentID, actorAgentID)
	}

	// In V0, can only cancel queued tasks (running tasks cannot be canceled)
	if task.Status != domain.TaskStatusQueued {
		return nil, fmt.Errorf("%w: cannot cancel task in state '%s' (only queued tasks can be canceled in V0)", domain.ErrInvalidTransition, task.Status)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	task.Status = domain.TaskStatusCanceled
	task.UpdatedAt = now

	updateQ := `UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?`
	if _, err := tx.ExecContext(ctx, updateQ, task.Status, task.UpdatedAt, task.ID); err != nil {
		return nil, err
	}

	if evt != nil {
		if err := s.insertEventInTx(ctx, tx, evt); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *SQLiteStore) AddEvent(ctx context.Context, evt *domain.Event) (int64, error) {
	if evt.CreatedAt == "" {
		evt.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	query := `INSERT INTO task_events (id, task_id, actor_agent_id, type, payload, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`
	res, err := s.db.ExecContext(ctx, query, evt.ID, evt.TaskID, evt.ActorAgentID, evt.Type, evt.Payload, evt.CreatedAt)
	if err != nil {
		return 0, err
	}
	seq, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	evt.Sequence = seq
	return seq, nil
}

func (s *SQLiteStore) GetEvents(ctx context.Context, taskID string, afterSeq int64) ([]*domain.Event, error) {
	query := `SELECT sequence, id, task_id, actor_agent_id, type, payload, created_at
		FROM task_events WHERE task_id = ? AND sequence > ? ORDER BY sequence ASC`
	rows, err := s.db.QueryContext(ctx, query, taskID, afterSeq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*domain.Event
	for rows.Next() {
		var e domain.Event
		if err := rows.Scan(&e.Sequence, &e.ID, &e.TaskID, &e.ActorAgentID, &e.Type, &e.Payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, &e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func (s *SQLiteStore) GetTaskMessages(ctx context.Context, taskID string) ([]*domain.Message, error) {
	query := `SELECT id, task_id, sender_agent_id, kind, content, created_at
		FROM task_messages WHERE task_id = ? ORDER BY created_at ASC`
	rows, err := s.db.QueryContext(ctx, query, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []*domain.Message
	for rows.Next() {
		var m domain.Message
		if err := rows.Scan(&m.ID, &m.TaskID, &m.SenderAgentID, &m.Kind, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, &m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return msgs, nil
}

// Helpers

func (s *SQLiteStore) getTaskForUpdate(ctx context.Context, tx *sql.Tx, id string) (*domain.Task, error) {
	query := `SELECT id, sender_agent_id, target_agent_id, idempotency_key, content, status, result, error, created_at, updated_at
		FROM tasks WHERE id = ?`
	var t domain.Task
	var res, errStr sql.NullString
	err := tx.QueryRowContext(ctx, query, id).Scan(
		&t.ID, &t.SenderAgentID, &t.TargetAgentID, &t.IdempotencyKey, &t.Content, &t.Status, &res, &errStr, &t.CreatedAt, &t.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	if res.Valid {
		t.Result = &res.String
	}
	if errStr.Valid {
		t.Error = &errStr.String
	}
	return &t, nil
}

func (s *SQLiteStore) insertMessageInTx(ctx context.Context, tx *sql.Tx, msg *domain.Message) error {
	if msg.CreatedAt == "" {
		msg.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	query := `INSERT INTO task_messages (id, task_id, sender_agent_id, kind, content, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`
	_, err := tx.ExecContext(ctx, query, msg.ID, msg.TaskID, msg.SenderAgentID, msg.Kind, msg.Content, msg.CreatedAt)
	return err
}

func (s *SQLiteStore) insertEventInTx(ctx context.Context, tx *sql.Tx, evt *domain.Event) error {
	if evt.CreatedAt == "" {
		evt.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	query := `INSERT INTO task_events (id, task_id, actor_agent_id, type, payload, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`
	res, err := tx.ExecContext(ctx, query, evt.ID, evt.TaskID, evt.ActorAgentID, evt.Type, evt.Payload, evt.CreatedAt)
	if err != nil {
		return err
	}
	seq, err := res.LastInsertId()
	if err == nil {
		evt.Sequence = seq
	}
	return nil
}
