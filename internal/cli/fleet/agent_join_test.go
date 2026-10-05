package fleet

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	fleetmodel "openagentx/internal/fleet"
	workerconfig "openagentx/internal/worker"
)

func TestAgentJoinPreparesOfflineAndRejectsConflictingReplay(t *testing.T) {
	f := newFleetFixture(t)
	deps, _, out, stderr := fixtureDeps(f, time.Now(), ownerConsole(time.Now()))
	deps.RunSystemctl = func(context.Context, ...string) (string, error) {
		t.Fatal("prepare must not touch services")
		return "", nil
	}
	deps.NewConsole = func(string) (consoleClient, error) { t.Fatal("prepare must not contact Control"); return nil, nil }
	handoff := filepath.Join(f.home, "handoff.md")
	if err := os.WriteFile(handoff, []byte("The original turn is still active; wait for explicit resume.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	args := append(f.args("join"), "--id", "research", "--name", "Research", "--workspace", f.home, "--role-text", "Research only.", "--handoff-file", handoff, "--thread-id", "thread-known", "--prepare")
	for i := 0; i < 2; i++ {
		if code := ExecuteAgent(args, deps); code != 0 {
			t.Fatalf("code=%d stderr=%s", code, stderr)
		}
	}
	if code := ExecuteAgent(append(f.args("status"), "--id", "research", "--json"), deps); code != 0 {
		t.Fatalf("prepared status unexpectedly needs a live service: %d %s", code, stderr)
	}
	if !strings.Contains(out.String(), `"runtime_state":"unverified"`) {
		t.Fatalf("prepared status invented live readiness: %s", out)
	}
	if _, err := os.Stat(f.database); !os.IsNotExist(err) {
		t.Fatalf("offline prepare touched database: %v", err)
	}
	manifest, err := fleetmodel.LoadFile(f.manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Agents) != 1 || manifest.Agents[0].Enabled {
		t.Fatalf("prepared agent admitted by bulk Fleet: %+v", manifest)
	}
	config, err := workerconfig.LoadProcessConfig(filepath.Join(f.workerDir, "research.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	backend := config.RuntimeBackendConfig[0]
	if backend.AdapterID != "codex-app-server" || backend.Options["thread_id"] != "thread-known" || backend.Options["binary"] != "codex" {
		t.Fatalf("config=%+v", backend)
	}
	data, err := os.ReadFile(filepath.Join(f.workerDir, "joins", "research.json"))
	if err != nil {
		t.Fatal(err)
	}
	var receipt joinReceipt
	if err = json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Ready || receipt.State != "local_prepared" || receipt.ThreadID != "thread-known" {
		t.Fatalf("receipt=%+v", receipt)
	}
	if !strings.Contains(out.String(), "未绑定 live CLI") {
		t.Fatalf("output=%s", out)
	}
	changed := append([]string{}, args...)
	for i := range changed {
		if changed[i] == "thread-known" {
			changed[i] = "different-thread"
		}
	}
	if code := ExecuteAgent(changed, deps); code == 0 {
		t.Fatal("conflicting thread silently accepted")
	}
	if err := os.WriteFile(handoff, []byte("different handoff"), 0600); err != nil {
		t.Fatal(err)
	}
	if code := ExecuteAgent(args, deps); code == 0 {
		t.Fatal("conflicting handoff silently accepted")
	}
}

func TestAgentJoinRejectsEnabledAndUnsafeEndpoint(t *testing.T) {
	f := newFleetFixture(t)
	writeManifest(t, f, "quote")
	deps, _, _, stderr := fixtureDeps(f, time.Now(), ownerConsole(time.Now()))
	args := append(f.args("join"), "--id", "quote", "--name", "quote", "--workspace", f.home, "--role-text", "Research only.")
	if code := ExecuteAgent(args, deps); code == 0 || !strings.Contains(stderr.String(), "already enabled") {
		t.Fatalf("code=%d err=%s", code, stderr)
	}
	for _, endpoint := range []string{"ws://user:secret@localhost:1234", "ws://example.com:1234", "ws://localhost:1234?token=secret", "http://localhost:1234"} {
		o := agentOptions{id: "new", endpoint: endpoint, workspace: f.home, paths: fleetPaths{workerDir: f.workerDir, manifest: f.manifest}}
		if err := prepareAgentJoin(&o, withDefaults(deps)); err == nil {
			t.Fatalf("accepted unsafe endpoint %q", endpoint)
		}
	}
}

func TestCodexEnvironmentUsesPersistentProxyAndPreservesExisting(t *testing.T) {
	f := newFleetFixture(t)
	deps, _, _, _ := fixtureDeps(f, time.Now(), ownerConsole(time.Now()))
	proxyDir := filepath.Join(f.home, ".config", "mihomo")
	if err := os.MkdirAll(proxyDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proxyDir, "config.yaml"), []byte("mixed-port: 17897\nsecret-other-field: do-not-copy\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HTTPS_PROXY", "http://ambient-invalid:1")
	t.Setenv("CODEX_HOME", filepath.Join(f.home, "codex-home"))
	o := agentOptions{runtime: "codex"}
	path := filepath.Join(f.workerDir, "research.yaml")
	if err := persistAgentEnvironment(o, path, withDefaults(deps)); err != nil {
		t.Fatal(err)
	}
	envPath := strings.TrimSuffix(path, ".yaml") + ".env"
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"HTTPS_PROXY=\"http://127.0.0.1:17897\"", "CODEX_HOME=\"" + filepath.Join(f.home, "codex-home"), "localhost", "127.0.0.1", "::1"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %s", want)
		}
	}
	if strings.Contains(string(data), "ambient-invalid") || strings.Contains(string(data), "do-not-copy") {
		t.Fatal("copied non-authoritative proxy or unrelated secret")
	}
	info, _ := os.Stat(envPath)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
	if err := os.WriteFile(filepath.Join(proxyDir, "config.yaml"), []byte("mixed-port: 9999\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := persistAgentEnvironment(o, path, withDefaults(deps)); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(envPath)
	if string(after) != string(data) {
		t.Fatal("existing environment silently changed")
	}
}

func TestNativeOpenRequiresBridgeAndPreservesManagedReferences(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	f := newFleetFixture(t)
	deps := withDefaults(Dependencies{IsInteractive: func() bool { return true }})
	deps.OpenNative = nil // Test the absent bridge fail-closed seam.
	o := agentOptions{id: "research", paths: fleetPaths{workerDir: f.workerDir, socket: f.socket, credentials: f.credentials}}
	if err := openAgentNative(context.Background(), o, deps); err == nil {
		t.Fatal("bare native launch accepted")
	}
	called := false
	deps.OpenNative = func(_ context.Context, r NativeOpenRequest) error {
		called = true
		if r.AgentID != "research" || r.Socket != f.socket || r.WorkerConfig != filepath.Join(f.workerDir, "research.yaml") {
			t.Fatalf("request=%+v", r)
		}
		return nil
	}
	if err := openAgentNative(context.Background(), o, deps); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("bridge not called")
	}
}

func TestPreparedRegistrationChecksIdentityOnceAndRejectsChangedInputs(t *testing.T) {
	f := newFleetFixture(t)
	now := time.Now()
	deps, _, _, stderr := fixtureDeps(f, now, ownerConsole(now))
	args := append(f.args("join"), "--id", "research", "--name", "Research", "--workspace", f.home, "--role-text", "Research only.")
	if code := ExecuteAgent(args, deps); code != 0 {
		t.Fatalf("prepare: %d %s", code, stderr)
	}
	password := filepath.Join(f.home, "password")
	if err := os.WriteFile(password, []byte("only-fixture-password-123"), 0600); err != nil {
		t.Fatal(err)
	}
	o := agentOptions{id: "research", passwordFile: password, username: "owner", organization: "default", paths: fleetPaths{manifest: f.manifest, database: f.database, socket: f.socket, workerDir: f.workerDir, credentials: f.credentials}}
	if err := registerPreparedAgent(context.Background(), &o, withDefaults(deps)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(password); err != nil {
		t.Fatal(err)
	}
	if err := registerPreparedAgent(context.Background(), &o, withDefaults(deps)); err != nil {
		t.Fatalf("repeated registration required password again: %v", err)
	}
	if code := ExecuteAgent(args, deps); code == 0 {
		t.Fatal("re-preparing a registered handoff must not claim offline state")
	}
	if err := os.WriteFile(filepath.Join(f.workerDir, "identities", "research.md"), []byte("Different role"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := registerPreparedAgent(context.Background(), &o, withDefaults(deps)); err == nil {
		t.Fatal("changed role accepted after registration")
	}
}
