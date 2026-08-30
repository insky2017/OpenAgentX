package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	openapi "openagentx/internal/api"
	"openagentx/internal/auth/web"
)

func TestSecurityHeaders(t *testing.T) {
	h := NewHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)

	expected := map[string]string{
		"Cache-Control":           "no-store",
		"Pragma":                  "no-cache",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
		"Permissions-Policy":      "camera=(), geolocation=(), microphone=()",
		"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
	}
	for header, value := range expected {
		if got := resp.Header().Get(header); got != value {
			t.Errorf("%s = %q, want %q", header, got, value)
		}
	}
}

func TestLoginSessionRotationAndLogoutLifecycle(t *testing.T) {
	digest, err := web.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	manager := web.NewManager(web.Config{})
	if err := manager.AddUser(web.User{
		ID: "human-owner", WebUserID: "web-owner", Username: "owner", Roles: []web.Role{web.RoleOwner}, PasswordDigest: digest,
	}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(manager)

	badLogin := httptest.NewRecorder()
	handler.ServeHTTP(badLogin, httptest.NewRequest(http.MethodPost, openapi.AuthLoginPath, bytes.NewBufferString(`{"username":"owner","password":"wrong"}`)))
	if badLogin.Code != http.StatusUnauthorized || len(badLogin.Result().Cookies()) != 0 {
		t.Fatalf("bad login status=%d cookies=%d", badLogin.Code, len(badLogin.Result().Cookies()))
	}

	login := httptest.NewRecorder()
	handler.ServeHTTP(login, httptest.NewRequest(http.MethodPost, openapi.AuthLoginPath, bytes.NewBufferString(`{"username":"owner","password":"correct horse battery staple"}`)))
	if login.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("insecure login cookie: %+v", cookies)
	}
	var initial openapi.WebSessionResponse
	if err := json.Unmarshal(login.Body.Bytes(), &initial); err != nil || initial.CSRFToken == "" {
		t.Fatalf("initial Session response=%s err=%v", login.Body.String(), err)
	}

	sessionRequest := httptest.NewRequest(http.MethodGet, openapi.AuthSessionPath, nil)
	sessionRequest.AddCookie(cookies[0])
	sessionResponse := httptest.NewRecorder()
	handler.ServeHTTP(sessionResponse, sessionRequest)
	var refreshed openapi.WebSessionResponse
	if err := json.Unmarshal(sessionResponse.Body.Bytes(), &refreshed); err != nil || refreshed.CSRFToken == "" || refreshed.CSRFToken == initial.CSRFToken {
		t.Fatalf("refreshed Session response=%s err=%v", sessionResponse.Body.String(), err)
	}

	staleLogoutRequest := httptest.NewRequest(http.MethodPost, openapi.AuthLogoutPath, nil)
	staleLogoutRequest.AddCookie(cookies[0])
	staleLogoutRequest.Header.Set("X-CSRF-Token", initial.CSRFToken)
	staleLogout := httptest.NewRecorder()
	handler.ServeHTTP(staleLogout, staleLogoutRequest)
	if staleLogout.Code != http.StatusForbidden {
		t.Fatalf("stale CSRF logout status=%d", staleLogout.Code)
	}

	logoutRequest := httptest.NewRequest(http.MethodPost, openapi.AuthLogoutPath, nil)
	logoutRequest.AddCookie(cookies[0])
	logoutRequest.Header.Set("X-CSRF-Token", refreshed.CSRFToken)
	logout := httptest.NewRecorder()
	handler.ServeHTTP(logout, logoutRequest)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout status=%d body=%s", logout.Code, logout.Body.String())
	}

	revokedRequest := httptest.NewRequest(http.MethodGet, openapi.AuthSessionPath, nil)
	revokedRequest.AddCookie(cookies[0])
	revoked := httptest.NewRecorder()
	handler.ServeHTTP(revoked, revokedRequest)
	if revoked.Code != http.StatusUnauthorized {
		t.Fatalf("revoked Session status=%d", revoked.Code)
	}
}
