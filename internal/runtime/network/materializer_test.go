package network

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"openagentx/internal/api"
	"openagentx/internal/domain"
)

func TestMaterializerUsesPersistentKeyedIntegrity(t *testing.T) {
	root := filepath.Join(t.TempDir(), "network")
	materializer, err := NewMaterializer(root)
	if err != nil {
		t.Fatal(err)
	}
	content := testNetworkContent("secret-v1")
	secret := &api.NetworkSecretPayload{Username: "fixture-user", Password: "guessable-password"}
	identity := domain.RuntimeIdentity{AdapterID: "agy-batch", AdapterVersion: "1"}
	policy, err := materializer.Materialize(content, secret, identity)
	if err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(policy.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	black, err := os.ReadFile(policy.BlackIPFile)
	if err != nil {
		t.Fatal(err)
	}
	plain := sha256.Sum256(append(append([]byte(nil), config...), black...))
	if policy.MaterializationDigest == hex.EncodeToString(plain[:]) {
		t.Fatal("materialization digest exposes an unkeyed offline secret oracle")
	}

	restarted, err := NewMaterializer(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.PrepareRun(policy); err != nil {
		t.Fatalf("persistent integrity key did not survive restart: %v", err)
	}
	if err := os.WriteFile(policy.ConfigFile, append(config, '#'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.PrepareRun(policy); !errors.Is(err, domain.ErrInvalidManifest) {
		t.Fatalf("tampered materialization error=%v", err)
	}
	info, err := os.Lstat(filepath.Join(root, materializerKeyFile))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 || !info.Mode().IsRegular() {
		t.Fatalf("integrity key mode=%v", info.Mode())
	}
}

func TestImportSourceIdentityIsNormalizedAndKeyed(t *testing.T) {
	root := filepath.Join(t.TempDir(), "network")
	materializer, err := NewMaterializer(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "proxy.conf")
	payload := []byte("select_proxy_mode = only_socks5\nsocks5 = proxy.example:1080\nsocks5_username = user\nsocks5_password = guessable-password\n")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := materializer.ImportConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	plain := sha256.Sum256(payload)
	if first.SourceIdentity == hex.EncodeToString(plain[:]) {
		t.Fatal("import source identity exposes an unkeyed offline secret oracle")
	}
	restarted, err := NewMaterializer(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := restarted.ImportConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if second.SourceIdentity != first.SourceIdentity {
		t.Fatal("import identity changed after materializer restart")
	}
	changed := []byte("select_proxy_mode=only_socks5\nsocks5=proxy.example:1080\nsocks5_username=user\nsocks5_password=other-password\n")
	if err := os.WriteFile(path, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	third, err := restarted.ImportConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if third.SourceIdentity == first.SourceIdentity {
		t.Fatal("import identity did not change with secret content")
	}
}

func TestMaterializerRejectsInvalidPersistentIntegrityKey(t *testing.T) {
	root := filepath.Join(t.TempDir(), "network")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, materializerKeyFile), []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewMaterializer(root); err == nil {
		t.Fatal("invalid integrity key was accepted")
	}
}

func TestMaterializerRejectsWhitespaceRoot(t *testing.T) {
	if _, err := NewMaterializer(" \t\n "); err == nil {
		t.Fatal("whitespace-only materialization root was accepted")
	}
}

func TestMaterializerInternalFilesRemainAnchoredAfterRootReplacement(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "network")
	anchored := filepath.Join(base, "network-original")
	materializer, err := NewMaterializer(root)
	if err != nil {
		t.Fatal(err)
	}
	content := testNetworkContent("secret-v1")
	identity := domain.RuntimeIdentity{AdapterID: "agy-batch", AdapterVersion: "1"}
	policy, err := materializer.Materialize(content, &api.NetworkSecretPayload{Username: "user", Password: "password"}, identity)
	if err != nil {
		t.Fatal(err)
	}
	configLeaf := filepath.Base(policy.ConfigFile)
	if err := os.Rename(root, anchored); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, configLeaf), []byte("attacker replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := materializer.PrepareRun(policy); err != nil {
		t.Fatalf("PrepareRun followed replaced root path: %v", err)
	}

	content.ContentVersion++
	content.ManifestDigest = content.ComputeManifestDigest()
	second, err := materializer.Materialize(content, &api.NetworkSecretPayload{Username: "user", Password: "password"}, identity)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(anchored, filepath.Base(second.ConfigFile))); err != nil {
		t.Fatalf("materialization was not written through anchored directory fd: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.Base(second.ConfigFile))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("materialization escaped into replacement directory: %v", err)
	}
}

func TestMaterializerConcurrentOpenPublishesOneCompleteKey(t *testing.T) {
	root := filepath.Join(t.TempDir(), "network")
	const count = 16
	materializers := make([]*Materializer, count)
	errorsSeen := make([]error, count)
	var wg sync.WaitGroup
	for i := range materializers {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			materializers[index], errorsSeen[index] = NewMaterializer(root)
		}(i)
	}
	wg.Wait()
	for _, err := range errorsSeen {
		if err != nil {
			t.Fatal(err)
		}
	}
	want := materializers[0].authenticate("fixture", []byte("payload"))
	for _, materializer := range materializers[1:] {
		if got := materializer.authenticate("fixture", []byte("payload")); got != want {
			t.Fatalf("concurrent materializers loaded different keys: got=%q want=%q", got, want)
		}
	}
	key, err := readIntegrityKey(filepath.Join(root, materializerKeyFile))
	if err != nil || len(key) != materializerKeySize {
		t.Fatalf("published materializer key length=%d err=%v", len(key), err)
	}
}

func TestMaterializerRendersNativeHTTPAddressWithoutURLScheme(t *testing.T) {
	materializer, err := NewMaterializer(filepath.Join(t.TempDir(), "network"))
	if err != nil {
		t.Fatal(err)
	}
	content := testNetworkContent("")
	content.Mode = "only_http_proxy"
	content.DirectIPs = nil
	content.ManifestDigest = content.ComputeManifestDigest()
	policy, err := materializer.Materialize(content, nil, domain.RuntimeIdentity{AdapterID: "agy-batch", AdapterVersion: "1"})
	if err != nil {
		t.Fatal(err)
	}
	config, err := readMaterialized(policy.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	want := "http_proxy = proxy.example:1080\n"
	if !strings.Contains(string(config), want) || strings.Contains(string(config), "http://") {
		t.Fatalf("native HTTP config=%q want line %q", config, want)
	}
}

func TestMaterializerDoesNotPublishSensitiveConfigWhenBlackIPCommitFails(t *testing.T) {
	root := filepath.Join(t.TempDir(), "network")
	materializer, err := NewMaterializer(root)
	if err != nil {
		t.Fatal(err)
	}
	content := testNetworkContent("secret-v1")
	identity := domain.RuntimeIdentity{AdapterID: "agy-batch", AdapterVersion: "1"}
	idBytes, err := json.Marshal(struct {
		Manifest, Secret string
		Identity         domain.RuntimeIdentity
	}{content.ManifestDigest, content.SecretVersion, identity})
	if err != nil {
		t.Fatal(err)
	}
	idSum := sha256.Sum256(idBytes)
	id := hex.EncodeToString(idSum[:])
	blackPath := filepath.Join(root, id+".blackip")
	if err := os.WriteFile(blackPath, []byte("corrupt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := materializer.Materialize(content, &api.NetworkSecretPayload{Username: "user", Password: "sensitive-value"}, identity); !errors.Is(err, domain.ErrInvalidManifest) {
		t.Fatalf("blackip conflict error=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, id+".conf")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("credential-bearing config was published before blackip commit: %v", err)
	}
}

func TestImportConfigRejectsDuplicateKeysAndTrailingPortText(t *testing.T) {
	materializer, err := NewMaterializer(filepath.Join(t.TempDir(), "network"))
	if err != nil {
		t.Fatal(err)
	}
	for name, payload := range map[string]string{
		"duplicate":     "select_proxy_mode=only_socks5\nsocks5=proxy.example:1080\nsocks5=other.example:1080\n",
		"trailing-port": "select_proxy_mode=only_socks5\nsocks5=proxy.example:1080junk\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "proxy.conf")
			if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := materializer.ImportConfig(path); err == nil {
				t.Fatal("invalid imported config was accepted")
			}
		})
	}
}

func testNetworkContent(secretVersion string) domain.NetworkProfileContent {
	content := domain.NetworkProfileContent{
		ProfileID: "proxy-main", ContentVersion: 1, Mode: "only_socks5",
		Host: "proxy.example", Port: 1080, DirectIPs: []string{"192.0.2.10"}, SecretVersion: secretVersion,
		CreatedBy: "owner", CreatedAt: time.Unix(1, 0).UTC(),
	}
	content.ManifestDigest = content.ComputeManifestDigest()
	return content
}
