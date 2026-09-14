package credentialstore

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testCredential(socket, installation, username, token string) Credential {
	digest := sha256.Sum256([]byte(token))
	return Credential{SocketPath: socket, InstallationID: installation, Username: username,
		TokenID: "token-" + username, Token: base64.RawURLEncoding.EncodeToString(digest[:]), AbsoluteExpires: time.Now().Add(time.Hour)}
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
	newCredential.Token = testCredential(newCredential.SocketPath, newCredential.InstallationID, newCredential.Username, "new-token").Token
	if err := failing.Save(newCredential); !errors.Is(err, injected) {
		t.Fatalf("atomic write error=%v", err)
	}
	loaded, err := store.Load(oldCredential.SocketPath, oldCredential.InstallationID, oldCredential.Username)
	if err != nil || loaded.Token != oldCredential.Token {
		t.Fatalf("previous credential not preserved: %+v err=%v", loaded, err)
	}
	temporary, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".credentials-*"))
	if err != nil || len(temporary) != 0 {
		t.Fatalf("temporary credential files=%v err=%v", temporary, err)
	}
}

func TestSaveReplacesPriorInstallationForSameSocketUserAndPreservesOtherUsers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile", "credentials.json")
	store, _ := New(path, Options{})
	oldOwner := testCredential("/run/openagentx.sock", "installation-old", "owner", "old-owner")
	operator := testCredential("/run/openagentx.sock", "installation-old", "operator", "operator")
	newOwner := testCredential("/run/openagentx.sock", "installation-new", "owner", "new-owner")
	for _, credential := range []Credential{oldOwner, operator, newOwner} {
		if err := store.Save(credential); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Load(oldOwner.SocketPath, oldOwner.InstallationID, oldOwner.Username); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old installation credential remains: %v", err)
	}
	if _, err := store.Load(operator.SocketPath, operator.InstallationID, operator.Username); err != nil {
		t.Fatalf("other user was removed: %v", err)
	}
	current, err := store.LoadCurrentForSocket(newOwner.SocketPath)
	if err != nil || current.InstallationID != newOwner.InstallationID || current.Username != newOwner.Username {
		t.Fatalf("current replacement=%+v err=%v", current, err)
	}
}

func TestStoreRejectsOversizedDuplicateDanglingAndInvalidDocumentsWithoutLeakingToken(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "credentials.json")
	credential := testCredential("/run/openagentx.sock", "installation-a", "owner", "private-token-material")
	tests := map[string][]byte{}
	duplicate := document{Version: 1, Credentials: []Credential{credential, credential}, Current: map[string]selection{}}
	tests["duplicate"] = mustJSON(t, duplicate)
	dangling := document{Version: 1, Credentials: []Credential{credential}, Current: map[string]selection{
		credential.SocketPath: {InstallationID: "installation-missing", Username: credential.Username},
	}}
	tests["dangling current"] = mustJSON(t, dangling)
	invalidToken := credential
	invalidToken.Token = "not-a-valid-token"
	tests["invalid token"] = mustJSON(t, document{Version: 1, Credentials: []Credential{invalidToken}, Current: map[string]selection{}})
	invalidTime := credential
	invalidTime.AbsoluteExpires = time.Time{}
	tests["invalid time"] = mustJSON(t, document{Version: 1, Credentials: []Credential{invalidTime}, Current: map[string]selection{}})
	invalidID := credential
	invalidID.TokenID = "token\nidentifier"
	tests["invalid id"] = mustJSON(t, document{Version: 1, Credentials: []Credential{invalidID}, Current: map[string]selection{}})
	invalidSocket := credential
	invalidSocket.SocketPath = "/run/../run/openagentx.sock"
	tests["noncanonical socket"] = mustJSON(t, document{Version: 1, Credentials: []Credential{invalidSocket}, Current: map[string]selection{}})
	tooMany := document{Version: 1, Current: map[string]selection{}}
	for index := 0; index <= maxCredentialEntries; index++ {
		tooMany.Credentials = append(tooMany.Credentials,
			testCredential("/run/openagentx.sock", "installation-a", fmt.Sprintf("user-%d", index), fmt.Sprintf("token-%d", index)))
	}
	tests["too many entries"] = mustJSON(t, tooMany)
	tests["oversized"] = []byte(`{"version":1,"credentials":[],"current":{},"padding":"` + strings.Repeat("x", maxCredentialDocumentBytes) + `"}`)

	for name, encoded := range tests {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, encoded, 0o600); err != nil {
				t.Fatal(err)
			}
			store, _ := New(path, Options{})
			_, err := store.LoadCurrentForSocket(credential.SocketPath)
			if !errors.Is(err, ErrUnsafeCredential) {
				t.Fatalf("unsafe document accepted: %v", err)
			}
			if strings.Contains(fmt.Sprint(err), credential.Token) {
				t.Fatalf("credential error leaked token")
			}
		})
	}
}

func TestCredentialLockSerializesConcurrentReplaceAndSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile", "credentials.json")
	first, _ := New(path, Options{})
	second, _ := New(path, Options{})
	entered := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		_, err := first.Replace(func() (Credential, error) {
			close(entered)
			<-release
			return testCredential("/run/openagentx.sock", "installation-a", "owner", "owner"), nil
		})
		firstDone <- err
	}()
	<-entered
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- second.Save(testCredential("/run/openagentx.sock", "installation-a", "operator", "operator"))
	}()
	select {
	case err := <-secondDone:
		t.Fatalf("concurrent Save bypassed process lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	for _, username := range []string{"owner", "operator"} {
		if _, err := first.Load("/run/openagentx.sock", "installation-a", username); err != nil {
			t.Fatalf("concurrent user %q was lost: %v", username, err)
		}
	}
}

func TestConcurrentSameUserReplaceCannotRestoreOlderCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile", "credentials.json")
	first, _ := New(path, Options{})
	second, _ := New(path, Options{})
	entered := make(chan struct{})
	release := make(chan struct{})
	var orderMu sync.Mutex
	var order []string
	replace := func(store *Store, label string, gate bool) <-chan error {
		done := make(chan error, 1)
		go func() {
			_, err := store.Replace(func() (Credential, error) {
				if gate {
					close(entered)
					<-release
				}
				orderMu.Lock()
				order = append(order, label)
				orderMu.Unlock()
				return testCredential("/run/openagentx.sock", "installation-a", "owner", label), nil
			})
			done <- err
		}()
		return done
	}
	oldDone := replace(first, "old", true)
	<-entered
	newDone := replace(second, "new", false)
	select {
	case err := <-newDone:
		t.Fatalf("new login bypassed process lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-oldDone; err != nil {
		t.Fatal(err)
	}
	if err := <-newDone; err != nil {
		t.Fatal(err)
	}
	loaded, err := first.Load("/run/openagentx.sock", "installation-a", "owner")
	if err != nil || loaded.Token != testCredential("/run/openagentx.sock", "installation-a", "owner", "new").Token {
		t.Fatalf("stale same-user credential won: %+v err=%v order=%v", loaded, err, order)
	}
}

func TestUnsafeLockFileBlocksMutationAndPreservesCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile", "credentials.json")
	store, _ := New(path, Options{})
	oldCredential := testCredential("/run/openagentx.sock", "installation-a", "owner", "old")
	if err := store.Save(oldCredential); err != nil {
		t.Fatal(err)
	}
	lockPath := path + ".lock"
	if err := os.Chmod(lockPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(testCredential(oldCredential.SocketPath, oldCredential.InstallationID, oldCredential.Username, "new")); !errors.Is(err, ErrUnsafeCredential) {
		t.Fatalf("unsafe lock accepted: %v", err)
	}
	if err := os.Chmod(lockPath, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(oldCredential.SocketPath, oldCredential.InstallationID, oldCredential.Username)
	if err != nil || loaded.Token != oldCredential.Token {
		t.Fatalf("lock failure changed old credential: %+v err=%v", loaded, err)
	}
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	lockTarget := filepath.Join(filepath.Dir(path), "lock-target")
	if err := os.WriteFile(lockTarget, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(lockTarget, lockPath); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(testCredential(oldCredential.SocketPath, oldCredential.InstallationID, oldCredential.Username, "newer")); !errors.Is(err, ErrUnsafeCredential) {
		t.Fatalf("symlink lock accepted: %v", err)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func mustMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
