package sqlite

import (
	"context"
	"fmt"
	"strings"
	"time"

	"openagentx/internal/domain"
)

const taskUpdatedTimeValue = sqliteTimeKeyFunction + "(updated_at)"

// QueryTasks provides the stable keyset order used by the command center.
// The task ID tie-breaker keeps pagination deterministic when timestamps match.
func (r *Repository) QueryTasks(
	ctx context.Context,
	targetAgentID string,
	status domain.TaskStatus,
	updatedAfter, updatedBefore time.Time,
	text, cursorUpdatedAt, cursorTaskID string,
	limit int,
) ([]domain.Task, error) {
	if limit <= 0 || limit > 101 {
		return nil, domain.ErrInvalidInput("task query limit must be between 1 and 101")
	}
	if status != "" && !status.Valid() {
		return nil, domain.ErrInvalidInput("invalid task status")
	}
	if (cursorUpdatedAt == "") != (cursorTaskID == "") {
		return nil, domain.ErrInvalidInput("task cursor timestamp and id are both required")
	}
	where := make([]string, 0, 6)
	args := make([]any, 0, 8)
	if targetAgentID != "" {
		where = append(where, "target_agent_id = ?")
		args = append(args, targetAgentID)
	}
	if status != "" {
		where = append(where, "status = ?")
		args = append(args, status)
	}
	if !updatedAfter.IsZero() {
		where = append(where, taskUpdatedTimeValue+" >= "+sqliteTimeKeyFunction+"(?)")
		args = append(args, formatTime(updatedAfter.UTC()))
	}
	if !updatedBefore.IsZero() {
		where = append(where, taskUpdatedTimeValue+" < "+sqliteTimeKeyFunction+"(?)")
		args = append(args, formatTime(updatedBefore.UTC()))
	}
	if text = strings.TrimSpace(text); text != "" {
		where = append(where, "instr(lower(task_id || ' ' || target_agent_id || ' ' || content), lower(?)) > 0")
		args = append(args, text)
	}
	if cursorUpdatedAt != "" {
		where = append(where, "("+taskUpdatedTimeValue+" < "+sqliteTimeKeyFunction+"(?) OR ("+taskUpdatedTimeValue+" = "+sqliteTimeKeyFunction+"(?) AND task_id < ?))")
		args = append(args, cursorUpdatedAt, cursorUpdatedAt, cursorTaskID)
	}
	query := `SELECT ` + taskColumns + ` FROM tasks`
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, " AND ")
	}
	query += ` ORDER BY ` + taskUpdatedTimeValue + ` DESC, task_id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query tasks: %w", err)
	}
	defer rows.Close()
	tasks := make([]domain.Task, 0, limit)
	for rows.Next() {
		task, scanErr := scanTask(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan task query: %w", scanErr)
		}
		tasks = append(tasks, *task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate task query: %w", err)
	}
	return tasks, nil
}

// ListTaskJournalBefore returns the newest events below beforeSequence in
// chronological order. beforeSequence must be positive and is exclusive.
func (r *Repository) ListTaskJournalBefore(ctx context.Context, taskID string, beforeSequence int64, limit int) ([]domain.JournalEvent, error) {
	if taskID == "" {
		return nil, domain.ErrInvalidInput("task id is required")
	}
	if beforeSequence <= 0 {
		return nil, domain.ErrInvalidInput("before_sequence must be positive")
	}
	if limit <= 0 || limit > 501 {
		return nil, domain.ErrInvalidInput("task journal limit must be between 1 and 501")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT e.sequence, e.event_id, e.organization_id,
		e.aggregate_type, e.aggregate_id, e.event_type, e.actor_principal_id,
		e.payload_json, e.created_at
		FROM event_journal e
		WHERE e.sequence < ? AND (
			(e.aggregate_type = 'task' AND e.aggregate_id = ?) OR
			(e.aggregate_type = 'run_attempt' AND EXISTS (
				SELECT 1 FROM run_attempts r WHERE r.run_id = e.aggregate_id AND r.task_id = ?
			))
		)
		ORDER BY e.sequence DESC LIMIT ?`, beforeSequence, taskID, taskID, limit)
	if err != nil {
		return nil, fmt.Errorf("list task journal history: %w", err)
	}
	defer rows.Close()
	events, err := scanJournalRows(rows)
	if err != nil {
		return nil, err
	}
	for left, right := 0, len(events)-1; left < right; left, right = left+1, right-1 {
		events[left], events[right] = events[right], events[left]
	}
	return events, nil
}

// ListTaskJournalRange returns a forward page bounded by an already observed
// Journal watermark. This prevents a concurrent event from being returned
// beyond the cursor the response claims to have covered.
func (r *Repository) ListTaskJournalRange(ctx context.Context, taskID string, afterSequence, throughSequence int64, limit int) ([]domain.JournalEvent, error) {
	if taskID == "" {
		return nil, domain.ErrInvalidInput("task id is required")
	}
	if afterSequence < 0 || throughSequence < afterSequence {
		return nil, domain.ErrInvalidInput("invalid task journal range")
	}
	if limit <= 0 || limit > 500 {
		return nil, domain.ErrInvalidInput("task journal range limit must be between 1 and 500")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT e.sequence, e.event_id, e.organization_id,
		e.aggregate_type, e.aggregate_id, e.event_type, e.actor_principal_id,
		e.payload_json, e.created_at
		FROM event_journal e
		WHERE e.sequence > ? AND e.sequence <= ? AND (
			(e.aggregate_type = 'task' AND e.aggregate_id = ?) OR
			(e.aggregate_type = 'run_attempt' AND EXISTS (
				SELECT 1 FROM run_attempts r WHERE r.run_id = e.aggregate_id AND r.task_id = ?
			))
		)
		ORDER BY e.sequence ASC LIMIT ?`, afterSequence, throughSequence, taskID, taskID, limit)
	if err != nil {
		return nil, fmt.Errorf("list task journal range: %w", err)
	}
	defer rows.Close()
	return scanJournalRows(rows)
}
