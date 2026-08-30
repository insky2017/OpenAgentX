package web

import (
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
	if err := ValidateCSRF(got, got.CSRFToken); err != nil {
		t.Fatal(err)
	}
	m.Revoke(got)
	if _, err := m.Authenticate(r); err == nil {
		t.Fatal("revoked session accepted")
	}
}
