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

func (r *Repository) IsInitialized(ctx context.Context) (bool, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM web_users`).Scan(&count); err != nil {
		return false, fmt.Errorf("inspect OpenAgentX initialization: %w", err)
	}
	return count > 0, nil
}

func (r *Repository) Initialize(
	ctx context.Context,
	owner *domain.Principal,
	systemPrincipal *domain.Principal,
	organization *domain.Organization,
	webUser *domain.WebUserRecord,
	events []*domain.JournalEvent,
) (bool, error) {
	if owner == nil || systemPrincipal == nil || organization == nil || webUser == nil {
		return false, domain.ErrInvalidInput("owner, system principal, organization and web user are required")
	}
	if len(events) != 4 {
		return false, domain.ErrInvalidInput("initialization requires four journal events")
	}
	if err := owner.Validate(); err != nil {
		return false, err
	}
	if owner.Kind != domain.PrincipalHuman || owner.Status != domain.IdentityActive {
		return false, domain.ErrInvalidInput("initial owner must be an active human principal")
	}
	if err := systemPrincipal.Validate(); err != nil {
		return false, err
	}
	if systemPrincipal.Kind != domain.PrincipalSystem || systemPrincipal.Status != domain.IdentityActive {
		return false, domain.ErrInvalidInput("daemon principal must be an active system principal")
	}
	if err := organization.Validate(); err != nil {
		return false, err
	}
	if err := webUser.Validate(); err != nil {
		return false, err
	}
	if webUser.PrincipalID != owner.ID || !webUser.HasRole(domain.WebRoleOwner) {
		return false, domain.ErrInvalidInput("initial web user must represent the owner principal")
	}

	now := r.now().UTC()
	owner.CreatedAt = normalizeTime(owner.CreatedAt, now)
	owner.UpdatedAt = normalizeTime(owner.UpdatedAt, owner.CreatedAt)
	systemPrincipal.CreatedAt = normalizeTime(systemPrincipal.CreatedAt, now)
	systemPrincipal.UpdatedAt = normalizeTime(systemPrincipal.UpdatedAt, systemPrincipal.CreatedAt)
	organization.CreatedAt = normalizeTime(organization.CreatedAt, now)
	organization.UpdatedAt = normalizeTime(organization.UpdatedAt, organization.CreatedAt)
	webUser.PasswordSetAt = normalizeTime(webUser.PasswordSetAt, now)
	webUser.CreatedAt = normalizeTime(webUser.CreatedAt, now)
	webUser.UpdatedAt = normalizeTime(webUser.UpdatedAt, webUser.CreatedAt)
	targets := []struct {
		aggregateType string
		aggregateID   string
	}{
		{aggregateType: "principal", aggregateID: owner.ID},
		{aggregateType: "principal", aggregateID: systemPrincipal.ID},
		{aggregateType: "organization", aggregateID: organization.ID},
		{aggregateType: "web_user", aggregateID: webUser.ID},
	}
	for index, target := range targets {
		if events[index] == nil || events[index].ActorPrincipalID != owner.ID {
			return false, domain.ErrInvalidInput("initialization events must be attributed to the owner")
		}
		if err := validateJournalForAggregate(events[index], target.aggregateType, target.aggregateID, now); err != nil {
			return false, err
		}
	}

	rolesJSON, err := json.Marshal(domain.SortWebRoles(webUser.Roles))
	if err != nil {
		return false, fmt.Errorf("encode owner roles: %w", err)
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var existing int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM web_users`).Scan(&existing); err != nil {
		return false, fmt.Errorf("inspect existing owner: %w", err)
	}
	if existing != 0 {
		return false, nil
	}
	for _, principal := range []*domain.Principal{owner, systemPrincipal} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO principals (
			principal_id, kind, display_name, status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?)`, principal.ID, principal.Kind, principal.DisplayName,
			principal.Status, formatTime(principal.CreatedAt), formatTime(principal.UpdatedAt)); err != nil {
			return false, fmt.Errorf("create initialization principal: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO organizations (
		organization_id, name, status, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?)`, organization.ID, organization.Name, organization.Status,
		formatTime(organization.CreatedAt), formatTime(organization.UpdatedAt)); err != nil {
		return false, fmt.Errorf("create initial organization: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO web_users (
		web_user_id, principal_id, username, password_hash, roles_json, status,
		password_changed_at, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, webUser.ID, webUser.PrincipalID, webUser.Username,
		webUser.PasswordDigest, string(rolesJSON), webUser.Status, formatTime(webUser.PasswordSetAt),
		formatTime(webUser.CreatedAt), formatTime(webUser.UpdatedAt)); err != nil {
		return false, fmt.Errorf("create initial owner login: %w", err)
	}
	if err := r.inject(FaultAfterStateWrite); err != nil {
		return false, err
	}
	for _, event := range events {
		if err := insertJournal(ctx, tx, event); err != nil {
			return false, err
		}
	}
	if err := r.inject(FaultBeforeCommit); err != nil {
		return false, err
	}
	if err := commit(tx); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Repository) GetWebUserByUsername(ctx context.Context, username string) (*domain.WebUserRecord, error) {
	return scanWebUser(r.db.QueryRowContext(ctx, `SELECT web_user_id, principal_id, username,
		password_hash, roles_json, status, password_changed_at, created_at, updated_at
		FROM web_users WHERE username=?`, strings.TrimSpace(username)))
}

func (r *Repository) ListWebUsers(ctx context.Context) ([]domain.WebUserRecord, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT web_user_id, principal_id, username,
		password_hash, roles_json, status, password_changed_at, created_at, updated_at
		FROM web_users WHERE status='active' ORDER BY username`)
	if err != nil {
		return nil, fmt.Errorf("list web users: %w", err)
	}
	defer rows.Close()
	result := make([]domain.WebUserRecord, 0)
	for rows.Next() {
		user, err := scanWebUser(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *user)
	}
	return result, rows.Err()
}

func scanWebUser(scanner rowScanner) (*domain.WebUserRecord, error) {
	var user domain.WebUserRecord
	var rolesJSON, passwordSetAt, createdAt, updatedAt string
	if err := scanner.Scan(&user.ID, &user.PrincipalID, &user.Username, &user.PasswordDigest,
		&rolesJSON, &user.Status, &passwordSetAt, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("scan web user: %w", err)
	}
	if err := json.Unmarshal([]byte(rolesJSON), &user.Roles); err != nil {
		return nil, fmt.Errorf("decode web user roles: %w", err)
	}
	var err error
	if user.PasswordSetAt, err = parseTime(passwordSetAt); err != nil {
		return nil, err
	}
	if user.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if user.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *Repository) ApplyAgent(
	ctx context.Context,
	principal *domain.Principal,
	agent *domain.AgentIdentity,
	profile *domain.AgentProfileRecord,
	events []*domain.JournalEvent,
) (bool, error) {
	if principal == nil || agent == nil || profile == nil {
		return false, domain.ErrInvalidInput("Agent principal, identity and profile are required")
	}
	if len(events) != 2 {
		return false, domain.ErrInvalidInput("Agent apply requires two journal events")
	}
	if err := principal.Validate(); err != nil {
		return false, err
	}
	if principal.Kind != domain.PrincipalAgent || principal.Status != domain.IdentityActive {
		return false, domain.ErrInvalidInput("Agent principal must be active and use kind agent")
	}
	if err := agent.Validate(); err != nil {
		return false, err
	}
	if err := profile.Validate(); err != nil {
		return false, err
	}
	if agent.PrincipalID != principal.ID || profile.AgentID != agent.ID {
		return false, domain.ErrInvalidInput("Agent principal, identity and profile IDs do not match")
	}
	actorID := events[0].ActorPrincipalID
	if actorID == "" || events[1].ActorPrincipalID != actorID {
		return false, domain.ErrInvalidInput("Agent apply events require one owner actor")
	}
	now := r.now().UTC()
	principal.CreatedAt = normalizeTime(principal.CreatedAt, now)
	principal.UpdatedAt = normalizeTime(principal.UpdatedAt, principal.CreatedAt)
	agent.CreatedAt = normalizeTime(agent.CreatedAt, now)
	agent.UpdatedAt = normalizeTime(agent.UpdatedAt, agent.CreatedAt)
	profile.CreatedAt = normalizeTime(profile.CreatedAt, now)
	profile.UpdatedAt = normalizeTime(profile.UpdatedAt, profile.CreatedAt)
	if err := validateJournalForAggregate(events[0], "principal", principal.ID, now); err != nil {
		return false, err
	}
	if err := validateJournalForAggregate(events[1], "agent", agent.ID, now); err != nil {
		return false, err
	}
	capabilities := append([]string(nil), profile.Capabilities...)
	sort.Strings(capabilities)
	capabilitiesJSON, err := json.Marshal(capabilities)
	if err != nil {
		return false, fmt.Errorf("encode Agent profile capabilities: %w", err)
	}

	tx, err := r.begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	owner, err := scanWebUser(tx.QueryRowContext(ctx, `SELECT web_user_id, principal_id, username,
		password_hash, roles_json, status, password_changed_at, created_at, updated_at
		FROM web_users WHERE principal_id=?`, actorID))
	if err != nil || owner.Status != domain.IdentityActive || !owner.HasRole(domain.WebRoleOwner) {
		return false, domain.ErrForbidden("Agent apply requires an active owner")
	}
	var organizationStatus domain.IdentityStatus
	if err := tx.QueryRowContext(ctx, `SELECT status FROM organizations WHERE organization_id=?`, agent.OrganizationID).Scan(&organizationStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, domain.ErrNotFound
		}
		return false, fmt.Errorf("load Agent organization: %w", err)
	}
	if organizationStatus != domain.IdentityActive {
		return false, domain.ErrConflict("Agent organization is disabled")
	}

	var existingAgentCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agents WHERE agent_id=?`, agent.ID).Scan(&existingAgentCount); err != nil {
		return false, fmt.Errorf("inspect existing Agent: %w", err)
	}
	if existingAgentCount != 0 {
		matches, err := agentBundleMatches(ctx, tx, principal, agent, profile, capabilities)
		if err != nil {
			return false, err
		}
		if !matches {
			return false, domain.ErrConflict("Agent already exists with a different identity or profile")
		}
		return false, nil
	}
	var existingPrincipalCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM principals WHERE principal_id=?`, principal.ID).Scan(&existingPrincipalCount); err != nil {
		return false, fmt.Errorf("inspect existing Agent principal: %w", err)
	}
	if existingPrincipalCount != 0 {
		return false, domain.ErrConflict("Agent principal already exists without the requested Agent identity")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO principals (
		principal_id, kind, display_name, status, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?)`, principal.ID, principal.Kind, principal.DisplayName,
		principal.Status, formatTime(principal.CreatedAt), formatTime(principal.UpdatedAt)); err != nil {
		return false, fmt.Errorf("create Agent principal: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO agents (
		agent_id, principal_id, organization_id, display_name, status, version, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, agent.ID, agent.PrincipalID, agent.OrganizationID,
		agent.DisplayName, agent.Status, agent.Version, formatTime(agent.CreatedAt), formatTime(agent.UpdatedAt)); err != nil {
		return false, fmt.Errorf("create Agent identity: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_profiles (
		agent_id, profile_version, instructions_path, workspace_root,
		default_execution_profile_id, capabilities_json, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, profile.AgentID, profile.Version, profile.InstructionsPath,
		profile.WorkspaceRoot, nullableString(profile.DefaultExecutionProfileID), string(capabilitiesJSON),
		formatTime(profile.CreatedAt), formatTime(profile.UpdatedAt)); err != nil {
		return false, fmt.Errorf("create Agent profile: %w", err)
	}
	if err := r.inject(FaultAfterStateWrite); err != nil {
		return false, err
	}
	for _, event := range events {
		if err := insertJournal(ctx, tx, event); err != nil {
			return false, err
		}
	}
	if err := r.inject(FaultBeforeCommit); err != nil {
		return false, err
	}
	if err := commit(tx); err != nil {
		return false, err
	}
	return true, nil
}

func agentBundleMatches(ctx context.Context, tx *sql.Tx, principal *domain.Principal, agent *domain.AgentIdentity, profile *domain.AgentProfileRecord, capabilities []string) (bool, error) {
	var existingPrincipal domain.Principal
	var existingAgent domain.AgentIdentity
	var existingProfile domain.AgentProfileRecord
	var principalCreated, principalUpdated, agentCreated, agentUpdated, profileCreated, profileUpdated string
	var defaultExecutionProfile sql.NullString
	var capabilitiesJSON string
	err := tx.QueryRowContext(ctx, `SELECT
		p.principal_id, p.kind, p.display_name, p.status, p.created_at, p.updated_at,
		a.agent_id, a.principal_id, a.organization_id, a.display_name, a.status, a.version, a.created_at, a.updated_at,
		ap.profile_version, ap.instructions_path, ap.workspace_root, ap.default_execution_profile_id,
		ap.capabilities_json, ap.created_at, ap.updated_at
		FROM agents a
		JOIN principals p ON p.principal_id=a.principal_id
		JOIN agent_profiles ap ON ap.agent_id=a.agent_id
		WHERE a.agent_id=?`, agent.ID).Scan(
		&existingPrincipal.ID, &existingPrincipal.Kind, &existingPrincipal.DisplayName, &existingPrincipal.Status,
		&principalCreated, &principalUpdated, &existingAgent.ID, &existingAgent.PrincipalID,
		&existingAgent.OrganizationID, &existingAgent.DisplayName, &existingAgent.Status, &existingAgent.Version,
		&agentCreated, &agentUpdated, &existingProfile.Version, &existingProfile.InstructionsPath,
		&existingProfile.WorkspaceRoot, &defaultExecutionProfile, &capabilitiesJSON, &profileCreated, &profileUpdated)
	if err != nil {
		return false, fmt.Errorf("load existing Agent bundle: %w", err)
	}
	existingProfile.AgentID = existingAgent.ID
	if defaultExecutionProfile.Valid {
		existingProfile.DefaultExecutionProfileID = &defaultExecutionProfile.String
	}
	var existingCapabilities []string
	if err := json.Unmarshal([]byte(capabilitiesJSON), &existingCapabilities); err != nil {
		return false, fmt.Errorf("decode existing Agent capabilities: %w", err)
	}
	sort.Strings(existingCapabilities)
	requestedDefault := ""
	if profile.DefaultExecutionProfileID != nil {
		requestedDefault = *profile.DefaultExecutionProfileID
	}
	existingDefault := ""
	if existingProfile.DefaultExecutionProfileID != nil {
		existingDefault = *existingProfile.DefaultExecutionProfileID
	}
	return existingPrincipal.ID == principal.ID && existingPrincipal.Kind == principal.Kind &&
		existingPrincipal.DisplayName == principal.DisplayName && existingPrincipal.Status == principal.Status &&
		existingAgent.PrincipalID == agent.PrincipalID && existingAgent.OrganizationID == agent.OrganizationID &&
		existingAgent.DisplayName == agent.DisplayName && existingAgent.Status == agent.Status && existingAgent.Version == agent.Version &&
		existingProfile.Version == profile.Version && existingProfile.InstructionsPath == profile.InstructionsPath &&
		existingProfile.WorkspaceRoot == profile.WorkspaceRoot && existingDefault == requestedDefault &&
		stringSliceEqual(existingCapabilities, capabilities), nil
}

func stringSliceEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
