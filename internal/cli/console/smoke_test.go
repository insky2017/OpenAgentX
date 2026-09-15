package console

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	"openagentx/internal/credentialstore"
	"openagentx/internal/domain"
)

const consoleTTYHelperEnvironment = "OPENAGENTX_TASK06_TTY_HELPER"
const consoleTTYFixtureSentinelSeconds = "86400"

type synchronizedTerminal struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (terminal *synchronizedTerminal) Write(data []byte) (int, error) {
	terminal.mu.Lock()
	defer terminal.mu.Unlock()
	return terminal.buffer.Write(data)
}

func (terminal *synchronizedTerminal) String() string {
	terminal.mu.Lock()
	defer terminal.mu.Unlock()
	return terminal.buffer.String()
}

func waitForRenderedTerminalText(terminal *synchronizedTerminal, expected string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(ansi.Strip(terminal.String()), expected) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return strings.Contains(ansi.Strip(terminal.String()), expected)
}

func TestConsoleTTYHelper(t *testing.T) {
	if os.Getenv(consoleTTYHelperEnvironment) != "1" {
		return
	}
	ready := os.Getenv("OPENAGENTX_TASK06_READY_FILE")
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			os.Exit(3)
		}
		time.Sleep(10 * time.Millisecond)
	}
	code := Execute([]string{"attach", "--agent", "quote", "--socket", os.Getenv("OPENAGENTX_TASK06_SOCKET"),
		"--credentials", os.Getenv("OPENAGENTX_TASK06_CREDENTIALS")}, DefaultDependencies())
	os.Exit(code)
}

func TestIsolatedTTYSmokeUsesAltScreenBindsAndPreservesExtraPanes(t *testing.T) {
	for _, binary := range []string{"tmux", "script"} {
		if _, err := exec.LookPath(binary); err != nil {
			t.Skipf("%s is required for isolated TTY smoke", binary)
		}
	}
	directory, err := os.MkdirTemp("", "oax6-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	socketPath := filepath.Join(directory, "s")
	credentialsPath := filepath.Join(directory, "credentials.json")
	readyPath := filepath.Join(directory, "ready")
	token := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	expires := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	store, err := credentialstore.New(credentialsPath, credentialstore.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(credentialstore.Credential{SocketPath: socketPath, InstallationID: "installation-smoke",
		Username: "owner", TokenID: "token-smoke", Token: token, AbsoluteExpires: expires}); err != nil {
		t.Fatal(err)
	}

	connected := make(chan struct{})
	var connectedOnce sync.Once
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != openapi.CLIInstallationProbePath && r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case openapi.CLIInstallationProbePath:
			_ = json.NewEncoder(w).Encode(openapi.CLIInstallationResponse{InstallationID: "installation-smoke"})
		case openapi.CLIAuthSessionPath:
			_ = json.NewEncoder(w).Encode(openapi.CLISessionResponse{InstallationID: "installation-smoke",
				Principal: openapi.CLIPrincipal{TokenID: "token-smoke", Username: "owner"}, AbsoluteExpiresAt: expires})
		case consoleapi.AgentsPath:
			_ = json.NewEncoder(w).Encode(consoleapi.AgentOptionsPage{Agents: []domain.ConsoleAgentOption{{AgentID: "quote",
				OrganizationID: "org-main", DisplayName: "Quote", WorkerStatus: domain.WorkerStatusOffline}}})
		case consoleapi.AttachPath:
			_ = json.NewEncoder(w).Encode(consoleapi.AttachResponse{AgentID: "quote", Mode: consoleapi.ModeNormal,
				WorkerStatus: domain.WorkerStatusOffline, SnapshotSequence: 0})
		case openapi.ObserveEventsStreamPath:
			if r.URL.Query().Get("agent_id") != "quote" || r.URL.Query().Get("mode") != consoleapi.ModeNormal ||
				r.URL.Query().Get("after_sequence") != "0" {
				http.Error(w, "invalid cursor", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			connectedOnce.Do(func() { close(connected) })
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Shutdown(context.Background())
		_ = listener.Close()
	})

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	socketName := fmt.Sprintf("oax6-%d-%d", os.Getpid(), time.Now().UnixNano())
	tmux := func(args ...string) (string, error) {
		command := exec.Command("tmux", append([]string{"-L", socketName}, args...)...)
		output, err := command.CombinedOutput()
		if err != nil {
			return string(output), fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
		}
		return string(output), nil
	}
	t.Cleanup(func() { _, _ = tmux("kill-server") })
	windowOutput, err := tmux("new-session", "-d", "-P", "-F", "#{window_id}", "-s", "OAX", "-n", "scratch",
		"env", consoleTTYHelperEnvironment+"=1", "OPENAGENTX_TASK06_READY_FILE="+readyPath,
		"OPENAGENTX_TASK06_SOCKET="+socketPath, "OPENAGENTX_TASK06_CREDENTIALS="+credentialsPath,
		"HOME="+directory, executable, "-test.run=^TestConsoleTTYHelper$")
	if err != nil {
		t.Fatal(err)
	}
	windowID := strings.TrimSpace(windowOutput)
	if _, err := tmux("set-option", "-w", "-t", windowID, "pane-base-index", "0"); err != nil {
		t.Fatal(err)
	}
	if _, err := tmux("set-option", "-w", "-t", windowID, "remain-on-exit", "on"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := tmux("split-window", "-d", "-t", windowID, "sleep", consoleTTYFixtureSentinelSeconds); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tmux("select-pane", "-t", windowID+".0"); err != nil {
		t.Fatal(err)
	}

	var terminal synchronizedTerminal
	attach := exec.Command("script", "-qfec", "tmux -L "+socketName+" attach-session -t OAX", "/dev/null")
	attach.Env = append(os.Environ(), "TERM=xterm-256color")
	stdin, err := attach.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	attach.Stdout = &terminal
	attach.Stderr = &terminal
	if err := attach.Start(); err != nil {
		t.Fatal(err)
	}
	var stopAttachOnce sync.Once
	stopAttach := func() {
		stopAttachOnce.Do(func() {
			_ = stdin.Close()
			_ = attach.Process.Kill()
			_ = attach.Wait()
		})
	}
	t.Cleanup(stopAttach)
	if err := os.WriteFile(readyPath, []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-connected:
	case <-time.After(10 * time.Second):
		stopAttach()
		t.Fatalf("Console did not reach the fake event stream: %s", terminal.String())
	}
	if !waitForRenderedTerminalText(&terminal, "> /help", 10*time.Second) {
		stopAttach()
		t.Fatalf("Console input was not ready: %q", terminal.String())
	}
	for _, key := range []byte("/quit") {
		if _, err := stdin.Write([]byte{key}); err != nil {
			stopAttach()
			t.Fatalf("write Console key: %v; terminal output=%q", err, terminal.String())
		}
		time.Sleep(30 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := stdin.Write([]byte{'\r'}); err != nil {
		stopAttach()
		t.Fatalf("write Console Enter: %v; terminal output=%q", err, terminal.String())
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		panes, paneErr := tmux("list-panes", "-t", windowID, "-F", "#{pane_index}:#{pane_dead}:#{pane_dead_status}")
		if paneErr == nil && strings.Contains(panes, "0:1:0") {
			break
		}
		if time.Now().After(deadline) {
			stopAttach()
			t.Fatalf("Console did not exit pane 0: panes=%q err=%v output=%s", panes, paneErr, terminal.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	stopAttach()

	output := terminal.String()
	plainOutput := ansi.Strip(output)
	if !strings.Contains(output, "\x1b[?1049h") || !strings.Contains(plainOutput, "> /help") {
		t.Fatalf("real TUI did not enter alt-screen Attach view: %q", output)
	}
	name, err := tmux("display-message", "-p", "-t", windowID, "-F", "#{window_name}")
	if err != nil || strings.TrimSpace(name) != "quote" {
		t.Fatalf("window name=%q err=%v", name, err)
	}
	managed, err := tmux("show-options", "-w", "-v", "-t", windowID, "@openagentx_managed")
	if err != nil || strings.TrimSpace(managed) != "1" {
		t.Fatalf("managed marker=%q err=%v", managed, err)
	}
	agentID, err := tmux("show-options", "-w", "-v", "-t", windowID, "@openagentx_agent_id")
	if err != nil || strings.TrimSpace(agentID) != "quote" {
		t.Fatalf("Agent marker=%q err=%v", agentID, err)
	}
	panes, err := tmux("list-panes", "-t", windowID, "-F", "#{pane_index}")
	if err != nil || strings.Fields(panes)[0] != "0" || !strings.Contains(panes, "1\n") || !strings.Contains(panes, "2\n") {
		t.Fatalf("pane topology=%q err=%v", panes, err)
	}
}

func TestStripTerminalControlsPreservesRenderedTextAcrossANSI(t *testing.T) {
	rendered := "> /\x1b(B\x1b[38;5;42mhelp\x1b[0m\r\n\x1b]0;ignored\x07Agent quote"
	plain := ansi.Strip(rendered)
	if !strings.Contains(plain, "> /help") || !strings.Contains(plain, "Agent quote") || strings.Contains(plain, "ignored") {
		t.Fatalf("stripped terminal output=%q", plain)
	}
}
