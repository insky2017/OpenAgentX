// Package nativebridge exposes the Codex TUI while routing every work input
// through OpenAgentX's durable control API. The app-server socket stays private
// to the Worker; closing this foreground view never cancels background work.
package nativebridge

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"openagentx/internal/api"
	consoleclient "openagentx/internal/client/console"
	"openagentx/internal/credentialstore"
	"openagentx/internal/domain"
	"openagentx/internal/runtime/codex"
	"openagentx/internal/worker"
)

type Options struct {
	AgentID, WorkerConfig, Socket, Credentials string
	In                                         io.Reader
	Out, Err                                   io.Writer
}
type engineState struct {
	Endpoint string `json:"endpoint"`
	ThreadID string `json:"thread_id"`
	TurnID   string `json:"turn_id"`
	TaskID   string `json:"task_id"`
	RunID    string `json:"run_id"`
	State    string `json:"state"`
}

func readState(path string) (engineState, error) {
	var s engineState
	raw, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(raw, &s)
	return s, err
}

type control interface {
	Dispatch(context.Context, api.CreateTaskRequest) (api.CreateTaskResponse, error)
	TaskSnapshot(context.Context, string, string) (api.ConsoleTaskSnapshot, error)
	Steer(context.Context, string, api.CreateMessageRequest) (api.CreateMessageResponse, error)
	Cancel(context.Context, string, api.CancelTaskRequest) (api.CancelTaskResponse, error)
	GetAgentModelSettings(context.Context, string, string) (domain.AgentModelSettings, error)
	SetAgentModelSettings(context.Context, string, domain.AgentModelSettingsUpdate) (domain.AgentModelSettings, error)
}
type bridge struct {
	settingsMu                                                        sync.Mutex
	inputMu                                                           sync.Mutex
	inputClientIDs                                                    map[string]string
	control                                                           control
	agentID, organizationID, backendID, threadID, statePath, endpoint string
}

func Open(ctx context.Context, o Options) error {
	cfg, err := worker.LoadProcessConfig(o.WorkerConfig)
	if err != nil {
		return err
	}
	var backend *worker.RuntimeBackendConfig
	for i := range cfg.RuntimeBackendConfig {
		if cfg.RuntimeBackendConfig[i].AdapterID == "codex-app-server" {
			backend = &cfg.RuntimeBackendConfig[i]
			break
		}
	}
	if backend == nil {
		return errors.New("此 Agent 未配置 Codex；请先使用 agent add --runtime codex")
	}
	stateDir, _ := backend.Options["state_dir"].(string)
	if stateDir == "" {
		stateDir = filepath.Join(filepath.Dir(o.WorkerConfig), "codex", o.AgentID)
	}
	if !filepath.IsAbs(stateDir) {
		stateDir = filepath.Join(filepath.Dir(o.WorkerConfig), stateDir)
	}
	statePath := filepath.Join(stateDir, "state.json")
	state, err := readState(statePath)
	if err != nil || state.Endpoint == "" {
		return fmt.Errorf("Codex 后台尚未就绪；先运行 openagentx agent resume %s --no-open", o.AgentID)
	}
	lock, err := os.OpenFile(filepath.Join(stateDir, "terminal.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("此 Agent 已打开受管原生终端；请返回已有终端或先退出它")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	client, err := consoleclient.NewUnixClient(o.Socket)
	if err != nil {
		return err
	}
	store, err := credentialstore.New(o.Credentials, credentialstore.Options{})
	if err != nil {
		return err
	}
	probe, err := client.ProbeInstallation(ctx)
	if err != nil {
		return err
	}
	cred, err := store.Load(o.Socket, probe.InstallationID, "")
	if err != nil {
		return fmt.Errorf("请先运行 openagentx console login：%w", err)
	}
	if err = client.UseCredential(ctx, cred.InstallationID, cred.Token); err != nil {
		return err
	}
	agents, err := client.ListAgentOptions(ctx)
	if err != nil {
		return err
	}
	organization := ""
	for _, a := range agents {
		if a.AgentID == o.AgentID {
			organization = a.OrganizationID
			break
		}
	}
	if organization == "" {
		return errors.New("Agent 尚未在控制台登记")
	}
	rpc, err := codex.DialRPC(ctx, state.Endpoint)
	if err != nil {
		return fmt.Errorf("Codex 后台连接失败；运行 agent resume 后重试：%w", err)
	}
	defer rpc.Close()
	if err = rpc.Initialize(ctx, "openagentx-native-open"); err != nil {
		return err
	}
	threadID := state.ThreadID
	if threadID == "" {
		fmt.Fprintln(o.Out, "正在初始化 Codex 角色与工作目录，完成后打开原生终端……")
		receipt, err := client.Dispatch(ctx, api.CreateTaskRequest{Meta: api.CommandMeta{IdempotencyKey: "native-init-" + uuid.NewString()}, TargetAgentID: o.AgentID, OrganizationID: organization, DispatchMode: domain.DispatchModeDirect, Intent: domain.TaskIntentQuery, Content: "请简洁确认你在本轮收到的领域角色与工作目录，然后等待下一项任务。不要修改任何文件。"})
		if err != nil {
			return err
		}
		initCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		timer := time.NewTicker(time.Second)
		defer timer.Stop()
		for {
			snapshot, err := client.TaskSnapshot(initCtx, o.AgentID, receipt.TaskID)
			if err != nil {
				return err
			}
			switch snapshot.Task.Status {
			case domain.TaskStatusSucceeded:
				saved, err := readState(filepath.Join(stateDir, "tasks", receipt.TaskID+".json"))
				if err != nil {
					return err
				}
				threadID = saved.ThreadID
			case domain.TaskStatusFailed, domain.TaskStatusCanceled, domain.TaskStatusUncertain:
				return fmt.Errorf("初始化任务 %s 状态为 %s；运行 agent open --console 查看原因", receipt.TaskID, snapshot.Task.Status)
			}
			if threadID != "" {
				break
			}
			select {
			case <-initCtx.Done():
				return fmt.Errorf("初始化仍在后台进行；稍后重试 agent open --native：%w", initCtx.Err())
			case <-timer.C:
			}
		}
	}
	if threadID == "" {
		return errors.New("Codex 未返回可连接会话")
	}
	b := &bridge{control: client, agentID: o.AgentID, organizationID: organization, backendID: backend.BackendID, threadID: threadID, statePath: statePath, endpoint: state.Endpoint}
	// Short private path avoids the Unix socket pathname limit for long profiles.
	temp, err := os.MkdirTemp("", "oax-native-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	socket := filepath.Join(temp, "bridge.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: http.HandlerFunc(b.serve), ReadHeaderTimeout: 10 * time.Second}
	defer server.Close()
	go server.Serve(listener)
	binary, _ := backend.Options["binary"].(string)
	if binary == "" {
		binary = "codex"
	}
	fmt.Fprintf(o.Out, "OpenAgentX · %s · 原生 Codex\n输入会进入任务队列；后台忙时自动等待。关闭此终端后，后台继续工作。\n\n", o.AgentID)
	command := exec.CommandContext(ctx, binary, "resume", "--remote", "unix://"+socket, "--no-alt-screen", threadID)
	command.Stdin = o.In
	command.Stdout = o.Out
	command.Stderr = o.Err
	if err := command.Run(); err != nil {
		return fmt.Errorf("原生终端连接已结束；后台任务状态可在工作台查看。重新连接：openagentx agent open %s --native（%w）", o.AgentID, err)
	}
	fmt.Fprintln(o.Out, "已关闭原生终端。后台任务与接单服务由 Worker 继续管理。")
	return nil
}

func (b *bridge) serve(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "" }}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetReadLimit(2 << 20)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	upstream, err := codex.DialRPC(ctx, b.endpoint)
	if err != nil {
		return
	}
	defer upstream.Close()
	events, unsubscribe := upstream.Subscribe()
	defer unsubscribe()
	var writeMu sync.Mutex
	send := func(m codex.RPCMessage) error {
		m = b.projectInputReceipts(m)
		writeMu.Lock()
		defer writeMu.Unlock()
		conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteJSON(m)
	}
	order := newTurnEventOrder(b, send)
	// Kept for this connection's lifetime: evicting a completed write receipt
	// could turn a delayed duplicate into new work. Readers need no receipt.
	receipts := make(map[string]*nativeReceipt)
	failConnection := func() { cancel(); _ = conn.Close() }
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					conn.Close()
					return
				}
				// Approval authority stays with the Worker/control plane. The view never
				// answers app-server requests directly, nor leaks them into an unmanaged UI.
				if len(event.ID) > 0 || !notificationForThread(event, b.threadID) {
					continue
				}
				if event.Method == "thread/settings/updated" {
					projected, err := b.projectSettings(ctx, event.Method, event.Params)
					if err != nil {
						failConnection()
						return
					}
					event.Params = projected
				}
				if err := order.notification(event); err != nil {
					failConnection()
					return
				}
			}
		}
	}()
	for {
		var request codex.RPCMessage
		if err := conn.ReadJSON(&request); err != nil {
			return
		}
		if len(request.ID) == 0 {
			if request.Method == "initialized" {
				if err := upstream.Notify(ctx, request.Method, json.RawMessage(request.Params)); err != nil {
					return
				}
			}
			continue
		}
		if request.Method == "" {
			continue
		}
		var receipt *nativeReceipt
		if nativeWrite(request.Method) {
			key := string(request.ID)
			signature := sha256.Sum256(append([]byte(request.Method+"\x00"), request.Params...))
			if existing := receipts[key]; existing != nil {
				if existing.signature != signature {
					_ = send(codex.RPCMessage{ID: request.ID, Error: &codex.RPCError{Code: -32600, Message: "RPC request ID was reused for different work"}})
					continue
				}
				go func(saved *nativeReceipt) {
					select {
					case <-saved.done:
						if err := send(saved.response); err != nil {
							failConnection()
						}
					case <-ctx.Done():
					}
				}(existing)
				continue
			}
			if len(receipts) >= maxNativeReceipts {
				_ = send(codex.RPCMessage{ID: request.ID, Error: &codex.RPCError{Code: -32001, Message: "Native write receipt limit reached; reopen the terminal before new work"}})
				continue
			}
			receipt = &nativeReceipt{signature: signature, done: make(chan struct{})}
			receipts[key] = receipt
		}
		if request.Method == "turn/start" {
			order.begin(string(request.ID))
		}
		go func(request codex.RPCMessage, receipt *nativeReceipt) {
			result, err := b.requestWithDispatch(ctx, upstream, request.Method, request.Params, func(taskID string) { order.dispatched(string(request.ID), taskID) })
			response := codex.RPCMessage{ID: request.ID}
			if err != nil {
				response.Error = &codex.RPCError{Code: -32001, Message: err.Error()}
			} else {
				response.Result = result
			}
			if request.Method == "turn/start" {
				err = order.reply(string(request.ID), response)
			} else {
				err = send(response)
			}
			if receipt != nil {
				receipt.response = response
				close(receipt.done)
			}
			if err != nil {
				failConnection()
			}
		}(request, receipt)
	}
}
func rawResult(v any) (json.RawMessage, error) { raw, err := json.Marshal(v); return raw, err }
func (b *bridge) request(ctx context.Context, upstream *codex.RPCClient, method string, params json.RawMessage) (json.RawMessage, error) {
	return b.requestWithDispatch(ctx, upstream, method, params, nil)
}
func (b *bridge) requestWithDispatch(ctx context.Context, upstream *codex.RPCClient, method string, params json.RawMessage, onDispatch func(string)) (json.RawMessage, error) {
	var p struct {
		ThreadID            string                        `json:"threadId"`
		TurnID              string                        `json:"turnId"`
		ExpectedTurnID      string                        `json:"expectedTurnId"`
		ClientUserMessageID string                        `json:"clientUserMessageId"`
		Input               []struct{ Type, Text string } `json:"input"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
	}
	if p.ThreadID != "" && p.ThreadID != b.threadID {
		return nil, errors.New("受管终端只能操作当前 Agent 的会话")
	}
	idempotencyKey := "native-" + uuid.NewString()
	if p.ClientUserMessageID != "" {
		// Keep native retries stable across foreground reconnections; content
		// changes with the same ID are rejected by the Control API's contract.
		encoded, _ := json.Marshal([]string{b.agentID, b.threadID, method, p.ClientUserMessageID})
		idempotencyKey = fmt.Sprintf("native-%x", sha256.Sum256(encoded))
	}
	switch method {
	case "thread/settings/update", "config/batchWrite":
		return b.updateSettings(ctx, method, params)
	case "turn/start":
		content, err := textInput(p.Input)
		if err != nil {
			return nil, err
		}
		receipt, err := b.control.Dispatch(ctx, api.CreateTaskRequest{Meta: api.CommandMeta{IdempotencyKey: idempotencyKey}, TargetAgentID: b.agentID, OrganizationID: b.organizationID, DispatchMode: domain.DispatchModeDirect, Intent: domain.TaskIntentMutation, Content: content, RuntimeSession: &domain.RuntimeSessionReference{BackendID: b.backendID, ProviderSessionID: b.threadID}})
		if err != nil {
			return nil, err
		}
		if onDispatch != nil {
			onDispatch(receipt.TaskID)
		}
		// Wait for this exact Task to get its own turn. In particular, never issue
		// turn/start against an active thread (Codex would implicitly steer it).
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			state, err := readState(filepath.Join(filepath.Dir(b.statePath), "tasks", receipt.TaskID+".json"))
			if err == nil && state.TaskID == receipt.TaskID && state.ThreadID == b.threadID && state.TurnID != "" {
				b.inputMu.Lock()
				b.rememberInputClientID(state.RunID, p.ClientUserMessageID)
				b.inputMu.Unlock()
				return rawResult(map[string]any{"turn": map[string]any{"id": state.TurnID, "status": "inProgress", "items": []any{}, "error": nil}})
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-ticker.C:
			}
			snapshot, err := b.control.TaskSnapshot(ctx, b.agentID, receipt.TaskID)
			if err != nil {
				return nil, err
			}
			if snapshot.Task.Status == domain.TaskStatusFailed || snapshot.Task.Status == domain.TaskStatusCanceled || snapshot.Task.Status == domain.TaskStatusUncertain {
				return nil, fmt.Errorf("OAX 任务 %s 已结束为 %s；请在工作台查看原因", receipt.TaskID, snapshot.Task.Status)
			}
		}
	case "turn/steer", "turn/interrupt":
		state, err := readState(b.statePath)
		if err != nil {
			return nil, err
		}
		expected := p.ExpectedTurnID
		if method == "turn/interrupt" {
			expected = p.TurnID
		}
		if state.ThreadID != b.threadID || state.TaskID == "" || state.TurnID == "" || expected != state.TurnID {
			return nil, errors.New("当前输入对应的轮次已变化；请刷新后重试")
		}
		snapshot, err := b.control.TaskSnapshot(ctx, b.agentID, state.TaskID)
		if err != nil {
			return nil, err
		}
		if method == "turn/interrupt" {
			_, err = b.control.Cancel(ctx, state.TaskID, api.CancelTaskRequest{Meta: api.CommandMeta{IdempotencyKey: idempotencyKey, ExpectedVersion: snapshot.Task.Version}})
			if err != nil {
				return nil, err
			}
			return rawResult(map[string]any{})
		}
		content, err := textInput(p.Input)
		if err != nil {
			return nil, err
		}
		// A fast Worker can commit the input before this API call returns.
		// Hold only input receipt projection until its authoritative Message ID
		// can be correlated with the foreground client's submission identity.
		b.inputMu.Lock()
		receipt, err := b.control.Steer(ctx, state.TaskID, api.CreateMessageRequest{Meta: api.CommandMeta{IdempotencyKey: idempotencyKey, ExpectedVersion: snapshot.Task.Version}, Content: content})
		if err == nil {
			b.rememberInputClientID(receipt.MessageID, p.ClientUserMessageID)
		}
		b.inputMu.Unlock()
		if err != nil {
			return nil, err
		}
		return rawResult(map[string]any{"turnId": state.TurnID})
	case "thread/resume":
		// Ignore TUI overrides: role/cwd and OAX Agent preferences remain
		// authoritative. Resuming here only attaches an observer.
		params, _ = json.Marshal(map[string]any{"threadId": b.threadID})
	default:
		if !readMethod(method) {
			return nil, fmt.Errorf("受管终端暂不支持 %s；任务/补充/取消请使用当前输入或 OAX 工作台", method)
		}
	}
	var result json.RawMessage
	if err := upstream.Call(ctx, method, params, &result); err != nil {
		return nil, err
	}
	return b.projectSettings(ctx, method, result)
}
func textInput(input []struct{ Type, Text string }) (string, error) {
	var parts []string
	for _, part := range input {
		if part.Type != "text" {
			return "", errors.New("受管终端目前接受文字输入；文件请在任务中引用工作目录路径")
		}
		parts = append(parts, part.Text)
	}
	text := strings.TrimSpace(strings.Join(parts, "\n"))
	if text == "" || len(text) > 1<<20 {
		return "", errors.New("请输入不超过 1 MiB 的任务文字")
	}
	return text, nil
}
func readMethod(method string) bool {
	switch method {
	case "initialize", "thread/read", "thread/list", "thread/loaded/list", "thread/turns/list", "thread/items/list", "thread/status/read", "model/list", "config/read", "configRequirements/read", "account/read", "account/rateLimits/read", "skills/list", "plugin/list", "mcpServerStatus/list", "collaborationMode/list", "experimentalFeature/list", "app/list":
		return true
	}
	return false
}
