package sqlite

import (
	"context"
	"fmt"
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
	if _, err := tx.ExecContext(ctx, `UPDATE mailbox_items SET state='pending', worker_instance_id=NULL, fencing_token=NULL, lease_until=NULL
		WHERE state='claimed' AND lease_until IS NOT NULL AND lease_until<=?`, nowText); err != nil {
		return fmt.Errorf("requeue expired mailbox claims: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE worker_instances SET status='offline', updated_at=?
		WHERE status <> 'offline' AND lease_until<=?`, nowText, nowText); err != nil {
		return fmt.Errorf("expire Worker leases: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE run_attempts SET status='uncertain', version=version+1, finished_at=?, result_json=?, updated_at=?
		WHERE status IN ('starting','running','waiting_approval','finishing') AND lease_until<=?`,
		nowText, `{"status":"uncertain","side_effects_known":false,"error":"RunAttempt lease expired during recovery"}`, nowText, nowText); err != nil {
		return fmt.Errorf("mark expired RunAttempts uncertain: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET status=CASE WHEN status='cancel_requested' THEN 'canceled' ELSE 'uncertain' END,
		version=version+1, updated_at=? WHERE task_id IN (
		SELECT t.task_id FROM tasks t JOIN run_attempts r ON r.task_id=t.task_id
		WHERE r.status='uncertain' AND r.finished_at=? AND t.status IN ('running','waiting_approval','cancel_requested'))`, nowText, nowText); err != nil {
		return fmt.Errorf("settle Tasks for expired RunAttempts: %w", err)
	}
	return commit(tx)
}
