package credentialstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testCredential(socket, installation, username, token string) Credential {
	return Credential{SocketPath: socket, InstallationID: installation, Username: username,
		TokenID: "token-" + username, Token: token, AbsoluteExpires: time.Now().Add(time.Hour)}
}

func TestStoreIsPrivateAtomicAndSupportsInstallationScopedUsers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile", "credentials.json")
	store, err := New(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	owner := testCredential("/run/openagentx.sock", "installation-a", "owner", "owner-token")
	operator := testCredential("/run/openagentx.sock", "installation-a", "operator", "operator-token")
	if err := store.Save(owner); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(operator); err != nil {
		t.Fatal(err)
	}
	if mode := mustMode(t, filepath.Dir(path)); mode != 0o700 {
		t.Fatalf("credential directory mode=%#o", mode)
	}
	if mode := mustMode(t, path); mode != 0o600 {
		t.Fatalf("credential file mode=%#o", mode)
	}
	loaded, err := store.Load(owner.SocketPath, owner.InstallationID, owner.Username)
	if err != nil || loaded.Token != owner.Token {
		t.Fatalf("load owner=%+v err=%v", loaded, err)
	}
	current, err := store.LoadCurrentForSocket(owner.SocketPath)
	if err != nil || current.Username != operator.Username {
		t.Fatalf("current selection=%+v err=%v", current, err)
	}
	if _, err := store.Load(owner.SocketPath, "installation-b", owner.Username); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-installation credential load error=%v", err)
	}
	if _, err := store.Load("/run/other-openagentx.sock", owner.InstallationID, owner.Username); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-socket credential load error=%v", err)
	}
}

func TestStoreRejectsSymlinkBroadPermissionsAndWrongOwner(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "credentials.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"credentials":[],"current":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	store, _ := New(path, Options{})
	if _, err := store.LoadCurrentForSocket("/run/openagentx.sock"); !errors.Is(err, ErrUnsafeCredential) {
		t.Fatalf("broad credential mode accepted: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadCurrentForSocket("/run/openagentx.sock"); !errors.Is(err, ErrUnsafeCredential) {
		t.Fatalf("symlink credential accepted: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	secure, _ := New(path, Options{})
	if err := secure.Save(testCredential("/run/openagentx.sock", "installation-a", "owner", "token")); err != nil {
		t.Fatal(err)
	}
	wrongOwner, _ := New(path, Options{CurrentUID: func() int { return os.Geteuid() + 1 }})
	if _, err := wrongOwner.LoadCurrentForSocket("/run/openagentx.sock"); !errors.Is(err, ErrUnsafeCredential) {
		t.Fatalf("wrong owner accepted: %v", err)
	}
}

func TestStoreRejectsBroadOrSymlinkDirectoryAndNonRegularCredential(t *testing.T) {
	root := t.TempDir()
	broadDirectory := filepath.Join(root, "broad")
	if err := os.Mkdir(broadDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	broadStore, _ := New(filepath.Join(broadDirectory, "credentials.json"), Options{})
	if err := broadStore.Save(testCredential("/run/openagentx.sock", "installation-a", "owner", "token")); !errors.Is(err, ErrUnsafeCredential) {
		t.Fatalf("broad credential directory accepted: %v", err)
	}

	secureDirectory := filepath.Join(root, "secure")
	if err := os.Mkdir(secureDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	credentialPath := filepath.Join(secureDirectory, "credentials.json")
	if err := os.Mkdir(credentialPath, 0o700); err != nil {
		t.Fatal(err)
	}
	nonRegular, _ := New(credentialPath, Options{})
	if _, err := nonRegular.LoadCurrentForSocket("/run/openagentx.sock"); !errors.Is(err, ErrUnsafeCredential) {
		t.Fatalf("non-regular credential accepted: %v", err)
	}

	targetDirectory := filepath.Join(root, "target-directory")
	if err := os.Mkdir(targetDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	symlinkDirectory := filepath.Join(root, "linked-directory")
	if err := os.Symlink(targetDirectory, symlinkDirectory); err != nil {
		t.Fatal(err)
	}
	symlinkStore, _ := New(filepath.Join(symlinkDirectory, "credentials.json"), Options{})
	if err := symlinkStore.Save(testCredential("/run/openagentx.sock", "installation-a", "owner", "token")); !errors.Is(err, ErrUnsafeCredential) {
		t.Fatalf("symlink credential directory accepted: %v", err)
	}
}

func TestFailedAtomicReplacementKeepsPreviousCredentialAndCleansTemporaryFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile", "credentials.json")
	store, _ := New(path, Options{})
	oldCredential := testCredential("/run/openagentx.sock", "installation-a", "owner", "old-token")
	if err := store.Save(oldCredential); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("simulated crash before rename")
	failing, _ := New(path, Options{BeforeRename: func() error { return injected }})
	newCredential := oldCredential
	newCredential.Token = "new-token"
	if err := failing.Save(newCredential); !errors.Is(err, injected) {
		t.Fatalf("atomic write error=%v", err)
	}
	loaded, err := store.Load(oldCredential.SocketPath, oldCredential.InstallationID, oldCredential.Username)
	if err != nil || loaded.Token != "old-token" {
		t.Fatalf("previous credential not preserved: %+v err=%v", loaded, err)
	}
	temporary, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".credentials-*"))
	if err != nil || len(temporary) != 0 {
		t.Fatalf("temporary credential files=%v err=%v", temporary, err)
	}
}

func mustMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
