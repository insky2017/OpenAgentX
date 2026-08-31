package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

func (r *Repository) RegisterWorker(
	ctx context.Context,
	registration domain.WorkerRegistration,
	backends []openruntime.BackendRegistration,
	event *domain.JournalEvent,
) (*domain.WorkerInstance, error) {
	if err := registration.Validate(); err != nil {
		return nil, err
	}
	if len(backends) == 0 {
		return nil, domain.ErrInvalidInput("worker must register at least one Backend")
	}
	seenBackends := make(map[string]struct{}, len(backends))
	for _, backend := range backends {
		if err := backend.Validate(); err != nil {
			return nil, err
		}
		if _, exists := seenBackends[backend.BackendID]; exists {
			return nil, domain.ErrInvalidInput("worker Backend IDs must be unique")
		}
		seenBackends[backend.BackendID] = struct{}{}
	}
	now := r.now().UTC()
	if !registration.TokenExpiresAt.After(now) || !registration.LeaseUntil.After(now) {
		return nil, domain.ErrInvalidInput("worker token and lease must expire in the future")
	}
	if err := validateJournalForAggregate(event, "worker_instance", registration.WorkerInstanceID, now); err != nil {
		return nil, err
	}

	capabilities := append([]string(nil), registration.Capabilities...)
	sort.Strings(capabilities)
	capabilitiesJSON, err := json.Marshal(capabilities)
	if err != nil {
		return nil, fmt.Errorf("encode Worker capabilities: %w", err)
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var agentStatus domain.AgentIdentityStatus
	if err := tx.QueryRowContext(ctx, `SELECT status FROM agents WHERE agent_id=?`, registration.AgentID).Scan(&agentStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrAgentNotFound
		}
		return nil, fmt.Errorf("load Worker Agent identity: %w", err)
	}
	if agentStatus != domain.AgentIdentityActive {
		return nil, domain.ErrAgentNotReady
	}
	var validActive int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM worker_instances
		WHERE agent_id = ? AND status IN ('bootstrapping','online','degraded','draining')
		AND lease_until > ?`, registration.AgentID, formatTime(now)).Scan(&validActive); err != nil {
		return nil, fmt.Errorf("inspect Active Worker: %w", err)
	}
	if validActive != 0 {
		return nil, domain.ErrConflict("logical Agent already has a valid Active Worker")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE worker_instances
		SET status='offline', fencing_token=fencing_token+1, updated_at=?
		WHERE agent_id=? AND status IN ('bootstrapping','online','degraded','draining')
		AND lease_until <= ?`, formatTime(now), registration.AgentID, formatTime(now)); err != nil {
		return nil, fmt.Errorf("expire previous Worker: %w", err)
	}
	var generation, fencingToken int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(generation),0)+1,
		COALESCE(MAX(fencing_token),0)+1 FROM worker_instances WHERE agent_id=?`, registration.AgentID).Scan(
		&generation, &fencingToken); err != nil {
		return nil, fmt.Errorf("allocate Worker generation/fencing: %w", err)
	}
	worker := &domain.WorkerInstance{
		ID: registration.WorkerInstanceID, AgentID: registration.AgentID, Generation: generation,
		Transport: registration.Transport, AuthenticatedPrincipal: registration.PrincipalID,
		Capabilities: capabilities, Status: domain.WorkerStatusBootstrapping,
		LastHeartbeatAt: now, LeaseUntil: registration.LeaseUntil.UTC(), FencingToken: fencingToken,
		StartedAt: now, UpdatedAt: now,
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO worker_instances (
		worker_instance_id, agent_id, generation, transport, authenticated_principal,
		capabilities_json, status, session_token_digest, session_token_expires_at,
		last_heartbeat_at, lease_until, fencing_token, started_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, worker.ID, worker.AgentID,
		worker.Generation, worker.Transport, worker.AuthenticatedPrincipal, string(capabilitiesJSON),
		worker.Status, registration.SessionTokenDigest, formatTime(registration.TokenExpiresAt),
		formatTime(worker.LastHeartbeatAt), formatTime(worker.LeaseUntil), worker.FencingToken,
		formatTime(worker.StartedAt), formatTime(worker.UpdatedAt)); err != nil {
		return nil, fmt.Errorf("register Worker: %w", err)
	}
	for _, backend := range backends {
		descriptorJSON, err := json.Marshal(backend.Descriptor)
		if err != nil {
			return nil, fmt.Errorf("encode Backend descriptor: %w", err)
		}
		registrationID := worker.ID + ":" + backend.BackendID
		if _, err := tx.ExecContext(ctx, `INSERT INTO runtime_backend_registrations (
			runtime_backend_registration_id, worker_instance_id, adapter_id, backend_id,
			descriptor_json, health, observed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)`, registrationID, worker.ID, backend.Descriptor.AdapterID,
			backend.BackendID, string(descriptorJSON), backend.Health, formatTime(now)); err != nil {
			return nil, fmt.Errorf("register Backend %s: %w", backend.BackendID, err)
		}
	}
	if err := r.inject(FaultAfterStateWrite); err != nil {
		return nil, err
	}
	if err := insertJournal(ctx, tx, event); err != nil {
		return nil, err
	}
	if err := r.inject(FaultBeforeCommit); err != nil {
		return nil, err
	}
	if err := commit(tx); err != nil {
		return nil, err
	}
	return worker, nil
}

func (r *Repository) GetWorkerCredential(ctx context.Context, workerID string) (*domain.WorkerCredential, error) {
	credential, err := scanWorkerCredential(r.db.QueryRowContext(ctx, `SELECT
		worker_instance_id, agent_id, generation, transport, authenticated_principal,
		capabilities_json, status, last_heartbeat_at, lease_until, fencing_token,
		started_at, updated_at, session_token_digest, session_token_expires_at
		FROM worker_instances WHERE worker_instance_id=?`, workerID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get Worker credential: %w", err)
	}
	return credential, nil
}

func scanWorkerCredential(scanner rowScanner) (*domain.WorkerCredential, error) {
	var credential domain.WorkerCredential
	var capabilitiesJSON string
	var lastHeartbeat, leaseUntil, startedAt, updatedAt, tokenExpires string
	err := scanner.Scan(&credential.Worker.ID, &credential.Worker.AgentID, &credential.Worker.Generation,
		&credential.Worker.Transport, &credential.Worker.AuthenticatedPrincipal, &capabilitiesJSON,
		&credential.Worker.Status, &lastHeartbeat, &leaseUntil, &credential.Worker.FencingToken,
		&startedAt, &updatedAt, &credential.SessionTokenDigest, &tokenExpires)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(capabilitiesJSON), &credential.Worker.Capabilities); err != nil {
		return nil, fmt.Errorf("decode Worker capabilities: %w", err)
	}
	var parseErr error
	if credential.Worker.LastHeartbeatAt, parseErr = parseTime(lastHeartbeat); parseErr != nil {
		return nil, parseErr
	}
	if credential.Worker.LeaseUntil, parseErr = parseTime(leaseUntil); parseErr != nil {
		return nil, parseErr
	}
	if credential.Worker.StartedAt, parseErr = parseTime(startedAt); parseErr != nil {
		return nil, parseErr
	}
	if credential.Worker.UpdatedAt, parseErr = parseTime(updatedAt); parseErr != nil {
		return nil, parseErr
	}
	if credential.TokenExpiresAt, parseErr = parseTime(tokenExpires); parseErr != nil {
		return nil, parseErr
	}
	return &credential, nil
}

func loadGuardedWorker(ctx context.Context, tx *sql.Tx, guard domain.WorkerWriteGuard) (*domain.WorkerCredential, error) {
	credential, err := scanWorkerCredential(tx.QueryRowContext(ctx, `SELECT
		worker_instance_id, agent_id, generation, transport, authenticated_principal,
		capabilities_json, status, last_heartbeat_at, lease_until, fencing_token,
		started_at, updated_at, session_token_digest, session_token_expires_at
		FROM worker_instances WHERE worker_instance_id=?`, guard.WorkerInstanceID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrUnauthorized
	}
	if err != nil {
		return nil, fmt.Errorf("load guarded Worker: %w", err)
	}
	if err := credential.Authorize(guard); err != nil {
		return nil, err
	}
	return credential, nil
}

func (r *Repository) HeartbeatWorker(
	ctx context.Context,
	guard domain.WorkerWriteGuard,
	status domain.WorkerStatus,
	backendHealth map[string]openruntime.BackendHealth,
	leaseUntil time.Time,
	tokenExpiresAt time.Time,
	event *domain.JournalEvent,
) (*domain.WorkerInstance, error) {
	if status != domain.WorkerStatusOnline && status != domain.WorkerStatusDegraded && status != domain.WorkerStatusDraining {
		return nil, domain.ErrInvalidInput("heartbeat status must be online, degraded, or draining")
	}
	if !leaseUntil.After(guard.CheckedAt) || !tokenExpiresAt.After(guard.CheckedAt) {
		return nil, domain.ErrInvalidInput("renewed Worker lease and token expiry must be in the future")
	}
	if err := validateJournalForAggregate(event, "worker_instance", guard.WorkerInstanceID, guard.CheckedAt); err != nil {
		return nil, err
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	credential, err := loadGuardedWorker(ctx, tx, guard)
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE worker_instances SET status=?, last_heartbeat_at=?,
		lease_until=?, session_token_expires_at=?, updated_at=?
		WHERE worker_instance_id=? AND generation=? AND fencing_token=?`,
		status, formatTime(guard.CheckedAt), formatTime(leaseUntil), formatTime(tokenExpiresAt), formatTime(guard.CheckedAt),
		guard.WorkerInstanceID, guard.Generation, guard.FencingToken)
	if err != nil {
		return nil, fmt.Errorf("heartbeat Worker: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return nil, domain.ErrFencingRejected
	}
	if _, err := tx.ExecContext(ctx, `UPDATE run_attempts
		SET lease_until=CASE WHEN lease_until < ? THEN ? ELSE lease_until END, updated_at=?
		WHERE worker_instance_id=? AND fencing_token=?
		AND status IN ('starting','running','waiting_approval','finishing')`,
		formatTime(leaseUntil), formatTime(leaseUntil), formatTime(guard.CheckedAt),
		guard.WorkerInstanceID, guard.FencingToken); err != nil {
		return nil, fmt.Errorf("renew Active RunAttempt lease: %w", err)
	}
	for backendID, health := range backendHealth {
		if !health.Valid() {
			return nil, domain.ErrInvalidInput("unsupported Backend health")
		}
		result, err := tx.ExecContext(ctx, `UPDATE runtime_backend_registrations
			SET health=?, observed_at=? WHERE worker_instance_id=? AND backend_id=?`,
			health, formatTime(guard.CheckedAt), guard.WorkerInstanceID, backendID)
		if err != nil {
			return nil, fmt.Errorf("update Backend health: %w", err)
		}
		count, err := result.RowsAffected()
		if err != nil || count != 1 {
			return nil, domain.ErrInvalidInput("heartbeat references an unregistered Backend")
		}
	}
	if err := insertJournal(ctx, tx, event); err != nil {
		return nil, err
	}
	if err := r.inject(FaultBeforeCommit); err != nil {
		return nil, err
	}
	if err := commit(tx); err != nil {
		return nil, err
	}
	credential.Worker.Status = status
	credential.Worker.LastHeartbeatAt = guard.CheckedAt
	credential.Worker.LeaseUntil = leaseUntil
	credential.Worker.UpdatedAt = guard.CheckedAt
	credential.TokenExpiresAt = tokenExpiresAt
	return &credential.Worker, nil
}

func (r *Repository) TryClaimMailbox(
	ctx context.Context,
	guard domain.WorkerWriteGuard,
	workCapacity int,
	claimUntil time.Time,
	event *domain.JournalEvent,
) (*domain.MailboxItem, error) {
	if workCapacity < 0 || !claimUntil.After(guard.CheckedAt) {
		return nil, domain.ErrInvalidInput("invalid work capacity or claim lease")
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	credential, err := loadGuardedWorker(ctx, tx, guard)
	if err != nil {
		return nil, err
	}
	if credential.Worker.Status == domain.WorkerStatusBootstrapping {
		return nil, domain.ErrAgentNotReady
	}
	if credential.Worker.Status == domain.WorkerStatusDraining {
		workCapacity = 0
	}
	if claimUntil.After(credential.Worker.LeaseUntil) {
		claimUntil = credential.Worker.LeaseUntil
	}

	item, err := scanMailbox(tx.QueryRowContext(ctx, `SELECT `+mailboxColumns+`
		FROM mailbox_items WHERE target_agent_id=? AND state='claimed'
		AND worker_instance_id=? AND fencing_token=? AND lease_until>?
		ORDER BY CASE lane WHEN 'control' THEN 0 ELSE 1 END ASC, sequence ASC LIMIT 1`,
		guard.AgentID, guard.WorkerInstanceID, guard.FencingToken, formatTime(guard.CheckedAt)))
	if err == nil {
		return item, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("load existing mailbox claim: %w", err)
	}

	item, err = scanMailbox(tx.QueryRowContext(ctx, `SELECT `+mailboxColumns+`
		FROM mailbox_items WHERE target_agent_id=?
		AND (state='pending' OR (state='claimed' AND lease_until<=?))
		AND (lane='control' OR (
			lane='work' AND ?>0 AND NOT EXISTS (
				SELECT 1 FROM run_attempts WHERE agent_id=?
				AND status IN ('starting','running','waiting_approval','finishing')
			)
		))
		ORDER BY CASE lane WHEN 'control' THEN 0 ELSE 1 END ASC, sequence ASC LIMIT 1`,
		guard.AgentID, formatTime(guard.CheckedAt), workCapacity, guard.AgentID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select mailbox claim: %w", err)
	}
	result, err := tx.ExecContext(ctx, `UPDATE mailbox_items SET state='claimed', worker_instance_id=?,
		fencing_token=?, lease_until=?, attempts=attempts+1
		WHERE mailbox_item_id=? AND (state='pending' OR (state='claimed' AND lease_until<=?))`,
		guard.WorkerInstanceID, guard.FencingToken, formatTime(claimUntil), item.ID, formatTime(guard.CheckedAt))
	if err != nil {
		return nil, fmt.Errorf("claim mailbox item: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return nil, domain.ErrConflict("mailbox item claim lost")
	}
	item.State = domain.MailboxStateClaimed
	item.WorkerInstanceID = guard.WorkerInstanceID
	item.FencingToken = guard.FencingToken
	item.LeaseUntil = &claimUntil
	item.Attempts++
	if err := validateJournalForAggregate(event, "mailbox_item", item.ID, guard.CheckedAt); err != nil {
		return nil, err
	}
	if err := insertJournal(ctx, tx, event); err != nil {
		return nil, err
	}
	if err := r.inject(FaultBeforeCommit); err != nil {
		return nil, err
	}
	if err := commit(tx); err != nil {
		return nil, err
	}
	return item, nil
}

func (r *Repository) AcceptMailboxItem(
	ctx context.Context,
	guard domain.WorkerWriteGuard,
	itemID string,
	expected domain.MailboxState,
	outcome domain.MailboxState,
	event *domain.JournalEvent,
) error {
	if expected != domain.MailboxStateClaimed {
		return domain.ErrInvalidInput("mailbox accept requires claimed expected state")
	}
	if outcome != domain.MailboxStateAccepted && outcome != domain.MailboxStateSuperseded && outcome != domain.MailboxStateFailed {
		return domain.ErrInvalidInput("invalid mailbox terminal outcome")
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := loadGuardedWorker(ctx, tx, guard); err != nil {
		return err
	}
	item, err := scanMailbox(tx.QueryRowContext(ctx, `SELECT `+mailboxColumns+`
		FROM mailbox_items WHERE mailbox_item_id=?`, itemID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return err
	}
	if expected == domain.MailboxStateClaimed && outcome == domain.MailboxStateSuperseded &&
		item.Kind == domain.MailboxKindMessage && item.Lane == domain.MailboxLaneWork {
		var deferred int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(
			SELECT 1 FROM event_journal WHERE aggregate_type='mailbox_item' AND aggregate_id=?
			AND event_type='mailbox.message_deferred'
		)`, item.ID).Scan(&deferred); err != nil {
			return fmt.Errorf("check idempotent Message defer: %w", err)
		}
		if deferred == 1 {
			return nil
		}
	}
	if item.State == outcome && item.TargetAgentID == guard.AgentID &&
		item.WorkerInstanceID == guard.WorkerInstanceID && item.FencingToken == guard.FencingToken {
		return nil
	}
	if item.State != expected || item.WorkerInstanceID != guard.WorkerInstanceID || item.FencingToken != guard.FencingToken {
		return domain.ErrConflict("mailbox item is not owned by Worker")
	}
	if item.LeaseUntil == nil || !guard.CheckedAt.Before(*item.LeaseUntil) {
		return domain.ErrLeaseExpired
	}
	deferMessage := false
	if outcome == domain.MailboxStateSuperseded && item.Kind == domain.MailboxKindMessage && item.Lane == domain.MailboxLaneControl {
		var status domain.RunAttemptStatus
		var version int64
		runErr := tx.QueryRowContext(ctx, `SELECT status, version FROM run_attempts WHERE run_id=?`, item.TargetRunID).Scan(&status, &version)
		if errors.Is(runErr, sql.ErrNoRows) || (runErr == nil && (!status.Active() || version != item.ExpectedRunVersion)) {
			var taskStatus domain.TaskStatus
			taskErr := tx.QueryRowContext(ctx, `SELECT status FROM tasks WHERE task_id=?`, item.TaskID).Scan(&taskStatus)
			if taskErr != nil {
				return fmt.Errorf("inspect superseded Message Task: %w", taskErr)
			}
			deferMessage = taskStatus != domain.TaskStatusCancelRequested &&
				taskStatus != domain.TaskStatusSucceeded && taskStatus != domain.TaskStatusFailed &&
				taskStatus != domain.TaskStatusCanceled && taskStatus != domain.TaskStatusUncertain
		} else if runErr != nil {
			return fmt.Errorf("inspect superseded Message target RunAttempt: %w", runErr)
		}
	}
	if deferMessage {
		// A native steer that loses its RunAttempt CAS remains a valid business
		// Message. Requeue the same item atomically for the next turn; Approval
		// and Cancel have narrower authority and must never take this path.
		result, err := tx.ExecContext(ctx, `UPDATE mailbox_items SET lane='work', state='pending',
			target_run_id=NULL, expected_run_version=NULL, worker_instance_id=NULL,
			fencing_token=NULL, lease_until=NULL, accepted_at=NULL
			WHERE mailbox_item_id=? AND state=? AND worker_instance_id=? AND fencing_token=? AND lease_until>?`,
			itemID, expected, guard.WorkerInstanceID, guard.FencingToken, formatTime(guard.CheckedAt))
		if err != nil {
			return fmt.Errorf("defer superseded Message mailbox item: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil || affected != 1 {
			return domain.ErrConflict("message mailbox defer CAS lost")
		}
		if event == nil {
			return domain.ErrInvalidInput("message defer event is required")
		}
		event.EventType = "mailbox.message_deferred"
		if err := validateJournalForAggregate(event, "mailbox_item", itemID, guard.CheckedAt); err != nil {
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
	result, err := tx.ExecContext(ctx, `UPDATE mailbox_items SET state=?, accepted_at=?
		WHERE mailbox_item_id=? AND state=? AND worker_instance_id=? AND fencing_token=? AND lease_until>?`,
		outcome, formatTime(guard.CheckedAt), itemID, expected, guard.WorkerInstanceID,
		guard.FencingToken, formatTime(guard.CheckedAt))
	if err != nil {
		return fmt.Errorf("accept mailbox item: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return domain.ErrConflict("mailbox accept CAS lost")
	}
	if err := validateJournalForAggregate(event, "mailbox_item", itemID, guard.CheckedAt); err != nil {
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
