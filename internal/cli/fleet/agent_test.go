package fleet

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	workercli "openagentx/internal/cli/worker"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	"openagentx/internal/domain"
	fleetmodel "openagentx/internal/fleet"
	workerconfig "openagentx/internal/worker"
)

type agentRoundTrip func(*http.Request) (*http.Response, error)

func (f agentRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixtureAgentAPI(t *testing.T, handler http.HandlerFunc) *agentAPI {
	t.Helper()
	return &agentAPI{token: "test-token", http: &http.Client{Transport: agentRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatal("missing formal auth")
		}
		rec := httptest.NewRecorder()
		handler(rec, r)
		return rec.Result(), nil
	})}}
}

func TestAgentAddIncrementalIdempotentAndConflict(t *testing.T) {
	f := newFleetFixture(t)
	now := time.Now()
	client := ownerConsole(now)
	deps, _, out, stderr := fixtureDeps(f, now, client)
	password := filepath.Join(f.home, "password")
	if err := os.WriteFile(password, []byte("only-fixture-password-123"), 0600); err != nil {
		t.Fatal(err)
	}
	add := func(id, name string) int {
		args := append(f.args("add"), "--id", id, "--name", name, "--workspace", f.home, "--role-text", "Review isolated fixture files.", "--password-file", password, "--configure-only")
		return ExecuteAgent(args, deps)
	}
	for _, id := range []string{"quote", "risk", "quote"} {
		if code := add(id, id); code != 0 {
			t.Fatalf("add %s: code=%d stderr=%s", id, code, stderr)
		}
	}
	manifest, err := fleetmodel.LoadFile(f.manifest)
	if err != nil || len(manifest.Agents) != 2 {
		t.Fatalf("manifest=%+v err=%v", manifest, err)
	}
	config, err := workerconfig.LoadProcessConfig(filepath.Join(f.workerDir, "quote.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if config.RuntimeBackendConfig[0].Options["timeout"] != "30m0s" {
		t.Fatal("run timeout not configured")
	}
	before, err := os.ReadFile(f.manifest)
	if err != nil {
		t.Fatal(err)
	}
	if code := add("quote", "changed display"); code == 0 {
		t.Fatal("identity conflict accepted")
	}
	after, _ := os.ReadFile(f.manifest)
	if string(before) != string(after) {
		t.Fatal("conflict changed fleet")
	}
	if strings.Contains(out.String()+stderr.String(), "only-fixture-password-123") {
		t.Fatal("password leaked")
	}
	t.Log("D fixture: formal init/apply, two incremental Agents, repeat unchanged, conflict preserved; systemd/credentials are mocks.")
}

func TestAgentNetworkOnlyPublishesSucceededAndWaitsForCurrentGeneration(t *testing.T) {
	for _, state := range []string{"succeeded", "failed"} {
		t.Run(state, func(t *testing.T) {
			a := consoleapi.AttachResponse{AgentID: "quote", WorkerInstanceID: "worker-new", Generation: 3}
			reads, posts := 0, []string{}
			api := fixtureAgentAPI(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					posts = append(posts, r.URL.Path)
					if r.Header.Get("Idempotency-Key") == "" {
						t.Fatal("missing idempotency key")
					}
					fmt.Fprint(w, `{"receipt":{"test_id":"test-one"}}`)
					return
				}
				reads++
				result := openapi.NetworkOverviewResponse{}
				if reads > 1 {
					result.ModeTests = []domain.NetworkModeTest{{ID: "test-one", State: state}}
				}
				if reads >= 3 {
					generation := int64(2)
					if reads >= 4 {
						generation = 3
					}
					result.Bindings = []domain.NetworkBinding{{AgentID: "quote", BackendID: "agy", DesiredStatus: "applied", Version: 1, AppliedBindingRevision: 1, AppliedWorkerID: "worker-new", AppliedGeneration: generation}}
				}
				_ = json.NewEncoder(w).Encode(result)
			})
			waits := 0
			deps := withDefaults(Dependencies{Wait: func(context.Context, time.Duration) error {
				waits++
				if waits > 5 {
					return fmt.Errorf("fixture wait exhausted")
				}
				return nil
			}})
			err := prepareNetwork(context.Background(), api, a, workerconfig.RuntimeBackendConfig{BackendID: "agy", AdapterID: "agy-batch"}, deps)
			if state == "failed" {
				if err == nil || len(posts) != 1 {
					t.Fatalf("failed test published: err=%v posts=%v", err, posts)
				}
			} else {
				if err != nil || len(posts) != 2 || reads != 4 {
					t.Fatalf("err=%v posts=%v reads=%d", err, posts, reads)
				}
			}
		})
	}
}

func TestAgentExistingNamedNetworkRequiresExplicitRecovery(t *testing.T) {
	a := consoleapi.AttachResponse{AgentID: "quote", WorkerInstanceID: "worker-2", Generation: 2}
	reads := 0
	api := fixtureAgentAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatal("existing named profile replaced")
		}
		reads++
		_ = json.NewEncoder(w).Encode(openapi.NetworkOverviewResponse{Bindings: []domain.NetworkBinding{{AgentID: "quote", BackendID: "agy", Mode: domain.NetworkNamedProfile, ProfileID: "existing", Version: 4, DesiredStatus: "applied", AppliedWorkerID: "worker-1", AppliedGeneration: 1, AppliedBindingRevision: 4}}})
	})
	err := prepareNetwork(context.Background(), api, a, workerconfig.RuntimeBackendConfig{BackendID: "agy"}, withDefaults(Dependencies{}))
	if err == nil || !strings.Contains(err.Error(), "现有绑定未修改") || reads != 1 {
		t.Fatalf("err=%v reads=%d", err, reads)
	}
}

func TestAgentExistingModeRecoveryUsesCurrentGenerationAndBindingCASHTTP(t *testing.T) {
	for _, mode := range []domain.NetworkMode{domain.NetworkInherit, domain.NetworkDirect} {
		for _, conflict := range []string{"", "test", "publish"} {
			t.Run(string(mode)+"/"+conflict, func(t *testing.T) {
				a := consoleapi.AttachResponse{AgentID: "quote", WorkerInstanceID: "worker-new", Generation: 3}
				posts := []string{}
				published := false
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Authorization") != "Bearer test-token" {
						t.Error("missing auth")
					}
					if r.Method == http.MethodGet {
						binding := domain.NetworkBinding{AgentID: "quote", BackendID: "agy", Mode: mode, Version: 7, DesiredStatus: "applied", AppliedWorkerID: "worker-old", AppliedGeneration: 2, AppliedMode: mode, AppliedBindingRevision: 7}
						if published {
							binding.Version = 8
							binding.AppliedBindingRevision = 8
							binding.AppliedWorkerID = a.WorkerInstanceID
							binding.AppliedGeneration = a.Generation
						}
						_ = json.NewEncoder(w).Encode(openapi.NetworkOverviewResponse{Bindings: []domain.NetworkBinding{binding}, ModeTests: []domain.NetworkModeTest{{ID: "test-current", State: "succeeded"}}})
						return
					}
					posts = append(posts, r.URL.Path)
					if r.Header.Get("Idempotency-Key") == "" {
						t.Error("missing idempotency key")
					}
					switch r.URL.Path {
					case openapi.ControlNetworkModeTestPath:
						var req openapi.TestNetworkModeRequest
						if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
							t.Error(err)
						}
						if req.Mode != mode || req.Meta.ExpectedVersion != 7 || req.WorkerInstanceID != a.WorkerInstanceID || req.Generation != a.Generation {
							t.Errorf("test changed mode or omitted CAS/current Worker: %+v", req)
						}
						if conflict == "test" {
							w.WriteHeader(http.StatusConflict)
							return
						}
					case openapi.ControlNetworkModePublishPath:
						var req openapi.PublishNetworkModeRequest
						if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
							t.Error(err)
						}
						if req.Meta.ExpectedVersion != 7 || req.WorkerInstanceID != a.WorkerInstanceID || req.Generation != a.Generation || req.TestID != "test-current" {
							t.Errorf("publish missing CAS/current Worker: %+v", req)
						}
						if conflict == "publish" {
							w.WriteHeader(http.StatusConflict)
							return
						}
						published = true
					default:
						t.Errorf("unexpected POST %s", r.URL.Path)
					}
					fmt.Fprint(w, `{"receipt":{"test_id":"test-current"}}`)
				}))
				defer server.Close()
				transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
				}}
				defer transport.CloseIdleConnections()
				api := &agentAPI{token: "test-token", http: &http.Client{Transport: transport}}
				deps := withDefaults(Dependencies{Wait: func(context.Context, time.Duration) error { return fmt.Errorf("unexpected wait") }})
				// The local default is intentionally different from an existing direct binding.
				err := prepareNetwork(context.Background(), api, a, workerconfig.RuntimeBackendConfig{BackendID: "agy"}, deps)
				if conflict == "" && (err != nil || !published || len(posts) != 2) {
					t.Fatalf("err=%v posts=%v", err, posts)
				}
				if conflict != "" && (err == nil || published) {
					t.Fatalf("conflict accepted: err=%v published=%v", err, published)
				}
				if conflict == "test" && len(posts) != 1 {
					t.Fatalf("publish after failed test: %v", posts)
				}
			})
		}
	}
}

func TestAgentPauseOnlySelectedAgentAndPersistsGracefulStop(t *testing.T) {
	f := newFleetFixture(t)
	now := time.Now()
	client := ownerConsole(now)
	client.sequences = map[string][]consoleapi.AttachResponse{"quote": {{WorkerInstanceID: "worker-q", Generation: 7, WorkerStatus: domain.WorkerStatusOnline, LeaseUntil: now.Add(time.Hour)}, {WorkerInstanceID: "worker-q", Generation: 7, WorkerStatus: domain.WorkerStatusDraining, LeaseUntil: now.Add(time.Hour)}, {WorkerStatus: domain.WorkerStatusOffline}}}
	deps, _, out, stderr := fixtureDeps(f, now, client)
	deps.Wait = func(context.Context, time.Duration) error { return nil }
	if code := ExecuteAgent(append(f.args("pause"), "quote"), deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if len(client.commands) != 1 || client.commands[0] != domain.WorkerCommandStop {
		t.Fatalf("commands=%v", client.commands)
	}
	if client.attachCalls["risk"] != 0 {
		t.Fatal("other Agent touched")
	}
	if !strings.Contains(out.String(), "已暂停") {
		t.Fatal("missing pause receipt")
	}
	t.Log("D fixture only: stop -> draining -> offline, no force-stop or systemctl kill")
}

func TestAgentURLUsesActualServiceOverride(t *testing.T) {
	deps := Dependencies{RunSystemctl: func(context.Context, ...string) (string, error) {
		return "{ argv[]=/bin/openagentx serve --http-addr 127.0.0.1:19444 ; }", nil
	}}
	link, err := agentLink(context.Background(), agentOptions{id: "quote"}, deps)
	if err != nil || link != "http://127.0.0.1:19444?agent=quote" {
		t.Fatalf("link=%s err=%v", link, err)
	}
	deps.RunSystemctl = func(context.Context, ...string) (string, error) { return "", nil }
	if _, err = agentLink(context.Background(), agentOptions{id: "quote"}, deps); err == nil {
		t.Fatal("empty service inferred a default URL")
	}
}

func TestAgentStatusIncludesReadinessLatestResultAndOpenAction(t *testing.T) {
	api := fixtureAgentAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == openapi.ObserveOverviewPath:
			fmt.Fprint(w, `{"agents":[{"agent_id":"quote","readiness":{"ready":true,"reason":"可开始工作"}}]}`)
		case strings.HasSuffix(r.URL.Path, "/tasks"):
			fmt.Fprint(w, `{"tasks":[{"task_id":"task-done","status":"completed"}]}`)
		default:
			fmt.Fprint(w, `{"task":{"task_id":"task-done","result":"fixture final reply"}}`)
		}
	})
	f := newFleetFixture(t)
	client := ownerConsole(time.Now())
	deps, _, out, _ := fixtureDeps(f, time.Now(), client)
	if err := printAgentStatus(context.Background(), api, client, agentOptions{id: "quote"}, deps); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"可开始工作", "fixture final reply", "openagentx agent open quote"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("status missing %s", want)
		}
	}
}

func TestAgentUnitInstallRefusesExistingConflict(t *testing.T) {
	f := newFleetFixture(t)
	deps, _, _, _ := fixtureDeps(f, time.Now(), ownerConsole(time.Now()))
	deps = withDefaults(deps)
	entry := preparedAgent{entry: fleetmodel.Agent{AgentID: "quote", Enabled: true}, workerPath: filepath.Join(f.workerDir, "quote.yaml")}
	if err := ensureAgentUnit(context.Background(), entry, deps); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.home, ".config", "systemd", "user", workerUnit("quote"))
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), entry.workerPath) || !strings.Contains(string(content), f.binary) {
		t.Fatal("unit paths not canonical")
	}
	if err = os.WriteFile(path, []byte("custom unit"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = ensureAgentUnit(context.Background(), entry, deps); err == nil {
		t.Fatal("conflicting unit accepted")
	}
}

func TestAgentParseWizardHasOnlyThreePrompts(t *testing.T) {
	f := newFleetFixture(t)
	now := time.Now()
	deps, _, _, stderr := fixtureDeps(f, now, ownerConsole(now))
	deps.IsInteractive = func() bool { return true }
	deps.In = strings.NewReader("quote\n" + f.home + "\nRead project documentation\n")
	password := filepath.Join(f.home, "password")
	_ = os.WriteFile(password, []byte("only-fixture-password-123"), 0600)
	if code := ExecuteAgent(append(f.args("add"), "--password-file", password, "--configure-only"), deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	for _, label := range []string{"Agent 名称:", "工作目录:", "职责文档路径或职责描述:"} {
		if !strings.Contains(stderr.String(), label) {
			t.Fatalf("missing %s", label)
		}
	}
}

func TestAgentResumeStartsOnlyTargetAndDoesNotReplayTasks(t *testing.T) {
	f := newFleetFixture(t)
	writeManifest(t, f, "quote", "risk")
	path := filepath.Join(f.workerDir, "quote.yaml")
	content, _ := os.ReadFile(path)
	content = []byte(strings.ReplaceAll(string(content), "adapter_id: fake", "adapter_id: agy-batch"))
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(f.workerDir, "risk.yaml")); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	client := ownerConsole(now)
	client.sequences = map[string][]consoleapi.AttachResponse{"quote": {{WorkerStatus: domain.WorkerStatusOffline}, {WorkerInstanceID: "worker-q-new", Generation: 2, WorkerStatus: domain.WorkerStatusOnline, LeaseUntil: now.Add(time.Hour)}}}
	deps, _, _, _ := fixtureDeps(f, now, client)
	calls := []string{}
	deps.RunSystemctl = func(_ context.Context, args ...string) (string, error) {
		call := strings.Join(args, " ")
		calls = append(calls, call)
		switch {
		case strings.Contains(call, "LoadState"):
			return "loaded", nil
		case strings.Contains(call, "ExecStart"):
			return fmt.Sprintf("{ argv[]=%s worker run --config %s ; }", f.binary, path), nil
		case strings.Contains(call, "WorkingDirectory"):
			return f.home, nil
		case strings.Contains(call, "EnvironmentFiles"):
			return filepath.Join(f.workerDir, "quote.env") + " (ignore_errors=yes)", nil
		}
		return "", nil
	}
	api := fixtureAgentAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatal("resume must not dispatch or replay tasks")
		}
		switch r.URL.Path {
		case openapi.ObserveNetworkProfilesPath:
			_ = json.NewEncoder(w).Encode(openapi.NetworkOverviewResponse{Bindings: []domain.NetworkBinding{{AgentID: "quote", BackendID: "local", Version: 1, DesiredStatus: "applied", AppliedWorkerID: "worker-q-new", AppliedGeneration: 2, AppliedBindingRevision: 1}}})
		case openapi.ObserveOverviewPath:
			fmt.Fprint(w, `{"agents":[{"agent_id":"quote","readiness":{"ready":true}}]}`)
		default:
			t.Fatalf("unexpected API %s", r.URL.Path)
		}
	})
	o := agentOptions{id: "quote", paths: fleetPaths{manifest: f.manifest, database: f.database, socket: f.socket, workerDir: f.workerDir, credentials: f.credentials}}
	if err := startAgent(context.Background(), client, api, o, withDefaults(deps)); err != nil {
		t.Fatal(err)
	}
	starts := 0
	for _, call := range calls {
		if strings.Contains(call, " start ") {
			starts++
			if call != "--user start openagentx-worker@quote.service" {
				t.Fatalf("wrong target: %s", call)
			}
		}
	}
	if starts != 1 {
		t.Fatalf("starts=%d", starts)
	}
	t.Log("D fixture: only selected unit starts despite other Agent config missing; existing binding must match new generation; no Task API write")
}

func TestAgentGeneratedConfigReachesRealWorkerRegistration(t *testing.T) {
	for _, model := range []string{"", "fixture-model"} {
		t.Run("model-"+model, func(t *testing.T) {
			f := newFleetFixture(t)
			dir, err := os.MkdirTemp("", "oax-parse-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			f.socket = filepath.Join(dir, "w.sock")
			binary := filepath.Join(dir, "fixture-agy")
			if err = os.WriteFile(binary, []byte("#!/bin/sh\necho fixture-runtime-1\n"), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("AGY_GRAFT_REAL_BIN", binary)
			t.Setenv("AGY_GRAFT_MGRAFTCP_BIN", binary)
			t.Setenv("AGY_GRAFT_NATIVE_PROXY", "1")
			now := time.Now()
			deps, _, _, stderr := fixtureDeps(f, now, ownerConsole(now))
			password := filepath.Join(f.home, "password")
			_ = os.WriteFile(password, []byte("only-fixture-password-123"), 0600)
			args := append(f.args("add"), "--name", "quote", "--workspace", f.home, "--role-text", "Fixture role", "--password-file", password, "--runtime-binary", binary, "--configure-only")
			if model != "" {
				args = append(args, "--model", model)
			}
			if code := ExecuteAgent(args, deps); code != 0 {
				t.Fatalf("add=%d: %s", code, stderr)
			}
			environment, err := os.ReadFile(filepath.Join(f.workerDir, "quote.env"))
			if err != nil || !strings.Contains(string(environment), "AGY_GRAFT_NATIVE_PROXY=\"1\"") {
				t.Fatalf("native proxy input not persisted: err=%v", err)
			}
			registration := make(chan openapi.RegisterRequest, 1)
			listener, err := net.Listen("unix", f.socket)
			if err != nil {
				t.Fatal(err)
			}
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request openapi.RegisterRequest
				if r.URL.Path != openapi.WorkerRegisterPath {
					t.Errorf("unexpected route %s", r.URL.Path)
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				registration <- request
				http.Error(w, "fixture ends after registration parsing", 503)
			})}
			go server.Serve(listener)
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err = workercli.RunWorkerProcess(ctx, filepath.Join(f.workerDir, "quote.yaml"))
			if err == nil || !strings.Contains(err.Error(), "register Worker") {
				t.Fatalf("did not reach registration: %v", err)
			}
			select {
			case request := <-registration:
				if len(request.Backends) != 1 {
					t.Fatalf("backends=%+v", request.Backends)
				}
				want := model
				if want == "" {
					want = "default"
				}
				descriptor := request.Backends[0].Descriptor
				if len(descriptor.Models) != 1 || descriptor.Models[0] != want || descriptor.DefaultTimeout != 30*time.Minute {
					t.Fatalf("descriptor=%+v", descriptor)
				}
			case <-ctx.Done():
				t.Fatal("Worker did not parse/register generated config")
			}
		})
	}
}

func TestAgentURLResolvesActualMainPIDEnvironment(t *testing.T) {
	command := exec.Command("/bin/sh", "-c", "printf 'ready\\n'; read ignored")
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Env = []string{"OPENAGENTX_HTTP_ADDR=127.0.0.1:19445", "UNRELATED_SECRET=must-not-appear"}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	if _, err = bufio.NewReader(output).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	deps := Dependencies{RunSystemctl: func(_ context.Context, args ...string) (string, error) {
		call := strings.Join(args, " ")
		if strings.Contains(call, "MainPID") {
			return fmt.Sprint(command.Process.Pid), nil
		}
		return "{ argv[]=/bin/openagentx serve --http-addr ${OPENAGENTX_HTTP_ADDR} ; }", nil
	}}
	link, err := agentLink(context.Background(), agentOptions{id: "quote"}, deps)
	if err != nil || link != "http://127.0.0.1:19445?agent=quote" {
		t.Fatalf("link=%s err=%v", link, err)
	}
	if strings.Contains(link, "must-not-appear") {
		t.Fatal("unrelated environment exposed")
	}
}

func TestAgentNavigationBoundsOutputAndRetainsJSONOption(t *testing.T) {
	longResult := strings.Repeat("中文结果 ", 200)
	api := fixtureAgentAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == openapi.ObserveOverviewPath:
			fmt.Fprint(w, `{"agents":[{"agent_id":"quote","display_name":"资料助手","readiness":{"ready":true,"reason":"可开始工作"}}]}`)
		case strings.HasSuffix(r.URL.Path, "/tasks"):
			fmt.Fprint(w, `{"tasks":[{"task_id":"latest","status":"succeeded"}]}`)
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"task": map[string]any{"task_id": "latest", "result": longResult}})
		}
	})
	f := newFleetFixture(t)
	client := ownerConsole(time.Now())
	deps, _, out, _ := fixtureDeps(f, time.Now(), client)
	if err := printAgentStatus(context.Background(), api, client, agentOptions{id: "quote"}, deps); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), "\n") > 7 || strings.Contains(out.String(), "latest_result") || !strings.Contains(out.String(), "…") || !strings.Contains(out.String(), "资料助手") {
		t.Fatalf("not concise: %s", out.String())
	}
	out.Reset()
	if err := printAgentStatus(context.Background(), api, client, agentOptions{id: "quote", jsonOutput: true}, deps); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "latest_result") || !strings.Contains(out.String(), longResult) {
		t.Fatal("diagnostic JSON lost full result")
	}
	var refreshed bytes.Buffer
	if err := writeAgentStatusFrame(&refreshed, []byte("导航\n"), true); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(refreshed.String(), "\x1b[H\x1b[2J") {
		t.Fatal("TTY watch must replace current screen")
	}
	if got := statusSummary("line one\nline two\x1b[31m", 80); strings.ContainsAny(got, "\n\x1b") {
		t.Fatal("terminal control sequence retained")
	}
}
