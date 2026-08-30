package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"openagentx/internal/domain"
	"openagentx/internal/persistence/sqlite/migrations"
)

const sqliteTimeLayout = time.RFC3339Nano

type FaultPoint string

const (
	FaultAfterStateWrite FaultPoint = "after_state_write"
	FaultAfterDelivery   FaultPoint = "after_delivery_write"
	FaultBeforeCommit    FaultPoint = "before_commit"
)

type Options struct {
	Now           func() time.Time
	FaultInjector func(FaultPoint) error
	MaxOpenConns  int
}

type Repository struct {
	db            *sql.DB
	now           func() time.Time
	faultInjector func(FaultPoint) error
}

func Open(ctx context.Context, databasePath string, options Options) (*Repository, error) {
	if strings.TrimSpace(databasePath) == "" {
		return nil, fmt.Errorf("database path cannot be empty")
	}
	absPath, err := filepath.Abs(databasePath)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	query := make(url.Values)
	query.Set("_journal_mode", "WAL")
	query.Set("_busy_timeout", "5000")
	query.Set("_foreign_keys", "ON")
	query.Set("_txlock", "immediate")
	dsn := (&url.URL{Scheme: "file", Path: absPath, RawQuery: query.Encode()}).String()
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	maxOpenConns := options.MaxOpenConns
	if maxOpenConns <= 0 {
		maxOpenConns = 8
	}
	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxOpenConns)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := migrations.Apply(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	repository := newRepository(db, options)
	return repository, nil
}

func newRepository(db *sql.DB, options Options) *Repository {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Repository{db: db, now: now, faultInjector: options.FaultInjector}
}

func (r *Repository) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

func (r *Repository) SchemaVersion(ctx context.Context) (int, error) {
	return migrations.Version(ctx, r.db)
}

func (r *Repository) inject(point FaultPoint) error {
	if r.faultInjector == nil {
		return nil
	}
	if err := r.faultInjector(point); err != nil {
		return fmt.Errorf("fault injected at %s: %w", point, err)
	}
	return nil
}

func (r *Repository) begin(ctx context.Context) (*sql.Tx, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin sqlite transaction: %w", err)
	}
	return tx, nil
}

func commit(tx *sql.Tx) error {
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit sqlite transaction: %w", err)
	}
	return nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format(sqliteTimeLayout)
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(sqliteTimeLayout, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse sqlite timestamp %q: %w", value, err)
	}
	return parsed, nil
}

func normalizeTime(value time.Time, fallback time.Time) time.Time {
	if value.IsZero() {
		return fallback.UTC()
	}
	return value.UTC()
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func isUniqueConstraint(err error, name string) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "UNIQUE constraint failed") || (name != "" && strings.Contains(message, name))
}

func validateJournalForAggregate(event *domain.JournalEvent, aggregateType string, aggregateID string, now time.Time) error {
	if event == nil {
		return domain.ErrInvalidInput("journal event is required")
	}
	event.Sequence = 0
	if event.AggregateType == "" {
		event.AggregateType = aggregateType
	}
	if event.AggregateID == "" {
		event.AggregateID = aggregateID
	}
	if event.AggregateType != aggregateType || event.AggregateID != aggregateID {
		return domain.ErrInvalidInput("journal aggregate does not match transaction target")
	}
	event.CreatedAt = normalizeTime(event.CreatedAt, now)
	return event.Validate()
}
