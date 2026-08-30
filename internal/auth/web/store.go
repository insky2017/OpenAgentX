package web

import (
	"context"
	"sync"
	"time"

	"openagentx/internal/domain"
)

type SessionStore interface {
	CreateWebSession(context.Context, *domain.WebSessionRecord) error
	GetWebSession(context.Context, string) (*domain.WebSessionRecord, error)
	TouchWebSession(context.Context, string, time.Time, time.Time) error
	RotateWebSessionCSRF(context.Context, string, string, time.Time) error
	RevokeWebSession(context.Context, string, time.Time) error
}

type memorySessionStore struct {
	mu       sync.Mutex
	sessions map[string]domain.WebSessionRecord
}

func NewMemorySessionStore() SessionStore {
	return &memorySessionStore{sessions: make(map[string]domain.WebSessionRecord)}
}

func (s *memorySessionStore) CreateWebSession(_ context.Context, record *domain.WebSessionRecord) error {
	if record == nil {
		return domain.ErrInvalidInput("Web Session is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.sessions[record.SessionDigest]; exists {
		return domain.ErrConflict("Web Session already exists")
	}
	s.sessions[record.SessionDigest] = *record
	return nil
}

func (s *memorySessionStore) GetWebSession(_ context.Context, digest string) (*domain.WebSessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, exists := s.sessions[digest]
	if !exists {
		return nil, domain.ErrNotFound
	}
	return cloneSessionRecord(record), nil
}

func (s *memorySessionStore) TouchWebSession(_ context.Context, digest string, lastActivity, idleExpires time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, exists := s.sessions[digest]
	if !exists || record.RevokedAt != nil {
		return domain.ErrNotFound
	}
	record.LastActivityAt = lastActivity
	record.IdleExpiresAt = idleExpires
	s.sessions[digest] = record
	return nil
}

func (s *memorySessionStore) RotateWebSessionCSRF(_ context.Context, digest, csrfDigest string, updatedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, exists := s.sessions[digest]
	if !exists || record.RevokedAt != nil {
		return domain.ErrNotFound
	}
	record.CSRFDigest = csrfDigest
	record.LastActivityAt = updatedAt
	s.sessions[digest] = record
	return nil
}

func (s *memorySessionStore) RevokeWebSession(_ context.Context, digest string, revokedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, exists := s.sessions[digest]
	if !exists {
		return nil
	}
	value := revokedAt.UTC()
	record.RevokedAt = &value
	s.sessions[digest] = record
	return nil
}

func cloneSessionRecord(record domain.WebSessionRecord) *domain.WebSessionRecord {
	if record.RevokedAt != nil {
		value := *record.RevokedAt
		record.RevokedAt = &value
	}
	return &record
}
