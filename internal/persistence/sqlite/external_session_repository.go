package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"openagentx/internal/domain"
)

const externalBindingColumns = `b.binding_id,b.agent_id,a.principal_id,a.organization_id,b.host_id,b.thread_id,b.generation,b.state,b.token_digest,b.token_expires_at,b.allowed_peer_agent_ids_json,b.created_at,b.updated_at`
const externalBindingJoin = ` FROM external_session_bindings b JOIN agents a ON a.agent_id=b.agent_id`

func scanExternalBinding(row rowScanner) (*domain.ExternalSessionBinding, error) {
	var b domain.ExternalSessionBinding
	var expiry, created, updated, peers string
	err := row.Scan(&b.ID, &b.AgentID, &b.PrincipalID, &b.OrganizationID, &b.HostID, &b.ThreadID, &b.Generation, &b.State, &b.TokenDigest, &expiry, &peers, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(peers), &b.AllowedPeerAgentIDs); err != nil {
		return nil, err
	}
	if b.TokenExpiresAt, err = parseTime(expiry); err != nil {
		return nil, err
	}
	if b.CreatedAt, err = parseTime(created); err != nil {
		return nil, err
	}
	b.UpdatedAt, err = parseTime(updated)
	return &b, err
}

func externalOwner(ctx context.Context, tx *sql.Tx, owner string) error {
	var count int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM web_users u JOIN principals p ON p.principal_id=u.principal_id WHERE u.principal_id=? AND u.status='active' AND p.status='active' AND p.kind='human' AND EXISTS(SELECT 1 FROM json_each(u.roles_json) WHERE value='owner')`, owner).Scan(&count)
	if err != nil {
		return err
	}
	if count == 0 {
		return domain.ErrForbidden("active owner required")
	}
	return nil
}

func externalActiveAgent(ctx context.Context, tx *sql.Tx, agentID string) (principal, organization string, err error) {
	err = tx.QueryRowContext(ctx, `SELECT a.principal_id,a.organization_id FROM agents a JOIN principals p ON p.principal_id=a.principal_id JOIN organizations o ON o.organization_id=a.organization_id WHERE a.agent_id=? AND a.status='active' AND p.status='active' AND p.kind='agent' AND o.status='active'`, agentID).Scan(&principal, &organization)
	if errors.Is(err, sql.ErrNoRows) {
		err = domain.ErrAgentNotReady
	}
	return
}

func authenticateExternalTx(ctx context.Context, tx *sql.Tx, digest string, now time.Time) (*domain.ExternalSessionBinding, error) {
	if digest == "" {
		return nil, domain.ErrUnauthorized
	}
	b, err := scanExternalBinding(tx.QueryRowContext(ctx, `SELECT `+externalBindingColumns+externalBindingJoin+` WHERE b.token_digest=?`, digest))
	if errors.Is(err, domain.ErrNotFound) {
		return nil, domain.ErrUnauthorized
	}
	if err != nil {
		return nil, err
	}
	if b.State != "active" || !now.Before(b.TokenExpiresAt) {
		return nil, domain.ErrUnauthorized
	}
	if _, _, err = externalActiveAgent(ctx, tx, b.AgentID); err != nil {
		return nil, domain.ErrUnauthorized
	}
	return b, nil
}

func (r *Repository) externalJournal(ctx context.Context, tx *sql.Tx, actor, org, aggregate, id, eventType string, payload any) error {
	if err := r.inject(FaultAfterStateWrite); err != nil {
		return err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	e := &domain.JournalEvent{ID: "event-" + uuid.NewString(), OrganizationID: org, AggregateType: aggregate, AggregateID: id, EventType: eventType, ActorPrincipalID: actor, Payload: raw, CreatedAt: r.now().UTC()}
	if err = e.Validate(); err != nil {
		return err
	}
	if err = insertJournal(ctx, tx, e); err != nil {
		return err
	}
	return r.inject(FaultBeforeCommit)
}

func (r *Repository) BindExternalSession(ctx context.Context, owner string, b domain.ExternalSessionBinding, expected int64) (*domain.ExternalSessionBinding, error) {
	input := domain.BindExternalSessionInput{AgentID: b.AgentID, HostID: b.HostID, ThreadID: b.ThreadID, AllowedPeerAgentIDs: b.AllowedPeerAgentIDs, ExpectedGeneration: expected}
	if err := input.Validate(); err != nil {
		return nil, err
	}
	if b.TokenDigest == "" || !b.TokenExpiresAt.After(r.now()) || b.State != "active" || b.Generation != expected+1 {
		return nil, domain.ErrInvalidInput("invalid external credential")
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = externalOwner(ctx, tx, owner); err != nil {
		return nil, err
	}
	b.PrincipalID, b.OrganizationID, err = externalActiveAgent(ctx, tx, b.AgentID)
	if err != nil {
		return nil, err
	}
	var unfinished int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE target_agent_id=? AND status NOT IN ('succeeded','failed','canceled','uncertain')`, b.AgentID).Scan(&unfinished); err != nil {
		return nil, err
	}
	if unfinished != 0 {
		return nil, domain.ErrConflict("finish or cancel managed tasks before binding an external session")
	}
	for _, peer := range b.AllowedPeerAgentIDs {
		_, org, e := externalActiveAgent(ctx, tx, peer)
		if e != nil {
			return nil, e
		}
		if org != b.OrganizationID {
			return nil, domain.ErrForbidden("peer must belong to same organization")
		}
	}
	old, err := scanExternalBinding(tx.QueryRowContext(ctx, `SELECT `+externalBindingColumns+externalBindingJoin+` WHERE b.agent_id=?`, b.AgentID))
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	if old == nil {
		if expected != 0 {
			return nil, domain.ErrStaleVersion
		}
	} else {
		if old.Generation != expected {
			return nil, domain.ErrStaleVersion
		}
		b.ID = old.ID
		b.CreatedAt = old.CreatedAt
	}
	peers, _ := json.Marshal(b.AllowedPeerAgentIDs)
	_, err = tx.ExecContext(ctx, `INSERT INTO external_session_bindings(binding_id,agent_id,host_id,thread_id,generation,state,token_digest,token_expires_at,allowed_peer_agent_ids_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(agent_id) DO UPDATE SET host_id=excluded.host_id,thread_id=excluded.thread_id,generation=excluded.generation,state=excluded.state,token_digest=excluded.token_digest,token_expires_at=excluded.token_expires_at,allowed_peer_agent_ids_json=excluded.allowed_peer_agent_ids_json,updated_at=excluded.updated_at`, b.ID, b.AgentID, b.HostID, b.ThreadID, b.Generation, b.State, b.TokenDigest, formatTime(b.TokenExpiresAt), string(peers), formatTime(b.CreatedAt), formatTime(b.UpdatedAt))
	if err != nil {
		if isUniqueConstraint(err, "") || strings.Contains(err.Error(), "conflicts with managed execution") {
			return nil, domain.ErrConflict("external binding conflicts with an existing binding or managed execution")
		}
		return nil, err
	}
	if err = r.externalJournal(ctx, tx, owner, b.OrganizationID, "external_session", b.ID, "external_session.bound", map[string]any{"agent_id": b.AgentID, "host_id": b.HostID, "thread_id": b.ThreadID, "generation": b.Generation}); err != nil {
		return nil, err
	}
	if err = commit(tx); err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *Repository) RevokeExternalSession(ctx context.Context, owner, agentID string, expected int64) error {
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = externalOwner(ctx, tx, owner); err != nil {
		return err
	}
	b, err := scanExternalBinding(tx.QueryRowContext(ctx, `SELECT `+externalBindingColumns+externalBindingJoin+` WHERE b.agent_id=?`, agentID))
	if err != nil {
		return err
	}
	if b.Generation != expected {
		return domain.ErrStaleVersion
	}
	if b.State == "revoked" {
		return nil
	}
	if _, err = tx.ExecContext(ctx, `UPDATE external_session_bindings SET state='revoked',generation=generation+1,updated_at=? WHERE binding_id=?`, formatTime(r.now()), b.ID); err != nil {
		return err
	}
	if err = r.externalJournal(ctx, tx, owner, b.OrganizationID, "external_session", b.ID, "external_session.revoked", map[string]any{"agent_id": agentID, "generation": expected + 1}); err != nil {
		return err
	}
	return commit(tx)
}

func (r *Repository) GetExternalSessionForOwner(ctx context.Context, owner, agentID string) (*domain.ExternalSessionBinding, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = externalOwner(ctx, tx, owner); err != nil {
		return nil, err
	}
	return scanExternalBinding(tx.QueryRowContext(ctx, `SELECT `+externalBindingColumns+externalBindingJoin+` WHERE b.agent_id=?`, agentID))
}
func (r *Repository) AuthenticateExternalSession(ctx context.Context, digest string) (*domain.ExternalSessionBinding, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return authenticateExternalTx(ctx, tx, digest, r.now())
}

const externalMessageColumns = `sequence,message_id,sender_agent_id,target_agent_id,sender_binding_id,sender_generation,kind,reply_to_message_id,content,idempotency_key,payload_digest,origin_task_id,delivery_state,created_at,acknowledged_at`

func scanExternalMessage(row rowScanner) (*domain.ExternalMessage, error) {
	var m domain.ExternalMessage
	var reply, origin, ack sql.NullString
	var created string
	err := row.Scan(&m.Sequence, &m.ID, &m.SenderAgentID, &m.TargetAgentID, &m.SenderBindingID, &m.SenderGeneration, &m.Kind, &reply, &m.Content, &m.IdempotencyKey, &m.PayloadDigest, &origin, &m.DeliveryState, &created, &ack)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	m.ReplyToMessageID = reply.String
	m.OriginTaskID = origin.String
	if m.CreatedAt, err = parseTime(created); err != nil {
		return nil, err
	}
	if ack.Valid {
		t, e := parseTime(ack.String)
		if e != nil {
			return nil, e
		}
		m.AcknowledgedAt = &t
	}
	return &m, nil
}
func externalString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
func hasExternalPeer(b *domain.ExternalSessionBinding, peer string) bool {
	for _, p := range b.AllowedPeerAgentIDs {
		if p == peer {
			return true
		}
	}
	return false
}

func (r *Repository) SendExternalMessage(ctx context.Context, digest string, m domain.ExternalMessage) (*domain.ExternalMessage, error) {
	input := domain.SendExternalMessageInput{TargetAgentID: m.TargetAgentID, Kind: m.Kind, ReplyToMessageID: m.ReplyToMessageID, Content: m.Content, IdempotencyKey: m.IdempotencyKey, OriginTaskID: m.OriginTaskID}
	if err := input.Validate(); err != nil {
		return nil, err
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	b, err := authenticateExternalTx(ctx, tx, digest, r.now())
	if err != nil {
		return nil, err
	}
	m.SenderAgentID = b.AgentID
	m.SenderBindingID = b.ID
	m.SenderGeneration = b.Generation
	prior, err := scanExternalMessage(tx.QueryRowContext(ctx, `SELECT `+externalMessageColumns+` FROM external_messages WHERE sender_agent_id=? AND idempotency_key=?`, b.AgentID, m.IdempotencyKey))
	if err == nil {
		if prior.PayloadDigest != m.PayloadDigest {
			return nil, domain.ErrIdempotencyConflict
		}
		return prior, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	if m.Kind == domain.ExternalMessageResult {
		original, e := scanExternalMessage(tx.QueryRowContext(ctx, `SELECT `+externalMessageColumns+` FROM external_messages WHERE message_id=?`, m.ReplyToMessageID))
		if e != nil {
			return nil, e
		}
		if original.TargetAgentID != b.AgentID {
			return nil, domain.ErrForbidden("only the request recipient can reply")
		}
		if original.Kind == domain.ExternalMessageResult {
			return nil, domain.ErrInvalidInput("results cannot request another result")
		}
		m.TargetAgentID = original.SenderAgentID
		m.OriginTaskID = original.OriginTaskID
	}
	if m.TargetAgentID == b.AgentID || !hasExternalPeer(b, m.TargetAgentID) {
		return nil, domain.ErrForbidden("target is not an allowed peer")
	}
	_, org, err := externalActiveAgent(ctx, tx, m.TargetAgentID)
	if err != nil {
		return nil, err
	}
	if org != b.OrganizationID {
		return nil, domain.ErrForbidden("target is outside organization")
	}
	peer, err := scanExternalBinding(tx.QueryRowContext(ctx, `SELECT `+externalBindingColumns+externalBindingJoin+` WHERE b.agent_id=?`, m.TargetAgentID))
	if err != nil {
		return nil, err
	}
	if peer.State != "active" || !hasExternalPeer(peer, b.AgentID) {
		return nil, domain.ErrForbidden("peer does not accept this sender")
	}
	if m.OriginTaskID != "" && m.Kind != domain.ExternalMessageResult {
		var target, org string
		if err = tx.QueryRowContext(ctx, `SELECT target_agent_id,organization_id FROM tasks WHERE task_id=?`, m.OriginTaskID).Scan(&target, &org); errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrTaskNotFound
		} else if err != nil {
			return nil, err
		}
		if target != b.AgentID || org != b.OrganizationID {
			return nil, domain.ErrForbidden("origin task must belong to sender")
		}
	}
	m.DeliveryState = "pending"
	m.CreatedAt = r.now().UTC()
	res, err := tx.ExecContext(ctx, `INSERT INTO external_messages(message_id,sender_agent_id,target_agent_id,sender_binding_id,sender_generation,kind,reply_to_message_id,content,idempotency_key,payload_digest,origin_task_id,delivery_state,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, m.ID, m.SenderAgentID, m.TargetAgentID, m.SenderBindingID, m.SenderGeneration, m.Kind, externalString(m.ReplyToMessageID), m.Content, m.IdempotencyKey, m.PayloadDigest, externalString(m.OriginTaskID), m.DeliveryState, formatTime(m.CreatedAt))
	if err != nil {
		if isUniqueConstraint(err, "") {
			return nil, domain.ErrConflict("request already has a result")
		}
		return nil, err
	}
	m.Sequence, err = res.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err = r.externalJournal(ctx, tx, b.PrincipalID, b.OrganizationID, "external_message", m.ID, "external_message.sent", map[string]any{"sender_agent_id": m.SenderAgentID, "target_agent_id": m.TargetAgentID, "kind": m.Kind, "reply_to_message_id": m.ReplyToMessageID, "sequence": m.Sequence}); err != nil {
		return nil, err
	}
	if err = commit(tx); err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *Repository) ListExternalInbox(ctx context.Context, digest string, after int64, limit int) ([]domain.ExternalMessage, error) {
	if after < 0 || limit < 1 || limit > 200 {
		return nil, domain.ErrInvalidInput("invalid inbox cursor or limit")
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	b, err := authenticateExternalTx(ctx, tx, digest, r.now())
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+externalMessageColumns+` FROM external_messages WHERE target_agent_id=? AND sequence>? ORDER BY sequence LIMIT ?`, b.AgentID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.ExternalMessage, 0)
	for rows.Next() {
		m, e := scanExternalMessage(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}
func (r *Repository) GetExternalMessage(ctx context.Context, digest, id string) (*domain.ExternalMessage, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	b, err := authenticateExternalTx(ctx, tx, digest, r.now())
	if err != nil {
		return nil, err
	}
	m, err := scanExternalMessage(tx.QueryRowContext(ctx, `SELECT `+externalMessageColumns+` FROM external_messages WHERE message_id=?`, id))
	if err != nil {
		return nil, err
	}
	if m.SenderAgentID != b.AgentID && m.TargetAgentID != b.AgentID {
		return nil, domain.ErrForbidden("message belongs to other agents")
	}
	return m, nil
}
func (r *Repository) AcknowledgeExternalMessage(ctx context.Context, digest, id string) (*domain.ExternalMessage, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	b, err := authenticateExternalTx(ctx, tx, digest, r.now())
	if err != nil {
		return nil, err
	}
	m, err := scanExternalMessage(tx.QueryRowContext(ctx, `SELECT `+externalMessageColumns+` FROM external_messages WHERE message_id=?`, id))
	if err != nil {
		return nil, err
	}
	if m.TargetAgentID != b.AgentID {
		return nil, domain.ErrForbidden("only recipient can acknowledge")
	}
	if m.DeliveryState == "acknowledged" {
		return m, nil
	}
	now := r.now().UTC()
	if _, err = tx.ExecContext(ctx, `UPDATE external_messages SET delivery_state='acknowledged',acknowledged_at=? WHERE message_id=?`, formatTime(now), id); err != nil {
		return nil, err
	}
	m.DeliveryState = "acknowledged"
	m.AcknowledgedAt = &now
	if err = r.externalJournal(ctx, tx, b.PrincipalID, b.OrganizationID, "external_message", m.ID, "external_message.acknowledged", map[string]any{"target_agent_id": b.AgentID, "sequence": m.Sequence}); err != nil {
		return nil, err
	}
	if err = commit(tx); err != nil {
		return nil, err
	}
	return m, nil
}
