package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"path/filepath"
	"strings"
	"testing"

	webAuth "openagentx/internal/auth/web"
	"openagentx/internal/domain"
	"openagentx/internal/localprofile"
)

type staticWebUsers struct {
	webAuth.SessionStore
	users []domain.WebUserRecord
	err   error
}

func TestServeAndSchemaUseProfilePathsWithExplicitPrecedence(t *testing.T) {
	root := t.TempDir()
	environmentDB := filepath.Join(root, "environment.db")
	environmentSocket := filepath.Join(root, "environment.sock")
	t.Setenv(localprofile.EnvDatabasePath, environmentDB)
	t.Setenv(localprofile.EnvSocketPath, environmentSocket)

	options, err := parseServeOptions(nil, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if options.dbPath != environmentDB || options.socketPath != environmentSocket {
		t.Fatalf("serve defaults=%+v", options)
	}
	explicitDB := filepath.Join(root, "explicit.db")
	explicitSocket := filepath.Join(root, "explicit.sock")
	options, err = parseServeOptions([]string{"--db", explicitDB, "--socket", explicitSocket}, &bytes.Buffer{})
	if err != nil || options.dbPath != explicitDB || options.socketPath != explicitSocket {
		t.Fatalf("serve explicit=%+v err=%v", options, err)
	}
	database, err := parseSchemaDatabase([]string{"verify"}, &bytes.Buffer{})
	if err != nil || database != environmentDB {
		t.Fatalf("schema default=%q err=%v", database, err)
	}
	database, err = parseSchemaDatabase([]string{"verify", "--db", explicitDB}, &bytes.Buffer{})
	if err != nil || database != explicitDB {
		t.Fatalf("schema explicit=%q err=%v", database, err)
	}
}

func TestServeSchemaInvalidPathsAndHelpFailClosedWithoutLeaks(t *testing.T) {
	root := t.TempDir()
	secretPath := filepath.Join(root, "private", "credentials-like-value")
	t.Setenv(localprofile.EnvDatabasePath, secretPath)
	t.Setenv(localprofile.EnvSocketPath, filepath.Join(root, "openagentx.sock"))

	for _, args := range [][]string{{"--db="}, {"--socket", "relative/openagentx.sock"}} {
		if _, err := parseServeOptions(args, &bytes.Buffer{}); err == nil {
			t.Fatalf("serve accepted invalid paths %v", args)
		}
	}
	if _, err := parseSchemaDatabase([]string{"verify", "--db="}, &bytes.Buffer{}); err == nil {
		t.Fatal("schema accepted explicit empty database path")
	}
	var help bytes.Buffer
	if _, err := parseServeOptions([]string{"--help"}, &help); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("serve help error=%v", err)
	}
	if strings.Contains(help.String(), secretPath) {
		t.Fatalf("serve help leaked environment value: %s", help.String())
	}
}

func (s staticWebUsers) ListWebUsers(context.Context) ([]domain.WebUserRecord, error) {
	return s.users, s.err
}

func TestLoadAuthManagerUsesPersistentWebUsersAndFailsClosedWhenEmpty(t *testing.T) {
	store := webAuth.NewMemorySessionStore()
	if _, err := loadAuthManager(context.Background(), staticWebUsers{SessionStore: store}); err == nil {
		t.Fatal("daemon auth accepted an uninitialized user store")
	}
	digest, err := webAuth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	manager, err := loadAuthManager(context.Background(), staticWebUsers{SessionStore: store, users: []domain.WebUserRecord{{
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
