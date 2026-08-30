package web

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
	"openagentx/internal/domain"
)

type Role string

const (
	RoleOwner    Role = "owner"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

type User struct {
	ID, WebUserID, Username string
	Roles                   []Role
	PasswordDigest          string
}
type Session struct {
	IDDigest, token, CSRFDigest                           string
	User                                                  User
	CSRFToken                                             string
	CreatedAt, LastSeen, IdleExpiresAt, AbsoluteExpiresAt time.Time
	Revoked                                               bool
}
type Config struct {
	IdleTimeout, AbsoluteTimeout time.Duration
	Store                        SessionStore
	Now                          func() time.Time
}
type Manager struct {
	mu           sync.RWMutex
	users        map[string]User
	usersByWebID map[string]User
	config       Config
}

func NewManager(config Config) *Manager {
	if config.IdleTimeout <= 0 {
		config.IdleTimeout = 30 * time.Minute
	}
	if config.AbsoluteTimeout <= 0 {
		config.AbsoluteTimeout = 24 * time.Hour
	}
	if config.Store == nil {
		config.Store = NewMemorySessionStore()
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Manager{users: make(map[string]User), usersByWebID: make(map[string]User), config: config}
}

func HashPassword(password string) (string, error) {
	if password == "" {
		return "", fmt.Errorf("password cannot be empty")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	sum := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return "argon2id$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(sum), nil
}
func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 3 || parts[0] != "argon2id" {
		return false
	}
	salt, e1 := base64.RawStdEncoding.DecodeString(parts[1])
	want, e2 := base64.RawStdEncoding.DecodeString(parts[2])
	if e1 != nil || e2 != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}
func (m *Manager) AddUser(user User) error {
	if strings.TrimSpace(user.ID) == "" || strings.TrimSpace(user.Username) == "" || user.PasswordDigest == "" {
		return fmt.Errorf("user identity and password digest are required")
	}
	if strings.TrimSpace(user.WebUserID) == "" {
		user.WebUserID = user.ID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[user.Username]; ok {
		return fmt.Errorf("user already exists")
	}
	if _, ok := m.usersByWebID[user.WebUserID]; ok {
		return fmt.Errorf("web user identity already exists")
	}
	m.users[user.Username] = user
	m.usersByWebID[user.WebUserID] = user
	return nil
}
func (m *Manager) Login(username, password string) (*Session, error) {
	m.mu.RLock()
	user, ok := m.users[username]
	m.mu.RUnlock()
	if !ok || !VerifyPassword(user.PasswordDigest, password) {
		return nil, fmt.Errorf("invalid credentials")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	csrf := make([]byte, 32)
	if _, err := rand.Read(csrf); err != nil {
		return nil, err
	}
	now := m.config.Now().UTC()
	id := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(id))
	csrfToken := base64.RawURLEncoding.EncodeToString(csrf)
	csrfDigest := sha256.Sum256([]byte(csrfToken))
	s := &Session{IDDigest: base64.RawURLEncoding.EncodeToString(digest[:]), token: id, User: user,
		CSRFToken: csrfToken, CSRFDigest: base64.RawURLEncoding.EncodeToString(csrfDigest[:]),
		CreatedAt: now, LastSeen: now, IdleExpiresAt: now.Add(m.config.IdleTimeout), AbsoluteExpiresAt: now.Add(m.config.AbsoluteTimeout)}
	record := &domain.WebSessionRecord{ID: s.IDDigest, WebUserID: user.WebUserID, SessionDigest: s.IDDigest,
		CSRFDigest: s.CSRFDigest, CreatedAt: s.CreatedAt, LastActivityAt: s.LastSeen,
		IdleExpiresAt: s.IdleExpiresAt, AbsoluteExpiresAt: s.AbsoluteExpiresAt}
	if err := m.config.Store.CreateWebSession(context.Background(), record); err != nil {
		return nil, err
	}
	return s, nil
}
func (m *Manager) Authenticate(r *http.Request) (*Session, error) {
	cookie, err := r.Cookie("openagentx_session")
	if err != nil {
		return nil, fmt.Errorf("unauthenticated")
	}
	sum := sha256.Sum256([]byte(cookie.Value))
	key := base64.RawURLEncoding.EncodeToString(sum[:])
	record, err := m.config.Store.GetWebSession(r.Context(), key)
	if err != nil {
		return nil, fmt.Errorf("session expired")
	}
	now := m.config.Now().UTC()
	if record.RevokedAt != nil || !now.Before(record.IdleExpiresAt) || !now.Before(record.AbsoluteExpiresAt) {
		return nil, fmt.Errorf("session expired")
	}
	m.mu.RLock()
	user, ok := m.usersByWebID[record.WebUserID]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("session user unavailable")
	}
	idleExpires := now.Add(m.config.IdleTimeout)
	if idleExpires.After(record.AbsoluteExpiresAt) {
		idleExpires = record.AbsoluteExpiresAt
	}
	if err := m.config.Store.TouchWebSession(r.Context(), key, now, idleExpires); err != nil {
		return nil, fmt.Errorf("session expired")
	}
	s := &Session{IDDigest: key, User: user, CSRFDigest: record.CSRFDigest, CreatedAt: record.CreatedAt,
		LastSeen: now, IdleExpiresAt: idleExpires, AbsoluteExpiresAt: record.AbsoluteExpiresAt}
	return s, nil
}
func (m *Manager) RefreshCSRF(ctx context.Context, session *Session) error {
	if session == nil || session.IDDigest == "" {
		return fmt.Errorf("session is required")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	digestString := base64.RawURLEncoding.EncodeToString(digest[:])
	if err := m.config.Store.RotateWebSessionCSRF(ctx, session.IDDigest, digestString, m.config.Now().UTC()); err != nil {
		return err
	}
	session.CSRFToken = token
	session.CSRFDigest = digestString
	return nil
}
func (m *Manager) Revoke(ctx context.Context, session *Session) error {
	if session == nil {
		return nil
	}
	if err := m.config.Store.RevokeWebSession(ctx, session.IDDigest, m.config.Now().UTC()); err != nil {
		return err
	}
	session.Revoked = true
	return nil
}
func SetSessionCookie(response http.ResponseWriter, session *Session) {
	if session == nil || session.token == "" {
		return
	}
	http.SetCookie(response, &http.Cookie{Name: "openagentx_session", Value: session.token, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, Expires: session.AbsoluteExpiresAt})
}
func RequireRole(session *Session, role Role) error {
	if session == nil {
		return fmt.Errorf("unauthenticated")
	}
	for _, candidate := range session.User.Roles {
		if candidate == role || candidate == RoleOwner {
			return nil
		}
	}
	return fmt.Errorf("forbidden")
}
func ValidateCSRF(session *Session, token string) error {
	if session == nil || token == "" || session.CSRFDigest == "" {
		return fmt.Errorf("invalid csrf token")
	}
	digest := sha256.Sum256([]byte(token))
	encoded := base64.RawURLEncoding.EncodeToString(digest[:])
	if subtle.ConstantTimeCompare([]byte(session.CSRFDigest), []byte(encoded)) != 1 {
		return fmt.Errorf("invalid csrf token")
	}
	return nil
}
