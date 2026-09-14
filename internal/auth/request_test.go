package auth

import (
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

func TestRoleHierarchy(t *testing.T) {
	operator := Principal{Roles: []domain.WebRole{domain.WebRoleOperator}}
	if !operator.HasRole(domain.WebRoleViewer) || !operator.HasRole(domain.WebRoleOperator) || operator.HasRole(domain.WebRoleOwner) {
		t.Fatalf("operator hierarchy mismatch")
	}
	owner := Principal{Roles: []domain.WebRole{domain.WebRoleOwner}}
	if !owner.HasRole(domain.WebRoleViewer) || !owner.HasRole(domain.WebRoleOperator) || !owner.HasRole(domain.WebRoleOwner) {
		t.Fatalf("owner hierarchy mismatch")
	}
}

func TestWebAuthorizerCarriesEffectiveIdleExpiry(t *testing.T) {
	now := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)
	digest, err := webauth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	manager := webauth.NewManager(webauth.Config{Now: func() time.Time { return now },
		IdleTimeout: 5 * time.Minute, AbsoluteTimeout: time.Hour})
	if err := manager.AddUser(webauth.User{ID: "human-owner", WebUserID: "web-owner", Username: "owner",
		Roles: []webauth.Role{webauth.RoleOwner}, PasswordDigest: digest}); err != nil {
		t.Fatal(err)
	}
	session, err := manager.Login("owner", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	cookies := httptest.NewRecorder()
	webauth.SetSessionCookie(cookies, session)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(cookies.Result().Cookies()[0])
	authorizer, err := NewWebAuthorizer(manager)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := authorizer.Authorize(request, Requirement{Role: domain.WebRoleViewer})
	if err != nil || !principal.ExpiresAt.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("Web principal expiry=%s err=%v", principal.ExpiresAt, err)
	}
}

func TestCLIAuthorizerWritesStableAuthenticationErrors(t *testing.T) {
	authorizer, err := NewCLIAuthorizer(new(cliauth.Service))
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		err    error
		status int
		code   string
	}{
		{err: cliauth.ErrUnauthenticated, status: http.StatusUnauthorized, code: openapi.ErrorCLIUnauthenticated},
		{err: cliauth.ErrForbidden, status: http.StatusForbidden, code: openapi.ErrorCLIForbidden},
	} {
		response := httptest.NewRecorder()
		authorizer.WriteFailure(response, testCase.err)
		var body openapi.ErrorResponse
		if response.Code != testCase.status || json.Unmarshal(response.Body.Bytes(), &body) != nil || body.Code != testCase.code {
			t.Fatalf("auth failure status=%d body=%s", response.Code, response.Body.String())
		}
	}
}
