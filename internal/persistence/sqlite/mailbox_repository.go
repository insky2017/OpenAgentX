package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"agentbus/internal/domain"
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
