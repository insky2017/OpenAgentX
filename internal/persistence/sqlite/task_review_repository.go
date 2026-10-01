package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"openagentx/internal/domain"
)

// Reviews are small immutable records in the existing transactional journal.
// A new schema or a second task lifecycle is unnecessary.
func (r *Repository) ReviewTaskResult(ctx context.Context, expectedVersion int64, review domain.TaskReview) (*domain.TaskReview, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var payload string
	var sequence int64
	err = tx.QueryRowContext(ctx, `SELECT payload_json, sequence FROM event_journal WHERE event_id=? AND event_type='task.result_reviewed'`, review.ID).Scan(&payload, &sequence)
	if err == nil {
		var old domain.TaskReview
		if err := json.Unmarshal([]byte(payload), &old); err != nil {
			return nil, err
		}
		if old.TaskID != review.TaskID || old.RunID != review.RunID || old.RunVersion != review.RunVersion || old.Decision != review.Decision || old.Note != review.Note || old.ReviewedBy != review.ReviewedBy {
			return nil, domain.ErrIdempotencyConflict
		}
		old.Sequence = sequence
		return &old, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	task, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE task_id=?`, review.TaskID))
	if err != nil {
		return nil, err
	}
	if task.Version != expectedVersion {
		return nil, domain.ErrStaleVersion
	}
	if !task.IsTerminal() {
		return nil, domain.ErrInvalidInput("wait for execution to finish before reviewing its result")
	}
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM run_attempts WHERE task_id=? AND status IN ('starting','running','waiting_approval','finishing')`, task.ID).Scan(&active); err != nil {
		return nil, err
	}
	if active != 0 {
		return nil, domain.ErrInvalidInput("execution has not stopped")
	}
	var run domain.RunAttempt
	var finished sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT run_id, version, status, result_json, finished_at FROM run_attempts WHERE task_id=? ORDER BY started_at DESC, run_id DESC LIMIT 1`, task.ID).Scan(&run.ID, &run.Version, &run.Status, &run.ResultJSON, &finished)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrInvalidInput("this task has no completed result to review")
	}
	if err != nil {
		return nil, err
	}
	if run.ID != review.RunID || run.Version != review.RunVersion {
		return nil, domain.ErrStaleVersion
	}
	if run.Status != domain.RunAttemptSucceeded || !finished.Valid {
		return nil, domain.ErrInvalidInput("only a confirmed completed execution can be reviewed; unresolved stopping remains unconfirmed")
	}
	if review.Decision != "accepted" && review.Decision != "rejected" {
		return nil, domain.ErrInvalidInput("review decision must be accepted or rejected")
	}
	digest := sha256.Sum256([]byte(run.ResultJSON))
	review.ResultSHA256 = hex.EncodeToString(digest[:])
	review.CreatedAt = r.now().UTC()
	review.TaskVersion = task.Version + 1
	body, err := json.Marshal(review)
	if err != nil {
		return nil, err
	}
	event := &domain.JournalEvent{ID: review.ID, OrganizationID: task.OrganizationID, AggregateType: "task", AggregateID: task.ID, EventType: "task.result_reviewed", ActorPrincipalID: review.ReviewedBy, Payload: body, CreatedAt: review.CreatedAt}
	result, err := tx.ExecContext(ctx, `UPDATE tasks SET version=version+1, updated_at=? WHERE task_id=? AND version=?`, formatTime(review.CreatedAt), task.ID, expectedVersion)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return nil, domain.ErrStaleVersion
	}
	if err := r.inject(FaultAfterStateWrite); err != nil {
		return nil, err
	}
	if err := insertJournal(ctx, tx, event); err != nil {
		return nil, err
	}
	if err := r.inject(FaultBeforeCommit); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit result review: %w", err)
	}
	review.Sequence = event.Sequence
	return &review, nil
}

func (r *Repository) GetTaskReview(ctx context.Context, taskID string) (*domain.TaskReview, error) {
	var payload string
	var sequence int64
	err := r.db.QueryRowContext(ctx, `SELECT payload_json, sequence FROM event_journal WHERE aggregate_type='task' AND aggregate_id=? AND event_type='task.result_reviewed' ORDER BY sequence DESC LIMIT 1`, taskID).Scan(&payload, &sequence)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var review domain.TaskReview
	if err := json.Unmarshal([]byte(payload), &review); err != nil {
		return nil, err
	}
	review.Sequence = sequence
	return &review, nil
}
