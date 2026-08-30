package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"agentbus/internal/domain"
)

func (r *Repository) CreatePrincipal(ctx context.Context, principal *domain.Principal, event *domain.JournalEvent) error {
	if principal == nil {
		return domain.ErrInvalidInput("principal is required")
	}
	if err := principal.Validate(); err != nil {
		return err
	}
	now := r.now().UTC()
	principal.CreatedAt = normalizeTime(principal.CreatedAt, now)
	principal.UpdatedAt = normalizeTime(principal.UpdatedAt, principal.CreatedAt)
	if err := validateJournalForAggregate(event, "principal", principal.ID, now); err != nil {
		return err
	}

	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO principals (
		principal_id, kind, display_name, status, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?)`, principal.ID, principal.Kind, principal.DisplayName,
		principal.Status, formatTime(principal.CreatedAt), formatTime(principal.UpdatedAt)); err != nil {
		return fmt.Errorf("create principal: %w", err)
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

func (r *Repository) CreateOrganization(ctx context.Context, organization *domain.Organization, event *domain.JournalEvent) error {
	if organization == nil {
		return domain.ErrInvalidInput("organization is required")
	}
	if err := organization.Validate(); err != nil {
		return err
	}
	now := r.now().UTC()
	organization.CreatedAt = normalizeTime(organization.CreatedAt, now)
	organization.UpdatedAt = normalizeTime(organization.UpdatedAt, organization.CreatedAt)
	if err := validateJournalForAggregate(event, "organization", organization.ID, now); err != nil {
		return err
	}

	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO organizations (
		organization_id, name, status, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?)`, organization.ID, organization.Name, organization.Status,
		formatTime(organization.CreatedAt), formatTime(organization.UpdatedAt)); err != nil {
		return fmt.Errorf("create organization: %w", err)
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

func (r *Repository) CreateAgent(ctx context.Context, agent *domain.AgentIdentity, profile *domain.AgentProfileRecord, event *domain.JournalEvent) error {
	if agent == nil || profile == nil {
		return domain.ErrInvalidInput("agent and profile are required")
	}
	if err := agent.Validate(); err != nil {
		return err
	}
	if err := profile.Validate(); err != nil {
		return err
	}
	if profile.AgentID != agent.ID {
		return domain.ErrInvalidInput("profile agent_id does not match Agent identity")
	}
	now := r.now().UTC()
	agent.CreatedAt = normalizeTime(agent.CreatedAt, now)
	agent.UpdatedAt = normalizeTime(agent.UpdatedAt, agent.CreatedAt)
	profile.CreatedAt = normalizeTime(profile.CreatedAt, now)
	profile.UpdatedAt = normalizeTime(profile.UpdatedAt, profile.CreatedAt)
	if err := validateJournalForAggregate(event, "agent", agent.ID, now); err != nil {
		return err
	}
	capabilities := append([]string(nil), profile.Capabilities...)
	sort.Strings(capabilities)
	capabilitiesJSON, err := json.Marshal(capabilities)
	if err != nil {
		return fmt.Errorf("encode profile capabilities: %w", err)
	}

	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO agents (
		agent_id, principal_id, organization_id, display_name, status, version, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, agent.ID, agent.PrincipalID, agent.OrganizationID,
		agent.DisplayName, agent.Status, agent.Version, formatTime(agent.CreatedAt), formatTime(agent.UpdatedAt)); err != nil {
		return fmt.Errorf("create Agent identity: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_profiles (
		agent_id, profile_version, instructions_path, workspace_root,
		default_execution_profile_id, capabilities_json, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, profile.AgentID, profile.Version, profile.InstructionsPath,
		profile.WorkspaceRoot, nullableString(profile.DefaultExecutionProfileID), string(capabilitiesJSON),
		formatTime(profile.CreatedAt), formatTime(profile.UpdatedAt)); err != nil {
		return fmt.Errorf("create Agent profile: %w", err)
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

func (r *Repository) GetAgent(ctx context.Context, agentID string) (*domain.AgentIdentity, *domain.AgentProfileRecord, error) {
	row := r.db.QueryRowContext(ctx, `SELECT
		a.agent_id, a.principal_id, a.organization_id, a.display_name, a.status, a.version,
		a.created_at, a.updated_at, p.profile_version, p.instructions_path, p.workspace_root,
		p.default_execution_profile_id, p.capabilities_json, p.created_at, p.updated_at
		FROM agents a JOIN agent_profiles p ON p.agent_id = a.agent_id WHERE a.agent_id = ?`, agentID)
	var agent domain.AgentIdentity
	var profile domain.AgentProfileRecord
	var agentCreated, agentUpdated, profileCreated, profileUpdated string
	var defaultExecutionProfile sql.NullString
	var capabilitiesJSON string
	err := row.Scan(&agent.ID, &agent.PrincipalID, &agent.OrganizationID, &agent.DisplayName, &agent.Status,
		&agent.Version, &agentCreated, &agentUpdated, &profile.Version, &profile.InstructionsPath,
		&profile.WorkspaceRoot, &defaultExecutionProfile, &capabilitiesJSON, &profileCreated, &profileUpdated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, domain.ErrAgentNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("get Agent identity: %w", err)
	}
	profile.AgentID = agent.ID
	if defaultExecutionProfile.Valid {
		profile.DefaultExecutionProfileID = &defaultExecutionProfile.String
	}
	if err := json.Unmarshal([]byte(capabilitiesJSON), &profile.Capabilities); err != nil {
		return nil, nil, fmt.Errorf("decode profile capabilities: %w", err)
	}
	if agent.CreatedAt, err = parseTime(agentCreated); err != nil {
		return nil, nil, err
	}
	if agent.UpdatedAt, err = parseTime(agentUpdated); err != nil {
		return nil, nil, err
	}
	if profile.CreatedAt, err = parseTime(profileCreated); err != nil {
		return nil, nil, err
	}
	if profile.UpdatedAt, err = parseTime(profileUpdated); err != nil {
		return nil, nil, err
	}
	return &agent, &profile, nil
}
