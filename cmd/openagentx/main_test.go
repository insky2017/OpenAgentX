package main

import (
	"context"
	"testing"

	webAuth "openagentx/internal/auth/web"
	"openagentx/internal/domain"
)

type staticWebUsers struct {
	users []domain.WebUserRecord
	err   error
}

func (s staticWebUsers) ListWebUsers(context.Context) ([]domain.WebUserRecord, error) {
	return s.users, s.err
}

func TestLoadAuthManagerUsesPersistentWebUsersAndFailsClosedWhenEmpty(t *testing.T) {
	if _, err := loadAuthManager(context.Background(), staticWebUsers{}); err == nil {
		t.Fatal("daemon auth accepted an uninitialized user store")
	}
	digest, err := webAuth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	manager, err := loadAuthManager(context.Background(), staticWebUsers{users: []domain.WebUserRecord{{
		ID: "web-owner", PrincipalID: "human-owner", Username: "owner", PasswordDigest: digest,
		Roles: []domain.WebRole{domain.WebRoleOwner}, Status: domain.IdentityActive,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	session, err := manager.Login("owner", "correct horse battery staple")
	if err != nil || session.User.ID != "human-owner" {
		t.Fatalf("persistent owner login session=%+v err=%v", session, err)
	}
}
