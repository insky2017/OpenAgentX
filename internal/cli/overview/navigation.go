package overview

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"openagentx/internal/domain"
	fleetmodel "openagentx/internal/fleet"
	"openagentx/internal/worker"
)

type navigator struct {
	runner                                     fleetmodel.CommandRunner
	socket, credentials, workerDir, sourcePane string
}

type paneProof struct {
	window, pane string
	pid          int
	record       string
}

func (n *navigator) target(ctx context.Context, agentID string) (paneProof, error) {
	windows, err := (fleetmodel.Workspace{Runner: n.runner}).Inspect(ctx)
	if err != nil {
		return paneProof{}, fmt.Errorf("无法读取 OAX 终端布局")
	}
	matches := 0
	var target fleetmodel.Window
	for _, w := range windows {
		if w.Name == agentID || w.AgentID.Value == agentID {
			matches++
			target = w
		}
	}
	if matches != 1 {
		return paneProof{}, fmt.Errorf("Agent 终端不存在或身份不唯一；未跳转")
	}
	if target.Name != agentID || target.Managed != (fleetmodel.OptionValue{Set: true, Value: "1"}) || target.AgentID != (fleetmodel.OptionValue{Set: true, Value: agentID}) || !target.PaneZeroSeen || target.PaneZeroDead {
		return paneProof{}, fmt.Errorf("缺少受管身份标记或活 pane 0；未跳转")
	}
	format := "#{session_name}\t#{window_id}\t#{window_name}\t#{@openagentx_managed}\t#{@openagentx_agent_id}\t#{pane_id}\t#{pane_index}\t#{pane_pid}\t#{pane_dead}"
	raw, err := n.runner.Run(ctx, "display-message", "-p", "-t", target.ID+".0", "-F", format)
	if err != nil {
		return paneProof{}, fmt.Errorf("无法复核目标 pane 0")
	}
	fields := strings.Split(strings.TrimSpace(raw), "\t")
	if len(fields) != 9 || fields[0] != fleetmodel.SessionName || fields[1] != target.ID || fields[2] != agentID || fields[3] != "1" || fields[4] != agentID || fields[6] != "0" || fields[8] != "0" || !validHandle(fields[5], '%') {
		return paneProof{}, fmt.Errorf("目标 pane 身份已改变；未跳转")
	}
	pid, err := strconv.Atoi(fields[7])
	if err != nil || pid <= 0 {
		return paneProof{}, fmt.Errorf("目标终端进程未知")
	}
	return paneProof{window: target.ID, pane: fields[5], pid: pid, record: strings.TrimSpace(raw)}, nil
}

func validHandle(s string, prefix byte) bool {
	if len(s) < 2 || s[0] != prefix {
		return false
	}
	_, err := strconv.ParseUint(s[1:], 10, 64)
	return err == nil
}

func (n *navigator) jump(ctx context.Context, agentID string) error {
	if domain.ValidateIdentifier("agent_id", agentID) != nil || agentID == fleetmodel.OverviewWindow {
		return fmt.Errorf("Agent 身份无效")
	}
	if !validHandle(n.sourcePane, '%') {
		return fmt.Errorf("请在 tmux 内运行总览后跳转；未创建任何终端")
	}
	target, err := n.target(ctx, agentID)
	if err != nil {
		return err
	}
	proofs, err := n.verifyProfile(ctx, target.pid, agentID)
	if err != nil {
		return err
	}
	// Resolve again immediately before selection. Renames, respawns and marker
	// changes must fail closed instead of redirecting to a different pane.
	again, err := n.target(ctx, agentID)
	if err != nil {
		return err
	}
	if target != again {
		return fmt.Errorf("终端布局已变化，请刷新后重试")
	}
	for _, p := range proofs {
		current, e := readProcess(p.pid)
		if e != nil || current.stamp != p.stamp || strings.Join(current.args, "\x00") != strings.Join(p.args, "\x00") {
			return fmt.Errorf("目标前台进程已变化，请刷新后重试")
		}
	}
	// Target an immutable pane handle, never a window index or bare Agent name.
	if _, err = n.runner.Run(ctx, "switch-client", "-t", target.pane); err != nil {
		return fmt.Errorf("无法切换当前 tmux 客户端；目标未启动或重建")
	}
	return nil
}

type process struct {
	pid                   int
	args                  []string
	stamp                 string
	tty, pgrp, foreground string
}

func readProcess(pid int) (process, error) {
	path := filepath.Join("/proc", strconv.Itoa(pid))
	stat, err := os.ReadFile(filepath.Join(path, "stat"))
	if err != nil {
		return process{}, err
	}
	end := strings.LastIndexByte(string(stat), ')')
	if end < 0 {
		return process{}, fmt.Errorf("invalid process")
	}
	fields := strings.Fields(string(stat[end+1:]))
	if len(fields) < 20 {
		return process{}, fmt.Errorf("invalid process")
	}
	raw, err := os.ReadFile(filepath.Join(path, "cmdline"))
	if err != nil {
		return process{}, err
	}
	return process{pid: pid, args: strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00"), stamp: fields[19], pgrp: fields[2], tty: fields[4], foreground: fields[5]}, nil
}
func descendants(ctx context.Context, pid int) ([]process, error) {
	type item struct{ pid, depth int }
	queue := []item{{pid, 0}}
	seen := map[int]bool{}
	var result []process
	for len(queue) > 0 {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if len(seen) >= 128 {
			return nil, fmt.Errorf("终端进程树超出核查范围")
		}
		current := queue[0]
		queue = queue[1:]
		if seen[current.pid] {
			continue
		}
		seen[current.pid] = true
		p, err := readProcess(current.pid)
		if err != nil {
			continue
		}
		result = append(result, p)
		if current.depth >= 5 {
			continue
		}
		// Go starts children on any OS thread; inspecting the leader alone misses
		// native terminals whose parent CLI was started by another thread.
		paths, _ := filepath.Glob(filepath.Join("/proc", strconv.Itoa(current.pid), "task", "*", "children"))
		for _, path := range paths {
			raw, e := os.ReadFile(path)
			if e != nil {
				continue
			}
			for _, s := range strings.Fields(string(raw)) {
				child, e := strconv.Atoi(s)
				if e == nil && !seen[child] {
					queue = append(queue, item{child, current.depth + 1})
				}
			}
		}
	}
	return result, nil
}

func (n *navigator) verifyProfile(ctx context.Context, pid int, agentID string) ([]process, error) {
	procs, err := descendants(ctx, pid)
	if err != nil {
		return nil, err
	}
	for _, p := range procs {
		args := p.args
		if len(args) < 4 {
			continue
		}
		console := args[1] == "console" && args[2] == "attach" && flagValue(args[3:], "--agent") == agentID
		native := args[1] == "agent" && args[2] == "open" && args[3] == agentID && has(args[4:], "--native")
		if !console && !native {
			continue
		}
		flags := args[3:]
		if native {
			flags = args[4:]
		}
		// Explicitly contradictory source paths always reject this candidate.
		if value := flagValue(flags, "--socket"); value != "" && !samePath(value, n.socket) {
			continue
		}
		if value := flagValue(flags, "--credentials"); value != "" && !samePath(value, n.credentials) {
			continue
		}
		if value := flagValue(flags, "--worker-dir"); value != "" && !samePath(value, n.workerDir) {
			continue
		}
		if value := flagValue(flags, "--socket"); console && value != "" && flagValue(flags, "--credentials") != "" && p.tty != "0" && p.pgrp == p.foreground {
			return []process{p}, nil
		}
		// /proc/exe and environ may be unreadable for an existing CLI. Native
		// sessions still have independent identity evidence: canonical Worker
		// config -> daemon socket -> persisted thread -> foreground Codex resume.
		if native {
			thread, binary, e := n.nativeThread(agentID)
			if e != nil {
				continue
			}
			children, e := descendants(ctx, p.pid)
			if e != nil {
				continue
			}
			for _, child := range children {
				a := child.args
				if len(a) < 4 || filepath.Base(a[0]) != filepath.Base(binary) || a[1] != "resume" || child.tty == "0" || child.pgrp != child.foreground {
					continue
				}
				remote := flagValue(a[2:], "--remote")
				if !strings.HasPrefix(remote, "unix://") || !has(a[2:], thread) {
					continue
				}
				info, e := os.Stat(strings.TrimPrefix(remote, "unix://"))
				if e != nil || info.Mode()&os.ModeSocket == 0 {
					continue
				}
				return []process{p, child}, nil
			}
		}
	}
	return nil, fmt.Errorf("无法确认前台终端属于此 profile；未跳转（需匹配受管 CLI 与原生会话）")
}

func (n *navigator) nativeThread(agentID string) (string, string, error) {
	configPath := filepath.Join(n.workerDir, agentID+".yaml")
	cfg, err := worker.LoadProcessConfig(configPath)
	if err != nil {
		return "", "", fmt.Errorf("无法读取目标 Worker 配置")
	}
	if cfg.AgentID != agentID || !samePath(cfg.UnixSocket, n.socket) {
		return "", "", fmt.Errorf("Worker 配置与当前 profile 不一致")
	}
	for _, backend := range cfg.RuntimeBackendConfig {
		if backend.AdapterID != "codex-app-server" {
			continue
		}
		stateDir, _ := backend.Options["state_dir"].(string)
		if stateDir == "" {
			stateDir = filepath.Join(n.workerDir, "codex", agentID)
		}
		if !filepath.IsAbs(stateDir) {
			stateDir = filepath.Join(n.workerDir, stateDir)
		}
		raw, e := fleetmodel.ReadSecureFile(filepath.Join(stateDir, "state.json"), fleetmodel.SecureFileOptions{MaximumBytes: 1 << 20})
		if e != nil {
			return "", "", e
		}
		var state struct {
			ThreadID string `json:"thread_id"`
		}
		if json.Unmarshal(raw, &state) != nil || state.ThreadID == "" {
			return "", "", fmt.Errorf("原生会话身份不可用")
		}
		binary, _ := backend.Options["binary"].(string)
		if binary == "" {
			binary = "codex"
		}
		return state.ThreadID, binary, nil
	}
	return "", "", fmt.Errorf("目标没有原生会话配置")
}
func samePath(a, b string) bool {
	if !filepath.IsAbs(a) || !filepath.IsAbs(b) {
		return false
	}
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if a == b {
		return true
	}
	aa, e1 := filepath.EvalSymlinks(a)
	bb, e2 := filepath.EvalSymlinks(b)
	return e1 == nil && e2 == nil && aa == bb
}
func flagValue(args []string, name string) string {
	result := ""
	for i, a := range args {
		if strings.HasPrefix(a, name+"=") {
			result = strings.TrimPrefix(a, name+"=")
		} else if a == name && i+1 < len(args) {
			result = args[i+1]
		}
	}
	return result
}
