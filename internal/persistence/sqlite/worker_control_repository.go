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
) (*domain.WorkerInstance, []domain.NetworkBinding, error) {
	if err := registration.Validate(); err != nil {
		return nil, nil, err
	}
	if len(backends) == 0 {
		return nil, nil, domain.ErrInvalidInput("worker must register at least one Backend")
	}
	seenBackends := make(map[string]struct{}, len(backends))
	for _, backend := range backends {
		if err := backend.Validate(); err != nil {
			return nil, nil, err
		}
		if _, exists := seenBackends[backend.BackendID]; exists {
			return nil, nil, domain.ErrInvalidInput("worker Backend IDs must be unique")
		}
		seenBackends[backend.BackendID] = struct{}{}
	}
	now := r.now().UTC()
	if !registration.TokenExpiresAt.After(now) || !registration.LeaseUntil.After(now) {
		return nil, nil, domain.ErrInvalidInput("worker token and lease must expire in the future")
	}
	if err := validateJournalForAggregate(event, "worker_instance", registration.WorkerInstanceID, now); err != nil {
		return nil, nil, err
	}

	capabilities := append([]string(nil), registration.Capabilities...)
	sort.Strings(capabilities)
	capabilitiesJSON, err := json.Marshal(capabilities)
	if err != nil {
		return nil, nil, fmt.Errorf("encode Worker capabilities: %w", err)
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	var agentStatus domain.AgentIdentityStatus
	if err := tx.QueryRowContext(ctx, `SELECT status FROM agents WHERE agent_id=?`, registration.AgentID).Scan(&agentStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, domain.ErrAgentNotFound
		}
		return nil, nil, fmt.Errorf("load Worker Agent identity: %w", err)
	}
	if agentStatus != domain.AgentIdentityActive {
		return nil, nil, domain.ErrAgentNotReady
	}
	var validActive int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM worker_instances
		WHERE agent_id = ? AND status IN ('bootstrapping','online','degraded','draining')
		AND lease_until > ?`, registration.AgentID, formatTime(now)).Scan(&validActive); err != nil {
		return nil, nil, fmt.Errorf("inspect Active Worker: %w", err)
	}
	if validActive != 0 {
		return nil, nil, domain.ErrConflict("logical Agent already has a valid Active Worker")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE worker_instances
		SET status='offline', fencing_token=fencing_token+1, updated_at=?
		WHERE agent_id=? AND status IN ('bootstrapping','online','degraded','draining')
		AND lease_until <= ?`, formatTime(now), registration.AgentID, formatTime(now)); err != nil {
		return nil, nil, fmt.Errorf("expire previous Worker: %w", err)
	}
	var generation, fencingToken int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(generation),0)+1,
		COALESCE(MAX(fencing_token),0)+1 FROM worker_instances WHERE agent_id=?`, registration.AgentID).Scan(
		&generation, &fencingToken); err != nil {
		return nil, nil, fmt.Errorf("allocate Worker generation/fencing: %w", err)
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
		return nil, nil, fmt.Errorf("register Worker: %w", err)
	}
	for _, backend := range backends {
		descriptorJSON, err := json.Marshal(backend.Descriptor)
		if err != nil {
			return nil, nil, fmt.Errorf("encode Backend descriptor: %w", err)
		}
		network := backend.Network
		if network.IsZero() {
			network.Mode = domain.NetworkInherit
		}
		networkJSON, err := json.Marshal(network)
		if err != nil {
			return nil, nil, fmt.Errorf("encode Backend network policy: %w", err)
		}
		registrationID := worker.ID + ":" + backend.BackendID
		if _, err := tx.ExecContext(ctx, `INSERT INTO runtime_backend_registrations (
			runtime_backend_registration_id, worker_instance_id, adapter_id, backend_id,
			descriptor_json, network_json, health, observed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, registrationID, worker.ID, backend.Descriptor.AdapterID,
			backend.BackendID, string(descriptorJSON), string(networkJSON), backend.Health, formatTime(now)); err != nil {
			return nil, nil, fmt.Errorf("register Backend %s: %w", backend.BackendID, err)
		}
	}
	if err := r.inject(FaultAfterStateWrite); err != nil {
		return nil, nil, err
	}
	if err := insertJournal(ctx, tx, event); err != nil {
		return nil, nil, err
	}
	bindings, err := listNetworkBindings(ctx, tx, registration.AgentID)
	if err != nil {
		return nil, nil, fmt.Errorf("load initial Worker network bindings: %w", err)
	}
	if err := r.inject(FaultBeforeCommit); err != nil {
		return nil, nil, err
	}
	if err := commit(tx); err != nil {
		return nil, nil, err
	}
	return worker, bindings, nil
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
	applications []domain.NetworkBindingApplication,
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
	for i := range applications {
		if err := applications[i].Validate(); err != nil {
			return nil, err
		}
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
	for _, application := range applications {
		var descriptorJSON string
		if err := tx.QueryRowContext(ctx, `SELECT descriptor_json FROM runtime_backend_registrations
			WHERE worker_instance_id=? AND backend_id=?`, guard.WorkerInstanceID, application.BackendID).
			Scan(&descriptorJSON); errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrInvalidInput("heartbeat references an unregistered network Backend")
		} else if err != nil {
			return nil, fmt.Errorf("load network Backend registration: %w", err)
		}
		var descriptor openruntime.AdapterDescriptor
		if err := json.Unmarshal([]byte(descriptorJSON), &descriptor); err != nil {
			return nil, fmt.Errorf("decode network Backend descriptor: %w", err)
		}
		supportsNamedProfile := false
		for _, mode := range descriptor.NetworkModes {
			if mode == string(domain.NetworkNamedProfile) {
				supportsNamedProfile = true
				break
			}
		}
		if !supportsNamedProfile {
			return nil, domain.ErrUnsupportedCapability
		}
		var bindingMode, profileID, desiredStatus, appliedWorkerID, appliedProfileID, diagnostic string
		var profileVersion, revision, appliedGeneration, appliedProfileVersion, appliedRevision int64
		err := tx.QueryRowContext(ctx, `SELECT mode,COALESCE(profile_id,''),COALESCE(profile_version,0),version,desired_status,
			COALESCE(applied_worker_id,''), COALESCE(applied_generation,0),
			COALESCE(applied_profile_id,''), COALESCE(applied_profile_version,0), COALESCE(applied_binding_revision,0), COALESCE(diagnostic,'')
			FROM network_profile_bindings WHERE agent_id=? AND backend_id=?`, guard.AgentID, application.BackendID).
			Scan(&bindingMode, &profileID, &profileVersion, &revision, &desiredStatus, &appliedWorkerID,
				&appliedGeneration, &appliedProfileID, &appliedProfileVersion, &appliedRevision, &diagnostic)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrInvalidInput("heartbeat references an unbound network Backend")
		}
		if err != nil {
			return nil, fmt.Errorf("load network binding acknowledgement target: %w", err)
		}
		if bindingMode != string(domain.NetworkNamedProfile) || profileID != application.ProfileID || profileVersion != application.ProfileVersion || revision != application.BindingRevision {
			// A legitimate Worker may race a rebind. Its old acknowledgement is
			// rejected without rolling back the heartbeat or stopping the control loop.
			continue
		}
		if application.State == "failed" && desiredStatus == "failed" && diagnostic == application.Diagnostic {
			continue
		}
		if application.State == "applied" && desiredStatus == "applied" &&
			appliedWorkerID == guard.WorkerInstanceID && appliedGeneration == guard.Generation &&
			appliedProfileID == application.ProfileID &&
			appliedProfileVersion == application.ProfileVersion && appliedRevision == application.BindingRevision &&
			diagnostic == application.Diagnostic {
			continue
		}
		if err := validateJournalForAggregate(application.Event, "network_binding", guard.AgentID+":"+application.BackendID, guard.CheckedAt); err != nil {
			return nil, err
		}
		var result sql.Result
		if application.State == "applied" {
			result, err = tx.ExecContext(ctx, `UPDATE network_profile_bindings SET desired_status='applied',
				applied_worker_id=?, applied_generation=?, applied_mode='named_profile',applied_profile_id=?, applied_profile_version=?, applied_policy_version=NULL,applied_binding_revision=?,
				diagnostic=NULL, updated_at=? WHERE agent_id=? AND backend_id=? AND profile_id=? AND profile_version=? AND version=?`,
				guard.WorkerInstanceID, guard.Generation, application.ProfileID, application.ProfileVersion, application.BindingRevision,
				formatTime(guard.CheckedAt), guard.AgentID, application.BackendID, application.ProfileID,
				application.ProfileVersion, application.BindingRevision)
		} else {
			result, err = tx.ExecContext(ctx, `UPDATE network_profile_bindings SET desired_status='failed',
				diagnostic=?, updated_at=? WHERE agent_id=? AND backend_id=? AND profile_id=? AND profile_version=? AND version=?`,
				application.Diagnostic, formatTime(guard.CheckedAt), guard.AgentID, application.BackendID,
				application.ProfileID, application.ProfileVersion, application.BindingRevision)
		}
		if err != nil {
			return nil, fmt.Errorf("acknowledge network binding: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil || affected != 1 {
			return nil, domain.ErrConflict("network binding acknowledgement CAS lost")
		}
		if err := insertJournal(ctx, tx, application.Event); err != nil {
			return nil, err
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
	if item.Kind == domain.MailboxKindApproval {
		decisionState := domain.ApprovalDecisionSuperseded
		if outcome == domain.MailboxStateAccepted {
			decisionState = domain.ApprovalDecisionApplied
		}
		result, err := tx.ExecContext(ctx, `UPDATE approval_decisions SET state=?
			WHERE approval_decision_id=? AND approval_request_id=? AND state='persisted'`,
			decisionState, item.ApprovalDecisionID, item.ApprovalRequestID)
		if err != nil {
			return fmt.Errorf("settle native Approval decision: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil || affected != 1 {
			return domain.ErrConflict("native Approval decision settlement CAS lost")
		}
		if outcome != domain.MailboxStateAccepted {
			if _, err := tx.ExecContext(ctx, `UPDATE approval_requests SET state='stale'
				WHERE approval_request_id=? AND mode='native' AND state IN ('approved','rejected')`,
				item.ApprovalRequestID); err != nil {
				return fmt.Errorf("mark native Approval request stale: %w", err)
			}
		}
		if outcome == domain.MailboxStateAccepted {
			// Applying a native approval is the Task-level linearization point:
			// reopen exactly the waiting Task in this same transaction. A
			// superseded approval never reaches this branch and cannot reopen it.
			task, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE task_id=?`, item.TaskID))
			if err != nil {
				return fmt.Errorf("load native Approval Task: %w", err)
			}
			if task.Status == domain.TaskStatusWaitingApproval {
				previousVersion := task.Version
				task.Version++
				task.Status = domain.TaskStatusRunning
				task.UpdatedAt = formatTime(guard.CheckedAt)
				result, err := tx.ExecContext(ctx, `UPDATE tasks SET version=?, status='running', updated_at=? WHERE task_id=? AND version=? AND status='waiting_approval'`, task.Version, task.UpdatedAt, task.ID, previousVersion)
				if err != nil {
					return fmt.Errorf("resume native Approval Task: %w", err)
				}
				affected, _ := result.RowsAffected()
				if affected != 1 {
					return domain.ErrStaleVersion
				}
				taskPayload, _ := json.Marshal(map[string]string{"mailbox_item_id": item.ID})
				taskEvent := &domain.JournalEvent{ID: item.ID + "-task-running", OrganizationID: task.OrganizationID, AggregateType: "task", AggregateID: task.ID, EventType: "task.running", ActorPrincipalID: guard.PrincipalID, Payload: taskPayload, CreatedAt: guard.CheckedAt}
				if err := validateJournalForAggregate(taskEvent, "task", task.ID, guard.CheckedAt); err != nil {
					return err
				}
				if err := insertJournal(ctx, tx, taskEvent); err != nil {
					return err
				}
			} else if task.Status != domain.TaskStatusRunning && !task.IsTerminal() {
				return domain.ErrConflict("native Approval Task is not waiting for approval")
			}
		}
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
