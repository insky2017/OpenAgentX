package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agentbus/internal/domain"
	_ "github.com/mattn/go-sqlite3"
)

//go:embed schema.sql
var schemaSQL string

type Store interface {
	Close() error
	RegisterAgent(ctx context.Context, agent *domain.Agent) error
	GetAgent(ctx context.Context, id string) (*domain.Agent, error)
	ListAgents(ctx context.Context) ([]*domain.Agent, error)

	SubmitTask(ctx context.Context, task *domain.Task, initialMsg *domain.Message, event *domain.Event) (*domain.Task, bool, error)
	GetTask(ctx context.Context, id string) (*domain.Task, error)
	ListTasks(ctx context.Context, agentID string, status string) ([]*domain.Task, error)

	AckTask(ctx context.Context, taskID string, actorAgentID string, event *domain.Event) (*domain.Task, error)
	UpdateTaskStatus(ctx context.Context, taskID string, actorAgentID string, msg *domain.Message, event *domain.Event) error
	SendMessage(ctx context.Context, taskID string, senderAgentID string, msg *domain.Message, event *domain.Event) error
	CompleteTask(ctx context.Context, taskID string, actorAgentID string, result string, event *domain.Event) (*domain.Task, error)
	FailTask(ctx context.Context, taskID string, actorAgentID string, errStr string, event *domain.Event) (*domain.Task, error)
	CancelTask(ctx context.Context, taskID string, actorAgentID string, event *domain.Event) (*domain.Task, error)

	AddEvent(ctx context.Context, event *domain.Event) (*domain.Event, error)
	GetEvents(ctx context.Context, taskID string, afterSeq int64) ([]*domain.Event, error)
	GetTaskMessages(ctx context.Context, taskID string) ([]*domain.Message, error)
}

type SQLiteStore struct {
	db *sql.DB
}

func OpenSQLite(dbPath string) (*SQLiteStore, error) {
	if dbPath != ":memory:" {
		dir := filepath.Dir(dbPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create db directory: %w", err)
		}
	}

	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON&_synchronous=NORMAL", dbPath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	db.SetMaxOpenConns(1) // Single writer for SQLite serialize safety
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to apply schema migration: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) RegisterAgent(ctx context.Context, agent *domain.Agent) error {
	if err := agent.Validate(); err != nil {
		return err
	}

	query := `
	INSERT INTO agents (id, role, connector, address, status, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		role = excluded.role,
		connector = excluded.connector,
		address = excluded.address,
		status = excluded.status,
		updated_at = excluded.updated_at
	`
	_, err := s.db.ExecContext(ctx, query,
		agent.ID,
		agent.Role,
		agent.Connector,
		agent.Address,
		agent.Status,
		agent.CreatedAt,
		agent.UpdatedAt,
	)
	return err
}

func (s *SQLiteStore) GetAgent(ctx context.Context, id string) (*domain.Agent, error) {
	query := `SELECT id, role, connector, address, status, created_at, updated_at FROM agents WHERE id = ?`
	row := s.db.QueryRowContext(ctx, query, id)

	var a domain.Agent
	if err := row.Scan(&a.ID, &a.Role, &a.Connector, &a.Address, &a.Status, &a.CreatedAt, &a.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrAgentNotFound
		}
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
	return agents, rows.Err()
}

func (s *SQLiteStore) SubmitTask(ctx context.Context, task *domain.Task, initialMsg *domain.Message, event *domain.Event) (*domain.Task, bool, error) {
	if err := task.Validate(); err != nil {
		return nil, false, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()

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
		// Idempotent duplicate: return existing task
		return &existingTask, true, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}

	// 2. Validate sender exists
	var senderID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM agents WHERE id = ?`, task.SenderAgentID).Scan(&senderID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, fmt.Errorf("%w: sender '%s'", domain.ErrAgentNotFound, task.SenderAgentID)
		}
		return nil, false, err
	}

	// 3. Validate target exists
	var targetID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM agents WHERE id = ?`, task.TargetAgentID).Scan(&targetID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, fmt.Errorf("%w: target '%s'", domain.ErrAgentNotFound, task.TargetAgentID)
		}
		return nil, false, err
	}

	// 4. Validate target worker active task count constraint (max 1 active task: queued or running)
	var activeCount int
	countQuery := `SELECT COUNT(*) FROM tasks WHERE target_agent_id = ? AND status IN ('queued', 'running')`
	if err := tx.QueryRowContext(ctx, countQuery, task.TargetAgentID).Scan(&activeCount); err != nil {
		return nil, false, err
	}
	if activeCount > 0 {
		return nil, false, domain.ErrWorkerBusy
	}

	// 5. Insert task
	insertTaskQuery := `
	INSERT INTO tasks (id, sender_agent_id, target_agent_id, idempotency_key, content, status, result, error, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, NULL, NULL, ?, ?)
	`
	if _, err := tx.ExecContext(ctx, insertTaskQuery,
		task.ID,
		task.SenderAgentID,
		task.TargetAgentID,
		task.IdempotencyKey,
		task.Content,
		task.Status,
		task.CreatedAt,
		task.UpdatedAt,
	); err != nil {
		if strings.Contains(err.Error(), "uq_tasks_target_active") || (strings.Contains(err.Error(), "UNIQUE constraint failed") && strings.Contains(err.Error(), "target_agent_id")) {
			return nil, false, domain.ErrWorkerBusy
		}
		return nil, false, err
	}

	// 6. Insert initial message
	if initialMsg != nil {
		if err := initialMsg.Validate(); err != nil {
			return nil, false, err
		}
		insertMsgQuery := `INSERT INTO messages (id, task_id, sender_agent_id, kind, content, created_at) VALUES (?, ?, ?, ?, ?, ?)`
		if _, err := tx.ExecContext(ctx, insertMsgQuery,
			initialMsg.ID,
			initialMsg.TaskID,
			initialMsg.SenderAgentID,
			initialMsg.Kind,
			initialMsg.Content,
			initialMsg.CreatedAt,
		); err != nil {
			return nil, false, err
		}
	}

	// 7. Insert event
	if event != nil {
		if err := event.Validate(); err != nil {
			return nil, false, err
		}
		insertEventQuery := `INSERT INTO events (id, task_id, actor_agent_id, type, payload, created_at) VALUES (?, ?, ?, ?, ?, ?)`
		resExec, err := tx.ExecContext(ctx, insertEventQuery,
			event.ID,
			event.TaskID,
			event.ActorAgentID,
			event.Type,
			event.Payload,
			event.CreatedAt,
		)
		if err != nil {
			return nil, false, err
		}
		seq, err := resExec.LastInsertId()
		if err == nil {
			event.Sequence = seq
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, false, err
	}

	return task, false, nil
}

func (s *SQLiteStore) GetTask(ctx context.Context, id string) (*domain.Task, error) {
	query := `SELECT id, sender_agent_id, target_agent_id, idempotency_key, content, status, result, error, created_at, updated_at FROM tasks WHERE id = ?`
	row := s.db.QueryRowContext(ctx, query, id)

	var t domain.Task
	var res, errStr sql.NullString
	if err := row.Scan(
		&t.ID,
		&t.SenderAgentID,
		&t.TargetAgentID,
		&t.IdempotencyKey,
		&t.Content,
		&t.Status,
		&res,
		&errStr,
		&t.CreatedAt,
		&t.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrTaskNotFound
		}
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
	var query strings.Builder
	query.WriteString(`SELECT id, sender_agent_id, target_agent_id, idempotency_key, content, status, result, error, created_at, updated_at FROM tasks WHERE 1=1`)
	var args []any

	if agentID != "" {
		query.WriteString(` AND (sender_agent_id = ? OR target_agent_id = ?)`)
		args = append(args, agentID, agentID)
	}
	if status != "" {
		query.WriteString(` AND status = ?`)
		args = append(args, status)
	}
	query.WriteString(` ORDER BY created_at DESC`)

	rows, err := s.db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*domain.Task
	for rows.Next() {
		var t domain.Task
		var res, errStr sql.NullString
		if err := rows.Scan(
			&t.ID,
			&t.SenderAgentID,
			&t.TargetAgentID,
			&t.IdempotencyKey,
			&t.Content,
			&t.Status,
			&res,
			&errStr,
			&t.CreatedAt,
			&t.UpdatedAt,
		); err != nil {
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
	return tasks, rows.Err()
}

func (s *SQLiteStore) AckTask(ctx context.Context, taskID string, actorAgentID string, event *domain.Event) (*domain.Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	task, err := getTaskTx(ctx, tx, taskID)
	if err != nil {
		return nil, err
	}

	if task.TargetAgentID != actorAgentID {
		return nil, domain.ErrUnauthorized
	}

	if task.Status != domain.TaskStatusQueued {
		return nil, fmt.Errorf("%w: cannot ack task in status %s", domain.ErrInvalidTransition, task.Status)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	task.Status = domain.TaskStatusRunning
	task.UpdatedAt = now

	updateQuery := `UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?`
	if _, err := tx.ExecContext(ctx, updateQuery, task.Status, task.UpdatedAt, task.ID); err != nil {
		return nil, err
	}

	if event != nil {
		if err := insertEventTx(ctx, tx, event); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return task, nil
}

func (s *SQLiteStore) UpdateTaskStatus(ctx context.Context, taskID string, actorAgentID string, msg *domain.Message, event *domain.Event) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	task, err := getTaskTx(ctx, tx, taskID)
	if err != nil {
		return err
	}

	if task.TargetAgentID != actorAgentID {
		return domain.ErrUnauthorized
	}

	if task.Status != domain.TaskStatusRunning {
		return fmt.Errorf("%w: cannot update status of task in status %s", domain.ErrInvalidState, task.Status)
	}

	if msg != nil {
		if err := insertMessageTx(ctx, tx, msg); err != nil {
			return err
		}
	}

	if event != nil {
		if err := insertEventTx(ctx, tx, event); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) SendMessage(ctx context.Context, taskID string, senderAgentID string, msg *domain.Message, event *domain.Event) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	task, err := getTaskTx(ctx, tx, taskID)
	if err != nil {
		return err
	}

	if task.SenderAgentID != senderAgentID {
		return domain.ErrUnauthorized
	}

	if !task.IsActive() {
		return fmt.Errorf("%w: task is in terminal status %s", domain.ErrTerminalState, task.Status)
	}

	if msg != nil {
		if err := insertMessageTx(ctx, tx, msg); err != nil {
			return err
		}
	}

	if event != nil {
		if err := insertEventTx(ctx, tx, event); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) CompleteTask(ctx context.Context, taskID string, actorAgentID string, result string, event *domain.Event) (*domain.Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	task, err := getTaskTx(ctx, tx, taskID)
	if err != nil {
		return nil, err
	}

	if task.TargetAgentID != actorAgentID {
		return nil, domain.ErrUnauthorized
	}

	if task.Status != domain.TaskStatusRunning {
		return nil, fmt.Errorf("%w: cannot complete task in status %s", domain.ErrInvalidTransition, task.Status)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	task.Status = domain.TaskStatusSucceeded
	task.Result = &result
	task.UpdatedAt = now

	updateQuery := `UPDATE tasks SET status = ?, result = ?, updated_at = ? WHERE id = ?`
	if _, err := tx.ExecContext(ctx, updateQuery, task.Status, result, task.UpdatedAt, task.ID); err != nil {
		return nil, err
	}

	if event != nil {
		if err := insertEventTx(ctx, tx, event); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return task, nil
}

func (s *SQLiteStore) FailTask(ctx context.Context, taskID string, actorAgentID string, errStr string, event *domain.Event) (*domain.Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	task, err := getTaskTx(ctx, tx, taskID)
	if err != nil {
		return nil, err
	}

	if task.TargetAgentID != actorAgentID {
		return nil, domain.ErrUnauthorized
	}

	if task.Status != domain.TaskStatusRunning {
		return nil, fmt.Errorf("%w: cannot fail task in status %s", domain.ErrInvalidTransition, task.Status)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	task.Status = domain.TaskStatusFailed
	task.Error = &errStr
	task.UpdatedAt = now

	updateQuery := `UPDATE tasks SET status = ?, error = ?, updated_at = ? WHERE id = ?`
	if _, err := tx.ExecContext(ctx, updateQuery, task.Status, errStr, task.UpdatedAt, task.ID); err != nil {
		return nil, err
	}

	if event != nil {
		if err := insertEventTx(ctx, tx, event); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return task, nil
}

func (s *SQLiteStore) CancelTask(ctx context.Context, taskID string, actorAgentID string, event *domain.Event) (*domain.Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	task, err := getTaskTx(ctx, tx, taskID)
	if err != nil {
		return nil, err
	}

	if task.SenderAgentID != actorAgentID {
		return nil, domain.ErrUnauthorized
	}

	if task.Status == domain.TaskStatusRunning {
		return nil, domain.ErrInvalidTransition
	}

	if task.Status != domain.TaskStatusQueued {
		return nil, fmt.Errorf("%w: cannot cancel task in status %s", domain.ErrTerminalState, task.Status)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	task.Status = domain.TaskStatusCanceled
	task.UpdatedAt = now

	updateQuery := `UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?`
	if _, err := tx.ExecContext(ctx, updateQuery, task.Status, task.UpdatedAt, task.ID); err != nil {
		return nil, err
	}

	if event != nil {
		if err := insertEventTx(ctx, tx, event); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return task, nil
}

func (s *SQLiteStore) AddEvent(ctx context.Context, event *domain.Event) (*domain.Event, error) {
	if err := event.Validate(); err != nil {
		return nil, err
	}

	query := `INSERT INTO events (id, task_id, actor_agent_id, type, payload, created_at) VALUES (?, ?, ?, ?, ?, ?)`
	res, err := s.db.ExecContext(ctx, query,
		event.ID,
		event.TaskID,
		event.ActorAgentID,
		event.Type,
		event.Payload,
		event.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	seq, err := res.LastInsertId()
	if err == nil {
		event.Sequence = seq
	}
	return event, nil
}

func (s *SQLiteStore) GetEvents(ctx context.Context, taskID string, afterSeq int64) ([]*domain.Event, error) {
	query := `SELECT sequence, id, task_id, actor_agent_id, type, payload, created_at FROM events WHERE task_id = ? AND sequence > ? ORDER BY sequence ASC`
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
	return events, rows.Err()
}

func (s *SQLiteStore) GetTaskMessages(ctx context.Context, taskID string) ([]*domain.Message, error) {
	query := `SELECT id, task_id, sender_agent_id, kind, content, created_at FROM messages WHERE task_id = ? ORDER BY created_at ASC`
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
	return msgs, rows.Err()
}

// Helpers for transactions

func getTaskTx(ctx context.Context, tx *sql.Tx, id string) (*domain.Task, error) {
	query := `SELECT id, sender_agent_id, target_agent_id, idempotency_key, content, status, result, error, created_at, updated_at FROM tasks WHERE id = ?`
	row := tx.QueryRowContext(ctx, query, id)

	var t domain.Task
	var res, errStr sql.NullString
	if err := row.Scan(
		&t.ID,
		&t.SenderAgentID,
		&t.TargetAgentID,
		&t.IdempotencyKey,
		&t.Content,
		&t.Status,
		&res,
		&errStr,
		&t.CreatedAt,
		&t.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrTaskNotFound
		}
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

func insertEventTx(ctx context.Context, tx *sql.Tx, event *domain.Event) error {
	if err := event.Validate(); err != nil {
		return err
	}
	query := `INSERT INTO events (id, task_id, actor_agent_id, type, payload, created_at) VALUES (?, ?, ?, ?, ?, ?)`
	res, err := tx.ExecContext(ctx, query,
		event.ID,
		event.TaskID,
		event.ActorAgentID,
		event.Type,
		event.Payload,
		event.CreatedAt,
	)
	if err != nil {
		return err
	}
	seq, err := res.LastInsertId()
	if err == nil {
		event.Sequence = seq
	}
	return nil
}

func insertMessageTx(ctx context.Context, tx *sql.Tx, msg *domain.Message) error {
	if err := msg.Validate(); err != nil {
		return err
	}
	query := `INSERT INTO messages (id, task_id, sender_agent_id, kind, content, created_at) VALUES (?, ?, ?, ?, ?, ?)`
	_, err := tx.ExecContext(ctx, query,
		msg.ID,
		msg.TaskID,
		msg.SenderAgentID,
		msg.Kind,
		msg.Content,
		msg.CreatedAt,
	)
	return err
}
