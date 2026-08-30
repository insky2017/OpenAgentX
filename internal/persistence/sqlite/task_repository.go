package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"agentbus/internal/domain"
)

const taskColumns = `task_id, version, status, sender_principal_id, target_agent_id, dispatch_mode,
	parent_task_id, organization_id, idempotency_key, content, result, error,
	cancel_requested_by, cancel_requested_at, created_at, updated_at`

type CreateTaskResult = domain.CreateTaskResult

func (r *Repository) CreateTask(
	ctx context.Context,
	task *domain.Task,
	initialMessage *domain.Message,
	mailboxItem *domain.MailboxItem,
	event *domain.JournalEvent,
) (*CreateTaskResult, error) {
	if task == nil || mailboxItem == nil {
		return nil, domain.ErrInvalidInput("task and mailbox item are required")
	}
	now := r.now().UTC()
	if task.Version == 0 {
		task.Version = 1
	}
	if task.Status == "" {
		task.Status = domain.TaskStatusQueued
	}
	if task.DispatchMode == "" {
		task.DispatchMode = domain.DispatchModeCoordinated
	}
	task.CreatedAt = normalizeStringTime(task.CreatedAt, now)
	task.UpdatedAt = normalizeStringTime(task.UpdatedAt, now)
	if err := task.ValidateTarget(); err != nil {
		return nil, err
	}
	if task.Status != domain.TaskStatusQueued {
		return nil, domain.ErrInvalidInput("new task must be queued")
	}

	if initialMessage != nil {
		if initialMessage.Version == 0 {
			initialMessage.Version = 1
		}
		if initialMessage.Sequence == 0 {
			initialMessage.Sequence = 1
		}
		if initialMessage.TaskID == "" {
			initialMessage.TaskID = task.ID
		}
		if initialMessage.SenderPrincipalID == "" {
			initialMessage.SenderPrincipalID = task.SenderPrincipalID
		}
		if initialMessage.TargetAgentID == "" {
			initialMessage.TargetAgentID = task.TargetAgentID
		}
		if initialMessage.Kind == "" {
			initialMessage.Kind = domain.MessageKindInstruction
		}
		initialMessage.CreatedAt = normalizeStringTime(initialMessage.CreatedAt, now)
		if err := initialMessage.ValidateTarget(); err != nil {
			return nil, err
		}
		if initialMessage.TaskID != task.ID || initialMessage.Sequence != 1 {
			return nil, domain.ErrInvalidInput("initial message must be sequence 1 for the new task")
		}
	}

	mailboxItem.TargetAgentID = task.TargetAgentID
	mailboxItem.Kind = domain.MailboxKindTask
	mailboxItem.Lane = domain.MailboxLaneWork
	mailboxItem.TaskID = task.ID
	if mailboxItem.State == "" {
		mailboxItem.State = domain.MailboxStatePending
	}
	mailboxItem.CreatedAt = normalizeTime(mailboxItem.CreatedAt, now)
	if err := mailboxItem.ValidateForInsert(); err != nil {
		return nil, err
	}
	if err := validateJournalForAggregate(event, "task", task.ID, now); err != nil {
		return nil, err
	}

	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	existing, err := getTaskByIdempotency(ctx, tx, task.SenderPrincipalID, task.IdempotencyKey)
	if err == nil {
		if !sameTaskCommand(existing, task) {
			return nil, domain.ErrIdempotencyConflict
		}
		existingMailbox, err := scanMailbox(tx.QueryRowContext(ctx, `SELECT `+mailboxColumns+`
			FROM mailbox_items WHERE task_id = ? AND kind = 'task' ORDER BY sequence ASC LIMIT 1`, existing.ID))
		if err != nil {
			return nil, fmt.Errorf("load idempotent task mailbox: %w", err)
		}
		return &CreateTaskResult{Task: *existing, MailboxItem: *existingMailbox, Replay: true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("check task idempotency: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO tasks (`+taskColumns+`) VALUES (
		?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.Version, task.Status, task.SenderPrincipalID, task.TargetAgentID, task.DispatchMode,
		nullableString(task.ParentTaskID), task.OrganizationID, task.IdempotencyKey, task.Content,
		nullableString(task.Result), nullableString(task.Error), nullableString(task.CancelRequestedBy),
		nullableString(task.CancelRequestedAt), task.CreatedAt, task.UpdatedAt); err != nil {
		if isUniqueConstraint(err, "") {
			return nil, domain.ErrIdempotencyConflict
		}
		return nil, fmt.Errorf("insert task: %w", err)
	}
	if initialMessage != nil {
		if err := insertMessage(ctx, tx, initialMessage); err != nil {
			return nil, err
		}
	}
	if err := r.inject(FaultAfterStateWrite); err != nil {
		return nil, err
	}
	if err := insertMailbox(ctx, tx, mailboxItem); err != nil {
		return nil, err
	}
	if err := r.inject(FaultAfterDelivery); err != nil {
		return nil, err
	}
	if err := insertJournal(ctx, tx, event); err != nil {
		return nil, err
	}
	if err := r.inject(FaultBeforeCommit); err != nil {
		return nil, err
	}
	if err := commit(tx); err != nil {
		return nil, err
	}
	return &CreateTaskResult{Task: *task, MailboxItem: *mailboxItem, Event: *event}, nil
}

func normalizeStringTime(value string, fallback time.Time) string {
	if strings.TrimSpace(value) == "" {
		return formatTime(fallback)
	}
	return value
}

func sameTaskCommand(left *domain.Task, right *domain.Task) bool {
	leftParent := nullableValue(left.ParentTaskID)
	rightParent := nullableValue(right.ParentTaskID)
	return left.TargetAgentID == right.TargetAgentID &&
		left.OrganizationID == right.OrganizationID &&
		left.DispatchMode == right.DispatchMode &&
		leftParent == rightParent && left.Content == right.Content
}

func nullableValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func insertMessage(ctx context.Context, tx *sql.Tx, message *domain.Message) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO messages (
		message_id, task_id, version, sequence, sender_principal_id, target_agent_id, kind, content, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, message.ID, message.TaskID, message.Version,
		message.Sequence, message.SenderPrincipalID, message.TargetAgentID, message.Kind,
		message.Content, message.CreatedAt); err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	return nil
}

func (r *Repository) GetTask(ctx context.Context, taskID string) (*domain.Task, error) {
	task, err := scanTask(r.db.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE task_id = ?`, taskID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrTaskNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get task: %w", err)
	}
	return task, nil
}

func getTaskByIdempotency(ctx context.Context, tx *sql.Tx, principalID string, key string) (*domain.Task, error) {
	return scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns+`
		FROM tasks WHERE sender_principal_id = ? AND idempotency_key = ?`, principalID, key))
}

func scanTask(scanner rowScanner) (*domain.Task, error) {
	var task domain.Task
	var parentTaskID, result, taskError, cancelBy, cancelAt sql.NullString
	if err := scanner.Scan(&task.ID, &task.Version, &task.Status, &task.SenderPrincipalID,
		&task.TargetAgentID, &task.DispatchMode, &parentTaskID, &task.OrganizationID,
		&task.IdempotencyKey, &task.Content, &result, &taskError, &cancelBy, &cancelAt,
		&task.CreatedAt, &task.UpdatedAt); err != nil {
		return nil, err
	}
	if parentTaskID.Valid {
		task.ParentTaskID = &parentTaskID.String
	}
	if result.Valid {
		task.Result = &result.String
	}
	if taskError.Valid {
		task.Error = &taskError.String
	}
	if cancelBy.Valid {
		task.CancelRequestedBy = &cancelBy.String
	}
	if cancelAt.Valid {
		task.CancelRequestedAt = &cancelAt.String
	}
	return &task, nil
}

func (r *Repository) ListTasks(ctx context.Context, targetAgentID string, limit int) ([]domain.Task, error) {
	if limit <= 0 || limit > 1000 {
		return nil, domain.ErrInvalidInput("task limit must be between 1 and 1000")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+taskColumns+`
		FROM tasks WHERE target_agent_id = ? ORDER BY created_at ASC, task_id ASC LIMIT ?`, targetAgentID, limit)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()
	tasks := make([]domain.Task, 0)
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, *task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tasks: %w", err)
	}
	return tasks, nil
}

type CreateMessageResult = domain.CreateMessageResult

func (r *Repository) CreateMessage(
	ctx context.Context,
	expectedTaskVersion int64,
	message *domain.Message,
	mailboxItem *domain.MailboxItem,
	event *domain.JournalEvent,
) (*CreateMessageResult, error) {
	if expectedTaskVersion <= 0 || message == nil || mailboxItem == nil {
		return nil, domain.ErrInvalidInput("expected task version, message, and mailbox item are required")
	}
	now := r.now().UTC()
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	task, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE task_id = ?`, message.TaskID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrTaskNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load task for message: %w", err)
	}
	if task.Version != expectedTaskVersion {
		return nil, domain.ErrStaleVersion
	}
	if task.IsTerminal() {
		return nil, domain.ErrTerminalState
	}
	var nextSequence int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) + 1 FROM messages WHERE task_id = ?`, task.ID).Scan(&nextSequence); err != nil {
		return nil, fmt.Errorf("allocate message sequence: %w", err)
	}
	if message.Version == 0 {
		message.Version = 1
	}
	message.Sequence = nextSequence
	message.TargetAgentID = task.TargetAgentID
	message.CreatedAt = normalizeStringTime(message.CreatedAt, now)
	if err := message.ValidateTarget(); err != nil {
		return nil, err
	}
	if err := insertMessage(ctx, tx, message); err != nil {
		return nil, err
	}
	task.Version++
	task.UpdatedAt = formatTime(now)
	result, err := tx.ExecContext(ctx, `UPDATE tasks SET version = ?, updated_at = ?
		WHERE task_id = ? AND version = ?`, task.Version, task.UpdatedAt, task.ID, expectedTaskVersion)
	if err != nil {
		return nil, fmt.Errorf("advance task version for message: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return nil, domain.ErrStaleVersion
	}
	if err := r.inject(FaultAfterStateWrite); err != nil {
		return nil, err
	}
	mailboxItem.TargetAgentID = task.TargetAgentID
	mailboxItem.TaskID = task.ID
	mailboxItem.MessageID = message.ID
	mailboxItem.Kind = domain.MailboxKindMessage
	if mailboxItem.State == "" {
		mailboxItem.State = domain.MailboxStatePending
	}
	mailboxItem.CreatedAt = normalizeTime(mailboxItem.CreatedAt, now)
	if err := insertMailbox(ctx, tx, mailboxItem); err != nil {
		return nil, err
	}
	if err := r.inject(FaultAfterDelivery); err != nil {
		return nil, err
	}
	if err := validateJournalForAggregate(event, "task", task.ID, now); err != nil {
		return nil, err
	}
	if err := insertJournal(ctx, tx, event); err != nil {
		return nil, err
	}
	if err := r.inject(FaultBeforeCommit); err != nil {
		return nil, err
	}
	if err := commit(tx); err != nil {
		return nil, err
	}
	return &CreateMessageResult{Task: *task, Message: *message, MailboxItem: *mailboxItem, Event: *event}, nil
}

func (r *Repository) ListMessages(ctx context.Context, taskID string) ([]domain.Message, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT message_id, task_id, version, sequence,
		sender_principal_id, target_agent_id, kind, content, created_at
		FROM messages WHERE task_id = ? ORDER BY sequence ASC`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()
	messages := make([]domain.Message, 0)
	for rows.Next() {
		var message domain.Message
		if err := rows.Scan(&message.ID, &message.TaskID, &message.Version, &message.Sequence,
			&message.SenderPrincipalID, &message.TargetAgentID, &message.Kind, &message.Content,
			&message.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}
	return messages, nil
}

type TaskTransition = domain.TaskTransition

func (r *Repository) TransitionTask(ctx context.Context, transition TaskTransition) (*domain.Task, error) {
	if transition.ExpectedVersion <= 0 || len(transition.AllowedFrom) == 0 || !transition.To.Valid() {
		return nil, domain.ErrInvalidInput("task transition requires version, allowed source states, and valid target state")
	}
	now := r.now().UTC()
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	task, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE task_id = ?`, transition.TaskID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	if task.Version != transition.ExpectedVersion {
		return nil, domain.ErrStaleVersion
	}
	allowed := false
	for _, status := range transition.AllowedFrom {
		if !status.Valid() {
			return nil, domain.ErrInvalidInput("task transition contains unknown source state")
		}
		if task.Status == status {
			allowed = true
		}
	}
	if !allowed {
		return nil, domain.ErrInvalidTransition
	}
	task.Version++
	task.Status = transition.To
	task.Result = transition.Result
	task.Error = transition.Error
	task.CancelRequestedBy = transition.CancelRequestedBy
	task.CancelRequestedAt = transition.CancelRequestedAt
	task.UpdatedAt = formatTime(now)
	result, err := tx.ExecContext(ctx, `UPDATE tasks SET version = ?, status = ?, result = ?, error = ?,
		cancel_requested_by = ?, cancel_requested_at = ?, updated_at = ?
		WHERE task_id = ? AND version = ?`, task.Version, task.Status, nullableString(task.Result),
		nullableString(task.Error), nullableString(task.CancelRequestedBy), nullableString(task.CancelRequestedAt),
		task.UpdatedAt, task.ID, transition.ExpectedVersion)
	if err != nil {
		return nil, fmt.Errorf("transition task: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return nil, domain.ErrStaleVersion
	}
	if err := r.inject(FaultAfterStateWrite); err != nil {
		return nil, err
	}
	if err := validateJournalForAggregate(transition.Event, "task", task.ID, now); err != nil {
		return nil, err
	}
	if err := insertJournal(ctx, tx, transition.Event); err != nil {
		return nil, err
	}
	if err := r.inject(FaultBeforeCommit); err != nil {
		return nil, err
	}
	if err := commit(tx); err != nil {
		return nil, err
	}
	return task, nil
}
