package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"openagentx/internal/domain"
)

func insertJournal(ctx context.Context, tx *sql.Tx, event *domain.JournalEvent) error {
	result, err := tx.ExecContext(ctx, `INSERT INTO event_journal (
		event_id, organization_id, aggregate_type, aggregate_id, event_type,
		actor_principal_id, payload_json, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, nullableOrganization(event.OrganizationID), event.AggregateType, event.AggregateID,
		event.EventType, event.ActorPrincipalID, string(event.Payload), formatTime(event.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("append event journal: %w", err)
	}
	sequence, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("read event journal sequence: %w", err)
	}
	event.Sequence = sequence
	return nil
}

func nullableOrganization(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (r *Repository) ListJournal(ctx context.Context, afterSequence int64, limit int) ([]domain.JournalEvent, error) {
	if afterSequence < 0 {
		return nil, domain.ErrInvalidInput("after_sequence cannot be negative")
	}
	if limit <= 0 || limit > 1000 {
		return nil, domain.ErrInvalidInput("journal limit must be between 1 and 1000")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT sequence, event_id, organization_id, aggregate_type,
		aggregate_id, event_type, actor_principal_id, payload_json, created_at
		FROM event_journal WHERE sequence > ? ORDER BY sequence ASC LIMIT ?`, afterSequence, limit)
	if err != nil {
		return nil, fmt.Errorf("list event journal: %w", err)
	}
	defer rows.Close()

	events := make([]domain.JournalEvent, 0)
	for rows.Next() {
		var event domain.JournalEvent
		var organization sql.NullString
		var payload string
		var createdAt string
		if err := rows.Scan(&event.Sequence, &event.ID, &organization, &event.AggregateType,
			&event.AggregateID, &event.EventType, &event.ActorPrincipalID, &payload, &createdAt); err != nil {
			return nil, fmt.Errorf("scan event journal: %w", err)
		}
		if organization.Valid {
			event.OrganizationID = organization.String
		}
		event.Payload = []byte(payload)
		event.CreatedAt, err = parseTime(createdAt)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate event journal: %w", err)
	}
	return events, nil
}
