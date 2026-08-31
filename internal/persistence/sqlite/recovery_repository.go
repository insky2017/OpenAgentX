package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"openagentx/internal/domain"
)

// ReconcileExpired applies the daemon-start recovery policy using the
// repository clock. Expired claims become pending again; an active RunAttempt
// whose lease expired is marked uncertain because its side effects cannot be
// proven absent. A cancellation intent remains authoritative.
func (r *Repository) ReconcileExpired(ctx context.Context) error {
	now := r.now().UTC()
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	nowText := formatTime(now)
	actor, err := recoveryActor(ctx, tx)
	if err != nil {
		return err
	}

	// Snapshot the rows before updating them so every actual state transition
	// gets one journal record in this same transaction.
	mailboxRows, err := queryExpiredMailbox(ctx, tx, nowText)
	if err != nil {
		return err
	}
	for _, row := range mailboxRows {
		result, err := tx.ExecContext(ctx, `UPDATE mailbox_items SET state='pending', worker_instance_id=NULL, fencing_token=NULL, lease_until=NULL
			WHERE mailbox_item_id=? AND state='claimed' AND lease_until IS NOT NULL AND lease_until<=?`, row.id, nowText)
		if err != nil {
			return fmt.Errorf("requeue expired mailbox claim %s: %w", row.id, err)
		}
		if changed, _ := result.RowsAffected(); changed == 1 {
			if err := appendRecoveryEvent(ctx, tx, actor, row.organizationID, "mailbox_item", row.id,
				"mailbox.reclaimed", map[string]any{"from_state": "claimed", "to_state": "pending", "reason": "lease_expired"}, now); err != nil {
				return err
			}
		}
	}

	workerRows, err := queryExpiredWorkers(ctx, tx, nowText)
	if err != nil {
		return err
	}
	for _, row := range workerRows {
		result, err := tx.ExecContext(ctx, `UPDATE worker_instances SET status='offline', updated_at=?
			WHERE worker_instance_id=? AND status <> 'offline' AND lease_until<=?`, nowText, row.id, nowText)
		if err != nil {
			return fmt.Errorf("expire Worker lease %s: %w", row.id, err)
		}
		if changed, _ := result.RowsAffected(); changed == 1 {
			if err := appendRecoveryEvent(ctx, tx, actor, row.organizationID, "worker_instance", row.id,
				"worker.offline", map[string]any{"from_state": row.status, "to_state": "offline", "reason": "lease_expired"}, now); err != nil {
				return err
			}
		}
	}

	runRows, err := queryExpiredRuns(ctx, tx, nowText)
	if err != nil {
		return err
	}
	for _, row := range runRows {
		result, err := tx.ExecContext(ctx, `UPDATE run_attempts SET status='uncertain', version=version+1, finished_at=?, result_json=?, updated_at=?
			WHERE run_id=? AND status IN ('starting','running','waiting_approval','finishing') AND lease_until<=?`,
			nowText, `{"status":"uncertain","side_effects_known":false,"error":"RunAttempt lease expired during recovery"}`, nowText, row.id, nowText)
		if err != nil {
			return fmt.Errorf("mark expired RunAttempt %s uncertain: %w", row.id, err)
		}
		if changed, _ := result.RowsAffected(); changed == 1 {
			if err := appendRecoveryEvent(ctx, tx, actor, row.organizationID, "run_attempt", row.id,
				"run_attempt.uncertain", map[string]any{"from_state": row.status, "to_state": "uncertain", "task_id": row.taskID, "reason": "lease_expired"}, now); err != nil {
				return err
			}
		}
	}

	for _, row := range runRows {
		var status domain.TaskStatus
		if err := tx.QueryRowContext(ctx, `SELECT status FROM tasks WHERE task_id=?`, row.taskID).Scan(&status); err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return fmt.Errorf("load task %s during recovery: %w", row.taskID, err)
		}
		if status != domain.TaskStatusRunning && status != domain.TaskStatusWaitingApproval && status != domain.TaskStatusCancelRequested {
			continue
		}
		toState := domain.TaskStatusUncertain
		if status == domain.TaskStatusCancelRequested {
			toState = domain.TaskStatusCanceled
		}
		result, err := tx.ExecContext(ctx, `UPDATE tasks SET status=?, version=version+1, updated_at=?
			WHERE task_id=? AND status=?`, toState, nowText, row.taskID, status)
		if err != nil {
			return fmt.Errorf("settle task %s for expired RunAttempt: %w", row.taskID, err)
		}
		if changed, _ := result.RowsAffected(); changed == 1 {
			eventType := "task.uncertain"
			if toState == domain.TaskStatusCanceled {
				eventType = "task.canceled"
			}
			if err := appendRecoveryEvent(ctx, tx, actor, row.organizationID, "task", row.taskID,
				eventType, map[string]any{"from_state": status, "to_state": toState, "run_id": row.id, "reason": "run_lease_expired"}, now); err != nil {
				return err
			}
		}
	}
	if err := r.inject(FaultBeforeCommit); err != nil {
		return err
	}
	return commit(tx)
}

type recoveryRow struct {
	id             string
	status         string
	taskID         string
	organizationID string
}

func recoveryActor(ctx context.Context, tx *sql.Tx) (string, error) {
	var actor string
	if err := tx.QueryRowContext(ctx, `SELECT principal_id FROM principals WHERE kind='system' AND status='active' ORDER BY principal_id LIMIT 1`).Scan(&actor); err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("recovery requires an active system principal")
		}
		return "", fmt.Errorf("load recovery system principal: %w", err)
	}
	return actor, nil
}

func queryExpiredMailbox(ctx context.Context, tx *sql.Tx, now string) ([]recoveryRow, error) {
	rows, err := tx.QueryContext(ctx, `SELECT m.mailbox_item_id, a.organization_id FROM mailbox_items m JOIN agents a ON a.agent_id=m.target_agent_id
		WHERE m.state='claimed' AND m.lease_until IS NOT NULL AND m.lease_until<=? ORDER BY m.sequence ASC`, now)
	if err != nil {
		return nil, fmt.Errorf("query expired mailbox claims: %w", err)
	}
	defer rows.Close()
	result := make([]recoveryRow, 0)
	for rows.Next() {
		var row recoveryRow
		if err := rows.Scan(&row.id, &row.organizationID); err != nil {
			return nil, fmt.Errorf("scan expired mailbox claim: %w", err)
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func queryExpiredWorkers(ctx context.Context, tx *sql.Tx, now string) ([]recoveryRow, error) {
	rows, err := tx.QueryContext(ctx, `SELECT w.worker_instance_id, w.status, a.organization_id FROM worker_instances w JOIN agents a ON a.agent_id=w.agent_id
		WHERE w.status <> 'offline' AND w.lease_until<=? ORDER BY w.worker_instance_id ASC`, now)
	if err != nil {
		return nil, fmt.Errorf("query expired Worker leases: %w", err)
	}
	defer rows.Close()
	result := make([]recoveryRow, 0)
	for rows.Next() {
		var row recoveryRow
		if err := rows.Scan(&row.id, &row.status, &row.organizationID); err != nil {
			return nil, fmt.Errorf("scan expired Worker lease: %w", err)
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func queryExpiredRuns(ctx context.Context, tx *sql.Tx, now string) ([]recoveryRow, error) {
	rows, err := tx.QueryContext(ctx, `SELECT r.run_id, r.status, r.task_id, t.organization_id FROM run_attempts r JOIN tasks t ON t.task_id=r.task_id
		WHERE r.status IN ('starting','running','waiting_approval','finishing') AND r.lease_until<=? ORDER BY r.run_id ASC`, now)
	if err != nil {
		return nil, fmt.Errorf("query expired RunAttempts: %w", err)
	}
	defer rows.Close()
	result := make([]recoveryRow, 0)
	for rows.Next() {
		var row recoveryRow
		if err := rows.Scan(&row.id, &row.status, &row.taskID, &row.organizationID); err != nil {
			return nil, fmt.Errorf("scan expired RunAttempt: %w", err)
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func appendRecoveryEvent(ctx context.Context, tx *sql.Tx, actor, organization, aggregateType, aggregateID, eventType string, payload map[string]any, now time.Time) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode recovery event %s: %w", eventType, err)
	}
	event := &domain.JournalEvent{ID: "event-recovery-" + uuid.NewString(), OrganizationID: organization,
		AggregateType: aggregateType, AggregateID: aggregateID, EventType: eventType,
		ActorPrincipalID: actor, Payload: encoded, CreatedAt: now}
	if err := validateJournalForAggregate(event, aggregateType, aggregateID, now); err != nil {
		return err
	}
	return insertJournal(ctx, tx, event)
}
