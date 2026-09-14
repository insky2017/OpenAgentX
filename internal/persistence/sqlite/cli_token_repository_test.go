package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	cliauth "openagentx/internal/auth/cli"
	webauth "openagentx/internal/auth/web"
	"openagentx/internal/domain"
)

func initializeCLITokenUser(t *testing.T, repository *Repository) domain.WebUserRecord {
	t.Helper()
	digest, err := webauth.HashPassword("correct password")
	if err != nil {
		t.Fatal(err)
	}
	owner := &domain.Principal{ID: "human-cli-owner", Kind: domain.PrincipalHuman, DisplayName: "Owner", Status: domain.IdentityActive}
	daemon := &domain.Principal{ID: "daemon", Kind: domain.PrincipalSystem, DisplayName: "Daemon", Status: domain.IdentityActive}
	organization := &domain.Organization{ID: "default", Name: "Default", Status: domain.IdentityActive}
	user := domain.WebUserRecord{ID: "web-cli-owner", PrincipalID: owner.ID, Username: "owner", PasswordDigest: digest,
		Roles: []domain.WebRole{domain.WebRoleOwner}, Status: domain.IdentityActive}
	if created, err := repository.Initialize(context.Background(), owner, daemon, organization, &user,
		bootstrapEvents("cli-token", owner.ID, organization.ID, user.ID)); err != nil || !created {
		t.Fatalf("initialize CLI user created=%v err=%v", created, err)
	}
	return user
}

func TestCLITokenRepositoryStoresDigestAndReplaceIsAtomic(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	user := initializeCLITokenUser(t, repository)
	ctx := context.Background()
	installationID, err := repository.InstallationID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	first := &domain.CLITokenRecord{ID: "public-token-1", TokenDigest: "sha256-digest-1", WebUserID: user.ID,
		PrincipalID: user.PrincipalID, Scopes: []domain.CLIScope{domain.CLIScopeConsoleRead}, InstallationID: installationID,
		CreatedAt: now, LastUsedAt: now, AbsoluteExpiresAt: now.Add(time.Hour)}
	if err := repository.ReplaceCLIToken(ctx, first); err != nil {
		t.Fatal(err)
	}
	var rawMatches int
	if err := repository.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cli_tokens WHERE token_digest=?`, "raw-secret-token").Scan(&rawMatches); err != nil || rawMatches != 0 {
		t.Fatalf("raw token found in database count=%d err=%v", rawMatches, err)
	}
	injected := errors.New("replace rollback")
	failing := newRepository(repository.db, Options{Now: repository.now, FaultInjector: func(point FaultPoint) error {
		if point == FaultBeforeCommit {
			return injected
		}
		return nil
	}})
	second := *first
	second.ID = "public-token-2"
	second.TokenDigest = "sha256-digest-2"
	second.CreatedAt = now.Add(time.Minute)
	second.LastUsedAt = second.CreatedAt
	second.AbsoluteExpiresAt = second.CreatedAt.Add(time.Hour)
	if err := failing.ReplaceCLIToken(ctx, &second); !errors.Is(err, injected) {
		t.Fatalf("replace error=%v", err)
	}
	loaded, err := repository.GetCLITokenByDigest(ctx, first.TokenDigest)
	if err != nil || loaded.RevokedAt != nil {
		t.Fatalf("old token changed after rollback record=%+v err=%v", loaded, err)
	}
	if _, err := repository.GetCLITokenByDigest(ctx, second.TokenDigest); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("new token survived rollback: %v", err)
	}
}

func TestCLIServiceNeverPersistsRawToken(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	initializeCLITokenUser(t, repository)
	service, err := cliauth.NewService(repository, cliauth.Config{})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := service.Login(context.Background(), "owner", "correct password")
	if err != nil {
		t.Fatal(err)
	}
	var matches int
	if err := repository.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM cli_tokens WHERE
		token_id=? OR token_digest=? OR web_user_id=? OR principal_id=? OR scopes_json=? OR installation_id=?`,
		issued.Token, issued.Token, issued.Token, issued.Token, issued.Token, issued.Token).Scan(&matches); err != nil {
		t.Fatal(err)
	}
	if matches != 0 {
		t.Fatal("raw CLI Token was persisted in a database field")
	}
}
