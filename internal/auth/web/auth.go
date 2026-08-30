package web

import (
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
)

type Role string

const (
	RoleOwner    Role = "owner"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

type User struct {
	ID, Username   string
	Roles          []Role
	PasswordDigest string
}
type Session struct {
	IDDigest                                              string
	token                                                 string
	User                                                  User
	CSRFToken                                             string
	CreatedAt, LastSeen, IdleExpiresAt, AbsoluteExpiresAt time.Time
	Revoked                                               bool
}
type Config struct{ IdleTimeout, AbsoluteTimeout time.Duration }
type Manager struct {
	mu       sync.Mutex
	users    map[string]User
	sessions map[string]*Session
	config   Config
}

func NewManager(config Config) *Manager {
	if config.IdleTimeout <= 0 {
		config.IdleTimeout = 30 * time.Minute
	}
	if config.AbsoluteTimeout <= 0 {
		config.AbsoluteTimeout = 24 * time.Hour
	}
	return &Manager{users: make(map[string]User), sessions: make(map[string]*Session), config: config}
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[user.Username]; ok {
		return fmt.Errorf("user already exists")
	}
	m.users[user.Username] = user
	return nil
}
func (m *Manager) Login(username, password string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	user, ok := m.users[username]
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
	now := time.Now().UTC()
	id := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(id))
	s := &Session{IDDigest: base64.RawURLEncoding.EncodeToString(digest[:]), token: id, User: user, CSRFToken: base64.RawURLEncoding.EncodeToString(csrf), CreatedAt: now, LastSeen: now, IdleExpiresAt: now.Add(m.config.IdleTimeout), AbsoluteExpiresAt: now.Add(m.config.AbsoluteTimeout)}
	m.sessions[s.IDDigest] = s
	return s, nil
}
func (m *Manager) Authenticate(r *http.Request) (*Session, error) {
	cookie, err := r.Cookie("openagentx_session")
	if err != nil {
		return nil, fmt.Errorf("unauthenticated")
	}
	sum := sha256.Sum256([]byte(cookie.Value))
	key := base64.RawURLEncoding.EncodeToString(sum[:])
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[key]
	now := time.Now().UTC()
	if !ok || s.Revoked || !now.Before(s.IdleExpiresAt) || !now.Before(s.AbsoluteExpiresAt) {
		return nil, fmt.Errorf("session expired")
	}
	s.LastSeen = now
	s.IdleExpiresAt = now.Add(m.config.IdleTimeout)
	return s, nil
}
func (m *Manager) Revoke(session *Session) {
	if session == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if current, ok := m.sessions[session.IDDigest]; ok {
		current.Revoked = true
	}
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
	if session == nil || token == "" || subtle.ConstantTimeCompare([]byte(session.CSRFToken), []byte(token)) != 1 {
		return fmt.Errorf("invalid csrf token")
	}
	return nil
}
