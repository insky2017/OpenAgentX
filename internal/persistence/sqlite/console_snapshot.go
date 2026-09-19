package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"openagentx/internal/domain"
)

func (r *Repository) ConsoleSnapshot(ctx context.Context, agentID string) (domain.ConsoleSnapshot, error) {
	return r.consoleSnapshot(ctx, agentID, nil)
}

// The hook is used only by the package concurrency test to pause after all
// state rows have been read and before the journal high-water is sampled.
func (r *Repository) consoleSnapshot(ctx context.Context, agentID string, afterStateRead func()) (domain.ConsoleSnapshot, error) {
	var snapshot domain.ConsoleSnapshot
	if err := domain.ValidateIdentifier("agent_id", agentID); err != nil {
		return snapshot, err
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return snapshot, fmt.Errorf("begin Console snapshot transaction: %w", err)
	}
	defer tx.Rollback()

	var createdAt, updatedAt string
	err = tx.QueryRowContext(ctx, `SELECT agent_id, principal_id, organization_id, display_name,
		status, version, created_at, updated_at FROM agents WHERE agent_id=?`, agentID).Scan(
		&snapshot.Agent.ID, &snapshot.Agent.PrincipalID, &snapshot.Agent.OrganizationID,
		&snapshot.Agent.DisplayName, &snapshot.Agent.Status, &snapshot.Agent.Version,
		&createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return snapshot, domain.ErrAgentNotFound
	}
	if err != nil {
		return snapshot, fmt.Errorf("read Console Agent snapshot: %w", err)
	}
	if snapshot.Agent.CreatedAt, err = parseTime(createdAt); err != nil {
		return snapshot, err
	}
	if snapshot.Agent.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return snapshot, err
	}

	worker, err := scanConsoleWorker(tx.QueryRowContext(ctx, `SELECT worker_instance_id, agent_id,
		generation, transport, authenticated_principal, capabilities_json, status,
		last_heartbeat_at, lease_until, fencing_token, started_at, updated_at
		FROM worker_instances WHERE agent_id=?
		ORDER BY generation DESC, updated_at DESC, worker_instance_id DESC LIMIT 1`, agentID))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return snapshot, fmt.Errorf("read Console Worker snapshot: %w", err)
	}
	if err == nil {
		snapshot.Worker = worker
		backends, backendErr := listWorkerBackends(ctx, tx, worker.ID)
		if backendErr != nil {
			return snapshot, fmt.Errorf("read Console Backend health: %w", backendErr)
		}
		snapshot.BackendHealth = make(map[string]string, len(backends))
		for _, backend := range backends {
			snapshot.BackendHealth[backend.BackendID] = string(backend.Health)
		}
	}

	snapshot.ActiveRun, err = scanRunAttempt(tx.QueryRowContext(ctx, `SELECT run_id, task_id,
		agent_id, version, status, worker_instance_id, fencing_token, lease_until,
		execution_spec_version, requested_execution_json, resolved_execution_json, adapter_id,
		backend_id, model, reasoning_mode, reasoning_value, started_at, finished_at, result_json,
		created_at, updated_at FROM run_attempts WHERE agent_id=?
		AND status IN ('starting','running','waiting_approval','finishing')
		ORDER BY started_at DESC, run_id DESC LIMIT 1`, agentID))
	if errors.Is(err, domain.ErrNotFound) {
		snapshot.ActiveRun = nil
	} else if err != nil {
		return snapshot, fmt.Errorf("read Console active RunAttempt: %w", err)
	}
	if snapshot.ActiveRun != nil {
		if err := tx.QueryRowContext(ctx, `SELECT generation FROM worker_instances
			WHERE worker_instance_id=?`, snapshot.ActiveRun.WorkerInstanceID).
			Scan(&snapshot.ActiveRunWorkerGeneration); err != nil {
			return snapshot, fmt.Errorf("read Console active RunAttempt Worker generation: %w", err)
		}
	}
	snapshot.SuggestedTask, err = latestSuggestedConsoleTask(ctx, tx, agentID)
	if err != nil {
		return snapshot, err
	}

	if afterStateRead != nil {
		afterStateRead()
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) FROM event_journal`).Scan(&snapshot.SnapshotSequence); err != nil {
		return snapshot, fmt.Errorf("read Console snapshot sequence: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return snapshot, fmt.Errorf("commit Console snapshot transaction: %w", err)
	}
	return snapshot, nil
}

func scanConsoleWorker(scanner rowScanner) (*domain.WorkerInstance, error) {
	var worker domain.WorkerInstance
	var capabilitiesJSON, heartbeatAt, leaseUntil, startedAt, updatedAt string
	if err := scanner.Scan(&worker.ID, &worker.AgentID, &worker.Generation, &worker.Transport,
		&worker.AuthenticatedPrincipal, &capabilitiesJSON, &worker.Status, &heartbeatAt,
		&leaseUntil, &worker.FencingToken, &startedAt, &updatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(capabilitiesJSON), &worker.Capabilities); err != nil {
		return nil, fmt.Errorf("decode Console Worker capabilities: %w", err)
	}
	var err error
	if worker.LastHeartbeatAt, err = parseTime(heartbeatAt); err != nil {
		return nil, err
	}
	if worker.LeaseUntil, err = parseTime(leaseUntil); err != nil {
		return nil, err
	}
	if worker.StartedAt, err = parseTime(startedAt); err != nil {
		return nil, err
	}
	if worker.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &worker, nil
}
