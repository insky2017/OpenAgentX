package secretstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"openagentx/internal/domain"
)

func TestFileStoreImmutablePermissionsAndKeyedFingerprint(t *testing.T) {
	root := filepath.Join(t.TempDir(), "secrets")
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	version := store.NewVersion()
	secret := []byte(`{"username":"u","password":"short"}`)
	if err := store.PutImmutable(context.Background(), version, secret); err != nil {
		t.Fatal(err)
	}
	if err := store.PutImmutable(context.Background(), version, secret); err != nil {
		t.Fatal(err)
	}
	if err := store.PutImmutable(context.Background(), version, []byte("different")); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("different overwrite error=%v", err)
	}
	info, err := os.Stat(filepath.Join(root, version+".secret"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("secret mode=%v err=%v", info.Mode().Perm(), err)
	}
	if fingerprint := store.Fingerprint(secret); len(fingerprint) != 64 || fingerprint == strings.Repeat("0", 64) {
		t.Fatalf("fingerprint=%q", fingerprint)
	}
	got, err := store.Get(context.Background(), version)
	if err != nil || string(got) != string(secret) {
		t.Fatalf("get=%q err=%v", got, err)
	}
}

func TestFileStoreRejectsLinksAndBadPermissions(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(link); err == nil {
		t.Fatal("symlink root accepted")
	}
	bad := filepath.Join(base, "bad")
	if err := os.Mkdir(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(bad); err == nil {
		t.Fatal("0755 root accepted")
	}
}

func TestFileStoreInternalFilesRemainAnchoredAfterRootReplacement(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "secrets")
	anchored := filepath.Join(base, "secrets-original")
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	firstVersion := store.NewVersion()
	if err := store.PutImmutable(context.Background(), firstVersion, []byte("original")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root, anchored); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, firstVersion+".secret"), []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(context.Background(), firstVersion)
	if err != nil || string(got) != "original" {
		t.Fatalf("read followed replaced root: payload=%q err=%v", got, err)
	}
	secondVersion := store.NewVersion()
	if err := store.PutImmutable(context.Background(), secondVersion, []byte("second")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(anchored, secondVersion+".secret")); err != nil {
		t.Fatalf("secret was not written through anchored directory fd: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, secondVersion+".secret")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("secret escaped into replacement directory: %v", err)
	}
}

func TestFileStoreConcurrentOpenPublishesOneCompleteKey(t *testing.T) {
	root := filepath.Join(t.TempDir(), "secrets")
	const count = 16
	stores := make([]*FileStore, count)
	errorsSeen := make([]error, count)
	var wg sync.WaitGroup
	for i := range stores {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			stores[index], errorsSeen[index] = Open(root)
		}(i)
	}
	wg.Wait()
	for _, err := range errorsSeen {
		if err != nil {
			t.Fatal(err)
		}
	}
	payload := []byte("same-payload")
	want := stores[0].Fingerprint(payload)
	for _, store := range stores[1:] {
		if got := store.Fingerprint(payload); got != want {
			t.Fatalf("concurrent Open loaded different keys: got=%q want=%q", got, want)
		}
	}
	key, err := readSecureFileBounded(filepath.Join(root, hmacKeyName), 32)
	if err != nil || len(key) != 32 {
		t.Fatalf("published key length=%d err=%v", len(key), err)
	}
}

func TestCommitImmutableCleansOnlyUnreferencedFailedCommit(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "secrets"))
	if err != nil {
		t.Fatal(err)
	}
	version := store.NewVersion()
	commitErr := errors.New("metadata commit failed")
	err = store.CommitImmutable(context.Background(), version, []byte("secret"), func() error {
		return commitErr
	}, func(context.Context, string) (bool, error) {
		return false, nil
	})
	if !errors.Is(err, commitErr) {
		t.Fatalf("commit error=%v", err)
	}
	if _, err := store.Get(context.Background(), version); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unreferenced failed secret remained: %v", err)
	}
}

func TestCommitImmutableSerializesFailedCleanupBeforeConcurrentSuccess(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "secrets"))
	if err != nil {
		t.Fatal(err)
	}
	version := store.NewVersion()
	payload := []byte("shared-secret")
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- store.CommitImmutable(context.Background(), version, payload, func() error {
			close(firstStarted)
			<-releaseFirst
			return errors.New("first metadata commit failed")
		}, func(context.Context, string) (bool, error) {
			return false, nil
		})
	}()
	<-firstStarted
	secondDone := make(chan error, 1)
	secondCommitted := make(chan struct{})
	go func() {
		secondDone <- store.CommitImmutable(context.Background(), version, payload, func() error {
			close(secondCommitted)
			return nil
		}, func(context.Context, string) (bool, error) {
			return false, nil
		})
	}()
	select {
	case <-secondCommitted:
		t.Fatal("concurrent commit bypassed version lock")
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseFirst)
	if err := <-firstDone; err == nil {
		t.Fatal("first metadata failure was lost")
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(context.Background(), version)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("concurrent successful secret=%q err=%v", got, err)
	}
}

func TestReconcileOrphansPreservesUncertainAndReferencedSecrets(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "secrets"))
	if err != nil {
		t.Fatal(err)
	}
	orphan := store.NewVersion()
	referenced := store.NewVersion()
	for _, version := range []string{orphan, referenced} {
		if err := store.PutImmutable(context.Background(), version, []byte(version)); err != nil {
			t.Fatal(err)
		}
	}
	queryErr := errors.New("reference query unavailable")
	if err := store.ReconcileOrphans(context.Background(), func(context.Context, string) (bool, error) {
		return false, queryErr
	}); !errors.Is(err, queryErr) {
		t.Fatalf("reconcile query error=%v", err)
	}
	if _, err := store.Get(context.Background(), orphan); err != nil {
		t.Fatalf("uncertain secret was removed: %v", err)
	}
	if err := store.ReconcileOrphans(context.Background(), func(_ context.Context, version string) (bool, error) {
		return version == referenced, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(context.Background(), orphan); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unreferenced secret survived retry: %v", err)
	}
	if _, err := store.Get(context.Background(), referenced); err != nil {
		t.Fatalf("referenced secret was removed: %v", err)
	}
}

func TestGetReferencedRejectsUncommittedSecretVersion(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "secrets"))
	if err != nil {
		t.Fatal(err)
	}
	version := store.NewVersion()
	if err := store.PutImmutable(context.Background(), version, []byte("uncommitted")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetReferenced(context.Background(), version, func(context.Context, string) (bool, error) {
		return false, nil
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("uncommitted secret read error=%v", err)
	}
}
