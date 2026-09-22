package network

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openagentx/internal/domain"
)

func TestInspectAgyRuntimeIdentitySeparatesWrapperNativeAndHelper(t *testing.T) {
	dir := t.TempDir()
	wrapper := writeIdentityFixture(t, dir, "wrapper", "#!/bin/sh\nexit 0\n")
	native := writeIdentityFixture(t, dir, "agy", "#!/bin/sh\nexit 0\n# native\n")
	helper := writeIdentityFixture(t, dir, "mgraftcp", "#!/bin/sh\nprintf 'v0.7.4-fixture\\n'\n")
	identity, err := InspectRuntimeIdentity(context.Background(), "agy-batch", "1", wrapper, helper, native)
	if err != nil {
		t.Fatal(err)
	}
	if identity.WrapperSHA256 == "" || identity.ExecutableSHA256 == "" || identity.HelperSHA256 == "" || identity.HelperVersion != "v0.7.4-fixture" {
		t.Fatalf("incomplete AGY identity: %+v", identity)
	}
	if identity.WrapperSHA256 == identity.ExecutableSHA256 {
		t.Fatalf("wrapper and native executable were not independently identified: %+v", identity)
	}
	if err := os.WriteFile(native, []byte("#!/bin/sh\nexit 0\n# replaced\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if changed, err := VerifyRuntimeIdentity(context.Background(), identity, wrapper, helper, native); err != nil || !changed {
		t.Fatalf("native executable replacement should warn: changed=%t err=%v", changed, err)
	}
}

func TestCompareRuntimeIdentityOnlyExecutableChangeIsAdvisory(t *testing.T) {
	expected := domain.RuntimeIdentity{AdapterID: "agy-batch", AdapterVersion: "1",
		ExecutableSHA256: strings.Repeat("a", 64), WrapperSHA256: strings.Repeat("b", 64),
		HelperSHA256: strings.Repeat("c", 64), HelperVersion: "fixture-1"}
	for _, tc := range []struct {
		name    string
		change  func(*domain.RuntimeIdentity)
		warning bool
		reject  bool
	}{
		{name: "unchanged", change: func(*domain.RuntimeIdentity) {}},
		{name: "executable", change: func(i *domain.RuntimeIdentity) { i.ExecutableSHA256 = strings.Repeat("d", 64) }, warning: true},
		{name: "adapter", change: func(i *domain.RuntimeIdentity) { i.AdapterID = "other" }, reject: true},
		{name: "protocol", change: func(i *domain.RuntimeIdentity) { i.AdapterVersion = "2" }, reject: true},
		{name: "wrapper", change: func(i *domain.RuntimeIdentity) { i.WrapperSHA256 = strings.Repeat("d", 64) }, reject: true},
		{name: "helper", change: func(i *domain.RuntimeIdentity) { i.HelperSHA256 = strings.Repeat("d", 64) }, reject: true},
		{name: "helper version", change: func(i *domain.RuntimeIdentity) { i.HelperVersion = "fixture-2" }, reject: true},
		{name: "missing digest", change: func(i *domain.RuntimeIdentity) { i.ExecutableSHA256 = "" }, reject: true},
		{name: "invalid digest", change: func(i *domain.RuntimeIdentity) { i.ExecutableSHA256 = "invalid" }, reject: true},
		{name: "executable and helper", change: func(i *domain.RuntimeIdentity) {
			i.ExecutableSHA256 = strings.Repeat("d", 64)
			i.HelperSHA256 = strings.Repeat("e", 64)
		}, reject: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actual := expected
			tc.change(&actual)
			warning, err := CompareRuntimeIdentity(expected, actual)
			if warning != tc.warning || (err != nil) != tc.reject {
				t.Fatalf("warning=%t err=%v", warning, err)
			}
		})
	}
}

func TestVerifyRuntimeIdentityRejectsUnsafeFilesAndNetworkHelperChanges(t *testing.T) {
	for _, target := range []string{"wrapper", "helper", "native missing", "native symlink", "native directory"} {
		t.Run(target, func(t *testing.T) {
			dir := t.TempDir()
			wrapper := writeIdentityFixture(t, dir, "wrapper", "#!/bin/sh\nexit 0\n")
			native := writeIdentityFixture(t, dir, "native", "#!/bin/sh\nexit 0\n")
			helper := writeIdentityFixture(t, dir, "helper", "#!/bin/sh\necho helper-1\n")
			identity, err := InspectRuntimeIdentity(context.Background(), "agy-batch", "1", wrapper, helper, native)
			if err != nil {
				t.Fatal(err)
			}
			switch target {
			case "wrapper":
				writeIdentityFixture(t, dir, "wrapper", "#!/bin/sh\nexit 1\n")
			case "helper":
				writeIdentityFixture(t, dir, "helper", "#!/bin/sh\necho helper-2\n")
			default:
				if err := os.Remove(native); err != nil {
					t.Fatal(err)
				}
				if target == "native symlink" {
					if err := os.Symlink(wrapper, native); err != nil {
						t.Fatal(err)
					}
				} else if target == "native directory" {
					if err := os.Mkdir(native, 0o700); err != nil {
						t.Fatal(err)
					}
				}
			}
			if changed, err := VerifyRuntimeIdentity(context.Background(), identity, wrapper, helper, native); err == nil || changed {
				t.Fatalf("unsafe change accepted: changed=%t err=%v", changed, err)
			}
		})
	}
}

func TestInspectAgyRuntimeIdentityRejectsUnboundedHelperVersion(t *testing.T) {
	dir := t.TempDir()
	wrapper := writeIdentityFixture(t, dir, "wrapper", "#!/bin/sh\nexit 0\n")
	native := writeIdentityFixture(t, dir, "agy", "#!/bin/sh\nexit 0\n")
	helper := writeIdentityFixture(t, dir, "mgraftcp", "#!/bin/sh\nprintf '%0130d\\n' 0\n")
	if _, err := InspectRuntimeIdentity(context.Background(), "agy-batch", "1", wrapper, helper, native); err == nil {
		t.Fatal("unbounded helper version was accepted")
	}
}

func writeIdentityFixture(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
