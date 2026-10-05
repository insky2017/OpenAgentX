package overview

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	"openagentx/internal/credentialstore"
	"openagentx/internal/domain"
	fleetmodel "openagentx/internal/fleet"
)

// The helper is a terminal process, not a Worker or a simulated task executor.
func TestMain(m *testing.M) {
	if os.Getenv("OAX_OVERVIEW_TERMINAL_HELPER") == "1" {
		for {
			time.Sleep(time.Hour)
		}
	}
	os.Exit(m.Run())
}

// Exercises the existing HTTP client over a real UDS, credential/profile
// binding, pagination beyond the old overview's 100-row limit, and the UI's
// stale-state boundary. Responses are contract fixtures, not live E2E evidence.
func TestUDSReadOnlyRefreshAndRecovery(t *testing.T) {
	root, err := os.MkdirTemp("", "oax-overview-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	socket := filepath.Join(root, "daemon.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var unavailable atomic.Bool
	var writes atomic.Int32
	var active, peak atomic.Int32
	var seen sync.Map
	expires := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	credential := credentialstore.Credential{SocketPath: socket, InstallationID: "installation-overview", Username: "viewer", TokenID: "token-overview", Token: strings.Repeat("A", 43), AbsoluteExpires: expires}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes.Add(1)
			http.Error(w, "write forbidden", 405)
			return
		}
		if unavailable.Load() {
			http.Error(w, "unavailable", 503)
			return
		}
		current := active.Add(1)
		defer active.Add(-1)
		for {
			old := peak.Load()
			if current <= old || peak.CompareAndSwap(old, current) {
				break
			}
		}
		if r.URL.Path != openapi.CLIInstallationProbePath && r.Header.Get("Authorization") != "Bearer "+credential.Token {
			http.Error(w, "bad identity", 401)
			return
		}
		var output any
		switch r.URL.Path {
		case openapi.CLIInstallationProbePath:
			output = openapi.CLIInstallationResponse{InstallationID: credential.InstallationID}
		case openapi.CLIAuthSessionPath:
			output = openapi.CLISessionResponse{InstallationID: credential.InstallationID, AbsoluteExpiresAt: expires, Principal: openapi.CLIPrincipal{TokenID: credential.TokenID, Username: credential.Username, Roles: []string{"viewer"}, Scopes: []string{string(domain.CLIScopeConsoleRead)}}}
		case consoleapi.AgentsPath:
			page := consoleapi.AgentOptionsPage{}
			start, end := 0, 100
			if r.URL.Query().Get("after_agent_id") != "" {
				start, end = 100, 103
			} else {
				page.HasMore = true
				page.NextCursor = "agent-099"
			}
			for i := start; i < end; i++ {
				page.Agents = append(page.Agents, domain.ConsoleAgentOption{AgentID: fmt.Sprintf("agent-%03d", i), DisplayName: "领域测试", OrganizationID: "default", WorkerStatus: domain.WorkerStatusOnline, Generation: 1})
			}
			output = page
		case consoleapi.AttachPath:
			id := r.URL.Query().Get("agent_id")
			seen.Store(id, true)
			output = consoleapi.AttachResponse{AgentID: id, WorkerStatus: domain.WorkerStatusOnline, Generation: 1, Readiness: &openapi.AgentReadiness{Ready: true, Reason: "可开始工作"}, SuggestedTask: &openapi.ConsoleTaskOption{TaskID: "task-" + id, Status: domain.TaskStatusWaitingApproval, Intent: domain.TaskIntentQuery, Summary: "核对行情边界"}}
		default:
			prefix := "/api/console/v1/agents/"
			parts := strings.Split(strings.TrimPrefix(r.URL.Path, prefix), "/")
			if len(parts) != 3 || parts[1] != "tasks" {
				http.NotFound(w, r)
				return
			}
			result := "只读结果\n" + strings.Repeat("中文内容需要能够滚动查看。", 100) + "\x1b[2J"
			output = openapi.ConsoleTaskSnapshot{Task: openapi.ConsoleTaskReadModel{TaskID: parts[2], AgentID: parts[0], Status: domain.TaskStatusWaitingApproval, Intent: domain.TaskIntentQuery, Content: "核对行情边界", Result: &result}, PendingApproval: &openapi.ConsoleApprovalReadModel{State: domain.ApprovalRequestPending}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(output)
	})}
	go server.Serve(ln)
	defer server.Close()
	store, err := credentialstore.New(filepath.Join(root, "credentials.json"), credentialstore.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Save(credential); err != nil {
		t.Fatal(err)
	}
	service := &reader{socket: socket, credentials: filepath.Join(root, "credentials.json"), store: store, now: time.Now}
	first := service.refresh(context.Background())
	if first.Err != nil || len(first.Rows) != 103 {
		t.Fatalf("initial read: agents=%d err=%v", len(first.Rows), first.Err)
	}
	if first.Rows[0].Error != "" || first.Rows[0].Task == nil {
		t.Fatalf("task detail missing: %+v", first.Rows[0])
	}
	if peak.Load() > 4 || writes.Load() != 0 {
		t.Fatalf("concurrency=%d writes=%d", peak.Load(), writes.Load())
	}
	if _, ok := seen.Load("agent-102"); !ok {
		t.Fatal("paginated Agent omitted")
	}
	m := newModel(context.Background(), service, &navigator{})
	next, _ := m.Update(first)
	m = next.(model)
	for _, size := range [][2]int{{80, 24}, {140, 38}, {36, 12}} {
		next, _ = m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(model)
		view := m.View()
		lines := strings.Split(view, "\n")
		if len(lines) > size[1] {
			t.Fatalf("height overflow %d", len(lines))
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > size[0] {
				t.Fatal("width overflow")
			}
		}
	}
	unavailable.Store(true)
	failed := service.refresh(context.Background())
	if failed.Err == nil {
		t.Fatal("outage hidden")
	}
	next, _ = m.Update(failed)
	m = next.(model)
	if m.connected || len(m.rows) != 103 || !strings.Contains(m.View(), "旧数据") {
		t.Fatal("stale snapshot lost or marked live")
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("disconnected navigation admitted")
	}
	unavailable.Store(false)
	next, _ = m.Update(service.refresh(context.Background()))
	m = next.(model)
	if !m.connected {
		t.Fatal("reconnect failed")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = next.(model)
	if m.detailOffset == 0 {
		t.Fatal("detail cannot scroll")
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("quit did not exit locally")
	}
	if writes.Load() != 0 {
		t.Fatal("read-only view made write request")
	}
	t.Logf("UDS read-only recovery PASS: 103 Agents, max concurrency %d, zero writes, three terminal sizes", peak.Load())
}

func TestIsolatedTmuxNavigationIdentityAndSource(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	ctx := context.Background()
	runner := fleetmodel.ExecRunner{SocketName: fmt.Sprintf("oax-overview-%d-%d", os.Getpid(), time.Now().UnixNano())}
	t.Cleanup(func() { _, _ = runner.Run(context.Background(), "kill-server") })
	run := func(args ...string) string {
		t.Helper()
		out, e := runner.Run(ctx, args...)
		if e != nil {
			t.Fatal(e)
		}
		return strings.TrimSpace(out)
	}
	source := run("new-session", "-d", "-P", "-F", "#{pane_id}", "-s", "OAX", "-n", "overview", "sleep", "3600")
	root := t.TempDir()
	socket := filepath.Join(root, "daemon.sock")
	credentials := filepath.Join(root, "credentials.json")
	target := run("new-window", "-d", "-P", "-F", "#{window_id}", "-t", "=OAX", "-n", "alpha", "env", "OAX_OVERVIEW_TERMINAL_HELPER=1", os.Args[0], "console", "attach", "--agent", "alpha", "--socket", socket, "--credentials", credentials)
	run("set-option", "-w", "-t", target, "automatic-rename", "off")
	run("set-option", "-w", "-t", target, "pane-base-index", "0")
	run("set-option", "-w", "-t", target, fleetmodel.ManagedOption, "1")
	run("set-option", "-w", "-t", target, fleetmodel.AgentIDOption, "alpha")
	extra := run("split-window", "-d", "-P", "-F", "#{pane_id}", "-t", target, "sleep", "3600")
	n := &navigator{runner: runner, socket: socket, credentials: credentials, sourcePane: source}
	proof, err := n.target(ctx, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if proof.pane == extra {
		t.Fatal("selected extra pane")
	}
	var processes []process
	deadline := time.Now().Add(2 * time.Second)
	for {
		processes, err = n.verifyProfile(ctx, proof.pid, "alpha")
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("live same-profile terminal rejected: %v", err)
	}
	before := run("list-panes", "-a", "-F", "#{pane_id}:#{pane_pid}")
	wrong := *n
	wrong.socket = filepath.Join(root, "other.sock")
	if _, err = wrong.verifyProfile(ctx, proof.pid, "alpha"); err == nil {
		t.Fatal("cross-profile source accepted")
	}
	run("set-option", "-w", "-u", "-t", target, fleetmodel.AgentIDOption)
	if _, err = n.target(ctx, "alpha"); err == nil {
		t.Fatal("unmarked pane accepted")
	}
	run("set-option", "-w", "-t", target, fleetmodel.AgentIDOption, "alpha")
	dup := run("new-window", "-d", "-P", "-F", "#{window_id}", "-t", "=OAX", "-n", "other", "sleep", "3600")
	run("set-option", "-w", "-t", dup, fleetmodel.AgentIDOption, "alpha")
	if _, err = n.target(ctx, "alpha"); err == nil {
		t.Fatal("duplicate identity accepted")
	}
	run("kill-window", "-t", dup)
	if after := run("list-panes", "-a", "-F", "#{pane_id}:#{pane_pid}"); after != before {
		t.Fatal("navigation validation changed existing processes")
	}
	if len(processes) != 1 || processes[0].pid <= 0 {
		t.Fatal("source process proof missing")
	}
	t.Logf("isolated tmux identity/source checks PASS: pane=%s pid=%d; extra pane=%s preserved; no client switch attempted", proof.pane, proof.pid, extra)
}

// Opt-in read-only probe for an explicitly supplied local runtime. It never
// selects or modifies tmux, starts a Runtime, or sends a business message.
func TestReadOnlyNativeProfileProbe(t *testing.T) {
	raw := os.Getenv("OPENAGENTX_OVERVIEW_NATIVE_PROBE")
	if raw == "" {
		t.Skip("explicit native probe not requested")
	}
	var input struct {
		Socket, Credentials, WorkerDir string
		Agents                         map[string]int
	}
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		t.Fatal(err)
	}
	n := &navigator{socket: input.Socket, credentials: input.Credentials, workerDir: input.WorkerDir}
	for agent, pid := range input.Agents {
		t.Run(agent, func(t *testing.T) {
			proof, err := n.verifyProfile(context.Background(), pid, agent)
			if err != nil {
				t.Fatal(err)
			}
			ids := []string{}
			for _, p := range proof {
				ids = append(ids, strconv.Itoa(p.pid))
			}
			t.Logf("read-only source proof agent=%s parent/foreground=%s", agent, strings.Join(ids, ","))
		})
	}
}

// Independent regression: matching outer native argv alone is not thread evidence.
func TestIndependentNativeOuterArgsCannotBypassThreadProof(t *testing.T) {
 ctx := context.Background()
 runner := fleetmodel.ExecRunner{SocketName: fmt.Sprintf("oax-review-%d-%d", os.Getpid(), time.Now().UnixNano())}
 t.Cleanup(func(){ _, _ = runner.Run(ctx, "kill-server") })
 root := t.TempDir()
 socket, credentials := filepath.Join(root,"daemon.sock"), filepath.Join(root,"credentials.json")
 out, err := runner.Run(ctx,"new-session","-d","-P","-F","#{pane_pid}","-s","OAX","-n","alpha","env","OAX_OVERVIEW_TERMINAL_HELPER=1",os.Args[0],"agent","open","alpha","--native","--socket",socket,"--credentials",credentials)
 if err != nil { t.Fatal(err) }
 pid, err := strconv.Atoi(strings.TrimSpace(out)); if err != nil { t.Fatal(err) }
 n := &navigator{runner:runner,socket:socket,credentials:credentials,workerDir:root}
 deadline:=time.Now().Add(2*time.Second)
 var seen bool
 for time.Now().Before(deadline) {
  proc, e := readProcess(pid)
  if e == nil && len(proc.args)>4 && proc.args[1]=="agent" && proc.pgrp==proc.foreground {seen=true;break}
  time.Sleep(20*time.Millisecond)
 }
 if !seen {t.Fatal("isolated foreground native-argv fixture not ready")}
 if proof,err:=n.verifyProfile(ctx,pid,"alpha"); err==nil {t.Fatalf("native without persisted thread or foreground Codex was accepted: %+v",proof)}
 t.Log("matching native argv rejected without canonical thread and foreground Codex proof; isolated real process, no native bridge/model invoked")
}
