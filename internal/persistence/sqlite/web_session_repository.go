package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"openagentx/internal/domain"
)

func (r *Repository) CreateWebSession(ctx context.Context, record *domain.WebSessionRecord) error {
	if record == nil {
		return domain.ErrInvalidInput("Web Session is required")
	}
	if err := record.Validate(); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO web_sessions (
		web_session_id, web_user_id, session_digest, csrf_digest, created_at,
		last_activity_at, idle_expires_at, absolute_expires_at, revoked_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL)`, record.ID, record.WebUserID, record.SessionDigest,
		record.CSRFDigest, formatTime(record.CreatedAt), formatTime(record.LastActivityAt),
		formatTime(record.IdleExpiresAt), formatTime(record.AbsoluteExpiresAt))
	if err != nil {
		return fmt.Errorf("create Web Session: %w", err)
	}
	return nil
}

func (r *Repository) GetWebSession(ctx context.Context, sessionDigest string) (*domain.WebSessionRecord, error) {
	var record domain.WebSessionRecord
	var createdAt, lastActivityAt, idleExpiresAt, absoluteExpiresAt string
	var revokedAt sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT web_session_id, web_user_id, session_digest, csrf_digest,
		created_at, last_activity_at, idle_expires_at, absolute_expires_at, revoked_at
		FROM web_sessions WHERE session_digest=?`, sessionDigest).Scan(&record.ID, &record.WebUserID,
		&record.SessionDigest, &record.CSRFDigest, &createdAt, &lastActivityAt, &idleExpiresAt,
		&absoluteExpiresAt, &revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get Web Session: %w", err)
	}
	if record.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if record.LastActivityAt, err = parseTime(lastActivityAt); err != nil {
		return nil, err
	}
	if record.IdleExpiresAt, err = parseTime(idleExpiresAt); err != nil {
		return nil, err
	}
	if record.AbsoluteExpiresAt, err = parseTime(absoluteExpiresAt); err != nil {
		return nil, err
	}
	if revokedAt.Valid {
		parsed, err := parseTime(revokedAt.String)
		if err != nil {
			return nil, err
		}
		record.RevokedAt = &parsed
	}
	return &record, nil
}

func (r *Repository) TouchWebSession(ctx context.Context, sessionDigest string, lastActivityAt, idleExpiresAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE web_sessions SET last_activity_at=?, idle_expires_at=?
		WHERE session_digest=? AND revoked_at IS NULL AND absolute_expires_at>?`, formatTime(lastActivityAt),
		formatTime(idleExpiresAt), sessionDigest, formatTime(lastActivityAt))
	if err != nil {
		return fmt.Errorf("touch Web Session: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *Repository) RotateWebSessionCSRF(ctx context.Context, sessionDigest, csrfDigest string, updatedAt time.Time) error {
	if csrfDigest == "" {
		return domain.ErrInvalidInput("CSRF digest is required")
	}
	result, err := r.db.ExecContext(ctx, `UPDATE web_sessions SET csrf_digest=?, last_activity_at=?
		WHERE session_digest=? AND revoked_at IS NULL AND absolute_expires_at>?`, csrfDigest,
		formatTime(updatedAt), sessionDigest, formatTime(updatedAt))
	if err != nil {
		return fmt.Errorf("rotate Web Session CSRF: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *Repository) RevokeWebSession(ctx context.Context, sessionDigest string, revokedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE web_sessions SET revoked_at=COALESCE(revoked_at, ?)
		WHERE session_digest=?`, formatTime(revokedAt), sessionDigest)
	if err != nil {
		return fmt.Errorf("revoke Web Session: %w", err)
	}
	return nil
}
