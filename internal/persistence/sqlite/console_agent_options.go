package sqlite

import (
	"context"
	"fmt"

	"openagentx/internal/domain"
)

// ListConsoleAgentOptions returns one deterministic page without exposing
// principal, Worker transport, fencing or runtime payload columns.
func (r *Repository) ListConsoleAgentOptions(ctx context.Context, afterAgentID string, limit int) ([]domain.ConsoleAgentOption, error) {
	if afterAgentID != "" {
		if err := domain.ValidateIdentifier("after_agent_id", afterAgentID); err != nil {
			return nil, err
		}
	}
	if limit <= 0 || limit > 201 {
		return nil, domain.ErrInvalidInput("Console Agent option limit must be between 1 and 201")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT a.agent_id, a.organization_id, a.display_name,
		COALESCE(w.status, 'offline'), COALESCE(w.generation, 0), COALESCE(run.status, '')
		FROM agents a
		LEFT JOIN worker_instances w ON w.worker_instance_id = (
			SELECT candidate.worker_instance_id FROM worker_instances candidate
			WHERE candidate.agent_id=a.agent_id
			ORDER BY candidate.generation DESC, candidate.updated_at DESC,
				candidate.worker_instance_id DESC LIMIT 1
		)
		LEFT JOIN run_attempts run ON run.run_id = (
			SELECT active.run_id FROM run_attempts active
			WHERE active.agent_id=a.agent_id
				AND active.worker_instance_id=w.worker_instance_id
				AND active.status IN ('starting','running','waiting_approval','finishing')
			ORDER BY active.started_at DESC, active.run_id DESC LIMIT 1
		)
		WHERE a.agent_id > ? ORDER BY a.agent_id LIMIT ?`, afterAgentID, limit)
	if err != nil {
		return nil, fmt.Errorf("list Console Agent options: %w", err)
	}
	defer rows.Close()
	options := make([]domain.ConsoleAgentOption, 0)
	for rows.Next() {
		var option domain.ConsoleAgentOption
		if err := rows.Scan(&option.AgentID, &option.OrganizationID, &option.DisplayName, &option.WorkerStatus,
			&option.Generation, &option.ActiveRunStatus); err != nil {
			return nil, fmt.Errorf("scan Console Agent option: %w", err)
		}
		if err := option.Validate(); err != nil {
			return nil, fmt.Errorf("validate Console Agent option: %w", err)
		}
		options = append(options, option)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list Console Agent options: %w", err)
	}
	return options, nil
}
