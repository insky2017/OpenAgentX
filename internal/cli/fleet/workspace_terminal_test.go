package fleet

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	consoleapi "openagentx/internal/api/console"
	"openagentx/internal/domain"
)

func setCodexWorker(t *testing.T, f fleetFixture, id string) string {
	t.Helper()
	path := filepath.Join(f.workerDir, id+".yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content = []byte(strings.ReplaceAll(string(content), "adapter_id: fake", "adapter_id: codex-app-server"))
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(f.workerDir, "codex", id, "state.json")
}

func TestFleetLegacyManifestSelectsNativeCodexAndExplicitConsoleWithoutReplacingLivePane(t *testing.T) {
	for _, consoleOnly := range []bool{false, true} {
		t.Run(fmt.Sprint(consoleOnly), func(t *testing.T) {
			f := newFleetFixture(t)
			writeManifest(t, f, "quote", "risk")
			setCodexWorker(t, f, "quote")
			before, _ := os.ReadFile(f.manifest)
			now := time.Now()
			deps, _, out, stderr := fixtureDeps(f, now, ownerConsole(now))
			args := f.args("workspace")
			if consoleOnly {
				args = append(args, "--console")
			}
			if code := Execute(args, deps); code != 0 {
				t.Fatalf("code=%d: %s", code, stderr.String())
			}
			calls := strings.Join(deps.Tmux.(*testTmux).calls, "\n")
			want := f.binary + " agent open quote --native --socket " + f.socket + " --credentials " + f.credentials + " --file " + f.manifest + " --worker-dir " + f.workerDir + " --db " + f.database
			if strings.Contains(calls, want) == consoleOnly {
				t.Fatalf("incorrect native routing: %s", calls)
			}
			if !strings.Contains(calls, f.binary+" console attach --socket "+f.socket+" --credentials "+f.credentials+" --agent risk") {
				t.Fatalf("unsupported Runtime did not retain Console: %s", calls)
			}
			if !strings.Contains(out.String(), "尚未适配") {
				t.Fatalf("missing capability boundary: %s", out.String())
			}
			if !strings.Contains(calls, "pane-border-format OAX 状态 Console · 原生交互终端尚未适配") {
				t.Fatalf("fallback pane has no visible capability label: %s", calls)
			}
			tmux := deps.Tmux.(*testTmux)
			tmux.calls = nil
			if code := Execute(append(f.args("workspace"), "--respawn-dead"), deps); code != 0 {
				t.Fatalf("reuse: %d %s", code, stderr.String())
			}
			for _, call := range tmux.calls {
				if strings.HasPrefix(call, "respawn-pane") || strings.HasPrefix(call, "kill-") {
					t.Fatalf("replaced live pane: %s", call)
				}
			}
			after, _ := os.ReadFile(f.manifest)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("legacy manifest rewritten")
			}
		})
	}
}

func TestFleetUpWaitsForNativeEndpointAfterWorkerStartBeforeCreatingPane(t *testing.T) {
	for _, initialize := range []bool{false, true} {
		t.Run(fmt.Sprintf("init_before_up_%v", initialize), func(t *testing.T) {
			testFleetUpNativeEndpoint(t, initialize)
		})
	}
}

func testFleetUpNativeEndpoint(t *testing.T, initialize bool) {
	f := newFleetFixture(t)
	writeManifest(t, f, "quote")
	statePath := setCodexWorker(t, f, "quote")
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			if _, _, err = conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	now := time.Now()
	console := ownerConsole(now)
	deps, _, _, stderr := fixtureDeps(f, now, console)
	tmux := deps.Tmux.(*testTmux)
	if initialize {
		if code := Execute(f.args("init"), deps); code != 0 {
			t.Fatalf("init=%d %s", code, stderr.String())
		}
		// The native CLI cannot connect before the Worker has started.
		for _, window := range tmux.windows {
			if window.name == "quote" {
				window.dead = true
			}
		}
		tmux.calls = nil
	}
	assertNotOpened := func() {
		for _, call := range tmux.calls {
			if strings.HasPrefix(call, "respawn-pane") {
				t.Fatalf("opened native pane before readiness: %s", call)
			}
		}
	}
	started, waited := false, false
	deps.RunSystemctl = func(_ context.Context, args ...string) (string, error) {
		call := strings.Join(args, " ")
		switch {
		case strings.Contains(call, "LoadState"):
			return "loaded", nil
		case strings.Contains(call, "ExecStart"):
			return fmt.Sprintf("{ path=%s ; argv[]=%s worker run --config %s ; ignore_errors=no ; }", f.binary, f.binary, filepath.Join(f.workerDir, "quote.yaml")), nil
		case strings.Contains(call, "WorkingDirectory"):
			return f.home, nil
		case strings.Contains(call, "EnvironmentFiles"):
			return filepath.Join(f.workerDir, "quote.env") + " (ignore_errors=yes)", nil
		case strings.Contains(call, " start "):
			assertNotOpened()
			started = true
			console.attached["quote"] = consoleapi.AttachResponse{WorkerInstanceID: "worker-quote", WorkerStatus: domain.WorkerStatusOnline, LeaseUntil: now.Add(time.Minute)}
		}
		return "", nil
	}
	deps.Wait = func(context.Context, time.Duration) error {
		assertNotOpened()
		if !started || waited {
			t.Fatal("wrong readiness/startup sequence")
		}
		waited = true
		if err := os.MkdirAll(filepath.Dir(statePath), 0700); err != nil {
			t.Fatal(err)
		}
		state, _ := json.Marshal(map[string]string{"endpoint": "ws" + strings.TrimPrefix(server.URL, "http")})
		return os.WriteFile(statePath, state, 0600)
	}
	if code := Execute(f.args("up"), deps); code != 0 {
		t.Fatalf("code=%d: %s", code, stderr.String())
	}
	if !started || !waited || !strings.Contains(strings.Join(tmux.calls, "\n"), "agent open quote --native") {
		t.Fatal("native terminal was not opened after readiness")
	}
	if initialize && strings.Contains(strings.Join(tmux.calls, "\n"), "respawn-pane -k") {
		t.Fatal("init-to-up recovery used a forceful pane replacement")
	}
}

func TestNativeReadinessFailureKeepsOtherAgentsAndLivePanes(t *testing.T) {
	f := newFleetFixture(t)
	writeManifest(t, f, "quote", "risk")
	setCodexWorker(t, f, "quote")
	now := time.Now()
	console := ownerConsole(now)
	deps, _, _, stderr := fixtureDeps(f, now, console)
	deps.Wait = func(context.Context, time.Duration) error { return context.DeadlineExceeded }
	options, _ := validateAgentOptions(console.options)
	manifest, prepared, err := prepare(f.manifest, f.workerDir, f.socket, options)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := newWorkspace(f.paths(), true, deps)
	if err != nil {
		t.Fatal(err)
	}
	configureNativeTerminals(&workspace, prepared, f.paths(), deps)
	ready, ok := readyNativeWorkspace(context.Background(), manifest, prepared, workspace, console, deps)
	if ok || len(ready.Agents) != 1 || ready.Agents[0].AgentID != "risk" || !strings.Contains(stderr.String(), "agent resume quote") {
		t.Fatalf("ready=%+v ok=%v stderr=%s", ready, ok, stderr.String())
	}
	if _, err := workspace.Reconcile(context.Background(), ready); err != nil {
		t.Fatal(err)
	}
	tmux := deps.Tmux.(*testTmux)
	for _, w := range tmux.windows {
		if w.name == "quote" {
			t.Fatal("unready native pane was created")
		}
	}
	// A live terminal is not a reason to wait for, or interrupt, its backend.
	tmux.nextID++
	tmux.windows[fmt.Sprintf("@%d", tmux.nextID)] = &fleetTestWindow{name: "quote", panes: []int{0}, managed: true, agentID: "quote"}
	ready, ok = readyNativeWorkspace(context.Background(), manifest, prepared, workspace, console, deps)
	if !ok || len(ready.Agents) != 2 {
		t.Fatalf("live pane unexpectedly waited: %+v %v", ready, ok)
	}
}
