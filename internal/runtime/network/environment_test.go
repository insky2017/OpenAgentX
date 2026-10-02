package network

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
	env, err := Environment([]string{"PATH=/first", "UNDECLARED_SECRET=value", "PATH=/last", "HTTP_PROXY=http://ignored.invalid", "AGY_GRAFT_CONFIG=/old", "AGY_GRAFT_REAL_BIN=/opt/agy", "AGY_GRAFT_NATIVE_PROXY=1"}, domain.NetworkPolicy{
		Mode: domain.NetworkNamedProfile, ProfileID: "proxy-main", ProfileVersion: 1, ConfigFile: path, BlackIPFile: blackPath, ProxyMode: "only_socks5",
	}, "agy-batch", "agy-graft")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "AGY_GRAFT_CONFIG="+path) || !strings.Contains(joined, "AGY_GRAFT_BLACKIP_FILE="+blackPath) || !strings.Contains(joined, "AGY_GRAFT_SELECT_PROXY_MODE=only_socks5") {
		t.Fatalf("profile environment missing: %q", joined)
	}
	if strings.Contains(joined, "HTTP_PROXY=") || strings.Contains(joined, "AGY_GRAFT_CONFIG=/old") || strings.Contains(joined, "AGY_GRAFT_NATIVE_PROXY=") || strings.Contains(joined, "UNDECLARED_SECRET=") || strings.Contains(joined, "PATH=/first") || !strings.Contains(joined, "PATH=/last") || !strings.Contains(joined, "AGY_GRAFT_REAL_BIN=/opt/agy") {
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

func TestEnvironmentInheritPreservesNativeProxyWrapperContract(t *testing.T) {
	env, err := Environment([]string{"AGY_GRAFT_NATIVE_PROXY=1", "HTTPS_PROXY=http://127.0.0.1:7897"}, domain.NetworkPolicy{Mode: domain.NetworkInherit}, "agy-batch", "agy-graft")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "AGY_GRAFT_NATIVE_PROXY=1") || !strings.Contains(joined, "HTTPS_PROXY=http://127.0.0.1:7897") {
		t.Fatalf("native proxy inputs lost: %q", joined)
	}
}

func TestCodexEnvironmentPreservesProviderAndLocalGateway(t *testing.T) {
	base := []string{"HOME=/home/fixture", "CODEX_HOME=/tmp/codex-fixture", "OPENAI_API_KEY=fixture-key", "OPENAI_BASE_URL=http://127.0.0.1:8080", "HTTPS_PROXY=http://proxy.invalid:3128", "NO_PROXY=service.internal,localhost", "no_proxy=other.internal", "AGY_GRAFT_NATIVE_PROXY=1", "LD_PRELOAD=/tmp/inject.so", "UNRELATED_TOKEN=not-for-codex"}
	env, err := Environment(base, domain.NetworkPolicy{Mode: domain.NetworkInherit}, "codex-app-server", "codex")
	if err != nil {
		t.Fatal(err)
	}
	values := environmentValues(env)
	for _, name := range []string{"HOME", "CODEX_HOME", "OPENAI_API_KEY", "OPENAI_BASE_URL", "HTTPS_PROXY"} {
		if values[name] != environmentValues(base)[name] {
			t.Errorf("required environment key %s was not preserved", name)
		}
	}
	for _, name := range []string{"AGY_GRAFT_NATIVE_PROXY", "LD_PRELOAD", "UNRELATED_TOKEN"} {
		if _, ok := values[name]; ok {
			t.Errorf("unrelated environment key %s leaked", name)
		}
	}
	want := "service.internal,localhost,other.internal,127.0.0.1,::1"
	if values["NO_PROXY"] != want || values["no_proxy"] != want {
		t.Fatal("local gateway or explicit bypass destinations were lost")
	}
}

func TestCodexDirectKeepsProviderWithoutProxyAndRejectsNamedProfile(t *testing.T) {
	base := []string{"CODEX_HOME=/tmp/codex-fixture", "OPENAI_API_KEY=fixture-key", "OPENAI_BASE_URL=http://127.0.0.1:8080", "HTTP_PROXY=http://proxy.invalid", "https_proxy=http://proxy.invalid", "ALL_PROXY=socks5://proxy.invalid", "NO_PROXY=old.invalid"}
	env, err := Environment(base, domain.NetworkPolicy{Mode: domain.NetworkDirect, DirectDestinations: []string{"service.internal"}}, "codex-app-server", "codex")
	if err != nil {
		t.Fatal(err)
	}
	values := environmentValues(env)
	for _, name := range []string{"HTTP_PROXY", "https_proxy", "ALL_PROXY"} {
		if _, ok := values[name]; ok {
			t.Errorf("direct mode retained %s", name)
		}
	}
	if values["CODEX_HOME"] == "" || values["OPENAI_API_KEY"] == "" || values["OPENAI_BASE_URL"] == "" {
		t.Fatal("direct mode dropped provider configuration")
	}
	if values["NO_PROXY"] != "service.internal,localhost,127.0.0.1,::1" || values["no_proxy"] != values["NO_PROXY"] {
		t.Fatal("direct destinations were not merged with local bypasses")
	}
	_, err = Environment(nil, domain.NetworkPolicy{Mode: domain.NetworkNamedProfile, ProfileID: "agy-only", ProfileVersion: 1, ConfigFile: "/not/read"}, "codex-app-server", "codex")
	if !errors.Is(err, domain.ErrUnsupportedCapability) {
		t.Fatalf("Codex named profile must remain unsupported, got %v", err)
	}
}

func TestCodexEnvironmentDoesNotChangeOtherAdapters(t *testing.T) {
	base := []string{"PATH=/bin", "NO_PROXY=explicit.internal", "CODEX_HOME=/tmp/codex-fixture", "OPENAI_API_KEY=fixture-key", "OPENAI_BASE_URL=http://127.0.0.1:8080"}
	for _, adapterID := range []string{"agy-batch", "codebuddy-cli", "fake"} {
		t.Run(adapterID, func(t *testing.T) {
			env, err := Environment(base, domain.NetworkPolicy{Mode: domain.NetworkInherit}, adapterID, "fixture")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(env, base[:2]) {
				t.Fatal("Codex environment rules changed a different adapter")
			}
		})
	}
}

func TestWithLoopbackNoProxyPreservesLastValuesAndInput(t *testing.T) {
	base := []string{"NO_PROXY=obsolete.invalid", "KEEP=value", "no_proxy=other.internal,127.0.0.1", "NO_PROXY= current.internal,localhost,current.internal, "}
	before := append([]string(nil), base...)
	got := WithLoopbackNoProxy(base)
	want := []string{"KEEP=value", "NO_PROXY=current.internal,localhost,other.internal,127.0.0.1,::1", "no_proxy=current.internal,localhost,other.internal,127.0.0.1,::1"}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(base, before) {
		t.Fatal("proxy bypass normalization changed input or lost last-value semantics")
	}
	if !reflect.DeepEqual(WithLoopbackNoProxy(got), got) {
		t.Fatal("proxy bypass normalization was not idempotent")
	}
	values := environmentValues(WithLoopbackNoProxy([]string{"NO_PROXY=*"}))
	if !strings.HasPrefix(values["NO_PROXY"], "*,") {
		t.Fatal("explicit all-destinations bypass was lost")
	}
}

func environmentValues(environment []string) map[string]string {
	values := make(map[string]string)
	for _, entry := range environment {
		name, value, _ := strings.Cut(entry, "=")
		values[name] = value
	}
	return values
}
