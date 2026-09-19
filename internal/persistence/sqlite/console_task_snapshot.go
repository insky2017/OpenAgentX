package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"openagentx/internal/domain"
)

func (r *Repository) ListConsoleTasks(
	ctx context.Context,
	agentID string,
	cursor domain.ConsoleTaskCursor,
	limit int,
) ([]domain.Task, error) {
	if err := domain.ValidateIdentifier("agent_id", agentID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 101 {
		return nil, domain.ErrInvalidInput("Console Task limit must be between 1 and 101")
	}
	if cursor.UpdatedAt.IsZero() != (cursor.TaskID == "") {
		return nil, domain.ErrInvalidInput("Console Task cursor timestamp and id are both required")
	}
	if cursor.TaskID != "" {
		if err := domain.ValidateOpaqueID("cursor task_id", cursor.TaskID); err != nil {
			return nil, err
		}
	}
	var exists int
	if err := r.db.QueryRowContext(ctx, `SELECT 1 FROM agents WHERE agent_id=?`, agentID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrAgentNotFound
	} else if err != nil {
		return nil, fmt.Errorf("read Console Task Agent: %w", err)
	}

	query := `SELECT ` + taskColumns + ` FROM tasks WHERE target_agent_id=?`
	args := []any{agentID}
	if cursor.TaskID != "" {
		query += ` AND (` + taskUpdatedTimeValue + ` < ` + sqliteTimeKeyFunction + `(?) OR (` +
			taskUpdatedTimeValue + ` = ` + sqliteTimeKeyFunction + `(?) AND task_id < ?))`
		formatted := formatTime(cursor.UpdatedAt)
		args = append(args, formatted, formatted, cursor.TaskID)
	}
	query += ` ORDER BY ` + taskUpdatedTimeValue + ` DESC, task_id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list Console Tasks: %w", err)
	}
	defer rows.Close()
	tasks := make([]domain.Task, 0, limit)
	for rows.Next() {
		task, scanErr := scanTask(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan Console Task: %w", scanErr)
		}
		tasks = append(tasks, *task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Console Tasks: %w", err)
	}
	return tasks, nil
}

func (r *Repository) ConsoleTaskSnapshot(ctx context.Context, agentID, taskID string) (domain.ConsoleTaskSnapshot, error) {
	return r.consoleTaskSnapshot(ctx, agentID, taskID, nil)
}

// afterStateRead is test-only synchronization used to prove that state rows
// and the Journal high-water share one SQLite read snapshot.
func (r *Repository) consoleTaskSnapshot(
	ctx context.Context,
	agentID, taskID string,
	afterStateRead func(),
) (domain.ConsoleTaskSnapshot, error) {
	var snapshot domain.ConsoleTaskSnapshot
	if err := domain.ValidateIdentifier("agent_id", agentID); err != nil {
		return snapshot, err
	}
	if err := domain.ValidateOpaqueID("task_id", taskID); err != nil {
		return snapshot, err
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return snapshot, fmt.Errorf("begin Console Task snapshot transaction: %w", err)
	}
	defer tx.Rollback()

	task, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns+`
		FROM tasks WHERE task_id=? AND target_agent_id=?`, taskID, agentID))
	if errors.Is(err, sql.ErrNoRows) {
		return snapshot, domain.ErrNotFound
	}
	if err != nil {
		return snapshot, fmt.Errorf("read Console Task snapshot: %w", err)
	}
	snapshot.Task = *task

	var workDeliveries int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM mailbox_items
		WHERE task_id=? AND target_agent_id=? AND kind='task' AND lane='work'`, taskID, agentID).Scan(&workDeliveries); err != nil {
		return snapshot, fmt.Errorf("count Console Task work deliveries: %w", err)
	}
	if workDeliveries > 1 {
		return snapshot, fmt.Errorf("Console Task has multiple original work deliveries")
	}
	if workDeliveries == 1 {
		snapshot.WorkDelivery, err = scanMailbox(tx.QueryRowContext(ctx, `SELECT `+mailboxColumns+`
			FROM mailbox_items WHERE task_id=? AND target_agent_id=? AND kind='task' AND lane='work'
			ORDER BY sequence ASC, mailbox_item_id ASC LIMIT 1`, taskID, agentID))
		if err != nil {
			return snapshot, fmt.Errorf("read Console Task work delivery: %w", err)
		}
	}

	snapshot.LatestRun, err = scanRunAttempt(tx.QueryRowContext(ctx, `SELECT run_id, task_id,
		agent_id, version, status, worker_instance_id, fencing_token, lease_until,
		execution_spec_version, requested_execution_json, resolved_execution_json, adapter_id,
		backend_id, model, reasoning_mode, reasoning_value, started_at, finished_at, result_json,
		created_at, updated_at FROM run_attempts WHERE task_id=? AND agent_id=?
		ORDER BY `+sqliteTimeKeyFunction+`(started_at) DESC, run_id DESC LIMIT 1`, taskID, agentID))
	if errors.Is(err, domain.ErrNotFound) {
		snapshot.LatestRun = nil
	} else if err != nil {
		return snapshot, fmt.Errorf("read Console Task latest RunAttempt: %w", err)
	}
	if snapshot.LatestRun != nil {
		if err := tx.QueryRowContext(ctx, `SELECT generation FROM worker_instances
			WHERE worker_instance_id=? AND agent_id=?`, snapshot.LatestRun.WorkerInstanceID, agentID).
			Scan(&snapshot.LatestRunWorkerGeneration); err != nil {
			return snapshot, fmt.Errorf("read Console Task Run Worker generation: %w", err)
		}
	}

	snapshot.LatestMessage, err = scanConsoleMessage(tx.QueryRowContext(ctx, `SELECT message_id,
		task_id, version, sequence, sender_principal_id, target_agent_id, kind, content, created_at
		FROM messages WHERE task_id=? AND target_agent_id=?
		ORDER BY sequence DESC, version DESC, message_id DESC LIMIT 1`, taskID, agentID))
	if errors.Is(err, sql.ErrNoRows) {
		snapshot.LatestMessage = nil
	} else if err != nil {
		return snapshot, fmt.Errorf("read Console Task latest Message: %w", err)
	}

	snapshot.PendingApproval, err = scanApprovalRequest(tx.QueryRowContext(ctx, `SELECT `+approvalRequestColumns+`
		FROM approval_requests WHERE task_id=? AND state='pending'
		ORDER BY `+sqliteTimeKeyFunction+`(created_at) DESC, approval_request_id DESC LIMIT 1`, taskID))
	if errors.Is(err, sql.ErrNoRows) {
		snapshot.PendingApproval = nil
	} else if err != nil {
		return snapshot, fmt.Errorf("read Console Task pending Approval: %w", err)
	}

	if afterStateRead != nil {
		afterStateRead()
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) FROM event_journal`).
		Scan(&snapshot.SnapshotSequence); err != nil {
		return snapshot, fmt.Errorf("read Console Task snapshot sequence: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return snapshot, fmt.Errorf("commit Console Task snapshot transaction: %w", err)
	}
	return snapshot, nil
}

func scanConsoleMessage(scanner rowScanner) (*domain.Message, error) {
	var message domain.Message
	if err := scanner.Scan(&message.ID, &message.TaskID, &message.Version, &message.Sequence,
		&message.SenderPrincipalID, &message.TargetAgentID, &message.Kind, &message.Content,
		&message.CreatedAt); err != nil {
		return nil, err
	}
	return &message, nil
}

func latestSuggestedConsoleTask(ctx context.Context, tx *sql.Tx, agentID string) (*domain.Task, error) {
	task, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks
		WHERE target_agent_id=? AND status NOT IN ('succeeded','failed','canceled','uncertain')
		ORDER BY `+taskUpdatedTimeValue+` DESC, task_id DESC LIMIT 1`, agentID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read suggested Console Task: %w", err)
	}
	return task, nil
}
