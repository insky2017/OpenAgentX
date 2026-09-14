package cli

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	webauth "openagentx/internal/auth/web"
	"openagentx/internal/domain"
)

type memoryRepository struct {
	mu             sync.Mutex
	installationID string
	users          map[string]domain.WebUserRecord
	tokens         map[string]domain.CLITokenRecord
}

func newMemoryRepository(t *testing.T, roles ...domain.WebRole) *memoryRepository {
	t.Helper()
	digest, err := webauth.HashPassword("correct password")
	if err != nil {
		t.Fatal(err)
	}
	user := domain.WebUserRecord{ID: "web-owner", PrincipalID: "human-owner", Username: "owner",
		PasswordDigest: digest, Roles: roles, Status: domain.IdentityActive,
		PasswordSetAt: time.Now().Add(-time.Hour), CreatedAt: time.Now().Add(-time.Hour), UpdatedAt: time.Now().Add(-time.Hour)}
	return &memoryRepository{installationID: "installation-a", users: map[string]domain.WebUserRecord{user.ID: user}, tokens: make(map[string]domain.CLITokenRecord)}
}

func (r *memoryRepository) InstallationID(context.Context) (string, error) {
	return r.installationID, nil
}
func (r *memoryRepository) GetWebUserByUsername(_ context.Context, username string) (*domain.WebUserRecord, error) {
	for _, user := range r.users {
		if user.Username == username {
			copy := user
			return &copy, nil
		}
	}
	return nil, domain.ErrNotFound
}
func (r *memoryRepository) GetWebUserByID(_ context.Context, id string) (*domain.WebUserRecord, error) {
	user, ok := r.users[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	copy := user
	return &copy, nil
}
func (r *memoryRepository) ReplaceCLIToken(_ context.Context, record *domain.CLITokenRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for digest, candidate := range r.tokens {
		if candidate.WebUserID == record.WebUserID && candidate.InstallationID == record.InstallationID && candidate.RevokedAt == nil {
			now := record.CreatedAt
			candidate.RevokedAt = &now
			r.tokens[digest] = candidate
		}
	}
	r.tokens[record.TokenDigest] = *record
	return nil
}
func (r *memoryRepository) GetCLITokenByDigest(_ context.Context, digest string) (*domain.CLITokenRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.tokens[digest]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &record, nil
}
func (r *memoryRepository) TouchCLIToken(_ context.Context, digest string, usedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.tokens[digest]
	if !ok || record.RevokedAt != nil || !usedAt.Before(record.AbsoluteExpiresAt) {
		return domain.ErrNotFound
	}
	record.LastUsedAt = usedAt
	r.tokens[digest] = record
	return nil
}
func (r *memoryRepository) RevokeCLIToken(_ context.Context, tokenID, installationID string, revokedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for digest, record := range r.tokens {
		if record.ID == tokenID && record.InstallationID == installationID && record.RevokedAt == nil {
			record.RevokedAt = &revokedAt
			r.tokens[digest] = record
		}
	}
	return nil
}
func (r *memoryRepository) RevokeCLITokensByWebUser(_ context.Context, webUserID string, revokedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for digest, record := range r.tokens {
		if record.WebUserID == webUserID && record.RevokedAt == nil {
			record.RevokedAt = &revokedAt
			r.tokens[digest] = record
		}
	}
	return nil
}

func TestTokenLifecycleIsAbsoluteRevocableAndReplaceLoginRevokesOld(t *testing.T) {
	now := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	repository := newMemoryRepository(t, domain.WebRoleOwner)
	service, err := NewService(repository, Config{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.Login(context.Background(), "owner", "correct password")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Token) < 43 || first.Principal.ExpiresAt != now.Add(MaximumTTL) {
		t.Fatalf("token entropy/expiry mismatch length=%d expires=%s", len(first.Token), first.Principal.ExpiresAt)
	}
	if _, ok := repository.tokens[first.Token]; ok {
		t.Fatal("repository indexed a raw CLI Token")
	}
	if _, err := service.Authenticate(context.Background(), first.Token); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	second, err := service.Login(context.Background(), "owner", "correct password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(context.Background(), first.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("replaced token accepted: %v", err)
	}
	if err := service.Revoke(context.Background(), second.Token); err != nil {
		t.Fatal(err)
	}
	if err := service.Revoke(context.Background(), second.Token); err != nil {
		t.Fatalf("repeated logout was not idempotent: %v", err)
	}
	if _, err := service.Authenticate(context.Background(), second.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("revoked token accepted: %v", err)
	}
}

func TestAuthenticationFailsClosedForCredentialsExpiryAudienceAndUserState(t *testing.T) {
	now := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	repository := newMemoryRepository(t, domain.WebRoleOperator)
	service, _ := NewService(repository, Config{TTL: time.Hour, Now: func() time.Time { return now }})
	if _, err := service.Login(context.Background(), "owner", "wrong"); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("wrong password error=%v", err)
	}
	issued, err := service.Login(context.Background(), "owner", "correct password")
	if err != nil {
		t.Fatal(err)
	}
	if err := Require(issued.Principal, domain.WebRoleOperator, domain.CLIScopeConsoleControl); err != nil {
		t.Fatal(err)
	}
	if err := Require(issued.Principal, domain.WebRoleOwner, domain.CLIScopeFleetLifecycle); !errors.Is(err, ErrForbidden) {
		t.Fatalf("operator lifecycle authorization error=%v", err)
	}
	repository.installationID = "installation-b"
	if _, err := service.Authenticate(context.Background(), issued.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("cross-installation token accepted: %v", err)
	}
	repository.installationID = "installation-a"
	user := repository.users["web-owner"]
	user.Roles = []domain.WebRole{domain.WebRoleViewer}
	repository.users[user.ID] = user
	if _, err := service.Authenticate(context.Background(), issued.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("role-downgraded token retained excessive scopes: %v", err)
	}
	user.Roles = []domain.WebRole{domain.WebRoleOperator}
	repository.users[user.ID] = user
	now = now.Add(time.Hour)
	if _, err := service.Authenticate(context.Background(), issued.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expired token accepted: %v", err)
	}
	now = now.Add(-30 * time.Minute)
	user = repository.users["web-owner"]
	user.Status = domain.IdentityDisabled
	repository.users[user.ID] = user
	if _, err := service.Authenticate(context.Background(), issued.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("disabled user token accepted: %v", err)
	}
}

func TestBearerParsingAndRevokeAllHook(t *testing.T) {
	repository := newMemoryRepository(t, domain.WebRoleViewer)
	service, _ := NewService(repository, Config{})
	issued, err := service.Login(context.Background(), "owner", "correct password")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/", nil)
	request.Header.Set("Authorization", "Bearer "+issued.Token)
	if _, err := service.AuthenticateRequest(request); err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "bearer "+issued.Token)
	if _, err := service.AuthenticateRequest(request); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("non-canonical bearer accepted: %v", err)
	}
	if err := service.RevokeAllForWebUser(context.Background(), issued.Principal.WebUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(context.Background(), issued.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("password-change revoke hook left token active: %v", err)
	}
}

func TestFrozenRoleScopeMatrix(t *testing.T) {
	tests := []struct {
		name    string
		role    domain.WebRole
		require domain.WebRole
		scope   domain.CLIScope
		allowed bool
	}{
		{name: "viewer read", role: domain.WebRoleViewer, require: domain.WebRoleViewer, scope: domain.CLIScopeConsoleRead, allowed: true},
		{name: "viewer control", role: domain.WebRoleViewer, require: domain.WebRoleOperator, scope: domain.CLIScopeConsoleControl},
		{name: "operator read", role: domain.WebRoleOperator, require: domain.WebRoleViewer, scope: domain.CLIScopeConsoleRead, allowed: true},
		{name: "operator control", role: domain.WebRoleOperator, require: domain.WebRoleOperator, scope: domain.CLIScopeConsoleControl, allowed: true},
		{name: "operator diagnostic", role: domain.WebRoleOperator, require: domain.WebRoleOwner, scope: domain.CLIScopeConsoleDiagnostic},
		{name: "operator lifecycle", role: domain.WebRoleOperator, require: domain.WebRoleOwner, scope: domain.CLIScopeFleetLifecycle},
		{name: "owner diagnostic", role: domain.WebRoleOwner, require: domain.WebRoleOwner, scope: domain.CLIScopeConsoleDiagnostic, allowed: true},
		{name: "owner lifecycle", role: domain.WebRoleOwner, require: domain.WebRoleOwner, scope: domain.CLIScopeFleetLifecycle, allowed: true},
		{name: "unknown scope", role: domain.WebRoleOwner, require: domain.WebRoleOwner, scope: "", allowed: false},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			repository := newMemoryRepository(t, testCase.role)
			service, _ := NewService(repository, Config{})
			issued, err := service.Login(context.Background(), "owner", "correct password")
			if err != nil {
				t.Fatal(err)
			}
			err = Require(issued.Principal, testCase.require, testCase.scope)
			if (err == nil) != testCase.allowed {
				t.Fatalf("Require error=%v allowed=%v scopes=%v", err, testCase.allowed, issued.Principal.Scopes)
			}
		})
	}
}
