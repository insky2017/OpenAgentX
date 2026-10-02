package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"openagentx/internal/domain"
)

func externalRolesTx(ctx context.Context, tx *sql.Tx, org string) (*domain.ExternalRoleCatalog, error) {
	c := &domain.ExternalRoleCatalog{OrganizationID: org, Rules: []domain.ExternalRoleRule{}}
	err := tx.QueryRowContext(ctx, `SELECT revision FROM external_role_catalogs WHERE organization_id=?`, org).Scan(&c.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT scope,owner_agent_id,description FROM external_role_scopes WHERE organization_id=? ORDER BY scope`, org)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v domain.ExternalRoleRule
		if err = rows.Scan(&v.Scope, &v.OwnerAgentID, &v.Description); err != nil {
			return nil, err
		}
		c.Rules = append(c.Rules, v)
	}
	return c, rows.Err()
}
func (r *Repository) ApplyExternalRoles(ctx context.Context, owner string, in domain.ApplyExternalRolesInput) (*domain.ExternalRoleCatalog, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = externalOwner(ctx, tx, owner); err != nil {
		return nil, err
	}
	current, err := externalRolesTx(ctx, tx, in.OrganizationID)
	if err != nil {
		return nil, err
	}
	if current.Revision != in.ExpectedVersion {
		return nil, domain.ErrStaleVersion
	}
	for _, rule := range in.Rules {
		_, org, e := externalActiveAgent(ctx, tx, rule.OwnerAgentID)
		if e != nil {
			return nil, e
		}
		if org != in.OrganizationID {
			return nil, domain.ErrForbidden("role owner must belong to the catalog organization")
		}
	}
	revision := current.Revision + 1
	if _, err = tx.ExecContext(ctx, `INSERT INTO external_role_catalogs(organization_id,revision) VALUES(?,?) ON CONFLICT(organization_id) DO UPDATE SET revision=excluded.revision`, in.OrganizationID, revision); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM external_role_scopes WHERE organization_id=?`, in.OrganizationID); err != nil {
		return nil, err
	}
	rules := append([]domain.ExternalRoleRule(nil), in.Rules...)
	sort.Slice(rules, func(i, j int) bool { return rules[i].Scope < rules[j].Scope })
	for _, rule := range rules {
		if _, err = tx.ExecContext(ctx, `INSERT INTO external_role_scopes(organization_id,scope,owner_agent_id,description) VALUES(?,?,?,?)`, in.OrganizationID, rule.Scope, rule.OwnerAgentID, rule.Description); err != nil {
			return nil, err
		}
	}
	result := &domain.ExternalRoleCatalog{OrganizationID: in.OrganizationID, Revision: revision, Rules: rules}
	if err = r.externalJournal(ctx, tx, owner, in.OrganizationID, "external_roles", in.OrganizationID, "external_roles.applied", result); err != nil {
		return nil, err
	}
	if err = commit(tx); err != nil {
		return nil, err
	}
	return result, nil
}
func (r *Repository) GetExternalRolesForOwner(ctx context.Context, owner, org string) (*domain.ExternalRoleCatalog, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = externalOwner(ctx, tx, owner); err != nil {
		return nil, err
	}
	return externalRolesTx(ctx, tx, org)
}
func (r *Repository) GetExternalRoles(ctx context.Context, digest string) (*domain.ExternalRoleCatalog, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	b, err := authenticateExternalTx(ctx, tx, digest, r.now())
	if err != nil {
		return nil, err
	}
	c, err := externalRolesTx(ctx, tx, b.OrganizationID)
	if err != nil {
		return nil, err
	}
	visible := []domain.ExternalRoleRule{}
	for _, rule := range c.Rules {
		if rule.OwnerAgentID == b.AgentID || hasExternalPeer(b, rule.OwnerAgentID) {
			visible = append(visible, rule)
		}
	}
	c.Rules = visible
	return c, nil
}
func validateExternalScope(ctx context.Context, tx *sql.Tx, org, sender, target, scope string) error {
	c, err := externalRolesTx(ctx, tx, org)
	if err != nil {
		return err
	}
	protected := false
	for _, rule := range c.Rules {
		if rule.Scope == scope {
			if rule.OwnerAgentID != target {
				return &domain.ExternalScopeError{Code: "OUT_OF_SCOPE", Scope: scope, OwnerAgentID: rule.OwnerAgentID, Message: "target does not own the declared scope"}
			}
			return nil
		}
		if rule.OwnerAgentID == sender || rule.OwnerAgentID == target {
			protected = true
		}
	}
	if scope != "" {
		return &domain.ExternalScopeError{Code: "UNKNOWN_SCOPE", Scope: scope, Message: "scope has no registered owner; clarification required"}
	}
	if protected {
		return &domain.ExternalScopeError{Code: "SCOPE_REQUIRED", Message: "registered domain agents require an explicit scope; clarification required"}
	}
	return nil
}
