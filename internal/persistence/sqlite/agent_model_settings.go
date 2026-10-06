package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

type modelSettingsQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type storedModelSettings struct {
	Kind      string `json:"kind"`
	BackendID string `json:"backend_id"`
	Model     string `json:"model"`
	Effort    string `json:"effort"`
}

func modelProfileID(agentID string) string { return "agent-model:" + agentID }

// ReadAgentModelPreference is consumed by the authoritative Run planner. It
// deliberately does not consult mutable Codex config files or the foreground.
func (r *Repository) ReadAgentModelPreference(ctx context.Context, agentID string) (*domain.AgentModelSettings, error) {
	return readModelPreference(ctx, r.db, agentID)
}

func readModelPreference(ctx context.Context, q modelSettingsQuery, agentID string) (*domain.AgentModelSettings, error) {
	var profileID, raw sql.NullString
	var version sql.NullInt64
	err := q.QueryRowContext(ctx, `SELECT p.default_execution_profile_id,e.execution_json,e.version
		FROM agent_profiles p LEFT JOIN execution_profiles e ON e.execution_profile_id=p.default_execution_profile_id
		WHERE p.agent_id=?`, agentID).Scan(&profileID, &raw, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrAgentNotFound
	}
	if err != nil {
		return nil, err
	}
	if !profileID.Valid {
		return nil, nil
	}
	if profileID.String != modelProfileID(agentID) {
		return nil, domain.ErrConflict("Agent already uses another execution profile")
	}
	var saved storedModelSettings
	if err := json.Unmarshal([]byte(raw.String), &saved); err != nil {
		return nil, err
	}
	if saved.Kind != "agent_model_settings_v1" || version.Int64 <= 0 {
		return nil, domain.ErrInvalidInput("invalid Agent model profile")
	}
	return &domain.AgentModelSettings{AgentID: agentID, BackendID: saved.BackendID, Model: saved.Model, Effort: saved.Effort, Version: version.Int64}, nil
}

func effectiveModelSettings(ctx context.Context, q modelSettingsQuery, agentID, backendID string) (domain.AgentModelSettings, error) {
	result := domain.AgentModelSettings{AgentID: agentID, BackendID: backendID}
	if err := domain.ValidateIdentifier("agent_id", agentID); err != nil {
		return result, err
	}
	if err := domain.ValidateIdentifier("backend_id", backendID); err != nil {
		return result, err
	}
	saved, err := readModelPreference(ctx, q, agentID)
	if err != nil {
		return result, err
	}
	var raw string
	err = q.QueryRowContext(ctx, `SELECT b.descriptor_json FROM runtime_backend_registrations b
		WHERE b.worker_instance_id=(SELECT worker_instance_id FROM worker_instances WHERE agent_id=?
		ORDER BY generation DESC,updated_at DESC,worker_instance_id DESC LIMIT 1) AND b.backend_id=?`, agentID, backendID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return result, domain.ErrConflict("Agent backend is not registered; start its Worker first")
	}
	if err != nil {
		return result, err
	}
	var d openruntime.AdapterDescriptor
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return result, err
	}
	if d.AdapterID != "codex-app-server" || len(d.Models) == 0 {
		return result, domain.ErrUnsupportedCapability
	}
	result.Model, result.Models, result.ModelEfforts = d.Models[0], d.Models, d.ModelReasoningEfforts
	if saved != nil {
		if saved.BackendID != backendID {
			return result, domain.ErrConflict("saved settings belong to another backend")
		}
		result.Model, result.Effort, result.Version = saved.Model, saved.Effort, saved.Version
	}
	return result, nil
}

func (r *Repository) GetAgentModelSettings(ctx context.Context, agentID, backendID string) (domain.AgentModelSettings, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.AgentModelSettings{}, err
	}
	defer tx.Rollback()
	result, err := effectiveModelSettings(ctx, tx, agentID, backendID)
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}

func (r *Repository) SetAgentModelSettings(ctx context.Context, agentID string, request domain.AgentModelSettingsUpdate, actor string) (domain.AgentModelSettings, error) {
	var result domain.AgentModelSettings
	if err := request.Validate(); err != nil {
		return result, err
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	result, err = effectiveModelSettings(ctx, tx, agentID, request.BackendID)
	if err != nil {
		return result, err
	}
	if request.ExpectedVersion != result.Version {
		return result, domain.ErrStaleVersion
	}
	if !slices.Contains(result.Models, request.Model) || (request.Effort != "" && !slices.Contains(result.ModelEfforts[request.Model], request.Effort)) {
		return result, domain.ErrInvalidInput("model or reasoning effort is not supported by this Agent backend")
	}
	// A replay of the same value has no extra journal entry or version change.
	if result.Version > 0 && result.Model == request.Model && result.Effort == request.Effort {
		return result, nil
	}
	var organization string
	if err := tx.QueryRowContext(ctx, "SELECT organization_id FROM agents WHERE agent_id=? AND status='active'", agentID).Scan(&organization); err != nil {
		return result, domain.ErrAgentNotFound
	}
	now := r.now().UTC()
	raw, _ := json.Marshal(storedModelSettings{Kind: "agent_model_settings_v1", BackendID: request.BackendID, Model: request.Model, Effort: request.Effort})
	profileID := modelProfileID(agentID)
	var otherReferences int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_profiles WHERE default_execution_profile_id=? AND agent_id<>?`, profileID, agentID).Scan(&otherReferences); err != nil {
		return result, err
	}
	if otherReferences != 0 {
		return result, domain.ErrConflict("Agent model profile is shared; refusing to change another Agent")
	}
	if result.Version == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO execution_profiles(execution_profile_id,organization_id,name,execution_json,version,created_at,updated_at) VALUES(?,?,?,?,1,?,?)`, profileID, organization, profileID, string(raw), formatTime(now), formatTime(now))
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE execution_profiles SET execution_json=?,version=version+1,updated_at=? WHERE execution_profile_id=? AND version=?`, string(raw), formatTime(now), profileID, result.Version)
	}
	if err != nil {
		return result, fmt.Errorf("save Agent model settings: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_profiles SET default_execution_profile_id=?,updated_at=? WHERE agent_id=?`, profileID, formatTime(now), agentID); err != nil {
		return result, err
	}
	result.Model, result.Effort, result.Version = request.Model, request.Effort, result.Version+1
	payload, _ := json.Marshal(map[string]any{"backend_id": result.BackendID, "model": result.Model, "effort": result.Effort, "version": result.Version})
	event := &domain.JournalEvent{ID: "event-model-" + uuid.NewString(), OrganizationID: organization, AggregateType: "agent", AggregateID: agentID, EventType: "agent.model_settings.updated", ActorPrincipalID: actor, Payload: payload, CreatedAt: now}
	if err = r.inject(FaultAfterStateWrite); err != nil {
		return result, err
	}
	if err = insertJournal(ctx, tx, event); err != nil {
		return result, err
	}
	if err = r.inject(FaultBeforeCommit); err != nil {
		return result, err
	}
	return result, commit(tx)
}
