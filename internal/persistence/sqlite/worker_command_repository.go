package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"openagentx/internal/domain"
)

const workerCommandColumns = `worker_command_id, worker_instance_id, generation, kind, state, requested_by, idempotency_key, lease_until, attempts, created_at, claimed_at, applied_at, result`

func scanWorkerCommand(scanner rowScanner) (*domain.WorkerCommand, error) {
	var c domain.WorkerCommand
	var lease, claimed, applied, result sql.NullString
	var created string
	if err := scanner.Scan(&c.ID, &c.WorkerInstanceID, &c.Generation, &c.Kind, &c.State, &c.RequestedBy, &c.IdempotencyKey, &lease, &c.Attempts, &created, &claimed, &applied, &result); err != nil {
		return nil, err
	}
	var err error
	if c.CreatedAt, err = parseTime(created); err != nil {
		return nil, err
	}
	if lease.Valid {
		v, e := parseTime(lease.String)
		if e != nil {
			return nil, e
		}
		c.LeaseUntil = &v
	}
	if claimed.Valid {
		v, e := parseTime(claimed.String)
		if e != nil {
			return nil, e
		}
		c.ClaimedAt = &v
	}
	if applied.Valid {
		v, e := parseTime(applied.String)
		if e != nil {
			return nil, e
		}
		c.AppliedAt = &v
	}
	if result.Valid {
		c.Result = result.String
	}
	return &c, nil
}

func (r *Repository) CreateWorkerCommand(ctx context.Context, command *domain.WorkerCommand, event *domain.JournalEvent) (*domain.WorkerCommand, error) {
	if command == nil {
		return nil, domain.ErrInvalidInput("Worker command is required")
	}
	now := r.now().UTC()
	command.CreatedAt = normalizeTime(command.CreatedAt, now)
	if command.State == "" {
		command.State = domain.WorkerCommandPending
	}
	if err := command.Validate(); err != nil {
		return nil, err
	}
	if event == nil {
		return nil, domain.ErrInvalidInput("Worker command event is required")
	}
	if err := validateJournalForAggregate(event, "worker_command", command.ID, now); err != nil {
		return nil, err
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var existing domain.WorkerCommand
	row := tx.QueryRowContext(ctx, `SELECT `+workerCommandColumns+` FROM worker_commands WHERE requested_by=? AND idempotency_key=?`, command.RequestedBy, command.IdempotencyKey)
	existingPtr, e := scanWorkerCommand(row)
	if e == nil {
		existing = *existingPtr
		if existing.WorkerInstanceID != command.WorkerInstanceID || existing.Kind != command.Kind {
			return nil, domain.ErrIdempotencyConflict
		}
		return &existing, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO worker_commands (worker_command_id,worker_instance_id,generation,kind,state,requested_by,idempotency_key,attempts,created_at) VALUES (?,?,?,?,?,?,?,?,?)`, command.ID, command.WorkerInstanceID, command.Generation, command.Kind, command.State, command.RequestedBy, command.IdempotencyKey, 0, formatTime(command.CreatedAt))
	if err != nil {
		return nil, fmt.Errorf("create Worker command: %w", err)
	}
	if err := insertJournal(ctx, tx, event); err != nil {
		return nil, err
	}
	if err := commit(tx); err != nil {
		return nil, err
	}
	return command, nil
}

func (r *Repository) ClaimWorkerCommand(ctx context.Context, guard domain.WorkerWriteGuard, leaseUntil time.Time, event *domain.JournalEvent) (*domain.WorkerCommand, error) {
	now := r.now().UTC()
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	credential, err := loadGuardedWorker(ctx, tx, guard)
	if err != nil {
		return nil, err
	}
	if leaseUntil.After(credential.Worker.LeaseUntil) {
		leaseUntil = credential.Worker.LeaseUntil
	}
	c, err := scanWorkerCommand(tx.QueryRowContext(ctx, `SELECT `+workerCommandColumns+` FROM worker_commands WHERE worker_instance_id=? AND generation=? AND (state='pending' OR (state='claimed' AND lease_until<=?)) ORDER BY created_at ASC LIMIT 1`, guard.WorkerInstanceID, guard.Generation, formatTime(now)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	until := leaseUntil
	if !until.After(now) {
		return nil, domain.ErrInvalidInput("command lease must be in future")
	}
	claimed := now
	c.State = domain.WorkerCommandClaimed
	c.Attempts++
	c.ClaimedAt = &claimed
	c.LeaseUntil = &until
	res, err := tx.ExecContext(ctx, `UPDATE worker_commands SET state='claimed',lease_until=?,attempts=attempts+1,claimed_at=? WHERE worker_command_id=? AND (state='pending' OR (state='claimed' AND lease_until<=?))`, formatTime(until), formatTime(now), c.ID, formatTime(now))
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, domain.ErrConflict("Worker command claim lost")
	}
	if event != nil {
		if err := validateJournalForAggregate(event, "worker_command", c.ID, now); err != nil {
			return nil, err
		}
		if err := insertJournal(ctx, tx, event); err != nil {
			return nil, err
		}
	}
	if err := commit(tx); err != nil {
		return nil, err
	}
	return c, nil
}

func (r *Repository) AcknowledgeWorkerCommand(ctx context.Context, guard domain.WorkerWriteGuard, commandID string, state domain.WorkerCommandState, result string, event *domain.JournalEvent) error {
	if state != domain.WorkerCommandApplied && state != domain.WorkerCommandFailed {
		return domain.ErrInvalidInput("invalid Worker command state")
	}
	now := r.now().UTC()
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := loadGuardedWorker(ctx, tx, guard); err != nil {
		return err
	}
	// ACK delivery is at-least-once. A retry after a committed terminal ACK is
	// a safe replay when the command/result are identical; do not append a
	// duplicate journal event or surface a spurious claim conflict.
	var commandWorkerID string
	var commandGeneration int64
	var currentState domain.WorkerCommandState
	var currentResult sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT worker_instance_id, generation, state, result
		FROM worker_commands WHERE worker_command_id=?`, commandID).
		Scan(&commandWorkerID, &commandGeneration, &currentState, &currentResult); errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("load Worker command for acknowledgement: %w", err)
	} else if commandWorkerID != guard.WorkerInstanceID || commandGeneration != guard.Generation {
		return domain.ErrForbidden("Worker command is not owned by Worker")
	} else if (currentState == domain.WorkerCommandApplied || currentState == domain.WorkerCommandFailed) &&
		currentState == state && currentResult.String == result {
		return nil
	}
	res, err := tx.ExecContext(ctx, `UPDATE worker_commands SET state=?,result=?,applied_at=? WHERE worker_command_id=? AND worker_instance_id=? AND generation=? AND state='claimed' AND (lease_until IS NULL OR lease_until>?)`, state, result, formatTime(now), commandID, guard.WorkerInstanceID, guard.Generation, formatTime(guard.CheckedAt))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return domain.ErrConflict("Worker command is not claimable")
	}
	if event != nil {
		if err := validateJournalForAggregate(event, "worker_command", commandID, now); err != nil {
			return err
		}
		if err := insertJournal(ctx, tx, event); err != nil {
			return err
		}
	}
	return commit(tx)
}

func (r *Repository) RevokeWorkerLease(ctx context.Context, workerID string, generation int64, event *domain.JournalEvent) (*domain.WorkerInstance, error) {
	now := r.now().UTC()
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var status domain.WorkerStatus
	var token int64
	var agent string
	var transport domain.WorkerTransport
	var principal string
	var caps, heartbeat, lease, started, updated string
	if err := tx.QueryRowContext(ctx, `SELECT agent_id,transport,authenticated_principal,capabilities_json,status,last_heartbeat_at,lease_until,fencing_token,started_at,updated_at FROM worker_instances WHERE worker_instance_id=? AND generation=?`, workerID, generation).Scan(&agent, &transport, &principal, &caps, &status, &heartbeat, &lease, &token, &started, &updated); errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	token++
	res, err := tx.ExecContext(ctx, `UPDATE worker_instances SET status='offline',fencing_token=fencing_token+1,lease_until=?,updated_at=? WHERE worker_instance_id=? AND generation=?`, formatTime(now), formatTime(now), workerID, generation)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, domain.ErrFencingRejected
	}
	if event != nil {
		if err := validateJournalForAggregate(event, "worker_instance", workerID, now); err != nil {
			return nil, err
		}
		if err := insertJournal(ctx, tx, event); err != nil {
			return nil, err
		}
	}
	if err := commit(tx); err != nil {
		return nil, err
	}
	parsedHeartbeat, _ := parseTime(heartbeat)
	parsedStarted, _ := parseTime(started)
	parsedUpdated, _ := parseTime(updated)
	var capabilities []string
	_ = json.Unmarshal([]byte(caps), &capabilities)
	return &domain.WorkerInstance{ID: workerID, AgentID: agent, Generation: generation, Transport: transport, AuthenticatedPrincipal: principal, Capabilities: capabilities, Status: domain.WorkerStatusOffline, LastHeartbeatAt: parsedHeartbeat, LeaseUntil: now, FencingToken: token, StartedAt: parsedStarted, UpdatedAt: parsedUpdated}, nil
}
