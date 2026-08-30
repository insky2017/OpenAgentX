package web

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestPasswordHashAndSessionSecurity(t *testing.T) {
	digest, err := HashPassword("correct horse")
	if err != nil || !VerifyPassword(digest, "correct horse") || VerifyPassword(digest, "wrong") {
		t.Fatal("Argon2id password verification failed")
	}
	m := NewManager(Config{})
	if err := m.AddUser(User{ID: "owner-1", Username: "owner", Roles: []Role{RoleOwner}, PasswordDigest: digest}); err != nil {
		t.Fatal(err)
	}
	session, err := m.Login("owner", "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	response := httptest.NewRecorder()
	SetSessionCookie(response, session)
	for _, cookie := range response.Result().Cookies() {
		r.AddCookie(cookie)
	}
	got, err := m.Authenticate(r)
	if err != nil || got.IDDigest == "" {
		t.Fatalf("authenticate session=%+v err=%v", got, err)
	}
	if err := ValidateCSRF(got, session.CSRFToken); err != nil {
		t.Fatal(err)
	}
	if err := m.Revoke(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Authenticate(r); err == nil {
		t.Fatal("revoked session accepted")
	}
}

func TestPersistentSessionSurvivesManagerReplacementAndRotatesCSRF(t *testing.T) {
	digest, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemorySessionStore()
	user := User{ID: "owner-1", WebUserID: "web-owner-1", Username: "owner", Roles: []Role{RoleOwner}, PasswordDigest: digest}
	first := NewManager(Config{Store: store})
	if err := first.AddUser(user); err != nil {
		t.Fatal(err)
	}
	created, err := first.Login("owner", "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	SetSessionCookie(response, created)
	request := httptest.NewRequest("GET", "/", nil)
	request.AddCookie(response.Result().Cookies()[0])

	replacement := NewManager(Config{Store: store})
	if err := replacement.AddUser(user); err != nil {
		t.Fatal(err)
	}
	restored, err := replacement.Authenticate(request)
	if err != nil {
		t.Fatalf("restore persistent session: %v", err)
	}
	if err := ValidateCSRF(restored, created.CSRFToken); err != nil {
		t.Fatalf("persisted CSRF digest rejected original token: %v", err)
	}
	if err := replacement.RefreshCSRF(context.Background(), restored); err != nil {
		t.Fatal(err)
	}
	if restored.CSRFToken == "" || restored.CSRFToken == created.CSRFToken {
		t.Fatal("CSRF token was not rotated after session recovery")
	}
	latest, err := replacement.Authenticate(request)
	if err != nil || ValidateCSRF(latest, restored.CSRFToken) != nil || ValidateCSRF(latest, created.CSRFToken) == nil {
		t.Fatalf("rotated CSRF state invalid err=%v", err)
	}
}

func TestRoleHierarchy(t *testing.T) {
	for name, testCase := range map[string]struct {
		roles   []Role
		require Role
		allowed bool
	}{
		"owner can operate":     {roles: []Role{RoleOwner}, require: RoleOperator, allowed: true},
		"owner can administer":  {roles: []Role{RoleOwner}, require: RoleOwner, allowed: true},
		"operator can operate":  {roles: []Role{RoleOperator}, require: RoleOperator, allowed: true},
		"operator is not owner": {roles: []Role{RoleOperator}, require: RoleOwner, allowed: false},
		"viewer cannot write":   {roles: []Role{RoleViewer}, require: RoleOperator, allowed: false},
	} {
		t.Run(name, func(t *testing.T) {
			err := RequireRole(&Session{User: User{Roles: testCase.roles}}, testCase.require)
			if (err == nil) != testCase.allowed {
				t.Fatalf("RequireRole() error=%v allowed=%v", err, testCase.allowed)
			}
		})
	}
}
