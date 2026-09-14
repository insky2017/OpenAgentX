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

func (r *Repository) InstallationID(ctx context.Context) (string, error) {
	var installationID string
	if err := r.db.QueryRowContext(ctx, `SELECT installation_id FROM installation_metadata WHERE singleton=1`).Scan(&installationID); err != nil {
		return "", fmt.Errorf("read installation identity: %w", err)
	}
	if err := domain.ValidateOpaqueID("installation_id", installationID); err != nil {
		return "", fmt.Errorf("invalid persisted installation identity: %w", err)
	}
	return installationID, nil
}

func (r *Repository) ReplaceCLIToken(ctx context.Context, record *domain.CLITokenRecord) error {
	if record == nil {
		return domain.ErrInvalidInput("CLI Token is required")
	}
	if err := record.Validate(); err != nil {
		return err
	}
	scopesJSON, err := json.Marshal(record.Scopes)
	if err != nil {
		return fmt.Errorf("encode CLI Token scopes: %w", err)
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var installationID string
	if err := tx.QueryRowContext(ctx, `SELECT installation_id FROM installation_metadata WHERE singleton=1`).Scan(&installationID); err != nil {
		return fmt.Errorf("read installation identity: %w", err)
	}
	if installationID != record.InstallationID {
		return domain.ErrInvalidInput("CLI Token installation audience does not match")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE cli_tokens SET revoked_at=COALESCE(revoked_at, ?)
		WHERE web_user_id=? AND installation_id=? AND revoked_at IS NULL`, formatTime(record.CreatedAt), record.WebUserID, record.InstallationID); err != nil {
		return fmt.Errorf("revoke replaced CLI Tokens: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO cli_tokens (
		token_id,token_digest,web_user_id,principal_id,scopes_json,installation_id,
		created_at,last_used_at,absolute_expires_at,revoked_at
	) VALUES(?,?,?,?,?,?,?,?,?,NULL)`, record.ID, record.TokenDigest, record.WebUserID, record.PrincipalID,
		string(scopesJSON), record.InstallationID, formatTime(record.CreatedAt), formatTime(record.LastUsedAt),
		formatTime(record.AbsoluteExpiresAt)); err != nil {
		return fmt.Errorf("create CLI Token: %w", err)
	}
	if err := r.inject(FaultBeforeCommit); err != nil {
		return err
	}
	return commit(tx)
}

func (r *Repository) GetCLITokenByDigest(ctx context.Context, digest string) (*domain.CLITokenRecord, error) {
	var record domain.CLITokenRecord
	var scopesJSON, createdAt, lastUsedAt, expiresAt string
	var revokedAt sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT token_id,token_digest,web_user_id,principal_id,scopes_json,
		installation_id,created_at,last_used_at,absolute_expires_at,revoked_at
		FROM cli_tokens WHERE token_digest=?`, digest).Scan(&record.ID, &record.TokenDigest, &record.WebUserID,
		&record.PrincipalID, &scopesJSON, &record.InstallationID, &createdAt, &lastUsedAt, &expiresAt, &revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get CLI Token: %w", err)
	}
	if err := json.Unmarshal([]byte(scopesJSON), &record.Scopes); err != nil {
		return nil, fmt.Errorf("decode CLI Token scopes: %w", err)
	}
	if record.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if record.LastUsedAt, err = parseTime(lastUsedAt); err != nil {
		return nil, err
	}
	if record.AbsoluteExpiresAt, err = parseTime(expiresAt); err != nil {
		return nil, err
	}
	if revokedAt.Valid {
		value, err := parseTime(revokedAt.String)
		if err != nil {
			return nil, err
		}
		record.RevokedAt = &value
	}
	if err := record.Validate(); err != nil {
		return nil, fmt.Errorf("invalid persisted CLI Token: %w", err)
	}
	return &record, nil
}

func (r *Repository) TouchCLIToken(ctx context.Context, digest string, usedAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE cli_tokens SET last_used_at=?
		WHERE token_digest=? AND revoked_at IS NULL AND absolute_expires_at>?`, formatTime(usedAt), digest, formatTime(usedAt))
	if err != nil {
		return fmt.Errorf("touch CLI Token: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *Repository) RevokeCLIToken(ctx context.Context, tokenID, installationID string, revokedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE cli_tokens SET revoked_at=COALESCE(revoked_at, ?)
		WHERE token_id=? AND installation_id=?`, formatTime(revokedAt), tokenID, installationID)
	if err != nil {
		return fmt.Errorf("revoke CLI Token: %w", err)
	}
	return nil
}

func (r *Repository) RevokeCLITokensByWebUser(ctx context.Context, webUserID string, revokedAt time.Time) error {
	if err := domain.ValidateOpaqueID("web_user_id", webUserID); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `UPDATE cli_tokens SET revoked_at=COALESCE(revoked_at, ?)
		WHERE web_user_id=?`, formatTime(revokedAt), webUserID)
	if err != nil {
		return fmt.Errorf("revoke CLI Tokens for user: %w", err)
	}
	return nil
}
