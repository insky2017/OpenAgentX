package network

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

func InspectRuntimeIdentity(ctx context.Context, adapterID, adapterVersion, binary, helper string, native ...string) (domain.RuntimeIdentity, error) {
	path, err := exec.LookPath(binary)
	if err != nil {
		return domain.RuntimeIdentity{}, err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return domain.RuntimeIdentity{}, err
	}
	sum, err := fileSHA(path)
	if err != nil {
		return domain.RuntimeIdentity{}, err
	}
	identity := domain.RuntimeIdentity{AdapterID: adapterID, AdapterVersion: adapterVersion, ExecutableSHA256: sum}
	if adapterID == "agy-batch" {
		identity.WrapperSHA256 = sum
		realBinary := "/home/sky/.local/bin/agy"
		if len(native) > 0 && strings.TrimSpace(native[0]) != "" {
			realBinary = native[0]
		}
		realPath, lookErr := exec.LookPath(realBinary)
		if lookErr != nil {
			return domain.RuntimeIdentity{}, lookErr
		}
		realPath, err = filepath.Abs(realPath)
		if err != nil {
			return domain.RuntimeIdentity{}, err
		}
		identity.ExecutableSHA256, err = fileSHA(realPath)
		if err != nil {
			return domain.RuntimeIdentity{}, err
		}
		if helper == "" {
			helper = "/home/sky/tools/bin/mgraftcp"
		}
		helper, err = filepath.Abs(helper)
		if err != nil {
			return domain.RuntimeIdentity{}, err
		}
		identity.HelperSHA256, err = fileSHA(helper)
		if err != nil {
			return domain.RuntimeIdentity{}, err
		}
		versionCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		output, runErr := exec.CommandContext(versionCtx, helper, "--version").Output()
		if runErr != nil {
			return domain.RuntimeIdentity{}, fmt.Errorf("inspect network helper version: %w", runErr)
		}
		identity.HelperVersion = strings.TrimSpace(string(bytes.TrimSpace(output)))
		if !validHelperVersion(identity.HelperVersion) {
			return domain.RuntimeIdentity{}, domain.ErrInvalidInput("network helper version is not a bounded printable line")
		}
	}
	return identity, identity.Validate()
}
func VerifyRuntimeIdentity(ctx context.Context, expected domain.RuntimeIdentity, binary, helper string, native ...string) (bool, error) {
	if err := expected.Validate(); err != nil {
		return false, err
	}
	actual, err := InspectRuntimeIdentity(ctx, expected.AdapterID, expected.AdapterVersion, binary, helper, native...)
	if err != nil {
		return false, err
	}
	changed, err := CompareRuntimeIdentity(expected, actual)
	if changed && err == nil {
		slog.WarnContext(ctx, "Runtime executable changed; continuing with current executable",
			"adapter_id", expected.AdapterID, "expected_sha256", expected.ExecutableSHA256,
			"actual_sha256", actual.ExecutableSHA256)
	}
	return changed, err
}

// CompareRuntimeIdentity treats executable updates as advisory without accepting
// a different protocol or network wrapper/helper, or incomplete digest evidence.
func CompareRuntimeIdentity(expected, actual domain.RuntimeIdentity) (bool, error) {
	if err := expected.Validate(); err != nil {
		return false, err
	}
	if err := actual.Validate(); err != nil {
		return false, err
	}
	changed := actual.ExecutableSHA256 != expected.ExecutableSHA256
	if changed && (actual.ExecutableSHA256 == "" || expected.ExecutableSHA256 == "") {
		return false, domain.ErrConflict("runtime executable identity is missing")
	}
	actual.ExecutableSHA256 = expected.ExecutableSHA256
	if actual != expected {
		return false, domain.ErrConflict("runtime identity changed")
	}
	return changed, nil
}

// EmitExecutableChangeWarning uses the same public event projection as Runtime
// output. No host paths, process environment or external diagnostics are copied.
func EmitExecutableChangeWarning(ctx context.Context, sink openruntime.EventSink) error {
	if sink == nil {
		return nil // VerifyRuntimeIdentity already recorded a server-side warning.
	}
	return sink.Emit(ctx, openruntime.RuntimeEvent{
		Type: "identity.warning", OccurredAt: time.Now().UTC(),
		Payload: []byte(`{"stage":"preflight","status":"warning","text":"Warning: Runtime executable changed since registration; continuing with the current executable."}`),
	})
}

func validHelperVersion(value string) bool {
	if value == "" || len(value) > 128 || strings.ContainsAny(value, "\r\n") {
		return false
	}
	for _, char := range []byte(value) {
		if char < 0x20 || char > 0x7e {
			return false
		}
	}
	return true
}
func fileSHA(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", domain.ErrInvalidInput("runtime binary must be a non-symlink regular file")
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}
