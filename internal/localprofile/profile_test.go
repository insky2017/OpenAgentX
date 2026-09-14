package localprofile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testResolver(environment map[string]string, home string, homeErr error) Resolver {
	return Resolver{
		LookupEnv: func(name string) (string, bool) {
			value, ok := environment[name]
			return value, ok
		},
		UserHomeDir: func() (string, error) { return home, homeErr },
	}
}

func TestResolvePriorityAndDefaults(t *testing.T) {
	resources := []struct {
		resource Resource
		env      string
		relative string
	}{
		{SocketPath, EnvSocketPath, filepath.Join("run", "openagentx.sock")},
		{DatabasePath, EnvDatabasePath, filepath.Join("data", "openagentx.db")},
		{FleetManifest, EnvFleetManifest, "fleet.yaml"},
		{WorkerConfigDir, EnvWorkerConfigDir, "workers"},
		{CredentialsPath, EnvCredentialsPath, "credentials.json"},
	}
	for _, item := range resources {
		t.Run(string(item.resource), func(t *testing.T) {
			explicit := filepath.Join(string(filepath.Separator), "explicit", "x", "..", "value")
			resolver := testResolver(map[string]string{
				item.env: "/resource/value",
				EnvHome:  "/environment/home",
			}, "", errors.New("must not resolve user home"))
			resolved, err := resolver.Resolve(item.resource, Override{Set: true, Value: explicit})
			if err != nil || resolved.Path != filepath.Clean(explicit) || resolved.Source != SourceExplicit {
				t.Fatalf("explicit resolved=%+v err=%v", resolved, err)
			}

			resolved, err = resolver.Resolve(item.resource, Override{})
			if err != nil || resolved.Path != "/resource/value" || resolved.Source != SourceResourceEnv {
				t.Fatalf("resource env resolved=%+v err=%v", resolved, err)
			}

			resolver = testResolver(map[string]string{EnvHome: "/environment/home/"}, "", errors.New("must not resolve user home"))
			resolved, err = resolver.Resolve(item.resource, Override{})
			if err != nil || resolved.Path != filepath.Join("/environment/home", item.relative) || resolved.Source != SourceHomeEnv {
				t.Fatalf("home env resolved=%+v err=%v", resolved, err)
			}

			resolver = testResolver(nil, "/users/one/", nil)
			resolved, err = resolver.Resolve(item.resource, Override{})
			want := filepath.Join("/users/one", ".openagentx", item.relative)
			if err != nil || resolved.Path != want || resolved.Source != SourceUserHome {
				t.Fatalf("user home resolved=%+v want=%q err=%v", resolved, want, err)
			}
		})
	}
}

func TestResolveFailsClosedWithoutFallback(t *testing.T) {
	tests := []struct {
		name     string
		resolver Resolver
		override Override
		want     error
	}{
		{name: "explicit empty", resolver: testResolver(map[string]string{EnvSocketPath: "/fallback"}, "/home/user", nil), override: Override{Set: true}, want: ErrEmptyPath},
		{name: "explicit relative", resolver: testResolver(map[string]string{EnvSocketPath: "/fallback"}, "/home/user", nil), override: Override{Set: true, Value: "run/socket"}, want: ErrRelativePath},
		{name: "explicit tilde", resolver: testResolver(map[string]string{EnvSocketPath: "/fallback"}, "/home/user", nil), override: Override{Set: true, Value: "~/run/socket"}, want: ErrRelativePath},
		{name: "explicit invalid", resolver: testResolver(nil, "/home/user", nil), override: Override{Set: true, Value: "/tmp/bad\x00path"}, want: ErrInvalidPath},
		{name: "resource env empty", resolver: testResolver(map[string]string{EnvSocketPath: "", EnvHome: "/fallback"}, "/home/user", nil), want: ErrEmptyPath},
		{name: "resource env relative", resolver: testResolver(map[string]string{EnvSocketPath: "run/socket", EnvHome: "/fallback"}, "/home/user", nil), want: ErrRelativePath},
		{name: "home env empty", resolver: testResolver(map[string]string{EnvHome: ""}, "/home/user", nil), want: ErrEmptyPath},
		{name: "home env relative", resolver: testResolver(map[string]string{EnvHome: ".openagentx"}, "/home/user", nil), want: ErrRelativePath},
		{name: "home lookup error", resolver: testResolver(nil, "", errors.New("lookup failed")), want: ErrHomeUnavailable},
		{name: "home lookup empty", resolver: testResolver(nil, "", nil), want: ErrHomeUnavailable},
		{name: "home lookup relative", resolver: testResolver(nil, "users/one", nil), want: ErrHomeUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolved, err := test.resolver.Resolve(SocketPath, test.override)
			if !errors.Is(err, test.want) || resolved.Path != "" {
				t.Fatalf("resolved=%+v err=%v want=%v", resolved, err, test.want)
			}
		})
	}
}

func TestDifferentHomesCanonicalIdentityAndNoSideEffects(t *testing.T) {
	root := t.TempDir()
	homeOne := filepath.Join(root, "one")
	homeTwo := filepath.Join(root, "two")
	one, err := testResolver(nil, homeOne, nil).Resolve(SocketPath, Override{})
	if err != nil {
		t.Fatal(err)
	}
	two, err := testResolver(nil, homeTwo, nil).Resolve(SocketPath, Override{})
	if err != nil {
		t.Fatal(err)
	}
	if one.Path == two.Path {
		t.Fatalf("different homes resolved to the same socket: %q", one.Path)
	}
	canonical, err := testResolver(nil, "", errors.New("unused")).Resolve(SocketPath, Override{Set: true, Value: filepath.Join(root, "one", ".openagentx", "run", "..", "run", "openagentx.sock") + string(filepath.Separator)})
	if err != nil || canonical.Path != one.Path {
		t.Fatalf("canonical=%+v one=%+v err=%v", canonical, one, err)
	}
	if _, err := os.Stat(filepath.Join(homeOne, ".openagentx")); !os.IsNotExist(err) {
		t.Fatalf("resolver created filesystem state: %v", err)
	}
}

func TestWorkerConfigRejectsAgentPathEscape(t *testing.T) {
	resolver := testResolver(map[string]string{EnvWorkerConfigDir: "/srv/openagentx/workers"}, "", errors.New("unused"))
	resolved, err := resolver.WorkerConfig(Override{}, "quote-service")
	if err != nil || resolved.Path != "/srv/openagentx/workers/quote-service.yaml" {
		t.Fatalf("resolved=%+v err=%v", resolved, err)
	}
	for _, agentID := range []string{"", "../escape", "nested/escape", ".", "agent name", strings.Repeat("a", 65)} {
		if _, err := resolver.WorkerConfig(Override{}, agentID); !errors.Is(err, ErrInvalidAgentID) {
			t.Fatalf("Agent ID %q error=%v", agentID, err)
		}
	}
}

func TestPathFlagDistinguishesUnsetFromExplicitEmpty(t *testing.T) {
	var path PathFlag
	if override := path.Override(); override.Set || override.Value != "" {
		t.Fatalf("unset override=%+v", override)
	}
	if err := path.Set(""); err != nil {
		t.Fatal(err)
	}
	if override := path.Override(); !override.Set || override.Value != "" {
		t.Fatalf("explicit empty override=%+v", override)
	}
}

func TestSourceHintDoesNotExposeEnvironmentValue(t *testing.T) {
	secretPath := "/tmp/do-not-print-sensitive-path"
	resolver := testResolver(map[string]string{EnvCredentialsPath: secretPath}, "/home/user", nil)
	hint := resolver.SourceHint(CredentialsPath)
	if hint != "$"+EnvCredentialsPath || strings.Contains(hint, secretPath) {
		t.Fatalf("unsafe source hint %q", hint)
	}
	secretRelativePath := "private/do-not-print-sensitive-path"
	resolver = testResolver(map[string]string{EnvCredentialsPath: secretRelativePath}, "/home/user", nil)
	if _, err := resolver.Resolve(CredentialsPath, Override{}); err == nil || strings.Contains(err.Error(), secretRelativePath) {
		t.Fatalf("unsafe resolver error %q", err)
	}
}

func TestEnsureDistinctRejectsPathCollision(t *testing.T) {
	err := EnsureDistinct(map[string]string{
		"database": "/tmp/openagentx/data/../same",
		"socket":   "/tmp/openagentx/same",
	})
	if !errors.Is(err, ErrPathConflict) {
		t.Fatalf("collision error=%v", err)
	}
	if err := EnsureDistinct(map[string]string{"database": "/tmp/db", "socket": "/tmp/socket"}); err != nil {
		t.Fatal(err)
	}
}
