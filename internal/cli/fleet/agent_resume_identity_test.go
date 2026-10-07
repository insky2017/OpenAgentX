package fleet

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
	admincli "openagentx/internal/cli/admin"
	"openagentx/internal/domain"
	fleetmodel "openagentx/internal/fleet"
	"openagentx/internal/persistence/sqlite"
)

// Real SQLite registration plus the CLI's owner-authenticated Console boundary.
// Runtime/service execution is excluded; production resume covers that separately.
func TestRegisteredResumeUsesCurrentIdentityInsteadOfBootstrapReceipt(t *testing.T) {
	f := newFleetFixture(t)
	now := time.Now()
	client := ownerConsole(now)
	deps, store, _, stderr := fixtureDeps(f, now, client)
	deps = withDefaults(deps)
	args := append(f.args("join"), "--id", "research", "--name", "Research", "--workspace", f.home, "--role-text", "Research only.")
	if code := ExecuteAgent(args, deps); code != 0 {
		t.Fatalf("prepare: %d %s", code, stderr)
	}
	password := filepath.Join(f.home, "password")
	if err := os.WriteFile(password, []byte("only-fixture-password-123"), 0600); err != nil {
		t.Fatal(err)
	}
	o := agentOptions{id: "research", passwordFile: password, username: "owner", organization: "default", paths: fleetPaths{manifest: f.manifest, database: f.database, socket: f.socket, workerDir: f.workerDir, credentials: f.credentials}}
	if err := registerPreparedAgent(context.Background(), &o, deps); err != nil {
		t.Fatal(err)
	}
	repo, err := sqlite.OpenMaintenance(context.Background(), f.database, true, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	installation, err := repo.InstallationID(context.Background())
	repo.Close()
	if err != nil {
		t.Fatal(err)
	}
	client.probe.InstallationID = installation
	client.session.InstallationID = installation
	store.credential.InstallationID = installation
	client.options = []domain.ConsoleAgentOption{{AgentID: "research", OrganizationID: "default", DisplayName: "Research", WorkerStatus: domain.WorkerStatusOffline}}
	if err = os.Remove(password); err != nil {
		t.Fatal(err)
	}
	identityPath := filepath.Join(f.workerDir, "identities", "research.yaml")
	workerPath := filepath.Join(f.workerDir, "research.yaml")
	rolePath := filepath.Join(f.workerDir, "identities", "research.md")
	receiptPath := joinReceiptPath(o)
	paths := []string{identityPath, workerPath, rolePath, receiptPath, receiptPath + ".registered", f.manifest}
	baseline := map[string][]byte{}
	for _, path := range paths {
		baseline[path], err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	write := func(path string, content []byte) {
		t.Helper()
		if err := os.WriteFile(path, content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	changeIdentity := func(change func(*admincli.AgentDefinition)) {
		definition, err := admincli.LoadAgentDefinition(identityPath)
		if err != nil {
			t.Fatal(err)
		}
		change(definition)
		content, err := yaml.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		write(identityPath, content)
	}
	for _, tc := range []struct {
		name   string
		mutate func()
		want   string
	}{
		{"legacy-manifest-without-identity", func() {
			manifest, err := fleetmodel.LoadFile(f.manifest)
			if err != nil {
				t.Fatal(err)
			}
			manifest.Agents[0].IdentityFile = ""
			encoded, err := fleetmodel.Encode(manifest)
			if err != nil {
				t.Fatal(err)
			}
			write(f.manifest, encoded)
			write(receiptPath, []byte("{ broken legacy receipt"))
		}, ""},
		{"damaged-receipt", func() { write(receiptPath, []byte("{ broken receipt")) }, ""},
		{"stale-marker-and-mutable-timeout", func() {
			write(workerPath, []byte(strings.ReplaceAll(string(baseline[workerPath]), "timeout: 0s", "timeout: 2h")))
			write(receiptPath+".registered", []byte("old-bootstrap-digest\n"))
		}, ""},
		{"mutable-execution-preference", func() {
			changeIdentity(func(d *admincli.AgentDefinition) {
				v := "agent-model:research"
				d.Profile.DefaultExecutionProfileID = &v
			})
		}, ""},
		{"principal-drift", func() { changeIdentity(func(d *admincli.AgentDefinition) { d.PrincipalID = "another-principal" }) }, "does not match registered Agent"},
		{"role-path-drift", func() {
			other := filepath.Join(f.home, "other-role.md")
			write(other, []byte("Research only.\n"))
			changeIdentity(func(d *admincli.AgentDefinition) { d.Profile.InstructionsPath = other })
		}, "does not match registered Agent"},
		{"workspace-drift", func() {
			changeIdentity(func(d *admincli.AgentDefinition) { d.Profile.WorkspaceRoot = filepath.Dir(f.home) })
		}, "does not match registered Agent"},
		{"capability-drift", func() {
			changeIdentity(func(d *admincli.AgentDefinition) { d.Profile.Capabilities = []string{"unregistered-capability"} })
		}, "does not match registered Agent"},
		{"worker-directory-drift", func() {
			write(workerPath, []byte(strings.ReplaceAll(string(baseline[workerPath]), "working_dir: "+f.home, "working_dir: "+filepath.Dir(f.home))))
		}, "working_dir does not match"},
		{"empty-role", func() { write(rolePath, nil) }, "non-empty regular file"},
		{"wrong-installation", func() {
			client.session.InstallationID = "other-installation"
			client.probe.InstallationID = "other-installation"
			store.credential.InstallationID = "other-installation"
		}, "installation does not match"},
		{"unauthorized", func() { client.session.Principal.Roles = []string{"viewer"} }, "authentication"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				for _, path := range paths {
					write(path, baseline[path])
				}
				client.session.InstallationID = installation
				client.probe.InstallationID = installation
				store.credential.InstallationID = installation
				client.session.Principal.Roles = []string{"owner"}
			}()
			tc.mutate()
			current := o
			current.registeredResume = false
			err := registerPreparedAgent(context.Background(), &current, deps)
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("want %q got %v", tc.want, err)
				}
				return
			}
			if err != nil || !current.registeredResume {
				t.Fatalf("resume=%v verified=%v", err, current.registeredResume)
			}
			if err = enablePreparedAgent(current, deps); err != nil {
				t.Fatalf("enable still depends on bootstrap: %v", err)
			}
			for _, path := range []string{receiptPath, receiptPath + ".registered"} {
				// No repair or checksum rewrite is performed by normal resume.
				if _, err = os.Stat(path); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestUnregisteredResumeRejectsDamagedReceipt(t *testing.T) {
	f := newFleetFixture(t)
	deps, _, _, _ := fixtureDeps(f, time.Now(), ownerConsole(time.Now()))
	o := agentOptions{id: "new-agent", paths: fleetPaths{database: f.database, workerDir: f.workerDir}}
	path := joinReceiptPath(o)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{ invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := registerPreparedAgent(context.Background(), &o, withDefaults(deps)); err == nil {
		t.Fatal("unregistered corrupt import accepted")
	}
	if _, err := os.Stat(f.database); !os.IsNotExist(err) {
		t.Fatalf("failed import created database: %v", err)
	}
}
