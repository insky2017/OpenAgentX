package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"openagentx/internal/domain"
)

const mailboxColumns = `sequence, mailbox_item_id, target_agent_id, kind, lane, task_id, message_id,
	approval_request_id, approval_decision_id, target_run_id, expected_run_version, state,
	worker_instance_id, fencing_token, lease_until, attempts, created_at, accepted_at`

type rowScanner interface {
	Scan(...any) error
}

func insertMailbox(ctx context.Context, tx *sql.Tx, item *domain.MailboxItem) error {
	if item == nil {
		return domain.ErrInvalidInput("mailbox item is required")
	}
	if err := item.ValidateForInsert(); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO mailbox_items (
		mailbox_item_id, target_agent_id, kind, lane, task_id, message_id,
		approval_request_id, approval_decision_id, target_run_id, expected_run_version,
		state, worker_instance_id, fencing_token, lease_until, attempts, created_at, accepted_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.TargetAgentID, item.Kind, item.Lane, nullableText(item.TaskID), nullableText(item.MessageID),
		nullableText(item.ApprovalRequestID), nullableText(item.ApprovalDecisionID), nullableText(item.TargetRunID),
		nullablePositive(item.ExpectedRunVersion), item.State, nullableText(item.WorkerInstanceID),
		nullablePositive(item.FencingToken), nullableTime(item.LeaseUntil), item.Attempts,
		formatTime(item.CreatedAt), nullableTime(item.AcceptedAt))
	if err != nil {
		return fmt.Errorf("insert mailbox item: %w", err)
	}
	sequence, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("read mailbox sequence: %w", err)
	}
	item.Sequence = sequence
	return nil
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullablePositive(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatTime(*value)
}

func scanMailbox(scanner rowScanner) (*domain.MailboxItem, error) {
	var item domain.MailboxItem
	var taskID, messageID, approvalRequestID, approvalDecisionID, targetRunID sql.NullString
	var expectedRunVersion, fencingToken sql.NullInt64
	var workerInstanceID, leaseUntil, acceptedAt sql.NullString
	var createdAt string
	if err := scanner.Scan(&item.Sequence, &item.ID, &item.TargetAgentID, &item.Kind, &item.Lane,
		&taskID, &messageID, &approvalRequestID, &approvalDecisionID, &targetRunID,
		&expectedRunVersion, &item.State, &workerInstanceID, &fencingToken, &leaseUntil,
		&item.Attempts, &createdAt, &acceptedAt); err != nil {
		return nil, err
	}
	item.TaskID = taskID.String
	item.MessageID = messageID.String
	item.ApprovalRequestID = approvalRequestID.String
	item.ApprovalDecisionID = approvalDecisionID.String
	item.TargetRunID = targetRunID.String
	item.ExpectedRunVersion = expectedRunVersion.Int64
	item.WorkerInstanceID = workerInstanceID.String
	item.FencingToken = fencingToken.Int64
	created, err := parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	item.CreatedAt = created
	if leaseUntil.Valid {
		parsed, err := parseTime(leaseUntil.String)
		if err != nil {
			return nil, err
		}
		item.LeaseUntil = &parsed
	}
	if acceptedAt.Valid {
		parsed, err := parseTime(acceptedAt.String)
		if err != nil {
			return nil, err
		}
		item.AcceptedAt = &parsed
	}
	return &item, nil
}

func (r *Repository) GetMailboxItem(ctx context.Context, itemID string) (*domain.MailboxItem, error) {
	item, err := scanMailbox(r.db.QueryRowContext(ctx, `SELECT `+mailboxColumns+`
		FROM mailbox_items WHERE mailbox_item_id = ?`, itemID))
	if err == sql.ErrNoRows {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get mailbox item: %w", err)
	}
	return item, nil
}

func (r *Repository) ResolveMailboxPayload(ctx context.Context, guard domain.WorkerWriteGuard, itemID string) (*domain.MailboxPayload, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := loadGuardedWorker(ctx, tx, guard); err != nil {
		return nil, err
	}
	item, err := scanMailbox(tx.QueryRowContext(ctx, `SELECT `+mailboxColumns+`
		FROM mailbox_items WHERE mailbox_item_id=?`, itemID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrForbidden("mailbox payload is not owned by Worker")
	}
	if err != nil {
		return nil, fmt.Errorf("load mailbox payload item: %w", err)
	}
	if item.TargetAgentID != guard.AgentID || item.State != domain.MailboxStateClaimed ||
		item.WorkerInstanceID != guard.WorkerInstanceID || item.FencingToken != guard.FencingToken {
		return nil, domain.ErrForbidden("mailbox payload is not owned by Worker")
	}
	if item.LeaseUntil == nil || !guard.CheckedAt.Before(*item.LeaseUntil) {
		return nil, domain.ErrLeaseExpired
	}

	switch item.Kind {
	case domain.MailboxKindMessage:
		var message domain.Message
		err := tx.QueryRowContext(ctx, `SELECT message_id, task_id, version, sequence,
			sender_principal_id, target_agent_id, kind, content, created_at
			FROM messages WHERE message_id=?`, item.MessageID).Scan(&message.ID, &message.TaskID,
			&message.Version, &message.Sequence, &message.SenderPrincipalID, &message.TargetAgentID,
			&message.Kind, &message.Content, &message.CreatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrForbidden("Message payload does not match mailbox authority")
		}
		if err != nil {
			return nil, fmt.Errorf("load mailbox Message payload: %w", err)
		}
		if message.ID != item.MessageID || message.TaskID != item.TaskID || message.TargetAgentID != guard.AgentID {
			return nil, domain.ErrForbidden("Message payload does not match mailbox authority")
		}
		return &domain.MailboxPayload{Message: &message}, nil
	case domain.MailboxKindApproval:
		var decision domain.ApprovalDecision
		var requestTaskID, requestTargetRunID, requestMode, requestState, createdAt string
		var requestExpectedRunVersion int64
		err := tx.QueryRowContext(ctx, `SELECT d.approval_decision_id, d.approval_request_id,
			d.decided_by, d.decision, d.state, d.idempotency_key, d.created_at,
			r.task_id, COALESCE(r.target_run_id, ''), COALESCE(r.expected_run_version, 0),
			r.mode, r.state
			FROM approval_decisions d
			JOIN approval_requests r ON r.approval_request_id=d.approval_request_id
			WHERE d.approval_decision_id=?`, item.ApprovalDecisionID).Scan(
			&decision.ID, &decision.ApprovalRequestID, &decision.DecidedBy, &decision.Decision,
			&decision.State, &decision.IdempotencyKey, &createdAt, &requestTaskID,
			&requestTargetRunID, &requestExpectedRunVersion, &requestMode, &requestState)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrForbidden("Approval payload does not match mailbox authority")
		}
		if err != nil {
			return nil, fmt.Errorf("load mailbox Approval payload: %w", err)
		}
		decision.CreatedAt, err = parseTime(createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse mailbox Approval payload: %w", err)
		}
		if decision.ID != item.ApprovalDecisionID || decision.ApprovalRequestID != item.ApprovalRequestID ||
			requestTaskID != item.TaskID || requestTargetRunID != item.TargetRunID ||
			requestExpectedRunVersion != item.ExpectedRunVersion || requestMode != string(domain.ApprovalModeNative) ||
			decision.State != domain.ApprovalDecisionPersisted ||
			(decision.Decision == domain.ApprovalDecisionApprove && requestState != string(domain.ApprovalRequestApproved)) ||
			(decision.Decision == domain.ApprovalDecisionReject && requestState != string(domain.ApprovalRequestRejected)) {
			return nil, domain.ErrForbidden("Approval payload does not match mailbox authority")
		}
		return &domain.MailboxPayload{ApprovalDecision: &decision}, nil
	default:
		return nil, domain.ErrForbidden("mailbox item has no resolvable control payload")
	}
}

func (r *Repository) ListMailbox(ctx context.Context, agentID string, afterSequence int64, limit int) ([]domain.MailboxItem, error) {
	if limit <= 0 || limit > 1000 || afterSequence < 0 {
		return nil, domain.ErrInvalidInput("invalid mailbox pagination")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+mailboxColumns+`
		FROM mailbox_items WHERE target_agent_id = ? AND sequence > ?
		ORDER BY sequence ASC LIMIT ?`,
		agentID, afterSequence, limit)
	if err != nil {
		return nil, fmt.Errorf("list mailbox: %w", err)
	}
	defer rows.Close()
	items := make([]domain.MailboxItem, 0)
	for rows.Next() {
		item, err := scanMailbox(rows)
		if err != nil {
			return nil, fmt.Errorf("scan mailbox item: %w", err)
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mailbox: %w", err)
	}
	return items, nil
}
