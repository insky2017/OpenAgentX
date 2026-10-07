package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"openagentx/internal/domain"
)

type sessionQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

const agentSessionColumns = `agent_id,backend_id,provider_session_id,COALESCE(context_task_id,''),version,COALESCE(pending_task_id,''),updated_at`

func scanAgentSession(row rowScanner) (*domain.AgentSession, error) {
	var s domain.AgentSession
	var at string
	err := row.Scan(&s.AgentID, &s.BackendID, &s.ThreadID, &s.ContextTaskID, &s.Version, &s.PendingTaskID, &at)
	if err != nil {
		return nil, err
	}
	s.UpdatedAt, err = parseTime(at)
	return &s, err
}
func readAgentSession(ctx context.Context, q sessionQuery, agent, backend string) (*domain.AgentSession, error) {
	s, err := scanAgentSession(q.QueryRowContext(ctx, `SELECT `+agentSessionColumns+` FROM agent_sessions WHERE agent_id=? AND backend_id=?`, agent, backend))
	if !errors.Is(err, sql.ErrNoRows) {
		return s, err
	}
	// Bootstrap is read-only. Once an explicit row exists, failed candidate Tasks
	// and legacy config can never move the authoritative pointer backwards.
	s = &domain.AgentSession{AgentID: agent, BackendID: backend}
	var at string
	err = q.QueryRowContext(ctx, `SELECT b.thread_id,b.managed_context_task_id,b.updated_at FROM external_session_bindings b JOIN session_bindings s ON s.context_id=b.managed_context_task_id AND s.agent_id=b.agent_id AND s.backend_id=b.managed_backend_id AND s.provider_session_id=b.thread_id AND s.state='active' WHERE b.agent_id=? AND b.managed_backend_id=? AND b.mode='managed' AND b.state='active'`, agent, backend).Scan(&s.ThreadID, &s.ContextTaskID, &at)
	if errors.Is(err, sql.ErrNoRows) {
		err = q.QueryRowContext(ctx, `SELECT s.provider_session_id,s.context_id,s.updated_at FROM session_bindings s JOIN tasks t ON t.task_id=s.context_id AND t.target_agent_id=s.agent_id WHERE s.agent_id=? AND s.backend_id=? AND s.state='active' AND EXISTS(SELECT 1 FROM run_attempts r WHERE r.task_id=s.context_id AND r.backend_id=s.backend_id AND (json_extract(NULLIF(r.result_json,''),'$.provider_session_id')=s.provider_session_id OR EXISTS(SELECT 1 FROM event_journal e WHERE e.aggregate_id=r.run_id AND e.event_type='runtime.session.bound'))) ORDER BY COALESCE((SELECT MAX(r.started_at) FROM run_attempts r WHERE r.task_id=s.context_id AND r.backend_id=s.backend_id),'') DESC,s.updated_at DESC,s.context_id DESC LIMIT 1`, agent, backend).Scan(&s.ThreadID, &s.ContextTaskID, &at)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	s.UpdatedAt, err = parseTime(at)
	return s, err
}
func (r *Repository) ReadAgentSession(ctx context.Context, agent, backend string) (*domain.AgentSession, error) {
	var exists int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agents WHERE agent_id=?`, agent).Scan(&exists); err != nil {
		return nil, err
	}
	if exists == 0 {
		return nil, domain.ErrAgentNotFound
	}
	return readAgentSession(ctx, r.db, agent, backend)
}
func (r *Repository) GetSessionHandoff(ctx context.Context, taskID string) (*domain.AgentSession, error) {
	s, err := scanAgentSession(r.db.QueryRowContext(ctx, `SELECT `+agentSessionColumns+` FROM agent_sessions WHERE pending_task_id=?`, taskID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return s, err
}
func sessionGate(ctx context.Context, tx *sql.Tx, agent, allowedTask string) error {
	var pending string
	err := tx.QueryRowContext(ctx, `SELECT pending_task_id FROM agent_sessions WHERE agent_id=? AND pending_task_id IS NOT NULL LIMIT 1`, agent).Scan(&pending)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if pending != allowedTask {
		return domain.ErrConflict("Agent session handoff is pending; wait for its Task before submitting work")
	}
	return nil
}
func validateSessionThread(ctx context.Context, tx *sql.Tx, agent, backend, thread string) error {
	if err := sessionGate(ctx, tx, agent, ""); err != nil {
		return err
	}
	s, err := readAgentSession(ctx, tx, agent, backend)
	if err != nil {
		return err
	}
	if s.ThreadID != "" && s.ThreadID != thread {
		return domain.ErrConflict("Agent session changed; reopen the native terminal before submitting work")
	}
	return nil
}
func validateTaskSessionTx(ctx context.Context, tx *sql.Tx, task *domain.Task) error {
	if err := sessionGate(ctx, tx, task.TargetAgentID, task.ID); err != nil {
		return err
	}
	var old int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM session_bindings b JOIN agent_sessions a ON a.agent_id=b.agent_id AND a.backend_id=b.backend_id WHERE b.context_id=? AND b.provider_session_id<>a.provider_session_id AND COALESCE(a.pending_task_id,'')<>?`, task.ID, task.ID).Scan(&old)
	if err != nil {
		return err
	}
	if old != 0 {
		return domain.ErrConflict("Task belongs to a retired Agent session; start new work in the active session")
	}
	return nil
}
func prepareSessionHandoffTx(ctx context.Context, tx *sql.Tx, task *domain.Task, request *domain.NewSessionRequest) (*domain.AgentSession, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if task.Intent != domain.TaskIntentQuery {
		return nil, domain.ErrInvalidInput("session handoff must be a query")
	}
	if err := externalOwner(ctx, tx, task.SenderPrincipalID); err != nil {
		return nil, err
	}
	_, org, err := externalActiveAgent(ctx, tx, task.TargetAgentID)
	if err != nil {
		return nil, err
	}
	if org != task.OrganizationID {
		return nil, domain.ErrForbidden("session handoff organization differs from Agent")
	}
	if err = sessionGate(ctx, tx, task.TargetAgentID, ""); err != nil {
		return nil, err
	}
	var backends int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_backend_registrations r JOIN worker_instances w ON w.worker_instance_id=r.worker_instance_id WHERE w.agent_id=? AND r.backend_id=? AND r.adapter_id='codex-app-server' AND json_extract(r.descriptor_json,'$.session_handoff')=1 AND w.status IN ('online','degraded') AND w.lease_until>? AND r.health IN ('healthy','degraded') AND w.generation=(SELECT MAX(w2.generation) FROM worker_instances w2 WHERE w2.agent_id=w.agent_id)`, task.TargetAgentID, request.BackendID, task.CreatedAt).Scan(&backends); err != nil {
		return nil, err
	}
	if backends == 0 {
		return nil, domain.ErrUnsupportedCapability
	}
	active, err := readAgentSession(ctx, tx, task.TargetAgentID, request.BackendID)
	if err != nil {
		return nil, err
	}
	if active.Version != request.ExpectedVersion || active.ThreadID != request.ExpectedThreadID {
		return nil, domain.ErrStaleVersion
	}
	var busy int
	if err = tx.QueryRowContext(ctx, `SELECT
 (SELECT COUNT(*) FROM tasks WHERE target_agent_id=? AND status NOT IN ('succeeded','failed','canceled','uncertain'))+
 (SELECT COUNT(*) FROM run_attempts WHERE agent_id=? AND status IN ('starting','running','waiting_approval','finishing'))+
 (SELECT COUNT(*) FROM mailbox_items WHERE target_agent_id=? AND state IN ('pending','claimed'))+
 (SELECT COUNT(*) FROM external_messages WHERE (kind<>'result' AND (sender_agent_id=? OR target_agent_id=?) AND processing_state NOT IN ('completed','out_of_scope')) OR (kind='result' AND target_agent_id=? AND delivery_state<>'acknowledged'))`, task.TargetAgentID, task.TargetAgentID, task.TargetAgentID, task.TargetAgentID, task.TargetAgentID, task.TargetAgentID).Scan(&busy); err != nil {
		return nil, err
	}
	if busy != 0 {
		return nil, domain.ErrConflict("finish current Tasks, queued input and pending consultations before starting a new session")
	}
	return active, nil
}
func persistSessionHandoffTx(ctx context.Context, tx *sql.Tx, active *domain.AgentSession, task *domain.Task, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO agent_sessions(agent_id,backend_id,provider_session_id,context_task_id,version,pending_task_id,updated_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(agent_id,backend_id) DO UPDATE SET pending_task_id=excluded.pending_task_id,updated_at=excluded.updated_at`, active.AgentID, active.BackendID, active.ThreadID, externalString(active.ContextTaskID), active.Version, task.ID, formatTime(now))
	return err
}

// Called from settlement transactions, after the final Task and binding exist.
func settleSessionHandoffTx(ctx context.Context, tx *sql.Tx, task *domain.Task, now time.Time) error {
	s, err := scanAgentSession(tx.QueryRowContext(ctx, `SELECT `+agentSessionColumns+` FROM agent_sessions WHERE pending_task_id=?`, task.ID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !task.IsTerminal() {
		return nil
	}
	eventType := "agent.session_handoff.failed"
	if task.Status == domain.TaskStatusSucceeded {
		var provider string
		if err = tx.QueryRowContext(ctx, `SELECT provider_session_id FROM session_bindings WHERE context_id=? AND agent_id=? AND backend_id=? AND state='active'`, task.ID, s.AgentID, s.BackendID).Scan(&provider); err != nil {
			return err
		}
		if provider == s.ThreadID || provider == "" {
			return domain.ErrConflict("handoff did not produce a new Codex thread")
		}
		// Generation advances but identity, credentials and peer grants are retained.
		// Admission excluded all pending requests before this operation was created.
		if _, err = tx.ExecContext(ctx, `UPDATE external_session_bindings SET thread_id=?,managed_context_task_id=?,generation=generation+1,updated_at=? WHERE agent_id=? AND mode='managed' AND managed_backend_id=?`, provider, task.ID, formatTime(now), s.AgentID, s.BackendID); err != nil {
			return err
		}
		result, e := tx.ExecContext(ctx, `UPDATE agent_sessions SET provider_session_id=?,context_task_id=?,version=version+1,pending_task_id=NULL,updated_at=? WHERE agent_id=? AND backend_id=? AND version=? AND pending_task_id=?`, provider, task.ID, formatTime(now), s.AgentID, s.BackendID, s.Version, task.ID)
		if e != nil {
			return e
		}
		if n, _ := result.RowsAffected(); n != 1 {
			return domain.ErrStaleVersion
		}
		eventType = "agent.session_handoff.completed"
	} else {
		if _, err = tx.ExecContext(ctx, `UPDATE agent_sessions SET pending_task_id=NULL,updated_at=? WHERE pending_task_id=?`, formatTime(now), task.ID); err != nil {
			return err
		}
	}
	payload, _ := json.Marshal(map[string]any{"task_id": task.ID, "backend_id": s.BackendID, "previous_version": s.Version, "status": task.Status})
	return insertJournal(ctx, tx, &domain.JournalEvent{ID: "event-session-" + uuid.NewString(), OrganizationID: task.OrganizationID, AggregateType: "agent", AggregateID: s.AgentID, EventType: eventType, ActorPrincipalID: task.SenderPrincipalID, Payload: payload, CreatedAt: now})
}

func validateSessionRunAdmissionTx(ctx context.Context, tx *sql.Tx, task *domain.Task, run *domain.RunAttempt) error {
	if err := validateTaskSessionTx(ctx, tx, task); err != nil {
		return err
	}
	var handoffBackend string
	handoffErr := tx.QueryRowContext(ctx, `SELECT backend_id FROM agent_sessions WHERE pending_task_id=?`, task.ID).Scan(&handoffBackend)
	if handoffErr != nil && !errors.Is(handoffErr, sql.ErrNoRows) {
		return handoffErr
	}
	var execution domain.ResolvedExecutionSpec
	if err := json.Unmarshal([]byte(run.ResolvedExecutionJSON), &execution); err != nil {
		return err
	}
	if handoffErr == nil {
		if run.BackendID != handoffBackend || !execution.Spec.Session.ForceNew || execution.Spec.Session.Mode != domain.SessionModeNew || run.AdapterID != "codex-app-server" {
			return domain.ErrConflict("handoff requires its frozen new Codex session")
		}
		var attempts int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM run_attempts WHERE task_id=?`, task.ID).Scan(&attempts); err != nil {
			return err
		}
		if attempts != 0 {
			return domain.ErrConflict("session handoff cannot be automatically retried")
		}
	} else if execution.Spec.Session.ForceNew {
		return domain.ErrForbidden("force_new requires an authoritative pending handoff")
	}

	return nil
}
