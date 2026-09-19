package console

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	consoleclient "openagentx/internal/client/console"
	"openagentx/internal/credentialstore"
	"openagentx/internal/domain"
)

const workflowTestPassword = "isolated owner password"

type workflowDialogStep struct {
	prompt string
	input  string
}

func TestIsolatedDefaultProfileWorkflowShowsTaskProgressAndKeepsWorkerResident(t *testing.T) {
	for _, binary := range []string{"go", "script", "tmux", "sh", "sleep"} {
		if _, err := exec.LookPath(binary); err != nil {
			t.Skipf("%s is required for the isolated workflow", binary)
		}
	}
	root, err := os.MkdirTemp("", "oax9-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	home := filepath.Join(root, "home")
	stateDir := filepath.Join(root, "state")
	toolsDir := filepath.Join(root, "tools")
	for _, directory := range []string{home, stateDir, toolsDir, filepath.Join(home, ".local", "bin")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	repositoryRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	openagentx := filepath.Join(home, ".local", "bin", "openagentx")
	build := exec.Command("go", "build", "-o", openagentx, "./cmd/openagentx")
	build.Dir = repositoryRoot
	if output, buildErr := build.CombinedOutput(); buildErr != nil {
		t.Fatalf("build isolated openagentx: %v\n%s", buildErr, output)
	}
	if err := os.Chmod(openagentx, 0o700); err != nil {
		t.Fatal(err)
	}

	tmuxBinary, err := exec.LookPath("tmux")
	if err != nil {
		t.Fatal(err)
	}
	tmuxSocket := fmt.Sprintf("oax9-%d-%d", os.Getpid(), time.Now().UnixNano())
	writeWorkflowExecutable(t, filepath.Join(toolsDir, "tmux"), fmt.Sprintf("#!/bin/sh\nexec %s -L \"$OAX_TEST_TMUX_SOCKET\" \"$@\"\n", shellQuote(tmuxBinary)))
	writeWorkflowExecutable(t, filepath.Join(toolsDir, "loginctl"), `#!/bin/sh
if [ "$1" = "show-user" ] && [ "$3" = "--property=Linger" ] && [ "$4" = "--value" ]; then
  echo yes
  exit 0
fi
echo "unexpected loginctl arguments" >&2
exit 1
`)
	writeWorkflowExecutable(t, filepath.Join(toolsDir, "systemctl"), `#!/bin/sh
set -eu
state=${OAX_TEST_STATE:?}
home=${HOME:?}
unit=openagentx-worker@quote-service.service
printf '%s\n' "$*" >> "$state/systemctl.calls"
[ "$1" = "--user" ] || { echo "user manager required" >&2; exit 1; }
case "$*" in
  "--user show $unit --property=LoadState --value")
    echo loaded
    ;;
  "--user show $unit --property=ExecStart --value")
    echo "{ path=$home/.local/bin/openagentx ; argv[]=$home/.local/bin/openagentx worker run --config $home/.openagentx/workers/quote-service.yaml ; ignore_errors=no ; }"
    ;;
  "--user show $unit --property=WorkingDirectory --value")
    echo "$home"
    ;;
  "--user show $unit --property=EnvironmentFiles --value")
    echo "$home/.openagentx/workers/quote-service.env (ignore_errors=yes)"
    ;;
  "--user start $unit")
    if [ -f "$state/worker.pid" ] && kill -0 "$(sed -n '1p' "$state/worker.pid")" 2>/dev/null; then
      exit 0
    fi
    nohup "$home/.local/bin/openagentx" worker run --config "$home/.openagentx/workers/quote-service.yaml" </dev/null >"$state/worker.log" 2>&1 &
    echo $! > "$state/worker.pid"
    ;;
  "--user is-active $unit")
    if [ -f "$state/worker.pid" ] && kill -0 "$(sed -n '1p' "$state/worker.pid")" 2>/dev/null; then
      echo active
      exit 0
    fi
    echo inactive
    exit 3
    ;;
  *)
    echo "unexpected systemctl arguments" >&2
    exit 1
    ;;
esac
`)

	path := toolsDir + string(os.PathListSeparator) + filepath.Join(home, ".local", "bin") +
		string(os.PathListSeparator) + os.Getenv("PATH")
	environment := isolatedWorkflowEnvironment(home, path, stateDir, tmuxSocket)
	identityPath, workerSource := writeWorkflowConfiguration(t, root, home)

	runWorkflowPTY(t, environment, "openagentx init", []workflowDialogStep{
		{prompt: "Owner password:", input: workflowTestPassword},
		{prompt: "Confirm owner password:", input: workflowTestPassword},
	})
	runWorkflowPTY(t, environment, "openagentx agent apply --file "+shellQuote(identityPath), []workflowDialogStep{
		{prompt: "Owner password:", input: workflowTestPassword},
	})

	webDir := filepath.Join(root, "web")
	if err := os.Mkdir(webDir, 0o700); err != nil {
		t.Fatal(err)
	}
	httpAddress := reserveWorkflowHTTPAddress(t)
	httpBaseURL := "http://" + httpAddress
	daemonLog := &synchronizedTerminal{}
	daemon := exec.Command(openagentx, "serve", "--http-addr", httpAddress, "--web-dir", webDir)
	daemon.Env = environment
	daemon.Stdout, daemon.Stderr = daemonLog, daemonLog
	if err := daemon.Start(); err != nil {
		t.Fatal(err)
	}
	daemonDone := make(chan error, 1)
	go func() { daemonDone <- daemon.Wait() }()
	t.Cleanup(func() { stopWorkflowProcess(t, daemon.Process, daemonDone, "daemon") })
	socketPath := filepath.Join(home, ".openagentx", "run", "openagentx.sock")
	waitForWorkflowPath(t, socketPath, daemonDone, daemonLog)
	waitForWorkflowHTTP(t, httpBaseURL, daemonDone, daemonLog)

	loginTranscript := runWorkflowPTY(t, environment, "openagentx console login", []workflowDialogStep{
		{prompt: "Console username:", input: "owner"},
		{prompt: "Console password:", input: workflowTestPassword},
	})
	if strings.Contains(loginTranscript, workflowTestPassword) {
		t.Fatal("PTY login transcript exposed the password")
	}

	initCommand := "openagentx fleet init --agent quote-service --worker-config quote-service=" + shellQuote(workerSource)
	runWorkflowCommand(t, environment, initCommand)
	runWorkflowCommand(t, environment, initCommand)
	runWorkflowCommand(t, environment, "openagentx fleet workspace")
	runWorkflowCommand(t, environment, "openagentx fleet workspace")
	runWorkflowCommand(t, environment, "openagentx fleet up")
	runWorkflowCommand(t, environment, "openagentx fleet up")
	statusOutput := runWorkflowCommand(t, environment, "openagentx fleet status")
	if !strings.Contains(statusOutput, `"quote-service": "active"`) {
		t.Fatalf("Fleet status did not report the real isolated Worker active: %s", statusOutput)
	}

	workerPID := readWorkflowPID(t, filepath.Join(stateDir, "worker.pid"))
	workerProcess, err := os.FindProcess(workerPID)
	if err != nil || workerProcess.Signal(syscall.Signal(0)) != nil {
		t.Fatalf("isolated Worker process %d is not alive: %v", workerPID, err)
	}
	t.Cleanup(func() { stopWorkflowPID(workerPID) })

	client := authenticatedWorkflowClient(t, socketPath, filepath.Join(home, ".openagentx", "credentials.json"))
	initialAttach := waitForWorkflowWorker(t, client, "quote-service")
	if initialAttach.WorkerInstanceID == "" || initialAttach.Generation <= 0 {
		t.Fatalf("invalid initial Worker identity: %+v", initialAttach)
	}
	bootstrapWorkflowNetworkBinding(t, httpBaseURL, initialAttach, filepath.Join(stateDir, "worker.log"))
	observedOutput, stopObserver := startWorkflowOutputObserver(t, client, "quote-service")
	t.Cleanup(stopObserver)

	tmux := func(args ...string) string {
		t.Helper()
		command := exec.Command(tmuxBinary, append([]string{"-L", tmuxSocket}, args...)...)
		output, commandErr := command.CombinedOutput()
		if commandErr != nil {
			t.Fatalf("tmux %s: %v: %s", strings.Join(args, " "), commandErr, output)
		}
		return string(output)
	}
	t.Cleanup(func() {
		command := exec.Command(tmuxBinary, "-L", tmuxSocket, "kill-server")
		_ = command.Run()
	})
	tmux("select-window", "-t", "=OAX:=quote-service")
	for range 2 {
		tmux("split-window", "-d", "-t", "=OAX:=quote-service", "sleep", consoleTTYFixtureSentinelSeconds)
	}
	tmux("select-pane", "-t", "=OAX:=quote-service.0")

	terminal, stdin, attach, stopAttach := startWorkflowConsole(t, environment, tmuxBinary, tmuxSocket)
	t.Cleanup(stopAttach)
	if !waitForRenderedTerminalText(terminal, "connection connected", 15*time.Second) ||
		!waitForRenderedTerminalText(terminal, "> /help", 15*time.Second) {
		stopAttach()
		t.Fatalf("Console Attach did not become ready: %s", ansi.Strip(terminal.String()))
	}

	writeWorkflowConsoleCommand(t, stdin, terminal, stopAttach, "/dispatch first isolated workflow task")
	if !waitForRenderedTerminalText(terminal, "dispatch succeeded", 15*time.Second) {
		stopAttach()
		t.Fatalf("dispatch outcome not visible: %s", ansi.Strip(terminal.String()))
	}
	firstTask := waitForWorkflowTaskCount(t, client, "quote-service", 1)[0]
	firstWaiting := waitForWorkflowTaskStatus(t, client, "quote-service", firstTask.TaskID, domain.TaskStatusWaitingInput,
		filepath.Join(stateDir, "worker.log"))
	if firstWaiting.LatestRun == nil || firstWaiting.LatestRun.WorkerInstanceID != initialAttach.WorkerInstanceID ||
		firstWaiting.LatestRun.WorkerGeneration == nil || *firstWaiting.LatestRun.WorkerGeneration != initialAttach.Generation {
		t.Fatalf("waiting-input Run lost Worker identity: %+v", firstWaiting.LatestRun)
	}
	if !waitForRenderedTerminalText(terminal, "waiting_input", 15*time.Second) {
		stopAttach()
		t.Fatalf("waiting-input progress not visible: %s", ansi.Strip(terminal.String()))
	}
	waitForWorkflowSafeOutput(t, observedOutput, "phase-one-safe-output")

	terminalOffset := len(terminal.String())
	writeWorkflowConsoleCommand(t, stdin, terminal, stopAttach, "/steer continue with the final reply")
	if !waitForRenderedTerminalText(terminal, "steer succeeded", 15*time.Second) {
		stopAttach()
		t.Fatalf("focused steer outcome not visible: %s", ansi.Strip(terminal.String()))
	}
	firstDone := waitForWorkflowTaskStatus(t, client, "quote-service", firstTask.TaskID, domain.TaskStatusSucceeded,
		filepath.Join(stateDir, "worker.log"))
	assertWorkflowTerminalSnapshot(t, firstDone, initialAttach, "final fixture reply")
	waitForWorkflowSafeOutput(t, observedOutput, "phase-two-safe-output")
	for _, expected := range []string{"status succeeded", "Task outcome result: final fixture reply", "Runtime reply: final fixture reply"} {
		if !revealWorkflowTimelineTextAfter(stdin, terminal, terminalOffset, expected) {
			stopAttach()
			t.Fatalf("Console did not show %q: %s", expected, ansi.Strip(terminal.String()))
		}
	}

	writeWorkflowConsoleCommand(t, stdin, terminal, stopAttach, "/diagnostic")
	if !waitForRenderedTerminalText(terminal, "mode diagnostic", 15*time.Second) {
		stopAttach()
		t.Fatalf("Diagnostic mode did not open: %s", ansi.Strip(terminal.String()))
	}
	overlayOffset := len(terminal.String())
	if _, err := stdin.Write([]byte{0x1b}); err != nil {
		stopAttach()
		t.Fatal(err)
	}
	if !waitForRenderedTerminalTextAfter(terminal, overlayOffset, "> /help", 15*time.Second) {
		stopAttach()
		t.Fatalf("Console input did not return after Diagnostic overlay: %s", ansi.Strip(terminal.String()))
	}
	normalOffset := len(terminal.String())
	writeWorkflowConsoleCommand(t, stdin, terminal, stopAttach, "/normal")
	if !waitForRenderedTerminalTextAfter(terminal, normalOffset, "mode normal | connection connected", 15*time.Second) {
		stopAttach()
		t.Fatalf("Normal mode did not resume: %s", ansi.Strip(terminal.String()))
	}

	writeWorkflowConsoleCommand(t, stdin, terminal, stopAttach, "/quit")
	waitForWorkflowPaneExit(t, tmux, terminal, stopAttach)
	stopAttach()
	_ = attach

	second, err := client.Dispatch(context.Background(), openapi.CreateTaskRequest{
		Meta: openapi.CommandMeta{IdempotencyKey: "workflow-second-task"}, TargetAgentID: "quote-service",
		OrganizationID: "default", DispatchMode: domain.DispatchModeDirect, Content: "second resident Worker task",
	})
	if err != nil {
		t.Fatal(err)
	}
	secondDone := waitForWorkflowTaskStatus(t, client, "quote-service", second.TaskID, domain.TaskStatusSucceeded,
		filepath.Join(stateDir, "worker.log"))
	assertWorkflowTerminalSnapshot(t, secondDone, initialAttach, "final fixture reply")
	waitForWorkflowSafeOutput(t, observedOutput, "second-task-safe-output")
	if workerProcess.Signal(syscall.Signal(0)) != nil || readWorkflowPID(t, filepath.Join(stateDir, "worker.pid")) != workerPID {
		t.Fatal("resident Worker process changed or exited after Console quit")
	}
	finalAttach := waitForWorkflowWorker(t, client, "quote-service")
	if finalAttach.WorkerInstanceID != initialAttach.WorkerInstanceID || finalAttach.Generation != initialAttach.Generation {
		t.Fatalf("resident Worker identity changed: before=%+v after=%+v", initialAttach, finalAttach)
	}

	assertWorkflowFilesAndCalls(t, home, stateDir)
}

func reserveWorkflowHTTPAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func bootstrapWorkflowNetworkBinding(t *testing.T, baseURL string, worker consoleapi.AttachResponse, workerLogPath string) {
	t.Helper()
	cookie, csrf := loginWorkflowWebSession(t, baseURL)
	testKey := "workflow-network-test"
	var testResponse openapi.NetworkCommandResponse
	workflowWebPOST(t, baseURL+openapi.ControlNetworkModeTestPath, cookie, csrf, testKey,
		openapi.TestNetworkModeRequest{
			Meta:    openapi.CommandMeta{IdempotencyKey: testKey, ExpectedVersion: 0},
			AgentID: "quote-service", BackendID: "local", Mode: domain.NetworkInherit,
			WorkerInstanceID: worker.WorkerInstanceID, Generation: worker.Generation,
		}, &testResponse)
	var testReceipt struct {
		TestID string `json:"test_id"`
	}
	if err := json.Unmarshal(testResponse.Receipt, &testReceipt); err != nil || testReceipt.TestID == "" {
		t.Fatalf("invalid network mode test receipt: err=%v", err)
	}
	waitForWorkflowModeTest(t, baseURL, cookie, testReceipt.TestID, worker, workerLogPath)

	publishKey := "workflow-network-publish"
	workflowWebPOST(t, baseURL+openapi.ControlNetworkModePublishPath, cookie, csrf, publishKey,
		openapi.PublishNetworkModeRequest{
			Meta:   openapi.CommandMeta{IdempotencyKey: publishKey, ExpectedVersion: 0},
			TestID: testReceipt.TestID, WorkerInstanceID: worker.WorkerInstanceID, Generation: worker.Generation,
		}, &openapi.NetworkCommandResponse{})
	waitForWorkflowBinding(t, baseURL, cookie, worker, workerLogPath)
}

func loginWorkflowWebSession(t *testing.T, baseURL string) (*http.Cookie, string) {
	t.Helper()
	body, err := json.Marshal(openapi.LoginRequest{Username: "owner", Password: workflowTestPassword})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, baseURL+openapi.AuthLoginPath, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Web login status=%d", response.StatusCode)
	}
	var session openapi.WebSessionResponse
	if err := json.NewDecoder(response.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	for _, cookie := range response.Cookies() {
		if cookie.Name == "openagentx_session" {
			return cookie, session.CSRFToken
		}
	}
	t.Fatal("Web login did not return a session cookie")
	return nil, ""
}

func workflowWebPOST(t *testing.T, endpoint string, cookie *http.Cookie, csrf, idempotencyKey string, requestBody, responseBody any) {
	t.Helper()
	body, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrf)
	request.Header.Set("Idempotency-Key", idempotencyKey)
	request.AddCookie(cookie)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Web control request %s status=%d", request.URL.Path, response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(responseBody); err != nil {
		t.Fatal(err)
	}
}

func workflowNetworkOverview(t *testing.T, baseURL string, cookie *http.Cookie) openapi.NetworkOverviewResponse {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, baseURL+openapi.ObserveNetworkProfilesPath+"?agent_id="+url.QueryEscape("quote-service"), nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(cookie)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("network overview status=%d", response.StatusCode)
	}
	var overview openapi.NetworkOverviewResponse
	if err := json.NewDecoder(response.Body).Decode(&overview); err != nil {
		t.Fatal(err)
	}
	return overview
}

func waitForWorkflowModeTest(t *testing.T, baseURL string, cookie *http.Cookie, testID string, worker consoleapi.AttachResponse, workerLogPath string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var last *domain.NetworkModeTest
	for time.Now().Before(deadline) {
		overview := workflowNetworkOverview(t, baseURL, cookie)
		for index := range overview.ModeTests {
			candidate := &overview.ModeTests[index]
			if candidate.ID != testID {
				continue
			}
			last = candidate
			if candidate.State == "failed" {
				t.Fatalf("network mode test failed: diagnostic=%s", candidate.DiagnosticCode)
			}
			if candidate.State == "succeeded" && candidate.WorkerInstanceID == worker.WorkerInstanceID && candidate.Generation == worker.Generation {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("network mode test was not applied by the isolated Worker: last=%+v worker_log=%s", last, readWorkflowLog(workerLogPath))
}

func waitForWorkflowBinding(t *testing.T, baseURL string, cookie *http.Cookie, worker consoleapi.AttachResponse, workerLogPath string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var last *domain.NetworkBinding
	for time.Now().Before(deadline) {
		overview := workflowNetworkOverview(t, baseURL, cookie)
		for index := range overview.Bindings {
			candidate := &overview.Bindings[index]
			if candidate.AgentID != "quote-service" || candidate.BackendID != "local" {
				continue
			}
			last = candidate
			if candidate.DesiredStatus == "failed" {
				t.Fatalf("network binding failed: diagnostic=%s", candidate.Diagnostic)
			}
			if candidate.DesiredStatus == "applied" && candidate.Mode == domain.NetworkInherit &&
				candidate.AppliedWorkerID == worker.WorkerInstanceID && candidate.AppliedGeneration == worker.Generation &&
				candidate.AppliedBindingRevision == candidate.Version {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("network binding was not applied by the isolated Worker: last=%+v worker_log=%s", last, readWorkflowLog(workerLogPath))
}

func readWorkflowLog(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return "unavailable"
	}
	return strings.TrimSpace(string(content))
}

func writeWorkflowConfiguration(t *testing.T, root, home string) (string, string) {
	t.Helper()
	agentDir := filepath.Join(root, "agent")
	workspace := filepath.Join(root, "workspace")
	for _, directory := range []string{agentDir, workspace} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(agentDir, "ROLE.md"), []byte("# Isolated workflow Agent\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	identityPath := filepath.Join(agentDir, "identity.yaml")
	identity := fmt.Sprintf(`version: 1
agent_id: quote-service
principal_id: agent-quote-service
organization_id: default
display_name: Quote Service
profile:
  instructions_path: ROLE.md
  workspace_root: %s
  capabilities: [testing]
`, workspace)
	if err := os.WriteFile(identityPath, []byte(identity), 0o600); err != nil {
		t.Fatal(err)
	}
	workerSource := filepath.Join(root, "quote-service.source.yaml")
	worker := fmt.Sprintf(`version: 1
agent_id: quote-service
transport: unix
unix_socket: %s
capabilities: [testing]
heartbeat_interval: 100ms
mailbox_wait: 1s
control_wait: 1s
shutdown_timeout: 2s
enable_worker_control: true
runtime_backends:
  - backend_id: local
    adapter_id: fake
    options:
      model: fake-workflow-model
      result: final fixture reply
      provider_session_id: fake-workflow-session
      result_status_sequence: [waiting_input, succeeded, succeeded]
      output_sequence: [phase-one-safe-output, phase-two-safe-output, second-task-safe-output]
`, filepath.Join(home, ".openagentx", "run", "openagentx.sock"))
	if err := os.WriteFile(workerSource, []byte(worker), 0o600); err != nil {
		t.Fatal(err)
	}
	return identityPath, workerSource
}

func isolatedWorkflowEnvironment(home, path, stateDir, tmuxSocket string) []string {
	result := make([]string, 0, len(os.Environ())+5)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if name == "HOME" || name == "PATH" || strings.HasPrefix(name, "OPENAGENTX_") ||
			name == "OAX_TEST_STATE" || name == "OAX_TEST_TMUX_SOCKET" || name == "TMUX" || name == "TMUX_PANE" {
			continue
		}
		result = append(result, entry)
	}
	return append(result, "HOME="+home, "PATH="+path, "TERM=xterm-256color",
		"OAX_TEST_STATE="+stateDir, "OAX_TEST_TMUX_SOCKET="+tmuxSocket)
}

func runWorkflowPTY(t *testing.T, environment []string, command string, steps []workflowDialogStep) string {
	t.Helper()
	var terminal synchronizedTerminal
	process := exec.Command("script", "-qefc", command, "/dev/null")
	process.Env = environment
	stdin, err := process.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	process.Stdout, process.Stderr = &terminal, &terminal
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		if !waitForRenderedTerminalText(&terminal, step.prompt, 15*time.Second) {
			_ = process.Process.Kill()
			_ = process.Wait()
			t.Fatalf("PTY command %q did not prompt for %q: %s", command, step.prompt, ansi.Strip(terminal.String()))
		}
		if _, err := fmt.Fprintln(stdin, step.input); err != nil {
			_ = process.Process.Kill()
			_ = process.Wait()
			t.Fatal(err)
		}
	}
	if err := process.Wait(); err != nil {
		t.Fatalf("PTY command %q failed: %v: %s", command, err, ansi.Strip(terminal.String()))
	}
	return ansi.Strip(terminal.String())
}

func runWorkflowCommand(t *testing.T, environment []string, command string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	process := exec.CommandContext(ctx, "sh", "-c", command)
	process.Env = environment
	output, err := process.CombinedOutput()
	if err != nil {
		t.Fatalf("command %q failed: %v: %s", command, err, output)
	}
	return string(output)
}

func startWorkflowConsole(t *testing.T, environment []string, tmuxBinary, tmuxSocket string) (*synchronizedTerminal, io.WriteCloser, *exec.Cmd, func()) {
	t.Helper()
	terminal := &synchronizedTerminal{}
	command := exec.Command("script", "-qefc", shellQuote(tmuxBinary)+" -L "+shellQuote(tmuxSocket)+" attach-session -t OAX", "/dev/null")
	command.Env = environment
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stdout, command.Stderr = terminal, terminal
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			_ = stdin.Close()
			if command.Process != nil {
				_ = command.Process.Kill()
			}
			_ = command.Wait()
		})
	}
	return terminal, stdin, command, stop
}

func writeWorkflowConsoleCommand(t *testing.T, stdin io.Writer, terminal *synchronizedTerminal, stop func(), command string) {
	t.Helper()
	for _, value := range []byte(command) {
		if _, err := stdin.Write([]byte{value}); err != nil {
			stop()
			t.Fatalf("write Console command %q: %v; output=%s", command, err, ansi.Strip(terminal.String()))
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := stdin.Write([]byte{'\r'}); err != nil {
		stop()
		t.Fatalf("submit Console command %q: %v; output=%s", command, err, ansi.Strip(terminal.String()))
	}
}

func revealWorkflowTimelineTextAfter(stdin io.Writer, terminal *synchronizedTerminal, offset int, expected string) bool {
	if waitForRenderedTerminalTextAfter(terminal, offset, expected, 500*time.Millisecond) {
		return true
	}
	// End establishes a deterministic starting point. PageUp then walks the
	// bounded history exactly as a user in a compact pane would inspect it.
	if _, err := stdin.Write([]byte("\x1b[F")); err != nil {
		return false
	}
	for range 32 {
		if waitForRenderedTerminalTextAfter(terminal, offset, expected, 100*time.Millisecond) {
			return true
		}
		if _, err := stdin.Write([]byte("\x1b[5~")); err != nil {
			return false
		}
	}
	return waitForRenderedTerminalTextAfter(terminal, offset, expected, 100*time.Millisecond)
}

func startWorkflowOutputObserver(t *testing.T, client *consoleclient.Client, agentID string) (<-chan openapi.JournalEventReadModel, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	outputs := make(chan openapi.JournalEventReadModel, 8)
	ready := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- client.Follow(ctx, agentID, consoleapi.ModeNormal,
			func(consoleapi.AttachResponse) error {
				select {
				case ready <- struct{}{}:
				default:
				}
				return nil
			},
			func(event openapi.JournalEventReadModel) error {
				if event.Output != nil {
					select {
					case outputs <- event:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				return nil
			},
			func(consoleclient.FollowState) error { return nil })
	}()
	select {
	case <-ready:
	case err := <-done:
		cancel()
		t.Fatalf("official Console observer stopped before Attach: %v", err)
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("official Console observer did not Attach")
	}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Errorf("official Console observer did not stop")
			}
		})
	}
	return outputs, stop
}

func waitForWorkflowSafeOutput(t *testing.T, events <-chan openapi.JournalEventReadModel, expected string) {
	t.Helper()
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case event := <-events:
			if event.Output == nil || event.Output.Text != expected {
				continue
			}
			if event.AggregateType != "runtime" || event.Output.Diagnostic != "" || event.Output.DiagnosticTruncated {
				t.Fatalf("unsafe Normal Console output projection: %+v", event)
			}
			return
		case <-deadline.C:
			t.Fatalf("official Console Follow did not deliver safe output %q", expected)
		}
	}
}

func authenticatedWorkflowClient(t *testing.T, socketPath, credentialsPath string) *consoleclient.Client {
	t.Helper()
	store, err := credentialstore.New(credentialsPath, credentialstore.Options{})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := store.LoadCurrentForSocket(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	client, err := consoleclient.NewUnixClient(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.UseCredential(context.Background(), credential.InstallationID, credential.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Session(context.Background()); err != nil {
		t.Fatal(err)
	}
	return client
}

func waitForWorkflowWorker(t *testing.T, client *consoleclient.Client, agentID string) consoleapi.AttachResponse {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var last consoleapi.AttachResponse
	var lastErr error
	for time.Now().Before(deadline) {
		last, lastErr = client.Attach(context.Background(), agentID, consoleapi.ModeNormal)
		if lastErr == nil && last.WorkerStatus == domain.WorkerStatusOnline && last.WorkerInstanceID != "" {
			return last
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("Worker did not become online: snapshot=%+v err=%v", last, lastErr)
	return consoleapi.AttachResponse{}
}

func waitForWorkflowTaskCount(t *testing.T, client *consoleclient.Client, agentID string, count int) []openapi.ConsoleTaskOption {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var tasks []openapi.ConsoleTaskOption
	var err error
	for time.Now().Before(deadline) {
		tasks, err = client.ListTaskOptions(context.Background(), agentID)
		if err == nil && len(tasks) >= count {
			return tasks
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("Task list count=%d want>=%d err=%v", len(tasks), count, err)
	return nil
}

func waitForWorkflowTaskStatus(t *testing.T, client *consoleclient.Client, agentID, taskID string, status domain.TaskStatus, workerLogPath string) openapi.ConsoleTaskSnapshot {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var snapshot openapi.ConsoleTaskSnapshot
	var err error
	for time.Now().Before(deadline) {
		snapshot, err = client.TaskSnapshot(context.Background(), agentID, taskID)
		if err == nil && snapshot.Task.Status == status {
			return snapshot
		}
		time.Sleep(50 * time.Millisecond)
	}
	workerLog, readErr := os.ReadFile(workerLogPath)
	if readErr != nil {
		workerLog = []byte("unavailable: " + readErr.Error())
	}
	t.Fatalf("Task %s status=%s want=%s snapshot=%+v err=%v worker_log=%s",
		taskID, snapshot.Task.Status, status, snapshot, err, strings.TrimSpace(string(workerLog)))
	return openapi.ConsoleTaskSnapshot{}
}

func assertWorkflowTerminalSnapshot(t *testing.T, snapshot openapi.ConsoleTaskSnapshot, worker consoleapi.AttachResponse, reply string) {
	t.Helper()
	if snapshot.Task.Status != domain.TaskStatusSucceeded || snapshot.Task.Result == nil || *snapshot.Task.Result != reply ||
		snapshot.SnapshotSequence <= 0 || snapshot.LatestRun == nil || snapshot.LatestRun.Status != domain.RunAttemptSucceeded ||
		snapshot.LatestRun.WorkerInstanceID != worker.WorkerInstanceID || snapshot.LatestRun.WorkerGeneration == nil ||
		*snapshot.LatestRun.WorkerGeneration != worker.Generation || snapshot.LatestRun.TurnResult == nil ||
		snapshot.LatestRun.TurnResult.RuntimeStatus != "succeeded" || snapshot.LatestRun.TurnResult.Body != reply {
		t.Fatalf("terminal Task/Run evidence is incomplete: %+v", snapshot)
	}
}

func waitForWorkflowPaneExit(t *testing.T, tmux func(...string) string, terminal *synchronizedTerminal, stop func()) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		panes := strings.TrimSpace(tmux("list-panes", "-t", "=OAX:=quote-service", "-F", "#{pane_index}:#{pane_dead}:#{pane_dead_status}"))
		lines := strings.Split(panes, "\n")
		if containsWorkflowPane(lines, "0:1:0") && containsWorkflowPane(lines, "1:0:") && containsWorkflowPane(lines, "2:0:") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	stop()
	t.Fatalf("Console pane did not exit cleanly while preserving pane 1/2: %s", ansi.Strip(terminal.String()))
}

func containsWorkflowPane(lines []string, prefix string) bool {
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func assertWorkflowFilesAndCalls(t *testing.T, home, stateDir string) {
	t.Helper()
	checks := map[string]os.FileMode{
		filepath.Join(home, ".openagentx"):                                  0o700,
		filepath.Join(home, ".openagentx", "credentials.json"):              0o600,
		filepath.Join(home, ".openagentx", "fleet.yaml"):                    0o600,
		filepath.Join(home, ".openagentx", "workers", "quote-service.yaml"): 0o600,
	}
	for path, want := range checks {
		info, err := os.Stat(path)
		var mode os.FileMode
		if err == nil {
			mode = info.Mode().Perm()
		}
		if err != nil || mode != want {
			t.Fatalf("path %s mode=%v want=%v err=%v", path, mode, want, err)
		}
	}
	calls, err := os.ReadFile(filepath.Join(stateDir, "systemctl.calls"))
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range strings.Split(strings.TrimSpace(string(calls)), "\n") {
		if !strings.HasPrefix(call, "--user ") {
			t.Fatalf("Fleet used non-user systemctl: %q", call)
		}
		if strings.Contains(strings.ToLower(call), "token") || strings.Contains(strings.ToLower(call), "password") {
			t.Fatalf("systemctl argv exposed a credential: %q", call)
		}
	}
}

func writeWorkflowExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func waitForWorkflowPath(t *testing.T, path string, processDone chan error, output *synchronizedTerminal) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
			return
		}
		select {
		case err := <-processDone:
			processDone <- err
			t.Fatalf("daemon exited before socket was ready: %v: %s", err, output.String())
		default:
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("daemon socket was not ready: %s", output.String())
}

func waitForWorkflowHTTP(t *testing.T, baseURL string, processDone chan error, output *synchronizedTerminal) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(baseURL + openapi.ObserveHealthPath)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		select {
		case processErr := <-processDone:
			processDone <- processErr
			t.Fatalf("daemon exited before HTTP was ready: %v: %s", processErr, output.String())
		default:
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("daemon HTTP endpoint was not ready: %s", output.String())
}

func readWorkflowPID(t *testing.T, path string) int {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil || pid <= 0 {
		t.Fatalf("invalid Worker PID %q: %v", content, err)
	}
	return pid
}

func stopWorkflowPID(pid int) {
	process, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = process.Signal(syscall.SIGTERM)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if process.Signal(syscall.Signal(0)) != nil {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	_ = process.Kill()
}

func stopWorkflowProcess(t *testing.T, process *os.Process, done <-chan error, name string) {
	t.Helper()
	if process == nil {
		return
	}
	_ = process.Signal(syscall.SIGTERM)
	select {
	case <-done:
		return
	case <-time.After(5 * time.Second):
		_ = process.Kill()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Errorf("%s process did not stop", name)
		}
	}
}
