package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"openagentx/internal/domain"
)

const approvalRequestColumns = `approval_request_id, task_id, mode, target_run_id, expected_run_version,
	scope_digest, state, expires_at, created_at`

func scanApprovalRequest(scanner rowScanner) (*domain.ApprovalRequest, error) {
	var request domain.ApprovalRequest
	var targetRun sql.NullString
	var expected sql.NullInt64
	var expires, created string
	if err := scanner.Scan(&request.ID, &request.TaskID, &request.Mode, &targetRun, &expected,
		&request.ScopeDigest, &request.State, &expires, &created); err != nil {
		return nil, err
	}
	request.TargetRunID = targetRun.String
	request.ExpectedRunVersion = expected.Int64
	var err error
	if request.ExpiresAt, err = parseTime(expires); err != nil {
		return nil, err
	}
	if request.CreatedAt, err = parseTime(created); err != nil {
		return nil, err
	}
	return &request, nil
}

func (r *Repository) CreateApprovalRequest(ctx context.Context, request *domain.ApprovalRequest, event *domain.JournalEvent) error {
	if request == nil {
		return domain.ErrInvalidInput("approval request is required")
	}
	now := r.now().UTC()
	request.CreatedAt = normalizeTime(request.CreatedAt, now)
	if request.State == "" {
		request.State = domain.ApprovalRequestPending
	}
	if err := request.Validate(); err != nil {
		return err
	}
	if event == nil {
		return domain.ErrInvalidInput("approval event is required")
	}
	if err := validateJournalForAggregate(event, "approval_request", request.ID, now); err != nil {
		return err
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO approval_requests (
		approval_request_id, task_id, mode, target_run_id, expected_run_version, scope_digest, state, expires_at, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, request.ID, request.TaskID, request.Mode,
		nullableText(request.TargetRunID), nullablePositive(request.ExpectedRunVersion), request.ScopeDigest,
		request.State, formatTime(request.ExpiresAt), formatTime(request.CreatedAt))
	if err != nil {
		return fmt.Errorf("create approval request: %w", err)
	}
	if err := insertJournal(ctx, tx, event); err != nil {
		return err
	}
	return commit(tx)
}

func (r *Repository) GetApprovalRequest(ctx context.Context, id string) (*domain.ApprovalRequest, error) {
	request, err := scanApprovalRequest(r.db.QueryRowContext(ctx, `SELECT `+approvalRequestColumns+` FROM approval_requests WHERE approval_request_id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get approval request: %w", err)
	}
	return request, nil
}

// RequestTaskCancel is the Task-level cancellation linearization point. The
// optional control item is only a best-effort interrupt for the active run.
func (r *Repository) RequestTaskCancel(ctx context.Context, taskID string, expectedVersion int64, requestedBy string,
	item *domain.MailboxItem, taskEvent *domain.JournalEvent, mailboxEvent *domain.JournalEvent) (*domain.Task, *domain.MailboxItem, error) {
	if expectedVersion <= 0 {
		return nil, nil, domain.ErrInvalidInput("expected task version is required")
	}
	now := r.now().UTC()
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	task, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE task_id=?`, taskID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, domain.ErrTaskNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	if task.Status == domain.TaskStatusCancelRequested {
		if task.CancelRequestedBy != nil && *task.CancelRequestedBy == requestedBy {
			return task, nil, nil
		}
		return nil, nil, domain.ErrConflict("task cancellation already requested by another principal")
	}
	if task.Version != expectedVersion {
		return nil, nil, domain.ErrStaleVersion
	}
	if task.IsTerminal() {
		return nil, nil, domain.ErrTerminalState
	}
	if taskEvent == nil {
		return nil, nil, domain.ErrInvalidInput("cancel task event is required")
	}
	task.Version++
	task.Status = domain.TaskStatusCancelRequested
	task.CancelRequestedBy = &requestedBy
	at := formatTime(now)
	task.CancelRequestedAt = &at
	task.UpdatedAt = at
	result, err := tx.ExecContext(ctx, `UPDATE tasks SET version=?, status=?, cancel_requested_by=?, cancel_requested_at=?, updated_at=? WHERE task_id=? AND version=?`,
		task.Version, task.Status, requestedBy, at, at, task.ID, expectedVersion)
	if err != nil {
		return nil, nil, fmt.Errorf("request task cancel: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return nil, nil, domain.ErrStaleVersion
	}
	if err := validateJournalForAggregate(taskEvent, "task", task.ID, now); err != nil {
		return nil, nil, err
	}
	if err := insertJournal(ctx, tx, taskEvent); err != nil {
		return nil, nil, err
	}
	var active *domain.RunAttempt
	active, err = scanRunAttempt(tx.QueryRowContext(ctx, `SELECT run_id, task_id, agent_id, version, status, worker_instance_id, fencing_token, lease_until,
		execution_spec_version, requested_execution_json, resolved_execution_json, adapter_id, backend_id, model, reasoning_mode, reasoning_value,
		started_at, finished_at, result_json, created_at, updated_at FROM run_attempts WHERE task_id=? AND status IN ('starting','running','waiting_approval','finishing') LIMIT 1`, task.ID))
	if err == nil && item != nil {
		item.TargetAgentID = task.TargetAgentID
		item.TaskID = task.ID
		item.Kind = domain.MailboxKindCancel
		item.Lane = domain.MailboxLaneControl
		item.TargetRunID = active.ID
		item.ExpectedRunVersion = active.Version
		item.State = domain.MailboxStatePending
		item.CreatedAt = now
		if err := item.ValidateForInsert(); err != nil {
			return nil, nil, err
		}
		if mailboxEvent == nil {
			return nil, nil, domain.ErrInvalidInput("cancel mailbox event is required")
		}
		if err := insertMailbox(ctx, tx, item); err != nil {
			return nil, nil, err
		}
		if err := validateJournalForAggregate(mailboxEvent, "mailbox_item", item.ID, now); err != nil {
			return nil, nil, err
		}
		if err := insertJournal(ctx, tx, mailboxEvent); err != nil {
			return nil, nil, err
		}
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, fmt.Errorf("find active run for cancel: %w", err)
	}
	if err := commit(tx); err != nil {
		return nil, nil, err
	}
	return task, item, nil
}

func (r *Repository) DecideApproval(ctx context.Context, requestID string, decision *domain.ApprovalDecision, item *domain.MailboxItem,
	decisionEvent *domain.JournalEvent, mailboxEvent *domain.JournalEvent) (*domain.ApprovalDecision, *domain.MailboxItem, error) {
	if decision == nil {
		return nil, nil, domain.ErrInvalidInput("approval decision is required")
	}
	if decision.State == "" {
		decision.State = domain.ApprovalDecisionPersisted
	}
	if err := decision.Validate(); err != nil {
		return nil, nil, err
	}
	now := r.now().UTC()
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	request, err := scanApprovalRequest(tx.QueryRowContext(ctx, `SELECT `+approvalRequestColumns+` FROM approval_requests WHERE approval_request_id=?`, requestID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	if decision.ApprovalRequestID != request.ID {
		return nil, nil, domain.ErrInvalidInput("decision request mismatch")
	}
	var existing domain.ApprovalDecision
	err = tx.QueryRowContext(ctx, `SELECT approval_decision_id, approval_request_id, decided_by, decision, state, idempotency_key, created_at FROM approval_decisions WHERE decided_by=? AND idempotency_key=?`, decision.DecidedBy, decision.IdempotencyKey).Scan(&existing.ID, &existing.ApprovalRequestID, &existing.DecidedBy, &existing.Decision, &existing.State, &existing.IdempotencyKey, new(string))
	if err == nil {
		if existing.ApprovalRequestID != request.ID || existing.Decision != decision.Decision {
			return nil, nil, domain.ErrIdempotencyConflict
		}
		return &existing, nil, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}
	if request.State != domain.ApprovalRequestPending || !now.Before(request.ExpiresAt) {
		_, _ = tx.ExecContext(ctx, `UPDATE approval_requests SET state=? WHERE approval_request_id=? AND state='pending'`, domain.ApprovalRequestExpired, request.ID)
		return nil, nil, domain.ErrApprovalStale
	}
	if request.Mode == domain.ApprovalModeNative {
		var status string
		var version int64
		if err := tx.QueryRowContext(ctx, `SELECT status, version FROM run_attempts WHERE run_id=?`, request.TargetRunID).Scan(&status, &version); err != nil || version != request.ExpectedRunVersion || !domain.RunAttemptStatus(status).Active() {
			_, _ = tx.ExecContext(ctx, `UPDATE approval_requests SET state=? WHERE approval_request_id=? AND state='pending'`, domain.ApprovalRequestStale, request.ID)
			if decisionEvent != nil {
				if validateErr := validateJournalForAggregate(decisionEvent, "approval_request", request.ID, now); validateErr == nil {
					_ = insertJournal(ctx, tx, decisionEvent)
				}
			}
			_ = commit(tx)
			return nil, nil, domain.ErrApprovalStale
		}
	}
	decision.State = domain.ApprovalDecisionPersisted
	decision.CreatedAt = now
	if _, err := tx.ExecContext(ctx, `INSERT INTO approval_decisions (approval_decision_id, approval_request_id, decided_by, decision, state, idempotency_key, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, decision.ID, request.ID, decision.DecidedBy, decision.Decision, decision.State, decision.IdempotencyKey, formatTime(now)); err != nil {
		return nil, nil, fmt.Errorf("persist approval decision: %w", err)
	}
	newState := domain.ApprovalRequestRejected
	if decision.Decision == domain.ApprovalDecisionApprove {
		newState = domain.ApprovalRequestApproved
	}
	if _, err := tx.ExecContext(ctx, `UPDATE approval_requests SET state=? WHERE approval_request_id=? AND state='pending'`, newState, request.ID); err != nil {
		return nil, nil, err
	}
	if decisionEvent == nil {
		return nil, nil, domain.ErrInvalidInput("approval decision event is required")
	}
	if err := validateJournalForAggregate(decisionEvent, "approval_request", request.ID, now); err != nil {
		return nil, nil, err
	}
	if err := insertJournal(ctx, tx, decisionEvent); err != nil {
		return nil, nil, err
	}
	if request.Mode == domain.ApprovalModeNative && decision.Decision == domain.ApprovalDecisionApprove && item != nil {
		item.Kind = domain.MailboxKindApproval
		item.Lane = domain.MailboxLaneControl
		item.TaskID = request.TaskID
		item.TargetRunID = request.TargetRunID
		item.ExpectedRunVersion = request.ExpectedRunVersion
		item.ApprovalRequestID = request.ID
		item.ApprovalDecisionID = decision.ID
		item.State = domain.MailboxStatePending
		item.CreatedAt = now
		if err := insertMailbox(ctx, tx, item); err != nil {
			return nil, nil, err
		}
		if mailboxEvent == nil {
			return nil, nil, domain.ErrInvalidInput("approval mailbox event is required")
		}
		if err := validateJournalForAggregate(mailboxEvent, "mailbox_item", item.ID, now); err != nil {
			return nil, nil, err
		}
		if err := insertJournal(ctx, tx, mailboxEvent); err != nil {
			return nil, nil, err
		}
	}
	if err := commit(tx); err != nil {
		return nil, nil, err
	}
	return decision, item, nil
}

func (r *Repository) ConsumePreflightApproval(ctx context.Context, taskID string, scopeDigest string, event *domain.JournalEvent) (*domain.ApprovalRequest, error) {
	now := r.now().UTC()
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	request, err := scanApprovalRequest(tx.QueryRowContext(ctx, `SELECT `+approvalRequestColumns+` FROM approval_requests WHERE task_id=? AND mode='preflight' AND state='approved' AND scope_digest=? ORDER BY created_at ASC LIMIT 1`, taskID, scopeDigest))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !now.Before(request.ExpiresAt) {
		_, _ = tx.ExecContext(ctx, `UPDATE approval_requests SET state='expired' WHERE approval_request_id=?`, request.ID)
		_ = tx.Commit()
		return nil, domain.ErrApprovalStale
	}
	if event == nil {
		return nil, domain.ErrInvalidInput("approval consume event is required")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE approval_requests SET state='consumed' WHERE approval_request_id=? AND state='approved'`, request.ID); err != nil {
		return nil, err
	}
	if err := validateJournalForAggregate(event, "approval_request", request.ID, now); err != nil {
		return nil, err
	}
	if err := insertJournal(ctx, tx, event); err != nil {
		return nil, err
	}
	if err := commit(tx); err != nil {
		return nil, err
	}
	request.State = domain.ApprovalRequestConsumed
	return request, nil
}
