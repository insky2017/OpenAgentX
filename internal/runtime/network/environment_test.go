package network

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openagentx/internal/api"

	"openagentx/internal/domain"
)

func TestNamedProfileDirectRulesMaterializeIntoFormalAgyEnvironment(t *testing.T) {
	materializer, err := NewMaterializer(filepath.Join(t.TempDir(), "network"))
	if err != nil {
		t.Fatal(err)
	}
	content := domain.NetworkProfileContent{
		ProfileID: "proxy", ContentVersion: 1, Mode: "only_socks5", Host: "proxy.example", Port: 1080,
		DirectIPs: []string{"192.0.2.10", "2001:db8::1"}, SecretVersion: "secret-v1",
		CreatedBy: "owner", CreatedAt: time.Unix(1, 0).UTC(),
	}
	content.ManifestDigest = content.ComputeManifestDigest()
	policy, err := materializer.Materialize(content, &api.NetworkSecretPayload{Username: "user", Password: "password"}, domain.RuntimeIdentity{AdapterID: "agy-batch", AdapterVersion: "1"})
	if err != nil {
		t.Fatal(err)
	}
	environment, err := Environment(nil, policy, "agy-batch", "agy-graft")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(environment, "\n")
	if !strings.Contains(joined, "AGY_GRAFT_CONFIG="+policy.ConfigFile) || !strings.Contains(joined, "AGY_GRAFT_BLACKIP_FILE="+policy.BlackIPFile) {
		t.Fatalf("formal AGY environment=%v", environment)
	}
	black, err := os.ReadFile(policy.BlackIPFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(black) != "192.0.2.10\n2001:0db8:0000:0000:0000:0000:0000:0001\n" {
		t.Fatalf("blackip content=%q", black)
	}
}

func TestEnvironmentDirectRemovesProxyVariables(t *testing.T) {
	env, err := Environment([]string{"PATH=/bin", "HTTPS_PROXY=http://secret.invalid", "NO_PROXY=internal", "LD_PRELOAD=/tmp/x.so"}, domain.NetworkPolicy{Mode: domain.NetworkDirect, DirectDestinations: []string{"quote.internal:443"}}, "codebuddy-cli", "codebuddy")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "HTTP_PROXY=") || strings.Contains(joined, "HTTPS_PROXY=") || strings.Contains(joined, "ALL_PROXY=") {
		t.Fatalf("proxy variables leaked: %q", joined)
	}
	if strings.Contains(joined, "LD_PRELOAD=") || !strings.Contains(joined, "NO_PROXY=quote.internal:443") {
		t.Fatalf("unsafe or direct destination environment: %q", joined)
	}
	if !strings.Contains(joined, "PATH=/bin") {
		t.Fatalf("non-proxy environment was not preserved: %q", joined)
	}
}

func TestEnvironmentNamedProfileUses0600AgyConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.conf")
	blackPath := filepath.Join(t.TempDir(), "profile.blackip")
	if err := os.WriteFile(path, []byte("select_proxy_mode = only_socks5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blackPath, []byte("203.0.113.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env, err := Environment([]string{"PATH=/first", "UNDECLARED_SECRET=value", "PATH=/last", "HTTP_PROXY=http://ignored.invalid", "AGY_GRAFT_CONFIG=/old", "AGY_GRAFT_REAL_BIN=/opt/agy"}, domain.NetworkPolicy{
		Mode: domain.NetworkNamedProfile, ProfileID: "proxy-main", ProfileVersion: 1, ConfigFile: path, BlackIPFile: blackPath, ProxyMode: "only_socks5",
	}, "agy-batch", "agy-graft")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "AGY_GRAFT_CONFIG="+path) || !strings.Contains(joined, "AGY_GRAFT_BLACKIP_FILE="+blackPath) || !strings.Contains(joined, "AGY_GRAFT_SELECT_PROXY_MODE=only_socks5") {
		t.Fatalf("profile environment missing: %q", joined)
	}
	if strings.Contains(joined, "HTTP_PROXY=") || strings.Contains(joined, "AGY_GRAFT_CONFIG=/old") || strings.Contains(joined, "UNDECLARED_SECRET=") || strings.Contains(joined, "PATH=/first") || !strings.Contains(joined, "PATH=/last") || !strings.Contains(joined, "AGY_GRAFT_REAL_BIN=/opt/agy") {
		t.Fatalf("old proxy configuration leaked: %q", joined)
	}
}

func TestEnvironmentRejectsUnsafeAgyDirectMode(t *testing.T) {
	_, err := Environment(nil, domain.NetworkPolicy{Mode: domain.NetworkDirect}, "agy-batch", "agy-graft")
	if err == nil {
		t.Fatal("expected direct mode to reject agy-graft")
	}
}

func TestEnvironmentRejectsInsecureProfileFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.conf")
	if err := os.WriteFile(path, []byte("socks5 = 127.0.0.1:1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Environment(nil, domain.NetworkPolicy{Mode: domain.NetworkNamedProfile, ProfileID: "p", ProfileVersion: 1, ConfigFile: path}, "agy-batch", "agy-graft")
	if err == nil {
		t.Fatal("expected mode 0600 validation failure")
	}
}
