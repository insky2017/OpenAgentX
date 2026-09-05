package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"openagentx/internal/domain"
)

func (r *Repository) CreateProxyProfile(ctx context.Context, profile *domain.ProxyProfile) error {
	if profile == nil {
		return domain.ErrInvalidInput("network profile is required")
	}
	if err := profile.Validate(); err != nil {
		return err
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO network_profiles(profile_id, version, status, mode, host, port, config_file, secret_ref, created_by, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, profile.ID, profile.Version, profile.Status, profile.Mode, profile.Host, profile.Port, nullableString(valueOrNil(profile.ConfigFile)), nullableString(valueOrNil(profile.SecretRef)), profile.CreatedBy, formatTime(profile.CreatedAt), formatTime(profile.UpdatedAt))
	if err != nil {
		return fmt.Errorf("create network profile: %w", err)
	}
	return commit(tx)
}

func (r *Repository) GetProxyProfile(ctx context.Context, id string, version int64) (*domain.ProxyProfile, error) {
	query := `SELECT profile_id, version, status, mode, host, port, config_file, secret_ref, created_by, created_at, updated_at FROM network_profiles WHERE profile_id=?`
	args := []any{id}
	if version > 0 {
		query += " AND version=?"
		args = append(args, version)
	} else {
		query += " ORDER BY version DESC LIMIT 1"
	}
	var p domain.ProxyProfile
	var configFile, secretRef, createdAt, updatedAt sql.NullString
	err := r.db.QueryRowContext(ctx, query, args...).Scan(&p.ID, &p.Version, &p.Status, &p.Mode, &p.Host, &p.Port, &configFile, &secretRef, &p.CreatedBy, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get network profile: %w", err)
	}
	p.ConfigFile, p.SecretRef = configFile.String, secretRef.String
	p.CreatedAt, err = parseTime(createdAt.String)
	if err != nil {
		return nil, err
	}
	p.UpdatedAt, err = parseTime(updatedAt.String)
	if err != nil {
		return nil, err
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Repository) ListProxyProfiles(ctx context.Context, limit int) ([]domain.ProxyProfile, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	rows, err := r.db.QueryContext(ctx, `SELECT p.profile_id, p.version, p.status, p.mode, p.host, p.port, p.config_file, p.secret_ref, p.created_by, p.created_at, p.updated_at FROM network_profiles p JOIN (SELECT profile_id, MAX(version) version FROM network_profiles GROUP BY profile_id) latest ON latest.profile_id=p.profile_id AND latest.version=p.version ORDER BY p.profile_id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	profiles := make([]domain.ProxyProfile, 0)
	for rows.Next() {
		var p domain.ProxyProfile
		var configFile, secretRef, createdAt, updatedAt sql.NullString
		if err := rows.Scan(&p.ID, &p.Version, &p.Status, &p.Mode, &p.Host, &p.Port, &configFile, &secretRef, &p.CreatedBy, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		p.ConfigFile, p.SecretRef = configFile.String, secretRef.String
		p.CreatedAt, err = parseTime(createdAt.String)
		if err != nil {
			return nil, err
		}
		p.UpdatedAt, err = parseTime(updatedAt.String)
		if err != nil {
			return nil, err
		}
		if err := p.Validate(); err != nil {
			return nil, err
		}
		profiles = append(profiles, p)
	}
	return profiles, rows.Err()
}

func (r *Repository) PublishProxyProfile(ctx context.Context, id string, expectedVersion int64, actor string, now time.Time) (*domain.ProxyProfile, error) {
	if expectedVersion <= 0 {
		return nil, domain.ErrInvalidInput("expected profile version must be positive")
	}
	if err := domain.ValidateOpaqueID("profile actor", actor); err != nil {
		return nil, err
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var p domain.ProxyProfile
	var configFile, secretRef, createdAt, updatedAt string
	err = tx.QueryRowContext(ctx, `SELECT profile_id, version, status, mode, host, port, COALESCE(config_file,''), COALESCE(secret_ref,''), created_by, created_at, updated_at FROM network_profiles WHERE profile_id=? AND version=?`, id, expectedVersion).Scan(&p.ID, &p.Version, &p.Status, &p.Mode, &p.Host, &p.Port, &configFile, &secretRef, &p.CreatedBy, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, domain.ErrConflict("network profile version is stale")
	}
	if err != nil {
		return nil, err
	}
	if p.Status == domain.NetworkProfilePublished {
		return nil, domain.ErrConflict("network profile is already published")
	}
	p.Version++
	p.Status = domain.NetworkProfilePublished
	p.CreatedBy = actor
	p.ConfigFile, p.SecretRef = configFile, secretRef
	p.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	p.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return nil, err
	}
	p.UpdatedAt = now.UTC()
	if err := p.Validate(); err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO network_profiles(profile_id, version, status, mode, host, port, config_file, secret_ref, created_by, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, p.ID, p.Version, p.Status, p.Mode, p.Host, p.Port, nullableString(valueOrNil(p.ConfigFile)), nullableString(valueOrNil(p.SecretRef)), p.CreatedBy, formatTime(p.CreatedAt), formatTime(p.UpdatedAt))
	if err != nil {
		return nil, err
	}
	if err := commit(tx); err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Repository) BindNetworkProfile(ctx context.Context, binding *domain.NetworkBinding, expectedVersion int64) error {
	if binding == nil {
		return domain.ErrInvalidInput("network binding is required")
	}
	if expectedVersion == 0 {
		if binding.Version == 0 {
			binding.Version = 1
		}
		if binding.DesiredStatus == "" {
			binding.DesiredStatus = "pending"
		}
	}
	if err := binding.Validate(); err != nil {
		return err
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM network_profiles WHERE profile_id=? AND version=?`, binding.ProfileID, binding.ProfileVersion).Scan(&status); err == sql.ErrNoRows {
		return domain.ErrNotFound
	} else if err != nil {
		return err
	}
	if status != string(domain.NetworkProfilePublished) {
		return domain.ErrConflict("only published network profiles can be bound")
	}
	if expectedVersion == 0 {
		binding.Version = 1
		_, err = tx.ExecContext(ctx, `INSERT INTO network_profile_bindings(agent_id, backend_id, profile_id, profile_version, version, desired_status, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?)`, binding.AgentID, binding.BackendID, binding.ProfileID, binding.ProfileVersion, binding.Version, "pending", formatTime(binding.UpdatedAt))
	} else {
		binding.Version = expectedVersion + 1
		result, execErr := tx.ExecContext(ctx, `UPDATE network_profile_bindings SET profile_id=?, profile_version=?, version=?, desired_status='pending', applied_worker_id=NULL, applied_generation=NULL, applied_profile_version=NULL, updated_at=? WHERE agent_id=? AND backend_id=? AND version=?`, binding.ProfileID, binding.ProfileVersion, binding.Version, formatTime(binding.UpdatedAt), binding.AgentID, binding.BackendID, expectedVersion)
		if execErr == nil {
			var count int64
			count, execErr = result.RowsAffected()
			if count != 1 {
				execErr = domain.ErrConflict("network binding version is stale")
			}
		}
		err = execErr
	}
	if err != nil {
		return fmt.Errorf("bind network profile: %w", err)
	}
	return commit(tx)
}

func (r *Repository) ListNetworkBindings(ctx context.Context, agentID string) ([]domain.NetworkBinding, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT agent_id, backend_id, profile_id, profile_version, version, desired_status, COALESCE(applied_worker_id,''), COALESCE(applied_generation,0), COALESCE(applied_profile_version,0), updated_at FROM network_profile_bindings WHERE (?='' OR agent_id=?) ORDER BY agent_id, backend_id`, agentID, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.NetworkBinding, 0)
	for rows.Next() {
		var b domain.NetworkBinding
		var updated string
		if err := rows.Scan(&b.AgentID, &b.BackendID, &b.ProfileID, &b.ProfileVersion, &b.Version, &b.DesiredStatus, &b.AppliedWorkerID, &b.AppliedGeneration, &b.AppliedProfileVersion, &updated); err != nil {
			return nil, err
		}
		b.UpdatedAt, err = parseTime(updated)
		if err != nil {
			return nil, err
		}
		if err := b.Validate(); err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

func valueOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
