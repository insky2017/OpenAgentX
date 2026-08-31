package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"openagentx/internal/domain"
)

func (r *Repository) CreateWorkerInstance(ctx context.Context, worker *domain.WorkerInstance, event *domain.JournalEvent) error {
	if worker == nil {
		return domain.ErrInvalidInput("worker instance is required")
	}
	now := r.now().UTC()
	worker.StartedAt = normalizeTime(worker.StartedAt, now)
	worker.LastHeartbeatAt = normalizeTime(worker.LastHeartbeatAt, now)
	worker.UpdatedAt = normalizeTime(worker.UpdatedAt, now)
	if worker.LeaseUntil.IsZero() {
		return domain.ErrInvalidInput("worker lease_until is required")
	}
	if err := worker.Validate(); err != nil {
		return err
	}
	if err := validateJournalForAggregate(event, "worker_instance", worker.ID, now); err != nil {
		return err
	}
	capabilities := append([]string(nil), worker.Capabilities...)
	sort.Strings(capabilities)
	capabilitiesJSON, err := json.Marshal(capabilities)
	if err != nil {
		return fmt.Errorf("encode Worker capabilities: %w", err)
	}

	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO worker_instances (
		worker_instance_id, agent_id, generation, transport, authenticated_principal,
		capabilities_json, status, last_heartbeat_at, lease_until, fencing_token, started_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, worker.ID, worker.AgentID, worker.Generation,
		worker.Transport, worker.AuthenticatedPrincipal, string(capabilitiesJSON), worker.Status,
		formatTime(worker.LastHeartbeatAt), formatTime(worker.LeaseUntil), worker.FencingToken,
		formatTime(worker.StartedAt), formatTime(worker.UpdatedAt)); err != nil {
		if isUniqueConstraint(err, "uq_worker_instances_agent_active") {
			return domain.ErrConflict("logical Agent already has an active Worker")
		}
		return fmt.Errorf("create Worker instance: %w", err)
	}
	if err := r.inject(FaultAfterStateWrite); err != nil {
		return err
	}
	if err := insertJournal(ctx, tx, event); err != nil {
		return err
	}
	if err := r.inject(FaultBeforeCommit); err != nil {
		return err
	}
	return commit(tx)
}

func (r *Repository) GetWorkerInstance(ctx context.Context, workerID string) (*domain.WorkerInstance, error) {
	var worker domain.WorkerInstance
	var capabilitiesJSON string
	var lastHeartbeat, leaseUntil, startedAt, updatedAt string
	err := r.db.QueryRowContext(ctx, `SELECT worker_instance_id, agent_id, generation, transport,
		authenticated_principal, capabilities_json, status, last_heartbeat_at, lease_until,
		fencing_token, started_at, updated_at FROM worker_instances WHERE worker_instance_id = ?`, workerID).Scan(
		&worker.ID, &worker.AgentID, &worker.Generation, &worker.Transport, &worker.AuthenticatedPrincipal,
		&capabilitiesJSON, &worker.Status, &lastHeartbeat, &leaseUntil, &worker.FencingToken,
		&startedAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get Worker instance: %w", err)
	}
	if err := json.Unmarshal([]byte(capabilitiesJSON), &worker.Capabilities); err != nil {
		return nil, fmt.Errorf("decode Worker capabilities: %w", err)
	}
	if worker.LastHeartbeatAt, err = parseTime(lastHeartbeat); err != nil {
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

func (r *Repository) BeginRunAttempt(
	ctx context.Context,
	expectedTaskVersion int64,
	run *domain.RunAttempt,
	taskEvent *domain.JournalEvent,
	runEvent *domain.JournalEvent,
) (*domain.Task, error) {
	if run == nil {
		return nil, domain.ErrInvalidInput("run attempt is required")
	}
	now := r.now().UTC()
	run.CreatedAt = normalizeTime(run.CreatedAt, now)
	run.UpdatedAt = normalizeTime(run.UpdatedAt, now)
	run.StartedAt = normalizeTime(run.StartedAt, now)
	if !run.Status.Active() {
		return nil, domain.ErrInvalidInput("new RunAttempt must be active")
	}
	if err := run.Validate(); err != nil {
		return nil, err
	}
	if err := validateJournalForAggregate(taskEvent, "task", run.TaskID, now); err != nil {
		return nil, err
	}
	if err := validateJournalForAggregate(runEvent, "run_attempt", run.ID, now); err != nil {
		return nil, err
	}

	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	task, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE task_id = ?`, run.TaskID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrTaskNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load Task for RunAttempt: %w", err)
	}
	if task.Version != expectedTaskVersion {
		return nil, domain.ErrStaleVersion
	}
	if task.TargetAgentID != run.AgentID {
		return nil, domain.ErrForbidden("RunAttempt Agent does not own target Task")
	}
	if !task.CanBeginAttempt() {
		if task.Status == domain.TaskStatusCancelRequested {
			return nil, domain.ErrTaskCancelRequested
		}
		return nil, domain.ErrInvalidTransition
	}
	task.Version++
	task.Status = domain.TaskStatusRunning
	task.UpdatedAt = formatTime(now)
	result, err := tx.ExecContext(ctx, `UPDATE tasks SET version = ?, status = ?, updated_at = ?
		WHERE task_id = ? AND version = ?`, task.Version, task.Status, task.UpdatedAt,
		task.ID, expectedTaskVersion)
	if err != nil {
		return nil, fmt.Errorf("mark Task running: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return nil, domain.ErrStaleVersion
	}
	if err := insertRunAttempt(ctx, tx, run); err != nil {
		return nil, err
	}
	if err := r.inject(FaultAfterStateWrite); err != nil {
		return nil, err
	}
	if err := insertJournal(ctx, tx, taskEvent); err != nil {
		return nil, err
	}
	if err := insertJournal(ctx, tx, runEvent); err != nil {
		return nil, err
	}
	if err := r.inject(FaultBeforeCommit); err != nil {
		return nil, err
	}
	if err := commit(tx); err != nil {
		return nil, err
	}
	return task, nil
}

func insertRunAttempt(ctx context.Context, tx *sql.Tx, run *domain.RunAttempt) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO run_attempts (
		run_id, task_id, agent_id, version, status, worker_instance_id, fencing_token, lease_until,
		execution_spec_version, requested_execution_json, resolved_execution_json, adapter_id,
		backend_id, model, reasoning_mode, reasoning_value, started_at, finished_at, result_json,
		created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.TaskID, run.AgentID, run.Version, run.Status, run.WorkerInstanceID, run.FencingToken,
		formatTime(run.LeaseUntil), run.ExecutionSpecVersion, run.RequestedExecutionJSON,
		run.ResolvedExecutionJSON, run.AdapterID, run.BackendID, run.Model, run.ReasoningMode,
		run.ReasoningValue, formatTime(run.StartedAt), nullableTime(run.FinishedAt),
		nullableJSON(run.ResultJSON), formatTime(run.CreatedAt), formatTime(run.UpdatedAt)); err != nil {
		if isUniqueConstraint(err, "uq_run_attempts_agent_active") || strings.Contains(err.Error(), "run_attempts.agent_id") {
			return domain.ErrConflict("logical Agent already has an Active RunAttempt")
		}
		return fmt.Errorf("create RunAttempt: %w", err)
	}
	return nil
}

func nullableJSON(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (r *Repository) GetRunAttempt(ctx context.Context, runID string) (*domain.RunAttempt, error) {
	return scanRunAttempt(r.db.QueryRowContext(ctx, `SELECT run_id, task_id, agent_id, version,
		status, worker_instance_id, fencing_token, lease_until, execution_spec_version,
		requested_execution_json, resolved_execution_json, adapter_id, backend_id, model,
		reasoning_mode, reasoning_value, started_at, finished_at, result_json, created_at, updated_at
		FROM run_attempts WHERE run_id = ?`, runID))
}

func scanRunAttempt(scanner rowScanner) (*domain.RunAttempt, error) {
	var run domain.RunAttempt
	var leaseUntil, startedAt, createdAt, updatedAt string
	var finishedAt, resultJSON sql.NullString
	err := scanner.Scan(&run.ID, &run.TaskID, &run.AgentID, &run.Version, &run.Status,
		&run.WorkerInstanceID, &run.FencingToken, &leaseUntil, &run.ExecutionSpecVersion,
		&run.RequestedExecutionJSON, &run.ResolvedExecutionJSON, &run.AdapterID, &run.BackendID,
		&run.Model, &run.ReasoningMode, &run.ReasoningValue, &startedAt, &finishedAt,
		&resultJSON, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan RunAttempt: %w", err)
	}
	if run.LeaseUntil, err = parseTime(leaseUntil); err != nil {
		return nil, err
	}
	if run.StartedAt, err = parseTime(startedAt); err != nil {
		return nil, err
	}
	if run.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if run.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	if finishedAt.Valid {
		parsed, err := parseTime(finishedAt.String)
		if err != nil {
			return nil, err
		}
		run.FinishedAt = &parsed
	}
	if resultJSON.Valid {
		run.ResultJSON = resultJSON.String
	}
	return &run, nil
}

func (r *Repository) CreateSessionBinding(ctx context.Context, binding *domain.SessionBinding, event *domain.JournalEvent) error {
	if binding == nil {
		return domain.ErrInvalidInput("session binding is required")
	}
	now := r.now().UTC()
	binding.CreatedAt = normalizeTime(binding.CreatedAt, now)
	binding.UpdatedAt = normalizeTime(binding.UpdatedAt, now)
	if err := binding.Validate(); err != nil {
		return err
	}
	if err := validateJournalForAggregate(event, "session_binding", binding.ID, now); err != nil {
		return err
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO session_bindings (
		session_binding_id, context_id, agent_id, backend_id, provider_session_id,
		state, version, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, binding.ID, binding.ContextID, binding.AgentID,
		binding.BackendID, binding.ProviderSessionID, binding.State, binding.Version,
		formatTime(binding.CreatedAt), formatTime(binding.UpdatedAt)); err != nil {
		return fmt.Errorf("create SessionBinding: %w", err)
	}
	if err := r.inject(FaultAfterStateWrite); err != nil {
		return err
	}
	if err := insertJournal(ctx, tx, event); err != nil {
		return err
	}
	if err := r.inject(FaultBeforeCommit); err != nil {
		return err
	}
	return commit(tx)
}

func (r *Repository) GetSessionBinding(ctx context.Context, contextID string, agentID string, backendID string) (*domain.SessionBinding, error) {
	var binding domain.SessionBinding
	var createdAt, updatedAt string
	err := r.db.QueryRowContext(ctx, `SELECT session_binding_id, context_id, agent_id, backend_id,
		provider_session_id, state, version, created_at, updated_at FROM session_bindings
		WHERE context_id = ? AND agent_id = ? AND backend_id = ?`, contextID, agentID, backendID).Scan(
		&binding.ID, &binding.ContextID, &binding.AgentID, &binding.BackendID,
		&binding.ProviderSessionID, &binding.State, &binding.Version, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get SessionBinding: %w", err)
	}
	if binding.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if binding.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	if err := binding.Validate(); err != nil {
		return nil, fmt.Errorf("invalid persisted SessionBinding: %w", err)
	}
	return &binding, nil
}
