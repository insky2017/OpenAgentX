package cli

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	webAuth "openagentx/internal/auth/web"
	"openagentx/internal/domain"
)

const MaximumTTL = 30 * 24 * time.Hour

var (
	ErrUnauthenticated = errors.New("CLI unauthenticated")
	ErrForbidden       = errors.New("CLI forbidden")
)

type Repository interface {
	InstallationID(context.Context) (string, error)
	GetWebUserByUsername(context.Context, string) (*domain.WebUserRecord, error)
	GetWebUserByID(context.Context, string) (*domain.WebUserRecord, error)
	ReplaceCLIToken(context.Context, *domain.CLITokenRecord) error
	GetCLITokenByDigest(context.Context, string) (*domain.CLITokenRecord, error)
	TouchCLIToken(context.Context, string, time.Time) error
	RevokeCLIToken(context.Context, string, string, time.Time) error
	RevokeCLITokensByWebUser(context.Context, string, time.Time) error
}

type Config struct {
	TTL  time.Duration
	Now  func() time.Time
	Rand io.Reader
}

type Service struct {
	repository Repository
	ttl        time.Duration
	now        func() time.Time
	random     io.Reader
}

type Principal struct {
	TokenID        string
	WebUserID      string
	PrincipalID    string
	Username       string
	Roles          []domain.WebRole
	Scopes         []domain.CLIScope
	InstallationID string
	ExpiresAt      time.Time
}

type IssuedToken struct {
	Token     string
	Principal Principal
}

func NewService(repository Repository, config Config) (*Service, error) {
	if repository == nil {
		return nil, fmt.Errorf("CLI Token repository is required")
	}
	if config.TTL == 0 {
		config.TTL = MaximumTTL
	}
	if config.TTL < 0 || config.TTL > MaximumTTL {
		return nil, fmt.Errorf("CLI Token TTL must be positive and no greater than 30 days")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Rand == nil {
		config.Rand = rand.Reader
	}
	return &Service{repository: repository, ttl: config.TTL, now: config.Now, random: config.Rand}, nil
}

func (s *Service) InstallationID(ctx context.Context) (string, error) {
	installationID, err := s.repository.InstallationID(ctx)
	if err != nil {
		return "", err
	}
	if err := domain.ValidateOpaqueID("installation_id", installationID); err != nil {
		return "", err
	}
	return installationID, nil
}

func (s *Service) Login(ctx context.Context, username, password string) (IssuedToken, error) {
	user, err := s.repository.GetWebUserByUsername(ctx, strings.TrimSpace(username))
	if err != nil || user.Status != domain.IdentityActive || !webAuth.VerifyPassword(user.PasswordDigest, password) {
		return IssuedToken{}, ErrUnauthenticated
	}
	if err := user.Validate(); err != nil {
		return IssuedToken{}, ErrUnauthenticated
	}
	scopes := scopesForRoles(user.Roles)
	if len(scopes) == 0 {
		return IssuedToken{}, ErrForbidden
	}
	installationID, err := s.InstallationID(ctx)
	if err != nil {
		return IssuedToken{}, fmt.Errorf("read CLI Token audience: %w", err)
	}
	token, err := s.randomValue(32)
	if err != nil {
		return IssuedToken{}, fmt.Errorf("generate CLI Token: %w", err)
	}
	tokenID, err := s.randomValue(16)
	if err != nil {
		return IssuedToken{}, fmt.Errorf("generate CLI Token ID: %w", err)
	}
	now := s.now().UTC()
	record := &domain.CLITokenRecord{ID: tokenID, TokenDigest: tokenDigest(token), WebUserID: user.ID,
		PrincipalID: user.PrincipalID, Scopes: scopes, InstallationID: installationID,
		CreatedAt: now, LastUsedAt: now, AbsoluteExpiresAt: now.Add(s.ttl)}
	if err := s.repository.ReplaceCLIToken(ctx, record); err != nil {
		return IssuedToken{}, err
	}
	return IssuedToken{Token: token, Principal: principalFrom(user, record)}, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (Principal, error) {
	if !validOpaqueToken(token) {
		return Principal{}, ErrUnauthenticated
	}
	digest := tokenDigest(token)
	record, err := s.repository.GetCLITokenByDigest(ctx, digest)
	if err != nil {
		return Principal{}, ErrUnauthenticated
	}
	if err := record.Validate(); err != nil || record.AbsoluteExpiresAt.Sub(record.CreatedAt) > MaximumTTL ||
		subtle.ConstantTimeCompare([]byte(record.TokenDigest), []byte(digest)) != 1 {
		return Principal{}, ErrUnauthenticated
	}
	installationID, err := s.InstallationID(ctx)
	if err != nil || subtle.ConstantTimeCompare([]byte(record.InstallationID), []byte(installationID)) != 1 {
		return Principal{}, ErrUnauthenticated
	}
	now := s.now().UTC()
	if record.RevokedAt != nil || !now.Before(record.AbsoluteExpiresAt) {
		return Principal{}, ErrUnauthenticated
	}
	user, err := s.repository.GetWebUserByID(ctx, record.WebUserID)
	if err != nil || user.Status != domain.IdentityActive || user.PrincipalID != record.PrincipalID {
		return Principal{}, ErrUnauthenticated
	}
	if err := user.Validate(); err != nil {
		return Principal{}, ErrUnauthenticated
	}
	if !scopesAllowedByRoles(record.Scopes, user.Roles) {
		return Principal{}, ErrUnauthenticated
	}
	if err := s.repository.TouchCLIToken(ctx, digest, now); err != nil {
		return Principal{}, ErrUnauthenticated
	}
	record.LastUsedAt = now
	return principalFrom(user, record), nil
}

func (s *Service) AuthenticateRequest(r *http.Request) (Principal, error) {
	if r == nil {
		return Principal{}, ErrUnauthenticated
	}
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || parts[0] != "Bearer" {
		return Principal{}, ErrUnauthenticated
	}
	return s.Authenticate(r.Context(), parts[1])
}

func (s *Service) Revoke(ctx context.Context, token string) error {
	if !validOpaqueToken(token) {
		return ErrUnauthenticated
	}
	digest := tokenDigest(token)
	record, err := s.repository.GetCLITokenByDigest(ctx, digest)
	if err != nil {
		return ErrUnauthenticated
	}
	if err := record.Validate(); err != nil || record.AbsoluteExpiresAt.Sub(record.CreatedAt) > MaximumTTL ||
		subtle.ConstantTimeCompare([]byte(record.TokenDigest), []byte(digest)) != 1 {
		return ErrUnauthenticated
	}
	installationID, err := s.InstallationID(ctx)
	if err != nil || subtle.ConstantTimeCompare([]byte(record.InstallationID), []byte(installationID)) != 1 {
		return ErrUnauthenticated
	}
	now := s.now().UTC()
	if !now.Before(record.AbsoluteExpiresAt) {
		return ErrUnauthenticated
	}
	if record.RevokedAt != nil {
		return nil
	}
	return s.repository.RevokeCLIToken(ctx, record.ID, record.InstallationID, now)
}

func (s *Service) RevokeRequest(r *http.Request) error {
	if r == nil {
		return ErrUnauthenticated
	}
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || parts[0] != "Bearer" {
		return ErrUnauthenticated
	}
	return s.Revoke(r.Context(), parts[1])
}

func (s *Service) RevokeAllForWebUser(ctx context.Context, webUserID string) error {
	return s.repository.RevokeCLITokensByWebUser(ctx, webUserID, s.now().UTC())
}

func Require(principal Principal, role domain.WebRole, scope domain.CLIScope) error {
	if !scope.Valid() || !hasRole(principal.Roles, role) {
		return ErrForbidden
	}
	for _, candidate := range principal.Scopes {
		if candidate == scope {
			return nil
		}
	}
	return ErrForbidden
}

func (s *Service) randomValue(bytes int) (string, error) {
	raw := make([]byte, bytes)
	if _, err := io.ReadFull(s.random, raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func tokenDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func validOpaqueToken(token string) bool {
	if strings.TrimSpace(token) != token || token == "" {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(raw) >= 32
}

func principalFrom(user *domain.WebUserRecord, record *domain.CLITokenRecord) Principal {
	return Principal{TokenID: record.ID, WebUserID: user.ID, PrincipalID: user.PrincipalID, Username: user.Username,
		Roles: append([]domain.WebRole(nil), user.Roles...), Scopes: append([]domain.CLIScope(nil), record.Scopes...),
		InstallationID: record.InstallationID, ExpiresAt: record.AbsoluteExpiresAt}
}

func scopesForRoles(roles []domain.WebRole) []domain.CLIScope {
	if hasRole(roles, domain.WebRoleOwner) {
		return []domain.CLIScope{domain.CLIScopeConsoleRead, domain.CLIScopeConsoleControl, domain.CLIScopeConsoleDiagnostic, domain.CLIScopeFleetLifecycle}
	}
	if hasRole(roles, domain.WebRoleOperator) {
		return []domain.CLIScope{domain.CLIScopeConsoleRead, domain.CLIScopeConsoleControl}
	}
	if hasRole(roles, domain.WebRoleViewer) {
		return []domain.CLIScope{domain.CLIScopeConsoleRead}
	}
	return nil
}

func scopesAllowedByRoles(scopes []domain.CLIScope, roles []domain.WebRole) bool {
	allowed := scopesForRoles(roles)
	for _, scope := range scopes {
		found := false
		for _, candidate := range allowed {
			if scope == candidate {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return len(scopes) > 0
}

func hasRole(roles []domain.WebRole, required domain.WebRole) bool {
	requiredRank := cliRoleRank(required)
	if requiredRank == 0 {
		return false
	}
	for _, role := range roles {
		if cliRoleRank(role) >= requiredRank {
			return true
		}
	}
	return false
}

func cliRoleRank(role domain.WebRole) int {
	switch role {
	case domain.WebRoleOwner:
		return 3
	case domain.WebRoleOperator:
		return 2
	case domain.WebRoleViewer:
		return 1
	default:
		return 0
	}
}
