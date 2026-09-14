package fleet

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	fleetmodel "openagentx/internal/fleet"
)

func TestVerifyUserUnitsRequiresExactCanonicalArgvAndUserManager(t *testing.T) {
	home := t.TempDir()
	writeTestExecutable(t, home)
	config := filepath.Join(home, ".openagentx", "workers", "quote.yaml")
	prepared := []preparedAgent{{entry: fleetmodel.Agent{AgentID: "quote", Enabled: true}, workerPath: config}}
	var calls []string
	deps := Dependencies{
		UserHomeDir: func() (string, error) { return home, nil },
		RunSystemctl: func(_ context.Context, args ...string) (string, error) {
			calls = append(calls, strings.Join(args, " "))
			call := calls[len(calls)-1]
			if strings.Contains(call, "LoadState") {
				return "loaded", nil
			}
			if strings.Contains(call, "ExecStart") {
				return fmt.Sprintf("{ path=%s/.local/bin/openagentx ; argv[]=%s/.local/bin/openagentx worker run --config %s ; ignore_errors=no ; }", home, home, config), nil
			}
			if strings.Contains(call, "WorkingDirectory") {
				return home, nil
			}
			return filepath.Join(home, ".openagentx", "workers", "quote.env") + " (ignore_errors=yes)", nil
		},
	}
	if err := verifyUserUnits(context.Background(), prepared, deps); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"--user show openagentx-worker@quote.service --property=LoadState --value",
		"--user show openagentx-worker@quote.service --property=ExecStart --value",
		"--user show openagentx-worker@quote.service --property=WorkingDirectory --value",
		"--user show openagentx-worker@quote.service --property=EnvironmentFiles --value",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("systemctl calls=%v", calls)
	}
}

func TestVerifyUserUnitsRejectsMissingOrMismatchedActualArgv(t *testing.T) {
	home := t.TempDir()
	writeTestExecutable(t, home)
	config := filepath.Join(home, ".openagentx", "workers", "quote.yaml")
	prepared := []preparedAgent{{entry: fleetmodel.Agent{AgentID: "quote", Enabled: true}, workerPath: config}}
	for name, execStart := range map[string]string{
		"different config": fmt.Sprintf("{ argv[]=%s/.local/bin/openagentx worker run --config /tmp/other.yaml ; }", home),
		"extra argument":   fmt.Sprintf("{ argv[]=%s/.local/bin/openagentx worker run --config %s --unsafe ; }", home, config),
		"unstructured":     fmt.Sprintf("%s/.local/bin/openagentx worker run --config %s", home, config),
	} {
		t.Run(name, func(t *testing.T) {
			deps := Dependencies{UserHomeDir: func() (string, error) { return home, nil }, RunSystemctl: func(_ context.Context, args ...string) (string, error) {
				if strings.Contains(strings.Join(args, " "), "LoadState") {
					return "loaded", nil
				}
				return execStart, nil
			}}
			if err := verifyUserUnits(context.Background(), prepared, deps); err == nil {
				t.Fatal("invalid unit accepted")
			}
		})
	}
}

func TestVerifyUserUnitsRejectsMismatchedWorkingDirectoryOrEnvironmentFile(t *testing.T) {
	home := t.TempDir()
	writeTestExecutable(t, home)
	config := filepath.Join(home, ".openagentx", "workers", "quote.yaml")
	prepared := []preparedAgent{{entry: fleetmodel.Agent{AgentID: "quote", Enabled: true}, workerPath: config}}
	for name, wrongProperty := range map[string]string{
		"working directory": "WorkingDirectory",
		"environment file":  "EnvironmentFiles",
	} {
		t.Run(name, func(t *testing.T) {
			deps := Dependencies{UserHomeDir: func() (string, error) { return home, nil }, RunSystemctl: func(_ context.Context, args ...string) (string, error) {
				call := strings.Join(args, " ")
				switch {
				case strings.Contains(call, "LoadState"):
					return "loaded", nil
				case strings.Contains(call, "ExecStart"):
					return fmt.Sprintf("{ argv[]=%s/.local/bin/openagentx worker run --config %s ; }", home, config), nil
				case strings.Contains(call, "WorkingDirectory"):
					if wrongProperty == "WorkingDirectory" {
						return "/tmp/wrong", nil
					}
					return home, nil
				case strings.Contains(call, "EnvironmentFiles"):
					if wrongProperty == "EnvironmentFiles" {
						return "/etc/openagentx/workers/quote.env (ignore_errors=yes)", nil
					}
					return filepath.Join(home, ".openagentx", "workers", "quote.env") + " (ignore_errors=yes)", nil
				}
				return "", nil
			}}
			if err := verifyUserUnits(context.Background(), prepared, deps); err == nil {
				t.Fatal("mismatched user unit property accepted")
			}
		})
	}
}

func TestCanonicalUserBinaryRejectsMissingSymlinkAndNonExecutable(t *testing.T) {
	home := t.TempDir()
	deps := Dependencies{UserHomeDir: func() (string, error) { return home, nil }}
	if _, err := canonicalUserBinary(deps); err == nil {
		t.Fatal("missing canonical binary accepted")
	}
	binary := filepath.Join(home, ".local", "bin", "openagentx")
	if err := os.MkdirAll(filepath.Dir(binary), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := canonicalUserBinary(deps); err == nil {
		t.Fatal("non-executable canonical binary accepted")
	}
	if err := os.Remove(binary); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, "binary-target")
	if err := os.WriteFile(target, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, binary); err != nil {
		t.Fatal(err)
	}
	if _, err := canonicalUserBinary(deps); err == nil {
		t.Fatal("symlink canonical binary accepted")
	}
	if err := os.Remove(binary); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("binary"), 0o722); err != nil {
		t.Fatal(err)
	}
	if _, err := canonicalUserBinary(deps); err == nil {
		t.Fatal("group/other-writable canonical binary accepted")
	}
}

func writeTestExecutable(t *testing.T, home string) {
	t.Helper()
	binary := filepath.Join(home, ".local", "bin", "openagentx")
	if err := os.MkdirAll(filepath.Dir(binary), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
}
