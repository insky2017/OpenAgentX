package fleet

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	fleetmodel "openagentx/internal/fleet"
)

func TestVerifyUserUnitsRequiresExactCanonicalArgvAndUserManager(t *testing.T) {
	home := t.TempDir()
	config := filepath.Join(home, ".openagentx", "workers", "quote.yaml")
	prepared := []preparedAgent{{entry: fleetmodel.Agent{AgentID: "quote", Enabled: true}, workerPath: config}}
	var calls []string
	deps := Dependencies{
		UserHomeDir: func() (string, error) { return home, nil },
		RunSystemctl: func(_ context.Context, args ...string) (string, error) {
			calls = append(calls, strings.Join(args, " "))
			if strings.Contains(calls[len(calls)-1], "LoadState") {
				return "loaded", nil
			}
			return fmt.Sprintf("{ path=%s/.local/bin/openagentx ; argv[]=%s/.local/bin/openagentx worker run --config %s ; ignore_errors=no ; }", home, home, config), nil
		},
	}
	if err := verifyUserUnits(context.Background(), prepared, deps); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"--user show openagentx-worker@quote.service --property=LoadState --value",
		"--user show openagentx-worker@quote.service --property=ExecStart --value",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("systemctl calls=%v", calls)
	}
}

func TestVerifyUserUnitsRejectsMissingOrMismatchedActualArgv(t *testing.T) {
	home := t.TempDir()
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
