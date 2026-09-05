package network

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openagentx/internal/domain"
)

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
	if err := os.WriteFile(path, []byte("select_proxy_mode = only_socks5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env, err := Environment([]string{"HTTP_PROXY=http://ignored.invalid", "AGY_GRAFT_CONFIG=/old"}, domain.NetworkPolicy{
		Mode: domain.NetworkNamedProfile, ProfileID: "proxy-main", ProfileVersion: 1, ConfigFile: path, ProxyMode: "only_socks5",
	}, "agy-batch", "agy-graft")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "AGY_GRAFT_CONFIG="+path) || !strings.Contains(joined, "AGY_GRAFT_SELECT_PROXY_MODE=only_socks5") {
		t.Fatalf("profile environment missing: %q", joined)
	}
	if strings.Contains(joined, "HTTP_PROXY=") || strings.Contains(joined, "AGY_GRAFT_CONFIG=/old") {
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
