package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

func (r *Repository) ListWorkerBackends(ctx context.Context, workerID string) ([]openruntime.BackendRegistration, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT backend_id, descriptor_json, health
		FROM runtime_backend_registrations WHERE worker_instance_id=? ORDER BY backend_id ASC`, workerID)
	if err != nil {
		return nil, fmt.Errorf("list Worker Backends: %w", err)
	}
	defer rows.Close()
	registrations := make([]openruntime.BackendRegistration, 0)
	for rows.Next() {
		var registration openruntime.BackendRegistration
		var descriptorJSON string
		if err := rows.Scan(&registration.BackendID, &descriptorJSON, &registration.Health); err != nil {
			return nil, fmt.Errorf("scan Worker Backend: %w", err)
		}
		if err := json.Unmarshal([]byte(descriptorJSON), &registration.Descriptor); err != nil {
			return nil, fmt.Errorf("decode Worker Backend descriptor: %w", err)
		}
		if err := registration.Validate(); err != nil {
			return nil, fmt.Errorf("invalid persisted Worker Backend registration: %w", err)
		}
		registrations = append(registrations, registration)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Worker Backends: %w", err)
	}
	return registrations, nil
}

func (r *Repository) SaveSessionBinding(ctx context.Context, binding *domain.SessionBinding, expectedVersion int64, event *domain.JournalEvent) error {
	if binding == nil {
		return domain.ErrInvalidInput("session binding is required")
	}
	now := r.now().UTC()
	if expectedVersion < 0 {
		return domain.ErrInvalidInput("expected SessionBinding version cannot be negative")
	}
	binding.UpdatedAt = normalizeTime(binding.UpdatedAt, now)
	if binding.CreatedAt.IsZero() {
		binding.CreatedAt = now
	}
	if binding.Version == 0 {
		binding.Version = 1
	}
	if err := binding.Validate(); err != nil {
		return err
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if expectedVersion == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO session_bindings
			(session_binding_id, context_id, agent_id, backend_id, provider_session_id, state, version, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, binding.ID, binding.ContextID, binding.AgentID, binding.BackendID,
			binding.ProviderSessionID, binding.State, binding.Version, formatTime(binding.CreatedAt), formatTime(binding.UpdatedAt))
	} else {
		binding.Version = expectedVersion + 1
		var result sql.Result
		result, err = tx.ExecContext(ctx, `UPDATE session_bindings SET provider_session_id=?, state=?, version=?, updated_at=?
			WHERE context_id=? AND agent_id=? AND backend_id=? AND version=?`, binding.ProviderSessionID, binding.State,
			binding.Version, formatTime(binding.UpdatedAt), binding.ContextID, binding.AgentID, binding.BackendID, expectedVersion)
		if err == nil {
			if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
				return domain.ErrStaleVersion
			}
		}
	}
	if err != nil {
		return fmt.Errorf("save SessionBinding: %w", err)
	}
	if event == nil {
		return domain.ErrInvalidInput("SessionBinding event is required")
	}
	if event.AggregateID == "" {
		event.AggregateID = binding.ID
	}
	if err := validateJournalForAggregate(event, "session_binding", binding.ID, now); err != nil {
		return err
	}
	if err := insertJournal(ctx, tx, event); err != nil {
		return err
	}
	return commit(tx)
}

func (r *Repository) BeginClaimedRunAttempt(
	ctx context.Context,
	guard domain.WorkerWriteGuard,
	mailboxItemID string,
	expectedTaskVersion int64,
	run *domain.RunAttempt,
	taskEvent *domain.JournalEvent,
	runEvent *domain.JournalEvent,
	mailboxEvent *domain.JournalEvent,
) (*domain.Task, *domain.MailboxItem, error) {
	if run == nil || expectedTaskVersion <= 0 {
		return nil, nil, domain.ErrInvalidInput("RunAttempt and expected Task version are required")
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	credential, err := loadGuardedWorker(ctx, tx, guard)
	if err != nil {
		return nil, nil, err
	}
	item, err := scanMailbox(tx.QueryRowContext(ctx, `SELECT `+mailboxColumns+`
		FROM mailbox_items WHERE mailbox_item_id=?`, mailboxItemID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("load claimed work item: %w", err)
	}
	if item.Lane != domain.MailboxLaneWork || item.State != domain.MailboxStateClaimed ||
		item.WorkerInstanceID != guard.WorkerInstanceID || item.FencingToken != guard.FencingToken {
		return nil, nil, domain.ErrConflict("work item is not claimed by Worker")
	}
	if item.LeaseUntil == nil || !guard.CheckedAt.Before(*item.LeaseUntil) {
		return nil, nil, domain.ErrLeaseExpired
	}
	if item.TaskID == "" || item.TaskID != run.TaskID {
		return nil, nil, domain.ErrInvalidInput("work item Task does not match RunAttempt")
	}
	task, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE task_id=?`, item.TaskID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, domain.ErrTaskNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	if task.Version != expectedTaskVersion {
		return nil, nil, domain.ErrStaleVersion
	}
	if task.TargetAgentID != guard.AgentID || run.AgentID != guard.AgentID {
		return nil, nil, domain.ErrForbidden("work item is not addressed to Worker Agent")
	}
	if !task.CanBeginAttempt() {
		if task.Status == domain.TaskStatusCancelRequested {
			return nil, nil, domain.ErrTaskCancelRequested
		}
		return nil, nil, domain.ErrInvalidTransition
	}
	run.WorkerInstanceID = guard.WorkerInstanceID
	run.FencingToken = guard.FencingToken
	run.AgentID = guard.AgentID
	run.CreatedAt = normalizeTime(run.CreatedAt, guard.CheckedAt)
	run.UpdatedAt = normalizeTime(run.UpdatedAt, guard.CheckedAt)
	run.StartedAt = normalizeTime(run.StartedAt, guard.CheckedAt)
	if run.LeaseUntil.After(credential.Worker.LeaseUntil) {
		run.LeaseUntil = credential.Worker.LeaseUntil
	}
	if !run.Status.Active() {
		return nil, nil, domain.ErrInvalidInput("new RunAttempt must be active")
	}
	if err := run.Validate(); err != nil {
		return nil, nil, err
	}
	if err := validateJournalForAggregate(taskEvent, "task", task.ID, guard.CheckedAt); err != nil {
		return nil, nil, err
	}
	if err := validateJournalForAggregate(runEvent, "run_attempt", run.ID, guard.CheckedAt); err != nil {
		return nil, nil, err
	}
	if err := validateJournalForAggregate(mailboxEvent, "mailbox_item", item.ID, guard.CheckedAt); err != nil {
		return nil, nil, err
	}
	task.Version++
	task.Status = domain.TaskStatusRunning
	task.UpdatedAt = formatTime(guard.CheckedAt)
	result, err := tx.ExecContext(ctx, `UPDATE tasks SET version=?, status='running', updated_at=?
		WHERE task_id=? AND version=?`, task.Version, task.UpdatedAt, task.ID, expectedTaskVersion)
	if err != nil {
		return nil, nil, fmt.Errorf("mark claimed Task running: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return nil, nil, domain.ErrStaleVersion
	}
	if err := insertRunAttempt(ctx, tx, run); err != nil {
		return nil, nil, err
	}
	result, err = tx.ExecContext(ctx, `UPDATE mailbox_items SET state='accepted', accepted_at=?
		WHERE mailbox_item_id=? AND state='claimed' AND worker_instance_id=? AND fencing_token=? AND lease_until>?`,
		formatTime(guard.CheckedAt), item.ID, guard.WorkerInstanceID, guard.FencingToken, formatTime(guard.CheckedAt))
	if err != nil {
		return nil, nil, fmt.Errorf("accept work item for RunAttempt: %w", err)
	}
	affected, err = result.RowsAffected()
	if err != nil || affected != 1 {
		return nil, nil, domain.ErrConflict("work item accept CAS lost")
	}
	item.State = domain.MailboxStateAccepted
	acceptedAt := guard.CheckedAt
	item.AcceptedAt = &acceptedAt
	if err := r.inject(FaultAfterDelivery); err != nil {
		return nil, nil, err
	}
	for _, event := range []*domain.JournalEvent{taskEvent, runEvent, mailboxEvent} {
		if err := insertJournal(ctx, tx, event); err != nil {
			return nil, nil, err
		}
	}
	if err := r.inject(FaultBeforeCommit); err != nil {
		return nil, nil, err
	}
	if err := commit(tx); err != nil {
		return nil, nil, err
	}
	return task, item, nil
}

func (r *Repository) AppendRunEvents(
	ctx context.Context,
	guard domain.WorkerWriteGuard,
	runID string,
	expectedRunVersion int64,
	events []*domain.JournalEvent,
) error {
	if expectedRunVersion <= 0 || len(events) == 0 {
		return domain.ErrInvalidInput("run version and events are required")
	}
	for _, event := range events {
		if err := validateJournalForAggregate(event, "run_attempt", runID, guard.CheckedAt); err != nil {
			return err
		}
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := loadGuardedWorker(ctx, tx, guard); err != nil {
		return err
	}
	run, err := scanRunAttempt(tx.QueryRowContext(ctx, `SELECT run_id, task_id, agent_id, version,
		status, worker_instance_id, fencing_token, lease_until, execution_spec_version,
		requested_execution_json, resolved_execution_json, adapter_id, backend_id, model,
		reasoning_mode, reasoning_value, started_at, finished_at, result_json, created_at, updated_at
		FROM run_attempts WHERE run_id=?`, runID))
	if err != nil {
		return err
	}
	if run.WorkerInstanceID != guard.WorkerInstanceID || run.AgentID != guard.AgentID || run.FencingToken != guard.FencingToken {
		return domain.ErrFencingRejected
	}
	if run.Version != expectedRunVersion {
		return domain.ErrStaleVersion
	}
	if !run.Status.Active() || !guard.CheckedAt.Before(run.LeaseUntil) {
		return domain.ErrLeaseExpired
	}
	for _, event := range events {
		if err := insertJournal(ctx, tx, event); err != nil {
			return err
		}
	}
	if err := r.inject(FaultBeforeCommit); err != nil {
		return err
	}
	return commit(tx)
}

func (r *Repository) FinishRun(
	ctx context.Context,
	guard domain.WorkerWriteGuard,
	runID string,
	expectedTaskVersion int64,
	expectedRunVersion int64,
	turnResult openruntime.TurnResult,
	binding *domain.SessionBinding,
	expectedBindingVersion int64,
	bindingEvent *domain.JournalEvent,
	taskEvent *domain.JournalEvent,
	runEvent *domain.JournalEvent,
) error {
	if err := turnResult.Validate(); err != nil {
		return err
	}
	if expectedTaskVersion <= 0 || expectedRunVersion <= 0 {
		return domain.ErrInvalidInput("finish requires expected Task and Run versions")
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := loadGuardedWorker(ctx, tx, guard); err != nil {
		return err
	}
	run, err := scanRunAttempt(tx.QueryRowContext(ctx, `SELECT run_id, task_id, agent_id, version,
		status, worker_instance_id, fencing_token, lease_until, execution_spec_version,
		requested_execution_json, resolved_execution_json, adapter_id, backend_id, model,
		reasoning_mode, reasoning_value, started_at, finished_at, result_json, created_at, updated_at
		FROM run_attempts WHERE run_id=?`, runID))
	if err != nil {
		return err
	}
	if run.WorkerInstanceID != guard.WorkerInstanceID || run.AgentID != guard.AgentID || run.FencingToken != guard.FencingToken {
		return domain.ErrFencingRejected
	}
	resultJSON, err := json.Marshal(turnResult)
	if err != nil {
		return fmt.Errorf("encode TurnResult: %w", err)
	}
	if !run.Status.Active() {
		if run.ResultJSON == string(resultJSON) {
			return nil
		}
		return domain.ErrConflict("RunAttempt already finished with a different result")
	}
	if run.Version != expectedRunVersion {
		return domain.ErrStaleVersion
	}
	if !guard.CheckedAt.Before(run.LeaseUntil) {
		return domain.ErrLeaseExpired
	}
	task, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE task_id=?`, run.TaskID))
	if err != nil {
		return err
	}
	// Control-plane inputs may advance the Task version while the physical turn
	// is still running. Message/finish races are safe to reconcile against the
	// latest non-terminal Task; cancellation is handled as a durable intent.
	cancelVersionDelta := task.Status == domain.TaskStatusCancelRequested && task.Version >= expectedTaskVersion+1
	messageVersionDelta := task.Version != expectedTaskVersion && !cancelVersionDelta &&
		(task.Status == domain.TaskStatusRunning || task.Status == domain.TaskStatusWaitingApproval || task.Status == domain.TaskStatusWaitingInput) && task.Version > expectedTaskVersion
	if task.Version != expectedTaskVersion && !cancelVersionDelta && !messageVersionDelta {
		return domain.ErrStaleVersion
	}
	hasPendingMessage := false
	if messageVersionDelta {
		var pending int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(
			SELECT 1 FROM mailbox_items
			WHERE task_id=? AND kind='message' AND state IN ('pending', 'claimed')
		)`, task.ID).Scan(&pending); err != nil {
			return fmt.Errorf("check pending Message at finish: %w", err)
		}
		hasPendingMessage = pending == 1
	}
	runStatus, taskStatus := terminalStatuses(turnResult.Status)
	// Task cancellation is a durable intent. Once it linearizes before finish,
	// a late successful/failed turn must not overwrite that intent or reopen the
	// task; the run itself still records the physical turn result.
	if task.Status == domain.TaskStatusCancelRequested {
		taskStatus = domain.TaskStatusCanceled
	} else if hasPendingMessage && (taskStatus == domain.TaskStatusSucceeded || taskStatus == domain.TaskStatusFailed) {
		// A message committed before finish is a durable follow-up. Keep the
		// Task open so the queued message can be consumed by the next turn.
		taskStatus = domain.TaskStatusWaitingInput
	}
	finishedAt := guard.CheckedAt
	run.Version++
	run.Status = runStatus
	run.FinishedAt = &finishedAt
	run.ResultJSON = string(resultJSON)
	run.UpdatedAt = guard.CheckedAt
	task.Version++
	task.Status = taskStatus
	task.UpdatedAt = formatTime(guard.CheckedAt)
	if turnResult.Result != "" {
		task.Result = &turnResult.Result
	}
	if turnResult.Error != "" {
		task.Error = &turnResult.Error
	}
	if binding != nil {
		var resolved domain.ResolvedExecutionSpec
		if err := json.Unmarshal([]byte(run.ResolvedExecutionJSON), &resolved); err != nil {
			return fmt.Errorf("decode RunAttempt resolved execution for SessionBinding: %w", err)
		}
		binding.CreatedAt = normalizeTime(binding.CreatedAt, guard.CheckedAt)
		binding.UpdatedAt = normalizeTime(binding.UpdatedAt, guard.CheckedAt)
		if expectedBindingVersion == 0 {
			binding.Version = 1
		} else {
			binding.Version = expectedBindingVersion + 1
		}
		if err := binding.Validate(); err != nil {
			return err
		}
		if binding.ContextID != resolved.Spec.Session.ContextID || binding.AgentID != run.AgentID || binding.BackendID != run.BackendID {
			return domain.ErrConflict("SessionBinding does not match RunAttempt")
		}
		if err := validateJournalForAggregate(bindingEvent, "session_binding", binding.ID, guard.CheckedAt); err != nil {
			return err
		}
		if expectedBindingVersion == 0 {
			result, err := tx.ExecContext(ctx, `INSERT INTO session_bindings
				(session_binding_id, context_id, agent_id, backend_id, provider_session_id, state, version, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(context_id, agent_id, backend_id) DO NOTHING`, binding.ID, binding.ContextID,
				binding.AgentID, binding.BackendID, binding.ProviderSessionID, binding.State, binding.Version,
				formatTime(binding.CreatedAt), formatTime(binding.UpdatedAt))
			if err != nil {
				return fmt.Errorf("create SessionBinding while finishing RunAttempt: %w", err)
			}
			affected, err := result.RowsAffected()
			if err != nil || affected != 1 {
				return domain.ErrStaleVersion
			}
		} else {
			result, err := tx.ExecContext(ctx, `UPDATE session_bindings
				SET provider_session_id=?, state=?, version=?, updated_at=?
				WHERE session_binding_id=? AND context_id=? AND agent_id=? AND backend_id=? AND version=?`,
				binding.ProviderSessionID, binding.State, binding.Version, formatTime(binding.UpdatedAt), binding.ID,
				binding.ContextID, binding.AgentID, binding.BackendID, expectedBindingVersion)
			if err != nil {
				return fmt.Errorf("update SessionBinding while finishing RunAttempt: %w", err)
			}
			affected, err := result.RowsAffected()
			if err != nil || affected != 1 {
				return domain.ErrStaleVersion
			}
		}
		if err := insertJournal(ctx, tx, bindingEvent); err != nil {
			return err
		}
	}
	if err := validateJournalForAggregate(taskEvent, "task", task.ID, guard.CheckedAt); err != nil {
		return err
	}
	if err := validateJournalForAggregate(runEvent, "run_attempt", run.ID, guard.CheckedAt); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE run_attempts SET version=?, status=?, finished_at=?,
		result_json=?, updated_at=? WHERE run_id=? AND version=? AND worker_instance_id=? AND fencing_token=?`,
		run.Version, run.Status, formatTime(finishedAt), run.ResultJSON, formatTime(run.UpdatedAt),
		run.ID, expectedRunVersion, guard.WorkerInstanceID, guard.FencingToken)
	if err != nil {
		return fmt.Errorf("finish RunAttempt: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return domain.ErrStaleVersion
	}
	currentTaskVersion := expectedTaskVersion
	if cancelVersionDelta || messageVersionDelta {
		currentTaskVersion = task.Version - 1
	}
	result, err = tx.ExecContext(ctx, `UPDATE tasks SET version=?, status=?, result=?, error=?, updated_at=?
		WHERE task_id=? AND version=?`, task.Version, task.Status, nullableString(task.Result), nullableString(task.Error),
		task.UpdatedAt, task.ID, currentTaskVersion)
	if err != nil {
		return fmt.Errorf("settle Task from RunAttempt: %w", err)
	}
	affected, err = result.RowsAffected()
	if err != nil || affected != 1 {
		return domain.ErrStaleVersion
	}
	for _, event := range []*domain.JournalEvent{runEvent, taskEvent} {
		if err := insertJournal(ctx, tx, event); err != nil {
			return err
		}
	}
	if err := r.inject(FaultBeforeCommit); err != nil {
		return err
	}
	return commit(tx)
}

func terminalStatuses(status openruntime.TurnResultStatus) (domain.RunAttemptStatus, domain.TaskStatus) {
	switch status {
	case openruntime.TurnResultSucceeded:
		return domain.RunAttemptSucceeded, domain.TaskStatusSucceeded
	case openruntime.TurnResultFailed:
		return domain.RunAttemptFailed, domain.TaskStatusFailed
	case openruntime.TurnResultCanceled:
		return domain.RunAttemptCanceled, domain.TaskStatusCanceled
	case openruntime.TurnResultWaitingInput:
		return domain.RunAttemptSucceeded, domain.TaskStatusWaitingInput
	default:
		return domain.RunAttemptUncertain, domain.TaskStatusUncertain
	}
}
