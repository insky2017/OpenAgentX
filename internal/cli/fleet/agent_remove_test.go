package fleet

import (
	"context"
	"errors"
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

func removalLocalFixture(t *testing.T) (fleetFixture, agentRemovalOptions, Dependencies) {
	t.Helper()
	f := newFleetFixture(t)
	deps, _, _, _ := fixtureDeps(f, time.Now(), ownerConsole(time.Now()))
	deps = withDefaults(deps)
	deps.RunSystemctl = func(_ context.Context, args ...string) (string, error) {
		if len(args) < 3 || args[1] != "show" {
			t.Fatalf("unexpected lifecycle mutation: %v", args)
		}
		return "not-found", nil
	}
	for _, id := range []string{"candidate", "retained"} {
		o := agentOptions{id: id, runtime: "codex", binary: "codex", workspace: filepath.Join(f.home, "workspace-"+id), timeout: time.Minute, paths: fleetPaths{workerDir: f.workerDir, socket: f.socket}}
		if err := os.MkdirAll(o.workspace, 0700); err != nil {
			t.Fatal(err)
		}
		content, err := agentRuntimeConfig(o)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(f.workerDir, id+".yaml")
		if _, err = fleetmodel.WriteExactFileAtomic(path, content, fleetmodel.AtomicFileOptions{}); err != nil {
			t.Fatal(err)
		}
		if err = fleetmodel.AddAgent(f.manifest, fleetmodel.Agent{AgentID: id, WorkerConfig: path, Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	return f, agentRemovalOptions{ids: []string{"candidate"}, purge: true, paths: fleetPaths{workerDir: f.workerDir, manifest: f.manifest, socket: f.socket}}, deps
}

func TestAgentRemovalLocalAtomicFailurePreservesRetainedAgent(t *testing.T) {
	f, o, deps := removalLocalFixture(t)
	before, err := os.ReadFile(f.manifest)
	if err != nil {
		t.Fatal(err)
	}
	p, err := planAgentRemovalLocal(context.Background(), o, deps)
	if err != nil || len(p.Blockers) > 0 {
		t.Fatalf("plan=%+v err=%v", p, err)
	}
	if len(p.Files) != 1 || p.Files[0].Path != filepath.Join(f.workerDir, "candidate.yaml") {
		t.Fatalf("unsafe files: %+v", p.Files)
	}
	unlock, err := lockAgentRemovalManifest(f.manifest)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if second, e := lockAgentRemovalManifest(f.manifest); e == nil {
		second()
		t.Fatal("concurrent lifecycle acquired removal lock")
	}
	injected := errors.New("rename fault")
	deps.BeforeAtomicRename = func(string) error { return injected }
	if err = writeAgentRemovalManifest(f.manifest, p, deps); !errors.Is(err, injected) {
		t.Fatalf("err=%v", err)
	}
	after, _ := os.ReadFile(f.manifest)
	if string(before) != string(after) {
		t.Fatal("failed removal changed manifest")
	}
	deps.BeforeAtomicRename = nil
	if err = writeAgentRemovalManifest(f.manifest, p, deps); err != nil {
		t.Fatal(err)
	}
	remaining, err := fleetmodel.LoadFile(f.manifest)
	if err != nil || len(remaining.Agents) != 1 || remaining.Agents[0].AgentID != "retained" || !remaining.Agents[0].Enabled {
		t.Fatalf("retained=%+v err=%v", remaining, err)
	}
	if err = deleteAgentRemovalFile(p.Files[0]); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(f.workerDir, "retained.yaml")); err != nil {
		t.Fatal("retained config removed")
	}
	if _, err = os.Stat(filepath.Join(f.home, "workspace-candidate")); err != nil {
		t.Fatal("workspace removed")
	}
}

func TestAgentRemovalRejectsSymlinksAndChangedArtifacts(t *testing.T) {
	f, o, deps := removalLocalFixture(t)
	state := filepath.Join(f.workerDir, "codex", "candidate")
	if err := os.MkdirAll(state, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(f.home, "workspace-retained")
	if err := os.Symlink(target, filepath.Join(state, "escape")); err != nil {
		t.Fatal(err)
	}
	p, err := planAgentRemovalLocal(context.Background(), o, deps)
	if err != nil || !strings.Contains(strings.Join(p.Blockers, " "), "nonregular removal entry") {
		t.Fatalf("plan=%+v err=%v", p, err)
	}
	if err = os.Remove(filepath.Join(state, "escape")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(state, "state.json")
	if err = os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := inspectAgentRemovalFile(state, "state_directory")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = deleteAgentRemovalFile(file); err == nil {
		t.Fatal("changed state was removed")
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatal("changed artifact not preserved")
	}
	// A selected canonical file must never resolve through a parent-directory symlink.
	alias := filepath.Join(f.home, "alias")
	if err = os.Symlink(f.workerDir, alias); err != nil {
		t.Fatal(err)
	}
	if _, err = inspectAgentRemovalFile(filepath.Join(alias, "candidate.yaml"), "worker_config"); err == nil {
		t.Fatal("symlink parent accepted")
	}
}

func TestAgentRemovalProtectedAndSharedPathsBlock(t *testing.T) {
	f, o, deps := removalLocalFixture(t)
	path := filepath.Join(f.workerDir, "retained.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.ReplaceAll(string(data), filepath.Join(f.workerDir, "codex", "retained"), filepath.Join(f.workerDir, "codex", "candidate")))
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(f.workerDir, "codex", "candidate"), 0700); err != nil {
		t.Fatal(err)
	}
	o.ids = append(o.ids, "openagentx")
	p, err := planAgentRemovalLocal(context.Background(), o, deps)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(p.Blockers, " ")
	if !strings.Contains(joined, "protected Agent: openagentx") || !strings.Contains(joined, "overlaps retained Agent") {
		t.Fatalf("blockers=%v", p.Blockers)
	}
}

func TestAgentRemovalInheritedTemplateNeverControlsAnotherProfile(t *testing.T) {
	for _, active := range []string{"inactive", "active"} {
		t.Run(active, func(t *testing.T) {
			_, o, deps := removalLocalFixture(t)
			deps.RunSystemctl = func(_ context.Context, args ...string) (string, error) {
				if len(args) < 4 || args[1] != "show" {
					t.Fatalf("unexpected service mutation: %v", args)
				}
				switch args[3] {
				case "--property=LoadState":
					return "loaded", nil
				case "--property=ExecStart":
					return "{ path=/other/profile/openagentx ; argv[]=/other/profile/openagentx worker run --config /other/profile/candidate.yaml ; }", nil
				case "--property=ActiveState":
					return active, nil
				case "--property=UnitFileState":
					return "disabled", nil
				case "--property=FragmentPath":
					return "/other/profile/openagentx-worker@.service", nil
				}
				t.Fatalf("unexpected property: %v", args)
				return "", nil
			}
			p, err := planAgentRemovalLocal(context.Background(), o, deps)
			if err != nil {
				t.Fatal(err)
			}
			if active == "inactive" {
				if len(p.Blockers) > 0 || len(p.Services) != 1 || p.Services[0].Loaded {
					t.Fatalf("unowned inherited template not preserved: %+v", p)
				}
			} else if len(p.Blockers) == 0 {
				t.Fatal("active service from another profile was not blocked")
			}
		})
	}
}

// Real SQLite and filesystem effects with a stubbed authenticated control transport:
// a failed manifest commit must resume from the durable purge receipt.
func TestAgentRemovalResumesLocalCleanupAfterDatabaseCommit(t *testing.T) {
	f, _, localDeps := removalLocalFixture(t)
	now := time.Now().UTC()
	console := ownerConsole(now)
	deps, store, out, stderr := fixtureDeps(f, now, console)
	deps.RunSystemctl = localDeps.RunSystemctl
	adminDeps := admincli.Dependencies{Out: out, Err: stderr, ReadPassword: func(string) (string, error) { return "isolated-test-password", nil }}
	if code := admincli.ExecuteInit([]string{"--db", f.database}, adminDeps); code != 0 {
		t.Fatalf("init %d: %s", code, stderr)
	}
	for _, id := range []string{"candidate", "retained"} {
		role := filepath.Join(f.home, id+".md")
		if err := os.WriteFile(role, []byte("fixture role"), 0600); err != nil {
			t.Fatal(err)
		}
		def := admincli.AgentDefinition{Version: 1, AgentID: id, PrincipalID: "agent-" + id, OrganizationID: "default", DisplayName: id}
		def.Profile.InstructionsPath = role
		def.Profile.WorkspaceRoot = filepath.Join(f.home, "workspace-"+id)
		data, err := yaml.Marshal(def)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(f.workerDir, "identities", id+".yaml")
		if _, err = fleetmodel.WriteExactFileAtomic(path, data, fleetmodel.AtomicFileOptions{}); err != nil {
			t.Fatal(err)
		}
		if code := admincli.ExecuteAgent([]string{"apply", "--db", f.database, "--file", path}, adminDeps); code != 0 {
			t.Fatalf("apply %d: %s", code, stderr)
		}
	}
	repo, err := sqlite.OpenMaintenance(context.Background(), f.database, true, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	installation, err := repo.InstallationID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	owner, err := repo.GetWebUserByUsername(context.Background(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	console.probe.InstallationID = installation
	console.session.InstallationID = installation
	console.session.Principal.PrincipalID = owner.PrincipalID
	store.credential.InstallationID = installation
	console.options = []domain.ConsoleAgentOption{{AgentID: "candidate", WorkerStatus: domain.WorkerStatusOffline}, {AgentID: "retained", WorkerStatus: domain.WorkerStatusOffline}}
	args := append(f.args("remove"), "candidate", "--purge-history", "--yes")
	fault := errors.New("manifest injected after database commit")
	deps.BeforeAtomicRename = func(string) error { return fault }
	if code := ExecuteAgent(args, deps); code != 1 || !strings.Contains(stderr.String(), fault.Error()) {
		t.Fatalf("expected partial failure code=%d stderr=%s out=%s", code, stderr, out)
	}
	if _, _, err = repo.GetAgent(context.Background(), "candidate"); !errors.Is(err, domain.ErrAgentNotFound) {
		t.Fatalf("purge did not commit: %v", err)
	}
	if _, err = os.Stat(filepath.Join(f.workerDir, "candidate.yaml")); err != nil {
		t.Fatal("config removed before manifest commit")
	}
	deps.BeforeAtomicRename = nil
	console.options = console.options[1:]
	out.Reset()
	stderr.Reset()
	if code := ExecuteAgent(args, deps); code != 0 {
		t.Fatalf("receipt retry code=%d stderr=%s out=%s", code, stderr, out)
	}
	if !strings.Contains(out.String(), `"replayed":true`) || !strings.Contains(out.String(), `"step":"selected_agent_removal_complete"`) {
		t.Fatalf("missing replay completion: %s", out)
	}
	if _, err = os.Stat(filepath.Join(f.workerDir, "candidate.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("candidate config remains: %v", err)
	}
	if _, _, err = repo.GetAgent(context.Background(), "retained"); err != nil {
		t.Fatal("retained Agent lost", err)
	}
	if _, err = os.Stat(filepath.Join(f.home, "workspace-candidate")); err != nil {
		t.Fatal("workspace lost", err)
	}
}

func TestAgentRemovalPreservesSelectedWorkspaceOverlappingState(t *testing.T) {
	for _, relation := range []string{"same", "workspace_parent", "workspace_child"} {
		t.Run(relation, func(t *testing.T) {
			f, o, deps := removalLocalFixture(t)
			state := filepath.Join(f.workerDir, "codex", "candidate")
			workspace := state
			switch relation {
			case "workspace_parent":
				workspace = filepath.Dir(state)
			case "workspace_child":
				workspace = filepath.Join(state, "workspace")
			}
			if err := os.MkdirAll(workspace, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(state, 0700); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(state, "workspace-data.txt")
			if err := os.WriteFile(marker, []byte("must survive removal planning"), 0600); err != nil {
				t.Fatal(err)
			}
			content, err := agentRuntimeConfig(agentOptions{id: "candidate", runtime: "codex", binary: "codex", workspace: workspace, timeout: time.Minute, paths: o.paths})
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(f.workerDir, "candidate.yaml"), content, 0600); err != nil {
				t.Fatal(err)
			}
			p, err := planAgentRemovalLocal(context.Background(), o, deps)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.Join(p.Blockers, " "), "overlaps preserved workspace") {
				t.Fatalf("workspace collision not blocked: %+v", p)
			}
			for _, file := range p.Files {
				if file.Path == state {
					t.Fatal("preserved workspace remains in deletion list")
				}
			}
			data, err := os.ReadFile(marker)
			if err != nil || string(data) != "must survive removal planning" {
				t.Fatalf("workspace artifact changed: %q %v", data, err)
			}
		})
	}
}
