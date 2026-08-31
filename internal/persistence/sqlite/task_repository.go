package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
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
	query := `SELECT ` + taskColumns + ` FROM tasks `
	args := []any{limit}
	if targetAgentID == "" {
		query += `ORDER BY created_at ASC, task_id ASC LIMIT ?`
	} else {
		query += `WHERE target_agent_id = ? ORDER BY created_at ASC, task_id ASC LIMIT ?`
		args = []any{targetAgentID, limit}
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
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
	// The target Agent is an immutable Task-owned routing field. Public command
	// callers intentionally omit it; normalize it before replay comparison so
	// the same idempotent request is not mistaken for a conflicting payload.
	message.TargetAgentID = task.TargetAgentID
	// Command Service derives the Message ID from its idempotency key.  Check
	// that identity before the Task CAS so a lost response can be replayed even
	// though the original command already advanced the Task version.
	if replay, replayErr := loadMessageReplay(ctx, tx, task, message); replayErr != nil {
		return nil, replayErr
	} else if replay != nil {
		return replay, nil
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
		// A concurrent writer (or a pre-existing malformed row) may win the
		// deterministic message_id unique key between the replay probe and
		// INSERT. Resolve that race to the same replay/conflict contract rather
		// than exposing SQLite's 500-level unique-constraint error.
		if isUniqueConstraint(err, "messages.message_id") || isUniqueConstraint(err, "messages") {
			if replay, replayErr := loadMessageReplay(ctx, tx, task, message); replayErr != nil {
				return nil, replayErr
			} else if replay != nil {
				return replay, nil
			}
			return nil, domain.ErrIdempotencyConflict
		}
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
	if mailboxItem.Lane == "" {
		if err := routeMessageMailbox(ctx, tx, task.TargetAgentID, now, mailboxItem); err != nil {
			return nil, err
		}
	}
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

// loadMessageReplay resolves the deterministic Message identity inside the
// caller's transaction. A matching row is replayable only when its immutable
// payload and mailbox association are intact; any mismatch is an idempotency
// conflict. A missing row returns (nil, nil).
func loadMessageReplay(ctx context.Context, tx *sql.Tx, task *domain.Task, message *domain.Message) (*CreateMessageResult, error) {
	var existing domain.Message
	var createdAt string
	err := tx.QueryRowContext(ctx, `SELECT message_id, task_id, version, sequence,
		sender_principal_id, target_agent_id, kind, content, created_at
		FROM messages WHERE message_id=?`, message.ID).Scan(&existing.ID, &existing.TaskID,
		&existing.Version, &existing.Sequence, &existing.SenderPrincipalID, &existing.TargetAgentID,
		&existing.Kind, &existing.Content, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("check message idempotency: %w", err)
	}
	if existing.TaskID != task.ID || existing.SenderPrincipalID != message.SenderPrincipalID ||
		existing.TargetAgentID != task.TargetAgentID || existing.TargetAgentID != message.TargetAgentID ||
		existing.Kind != message.Kind || existing.Content != message.Content {
		return nil, domain.ErrIdempotencyConflict
	}
	if _, err := parseTime(createdAt); err != nil {
		return nil, domain.ErrIdempotencyConflict
	}
	existing.CreatedAt = createdAt
	existingMailbox, mailboxErr := scanMailbox(tx.QueryRowContext(ctx, `SELECT `+mailboxColumns+`
		FROM mailbox_items WHERE message_id=? AND task_id=? ORDER BY sequence ASC LIMIT 1`, existing.ID, task.ID))
	if errors.Is(mailboxErr, sql.ErrNoRows) {
		return nil, domain.ErrIdempotencyConflict
	}
	if mailboxErr != nil {
		return nil, fmt.Errorf("load idempotent message mailbox: %w", mailboxErr)
	}
	if existingMailbox.TargetAgentID != task.TargetAgentID || existingMailbox.TaskID != task.ID ||
		existingMailbox.MessageID != existing.ID || existingMailbox.Kind != domain.MailboxKindMessage {
		return nil, domain.ErrIdempotencyConflict
	}
	return &CreateMessageResult{Task: *task, Message: existing, MailboxItem: *existingMailbox}, nil
}

// routeMessageMailbox determines the sole legal delivery route for a public
// Message command.  An active run pins the decision to its registered backend;
// otherwise a currently viable backend must explicitly support a deferred
// follow-up.  Callers that construct an internal mailbox item with a lane keep
// their explicit route for migration/test plumbing.
func routeMessageMailbox(ctx context.Context, tx *sql.Tx, agentID string, now time.Time, item *domain.MailboxItem) error {
	var runID, workerID, backendID string
	var runVersion int64
	err := tx.QueryRowContext(ctx, `SELECT run_id, worker_instance_id, backend_id, version
		FROM run_attempts WHERE agent_id=? AND task_id=? AND status IN ('starting','running','waiting_approval','finishing')
		ORDER BY started_at DESC LIMIT 1`, agentID, item.TaskID).Scan(&runID, &workerID, &backendID, &runVersion)
	if err == nil {
		descriptor, health, err := loadBackendDescriptor(ctx, tx, workerID, backendID)
		if err != nil {
			return err
		}
		if health == openruntime.BackendUnavailable {
			return domain.ErrUnsupportedCapability
		}
		switch descriptor.Steer {
		case openruntime.SteerNative:
			item.Lane = domain.MailboxLaneControl
			item.TargetRunID = runID
			item.ExpectedRunVersion = runVersion
			return nil
		case openruntime.SteerQueued:
			item.Lane = domain.MailboxLaneWork
			return nil
		default:
			return domain.ErrUnsupportedCapability
		}
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("find active RunAttempt for Message: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `SELECT registration.backend_id, registration.worker_instance_id,
		registration.descriptor_json, registration.health
		FROM runtime_backend_registrations AS registration
		JOIN worker_instances AS worker ON worker.worker_instance_id=registration.worker_instance_id
		WHERE worker.agent_id=? AND worker.status IN ('online','degraded') AND worker.lease_until>?
		ORDER BY registration.backend_id ASC`, agentID, formatTime(now))
	if err != nil {
		return fmt.Errorf("list effective Message Backends: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var backendID, workerID, raw string
		var health openruntime.BackendHealth
		if err := rows.Scan(&backendID, &workerID, &raw, &health); err != nil {
			return fmt.Errorf("scan effective Message Backend: %w", err)
		}
		var descriptor openruntime.AdapterDescriptor
		if err := json.Unmarshal([]byte(raw), &descriptor); err != nil {
			return fmt.Errorf("decode effective Message Backend descriptor: %w", domain.ErrUnsupportedCapability)
		}
		if err := descriptor.Validate(); err != nil || !health.Valid() {
			return fmt.Errorf("invalid effective Message Backend registration: %w", domain.ErrUnsupportedCapability)
		}
		if health != openruntime.BackendHealthy && health != openruntime.BackendDegraded {
			continue
		}
		bindingActive, bindingErr := descriptorHasActiveBinding(ctx, tx, item.TaskID, agentID, backendID)
		if bindingErr != nil {
			return bindingErr
		}
		if (descriptor.Steer == openruntime.SteerNative || descriptor.Steer == openruntime.SteerQueued) &&
			(len(descriptor.Models) > 0 && len(descriptor.ReasoningModes) > 0) &&
			((descriptorSupportsNew(descriptor)) || (descriptorSupportsResume(descriptor) && bindingActive)) {
			item.Lane = domain.MailboxLaneWork
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate effective Message Backends: %w", err)
	}
	return domain.ErrUnsupportedCapability
}

func descriptorSupportsNew(descriptor openruntime.AdapterDescriptor) bool {
	for _, mode := range descriptor.SessionModes {
		if mode == domain.SessionModeNew {
			return true
		}
	}
	return false
}

func descriptorSupportsResume(descriptor openruntime.AdapterDescriptor) bool {
	for _, mode := range descriptor.SessionModes {
		if mode == domain.SessionModeResume {
			return true
		}
	}
	return false
}

// descriptorHasActiveBinding is deliberately fail-closed: a malformed
// persisted binding is not treated as a resumable session.
func descriptorHasActiveBinding(ctx context.Context, tx *sql.Tx, contextID, agentID, backendID string) (bool, error) {
	var id, provider, state, createdAt, updatedAt string
	var version int64
	err := tx.QueryRowContext(ctx, `SELECT session_binding_id, provider_session_id, state, version, created_at, updated_at
		FROM session_bindings WHERE context_id=? AND agent_id=? AND backend_id=?`, contextID, agentID, backendID).
		Scan(&id, &provider, &state, &version, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect Message SessionBinding: %w", domain.ErrUnsupportedCapability)
	}
	if state != string(domain.SessionBindingActive) {
		if domain.SessionBindingState(state).Valid() {
			return false, nil
		}
		return false, fmt.Errorf("invalid Message SessionBinding state: %w", domain.ErrUnsupportedCapability)
	}
	if provider == "" || version <= 0 {
		return false, fmt.Errorf("invalid active Message SessionBinding: %w", domain.ErrUnsupportedCapability)
	}
	created, err := parseTime(createdAt)
	if err != nil {
		return false, fmt.Errorf("invalid active Message SessionBinding: %w", domain.ErrUnsupportedCapability)
	}
	updated, err := parseTime(updatedAt)
	if err != nil {
		return false, fmt.Errorf("invalid active Message SessionBinding: %w", domain.ErrUnsupportedCapability)
	}
	binding := domain.SessionBinding{ID: id, ContextID: contextID, AgentID: agentID, BackendID: backendID,
		ProviderSessionID: provider, State: domain.SessionBindingState(state), Version: version, CreatedAt: created, UpdatedAt: updated}
	if err := binding.Validate(); err != nil {
		return false, fmt.Errorf("invalid active Message SessionBinding: %w", domain.ErrUnsupportedCapability)
	}
	return true, nil
}

func loadBackendDescriptor(ctx context.Context, tx *sql.Tx, workerID, backendID string) (openruntime.AdapterDescriptor, openruntime.BackendHealth, error) {
	var raw string
	var health openruntime.BackendHealth
	err := tx.QueryRowContext(ctx, `SELECT descriptor_json, health FROM runtime_backend_registrations
		WHERE worker_instance_id=? AND backend_id=?`, workerID, backendID).Scan(&raw, &health)
	if errors.Is(err, sql.ErrNoRows) {
		return openruntime.AdapterDescriptor{}, "", domain.ErrUnsupportedCapability
	}
	if err != nil {
		return openruntime.AdapterDescriptor{}, "", fmt.Errorf("load active RunAttempt Backend descriptor: %w", err)
	}
	var descriptor openruntime.AdapterDescriptor
	if err := json.Unmarshal([]byte(raw), &descriptor); err != nil {
		return openruntime.AdapterDescriptor{}, "", fmt.Errorf("decode active RunAttempt Backend descriptor: %w", domain.ErrUnsupportedCapability)
	}
	if err := descriptor.Validate(); err != nil || !health.Valid() {
		return openruntime.AdapterDescriptor{}, "", fmt.Errorf("invalid active RunAttempt Backend registration: %w", domain.ErrUnsupportedCapability)
	}
	return descriptor, health, nil
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

func (r *Repository) GetMessage(ctx context.Context, messageID string) (*domain.Message, error) {
	var message domain.Message
	err := r.db.QueryRowContext(ctx, `SELECT message_id, task_id, version, sequence,
		sender_principal_id, target_agent_id, kind, content, created_at
		FROM messages WHERE message_id=?`, messageID).Scan(&message.ID, &message.TaskID, &message.Version,
		&message.Sequence, &message.SenderPrincipalID, &message.TargetAgentID, &message.Kind,
		&message.Content, &message.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get Message: %w", err)
	}
	return &message, nil
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
