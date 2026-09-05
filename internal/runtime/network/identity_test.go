package network

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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
	if err := VerifyRuntimeIdentity(context.Background(), identity, wrapper, helper, native); err == nil {
		t.Fatal("native executable replacement was not detected")
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
