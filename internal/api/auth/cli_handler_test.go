package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	openapi "openagentx/internal/api"
	cliauth "openagentx/internal/auth/cli"
	webauth "openagentx/internal/auth/web"
	"openagentx/internal/domain"
)

type cliAuthRepository struct {
	user   domain.WebUserRecord
	tokens map[string]domain.CLITokenRecord
}

func (r *cliAuthRepository) InstallationID(context.Context) (string, error) {
	return "installation-api", nil
}
func (r *cliAuthRepository) GetWebUserByUsername(_ context.Context, username string) (*domain.WebUserRecord, error) {
	if username != r.user.Username {
		return nil, domain.ErrNotFound
	}
	value := r.user
	return &value, nil
}
func (r *cliAuthRepository) GetWebUserByID(_ context.Context, id string) (*domain.WebUserRecord, error) {
	if id != r.user.ID {
		return nil, domain.ErrNotFound
	}
	value := r.user
	return &value, nil
}
func (r *cliAuthRepository) ReplaceCLIToken(_ context.Context, record *domain.CLITokenRecord) error {
	for digest, token := range r.tokens {
		if token.WebUserID == record.WebUserID && token.RevokedAt == nil {
			now := record.CreatedAt
			token.RevokedAt = &now
			r.tokens[digest] = token
		}
	}
	r.tokens[record.TokenDigest] = *record
	return nil
}
func (r *cliAuthRepository) GetCLITokenByDigest(_ context.Context, digest string) (*domain.CLITokenRecord, error) {
	record, ok := r.tokens[digest]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &record, nil
}
func (r *cliAuthRepository) TouchCLIToken(_ context.Context, digest string, usedAt time.Time) error {
	record, ok := r.tokens[digest]
	if !ok || record.RevokedAt != nil {
		return domain.ErrNotFound
	}
	record.LastUsedAt = usedAt
	r.tokens[digest] = record
	return nil
}
func (r *cliAuthRepository) RevokeCLIToken(_ context.Context, tokenID, _ string, revokedAt time.Time) error {
	for digest, record := range r.tokens {
		if record.ID == tokenID {
			record.RevokedAt = &revokedAt
			r.tokens[digest] = record
		}
	}
	return nil
}
func (r *cliAuthRepository) RevokeCLITokensByWebUser(context.Context, string, time.Time) error {
	return nil
}

func TestCLIEndpointsIssueOnceAuthenticateAndRevokeWithStableErrors(t *testing.T) {
	digest, err := webauth.HashPassword("correct password")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	repository := &cliAuthRepository{user: domain.WebUserRecord{ID: "web-owner", PrincipalID: "human-owner", Username: "owner",
		PasswordDigest: digest, Roles: []domain.WebRole{domain.WebRoleOwner}, Status: domain.IdentityActive,
		PasswordSetAt: now.Add(-time.Hour), CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)}, tokens: make(map[string]domain.CLITokenRecord)}
	service, err := cliauth.NewService(repository, cliauth.Config{Now: func() time.Time { return now }, Rand: bytes.NewReader(bytes.Repeat([]byte{7}, 256))})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewCLIHandler(service)
	if err != nil {
		t.Fatal(err)
	}

	probe := httptest.NewRecorder()
	handler.ServeHTTP(probe, httptest.NewRequest(http.MethodGet, openapi.CLIInstallationProbePath, nil))
	if probe.Code != http.StatusOK || !bytes.Contains(probe.Body.Bytes(), []byte("installation-api")) {
		t.Fatalf("probe status=%d body=%s", probe.Code, probe.Body.String())
	}
	bad := httptest.NewRecorder()
	handler.ServeHTTP(bad, httptest.NewRequest(http.MethodPost, openapi.CLIAuthLoginPath, bytes.NewBufferString(`{"username":"owner","password":"wrong"}`)))
	assertCLIError(t, bad, http.StatusUnauthorized, openapi.ErrorCLIUnauthenticated)

	login := httptest.NewRecorder()
	handler.ServeHTTP(login, httptest.NewRequest(http.MethodPost, openapi.CLIAuthLoginPath, bytes.NewBufferString(`{"username":"owner","password":"correct password"}`)))
	var issued openapi.CLILoginResponse
	if login.Code != http.StatusOK || json.Unmarshal(login.Body.Bytes(), &issued) != nil || issued.Token == "" {
		t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
	}
	sessionRequest := httptest.NewRequest(http.MethodGet, openapi.CLIAuthSessionPath, nil)
	sessionRequest.Header.Set("Authorization", "Bearer "+issued.Token)
	session := httptest.NewRecorder()
	handler.ServeHTTP(session, sessionRequest)
	if session.Code != http.StatusOK || bytes.Contains(session.Body.Bytes(), []byte(issued.Token)) {
		t.Fatalf("session status=%d leaked token=%v body=%s", session.Code, bytes.Contains(session.Body.Bytes(), []byte(issued.Token)), session.Body.String())
	}
	logoutRequest := httptest.NewRequest(http.MethodPost, openapi.CLIAuthLogoutPath, nil)
	logoutRequest.Header.Set("Authorization", "Bearer "+issued.Token)
	logout := httptest.NewRecorder()
	handler.ServeHTTP(logout, logoutRequest)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout status=%d body=%s", logout.Code, logout.Body.String())
	}
	repeated := httptest.NewRecorder()
	handler.ServeHTTP(repeated, logoutRequest.Clone(context.Background()))
	if repeated.Code != http.StatusNoContent {
		t.Fatalf("repeated logout status=%d body=%s", repeated.Code, repeated.Body.String())
	}
}

func assertCLIError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	var body openapi.ErrorResponse
	if response.Code != status || json.Unmarshal(response.Body.Bytes(), &body) != nil || body.Code != code {
		t.Fatalf("CLI error status=%d body=%s", response.Code, response.Body.String())
	}
}
