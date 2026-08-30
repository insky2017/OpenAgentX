package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"openagentx/internal/domain"
)

func (r *Repository) ListAgents(ctx context.Context, limit int) ([]domain.AgentIdentity, error) {
	if limit <= 0 || limit > 1000 {
		return nil, domain.ErrInvalidInput("agent limit must be between 1 and 1000")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT agent_id, principal_id, organization_id, display_name, status, version, created_at, updated_at FROM agents ORDER BY agent_id LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()
	result := make([]domain.AgentIdentity, 0)
	for rows.Next() {
		var a domain.AgentIdentity
		var created, updated string
		if err := rows.Scan(&a.ID, &a.PrincipalID, &a.OrganizationID, &a.DisplayName, &a.Status, &a.Version, &created, &updated); err != nil {
			return nil, err
		}
		a.CreatedAt, err = parseTime(created)
		if err != nil {
			return nil, err
		}
		a.UpdatedAt, err = parseTime(updated)
		if err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

func (r *Repository) LatestJournalSequence(ctx context.Context) (int64, error) {
	var sequence int64
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) FROM event_journal`).Scan(&sequence); err != nil {
		return 0, fmt.Errorf("read latest Event Journal sequence: %w", err)
	}
	return sequence, nil
}

func (r *Repository) ListWorkers(ctx context.Context, limit int) ([]domain.WorkerInstance, error) {
	if limit <= 0 || limit > 1000 {
		return nil, domain.ErrInvalidInput("worker limit must be between 1 and 1000")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT worker_instance_id, agent_id, generation, transport, authenticated_principal, capabilities_json, status, last_heartbeat_at, lease_until, fencing_token, started_at, updated_at FROM worker_instances ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list workers: %w", err)
	}
	defer rows.Close()
	result := make([]domain.WorkerInstance, 0)
	for rows.Next() {
		var w domain.WorkerInstance
		var caps, hb, lease, started, updated string
		if err := rows.Scan(&w.ID, &w.AgentID, &w.Generation, &w.Transport, &w.AuthenticatedPrincipal, &caps, &w.Status, &hb, &lease, &w.FencingToken, &started, &updated); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(caps), &w.Capabilities); err != nil {
			return nil, err
		}
		var e error
		if w.LastHeartbeatAt, e = parseTime(hb); e != nil {
			return nil, e
		}
		if w.LeaseUntil, e = parseTime(lease); e != nil {
			return nil, e
		}
		if w.StartedAt, e = parseTime(started); e != nil {
			return nil, e
		}
		if w.UpdatedAt, e = parseTime(updated); e != nil {
			return nil, e
		}
		result = append(result, w)
	}
	return result, rows.Err()
}

func (r *Repository) CountPendingMailbox(ctx context.Context, agentID string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mailbox_items WHERE target_agent_id=? AND state IN ('pending','claimed')`, agentID).Scan(&count)
	return count, err
}

func (r *Repository) GetActiveRunForAgent(ctx context.Context, agentID string) (*domain.RunAttempt, error) {
	row := r.db.QueryRowContext(ctx, `SELECT run_id FROM run_attempts WHERE agent_id=? AND status IN ('starting','running','waiting_approval','finishing') ORDER BY started_at DESC LIMIT 1`, agentID)
	var id string
	if err := row.Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return r.GetRunAttempt(ctx, id)
}

func (r *Repository) ListPendingApprovals(ctx context.Context, limit int) ([]domain.ApprovalRequest, error) {
	if limit <= 0 || limit > 1000 {
		return nil, domain.ErrInvalidInput("approval limit must be between 1 and 1000")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT approval_request_id, task_id, mode, target_run_id, expected_run_version, scope_digest, state, expires_at, created_at FROM approval_requests WHERE state='pending' ORDER BY created_at ASC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.ApprovalRequest, 0)
	for rows.Next() {
		v, err := scanApprovalRequest(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *v)
	}
	return result, rows.Err()
}
