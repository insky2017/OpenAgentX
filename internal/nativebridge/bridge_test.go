package nativebridge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"openagentx/internal/api"
	"openagentx/internal/domain"
	"openagentx/internal/runtime/codex"
)

// Isolated protocol peers below are D evidence; no real Codex execution claim.
type bridgeControlFixture struct {
	settings   domain.AgentModelSettings
	updates    []domain.AgentModelSettingsUpdate
	mu         sync.Mutex
	dispatches []api.CreateTaskRequest
	steers     []api.CreateMessageRequest
	cancels    []api.CancelTaskRequest
	snapshots  int
	onSnapshot func()
	onDispatch func()
	onSteer    func()
}

func (c *bridgeControlFixture) Dispatch(_ context.Context, r api.CreateTaskRequest) (api.CreateTaskResponse, error) {
	c.mu.Lock()
	c.dispatches = append(c.dispatches, r)
	onDispatch := c.onDispatch
	c.mu.Unlock()
	if onDispatch != nil {
		onDispatch()
	}
	return api.CreateTaskResponse{TaskID: "task-owned"}, nil
}
func (c *bridgeControlFixture) TaskSnapshot(_ context.Context, agent, task string) (api.ConsoleTaskSnapshot, error) {
	c.mu.Lock()
	c.snapshots++
	onSnapshot := c.onSnapshot
	c.mu.Unlock()
	if onSnapshot != nil {
		onSnapshot()
	}
	return api.ConsoleTaskSnapshot{Task: api.ConsoleTaskReadModel{TaskID: task, AgentID: agent, Version: 19, Status: domain.TaskStatusRunning}}, nil
}
func (c *bridgeControlFixture) Steer(_ context.Context, _ string, r api.CreateMessageRequest) (api.CreateMessageResponse, error) {
	c.mu.Lock()
	c.steers = append(c.steers, r)
	onSteer := c.onSteer
	c.mu.Unlock()
	if onSteer != nil {
		onSteer()
	}
	return api.CreateMessageResponse{MessageID: "message-owned"}, nil
}
func (c *bridgeControlFixture) Cancel(_ context.Context, _ string, r api.CancelTaskRequest) (api.CancelTaskResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancels = append(c.cancels, r)
	return api.CancelTaskResponse{}, nil
}

func (c *bridgeControlFixture) GetAgentModelSettings(_ context.Context, agent, backend string) (domain.AgentModelSettings, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.settings
	s.AgentID, s.BackendID = agent, backend
	return s, nil
}
func (c *bridgeControlFixture) SetAgentModelSettings(_ context.Context, agent string, r domain.AgentModelSettingsUpdate) (domain.AgentModelSettings, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if r.ExpectedVersion != c.settings.Version {
		return domain.AgentModelSettings{}, errors.New("CAS conflict")
	}
	valid := false
	for _, m := range c.settings.Models {
		valid = valid || m == r.Model
	}
	if !valid {
		return domain.AgentModelSettings{}, errors.New("unsupported model")
	}
	if r.Effort != "" {
		valid = false
		for _, e := range c.settings.ModelEfforts[r.Model] {
			valid = valid || e == r.Effort
		}
		if !valid {
			return domain.AgentModelSettings{}, errors.New("unsupported effort")
		}
	}
	c.updates = append(c.updates, r)
	c.settings.Model, c.settings.Effort = r.Model, r.Effort
	c.settings.Version++
	return c.settings, nil
}

func bridgeFixture(t *testing.T) (*bridge, *bridgeControlFixture) {
	t.Helper()
	c := &bridgeControlFixture{settings: domain.AgentModelSettings{Model: "model-default", Models: []string{"model-default", "model-other"}, ModelEfforts: map[string][]string{"model-default": {"high"}, "model-other": {"high", "max"}}}}
	b := &bridge{control: c, agentID: "agent-test", organizationID: "org-test", backendID: "codex-local",
		threadID: "thread-owned", statePath: filepath.Join(t.TempDir(), "state.json")}
	bridgeState(t, b, "task-owned", "turn-owned")
	return b, c
}

func bridgeState(t *testing.T, b *bridge, task, turn string) {
	t.Helper()
	raw, err := json.Marshal(engineState{ThreadID: b.threadID, TaskID: task, TurnID: turn, State: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b.statePath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	taskDir := filepath.Join(filepath.Dir(b.statePath), "tasks")
	if err := os.MkdirAll(taskDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, task+".json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestNativeBridgeTurnStartUsesDurableDispatchAndExactTaskTurn(t *testing.T) {
	for _, queued := range []bool{false, true} {
		name := "ready"
		if queued {
			name = "queued-behind-another-task"
		}
		t.Run(name, func(t *testing.T) {
			b, c := bridgeFixture(t)
			if queued {
				bridgeState(t, b, "task-previous", "turn-previous")
				if err := os.Remove(filepath.Join(filepath.Dir(b.statePath), "tasks", "task-owned.json")); err != nil {
					t.Fatal(err)
				}
				c.onSnapshot = func() { bridgeState(t, b, "task-owned", "turn-owned") }
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			// Nil upstream proves that native turn/start is never sent directly.
			result, err := b.request(ctx, nil, "turn/start", json.RawMessage(`{"threadId":"thread-owned","input":[{"type":"text","text":"first"},{"type":"text","text":"second"}]}`))
			if err != nil {
				t.Fatal(err)
			}
			if len(c.dispatches) != 1 {
				t.Fatalf("dispatch count=%d", len(c.dispatches))
			}
			req := c.dispatches[0]
			if req.TargetAgentID != b.agentID || req.OrganizationID != b.organizationID || req.Intent != domain.TaskIntentMutation || req.Content != "first\nsecond" || req.Meta.IdempotencyKey == "" || req.RuntimeSession == nil || req.RuntimeSession.BackendID != b.backendID || req.RuntimeSession.ProviderSessionID != b.threadID {
				t.Fatalf("dispatch=%+v", req)
			}
			var response struct {
				Turn struct {
					ID string `json:"id"`
				} `json:"turn"`
			}
			if err := json.Unmarshal(result, &response); err != nil || response.Turn.ID != "turn-owned" {
				t.Fatalf("turn response=%s err=%v", result, err)
			}
			if queued && c.snapshots == 0 {
				t.Fatal("returned a turn before observing the newly dispatched Task")
			}
		})
	}
}

func TestNativeBridgeSteerAndInterruptUseControlCAS(t *testing.T) {
	b, c := bridgeFixture(t)
	ctx := context.Background()
	if _, err := b.request(ctx, nil, "turn/steer", json.RawMessage(`{"threadId":"thread-owned","expectedTurnId":"turn-owned","input":[{"type":"text","text":"follow up"}]}`)); err != nil {
		t.Fatal(err)
	}
	if len(c.steers) != 1 || c.steers[0].Meta.ExpectedVersion != 19 || c.steers[0].Content != "follow up" || c.steers[0].Meta.IdempotencyKey == "" {
		t.Fatalf("steer=%+v", c.steers)
	}
	if _, err := b.request(ctx, nil, "turn/interrupt", json.RawMessage(`{"threadId":"thread-owned","turnId":"turn-owned"}`)); err != nil {
		t.Fatal(err)
	}
	if len(c.cancels) != 1 || c.cancels[0].Meta.ExpectedVersion != 19 || c.cancels[0].Meta.IdempotencyKey == "" {
		t.Fatalf("cancel=%+v", c.cancels)
	}
	for _, method := range []string{"turn/steer", "turn/interrupt"} {
		if _, err := b.request(ctx, nil, method, json.RawMessage(`{"threadId":"thread-owned","turnId":"turn-stale","expectedTurnId":"turn-stale","input":[{"type":"text","text":"wrong turn"}]}`)); err == nil {
			t.Fatalf("%s accepted stale turn", method)
		}
	}
	if len(c.steers) != 1 || len(c.cancels) != 1 || c.snapshots != 2 {
		t.Fatalf("stale turn reached control API: %+v", c)
	}
}

func TestNativeBridgeRejectsCrossThreadUnsupportedWritesAndNonText(t *testing.T) {
	b, c := bridgeFixture(t)
	for _, method := range []string{"turn/start", "turn/steer", "turn/interrupt", "thread/read", "thread/resume"} {
		if _, err := b.request(context.Background(), nil, method, json.RawMessage(`{"threadId":"thread-other","input":[{"type":"text","text":"wrong thread"}]}`)); err == nil {
			t.Fatalf("cross-thread %s accepted", method)
		}
	}
	for _, method := range []string{"thread/start", "thread/fork", "thread/rollback", "thread/archive", "config/value/write", "review/start", "command/exec", "account/login/start"} {
		if _, err := b.request(context.Background(), nil, method, json.RawMessage(`{}`)); err == nil {
			t.Fatalf("unmanaged write %s accepted", method)
		}
	}
	for _, input := range []string{`[]`, `[{"type":"image","url":"file:///tmp/example"}]`, `[{"type":"text","text":"  "}]`} {
		if _, err := b.request(context.Background(), nil, "turn/start", json.RawMessage(`{"threadId":"thread-owned","input":`+input+`}`)); err == nil {
			t.Fatalf("unsupported input accepted: %s", input)
		}
	}
	if len(c.dispatches)+len(c.steers)+len(c.cancels)+c.snapshots != 0 {
		t.Fatalf("rejected request caused control traffic: %+v", c)
	}
}

func bridgeUpstream(t *testing.T, events []codex.RPCMessage) (string, <-chan codex.RPCMessage) {
	t.Helper()
	received := make(chan codex.RPCMessage, 16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var request codex.RPCMessage
			if conn.ReadJSON(&request) != nil {
				return
			}
			received <- request
			if len(request.ID) == 0 {
				continue
			}
			if conn.WriteJSON(codex.RPCMessage{ID: request.ID, Result: json.RawMessage(`{"source":"actual-upstream-fixture","thread":{"id":"thread-owned"}}`)}) != nil {
				return
			}
			for _, event := range events {
				if conn.WriteJSON(event) != nil {
					return
				}
			}
		}
	}))
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http"), received
}

func TestNativeBridgeResumeStripsOverridesAndReturnsUpstreamFacts(t *testing.T) {
	b, _ := bridgeFixture(t)
	endpoint, received := bridgeUpstream(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rpc, err := codex.DialRPC(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer rpc.Close()
	result, err := b.request(ctx, rpc, "thread/resume", json.RawMessage(`{"threadId":"thread-owned","cwd":"/wrong","model":"wrong","approvalPolicy":"untrusted","developerInstructions":"wrong role"}`))
	if err != nil {
		t.Fatal(err)
	}
	request := <-received
	var params map[string]any
	if json.Unmarshal(request.Params, &params) != nil || !reflect.DeepEqual(params, map[string]any{"threadId": b.threadID}) {
		t.Fatalf("forwarded overrides=%s", request.Params)
	}
	if !strings.Contains(string(result), `"source":"actual-upstream-fixture"`) {
		t.Fatalf("response was not sourced from upstream: %s", result)
	}
}

func TestNativeBridgeNotificationBoundaryKeepsOnlyCurrentThread(t *testing.T) {
	b, _ := bridgeFixture(t)
	endpoint, _ := bridgeUpstream(t, []codex.RPCMessage{
		{ID: json.RawMessage(`55`), Method: "item/commandExecution/requestApproval", Params: json.RawMessage(`{"threadId":"thread-owned"}`)},
		{Method: "item/agentMessage/delta", Params: json.RawMessage(`{"threadId":"thread-other","delta":"OTHER-THREAD-OUTPUT"}`)},
		{Method: "thread/started", Params: json.RawMessage(`{"thread":{"id":"thread-other","title":"OTHER-THREAD-OUTPUT"}}`)},
		{Method: "codex/event/agent_message", Params: json.RawMessage(`{"id":"thread-other","msg":{"message":"OTHER-THREAD-OUTPUT"}}`)},
		{Method: "account/rateLimits/updated", Params: json.RawMessage(`{"source":"GLOBAL-STATE"}`)},
		{Method: "item/agentMessage/delta", Params: json.RawMessage(`{"threadId":"thread-owned","delta":"OWNED-THREAD-OUTPUT"}`)},
	})
	b.endpoint = endpoint
	server := httptest.NewServer(http.HandlerFunc(b.serve))
	defer server.Close()
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := client.WriteJSON(codex.RPCMessage{ID: json.RawMessage(`"tui-init"`), Method: "initialize", Params: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	gotResponse, gotOwned, gotGlobal := false, false, false
	for !gotResponse || !gotOwned || !gotGlobal {
		var event codex.RPCMessage
		if err := client.ReadJSON(&event); err != nil {
			t.Fatal(err)
		}
		if event.Method == "item/commandExecution/requestApproval" {
			t.Fatal("approval authority leaked to the native view")
		}
		if strings.Contains(string(event.Params), "OTHER-THREAD-OUTPUT") {
			t.Fatal("notification from another native thread reached current TUI")
		}
		gotResponse = gotResponse || string(event.ID) == `"tui-init"`
		gotOwned = gotOwned || strings.Contains(string(event.Params), "OWNED-THREAD-OUTPUT")
		gotGlobal = gotGlobal || strings.Contains(string(event.Params), "GLOBAL-STATE")
	}
}

func TestNativeBridgeClientMessageIdentityProducesStableDispatchKey(t *testing.T) {
	b, c := bridgeFixture(t)
	params := json.RawMessage(`{"threadId":"thread-owned","clientUserMessageId":"user-message-stable","input":[{"type":"text","text":"do this once"}]}`)
	for i := 0; i < 2; i++ {
		if _, err := b.request(context.Background(), nil, "turn/start", params); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.dispatches) != 2 || c.dispatches[0].Meta.IdempotencyKey == "" || c.dispatches[0].Meta.IdempotencyKey != c.dispatches[1].Meta.IdempotencyKey {
		t.Fatalf("same native message must use the same durable command key: %+v", c.dispatches)
	}
}

// A real WebSocket bridge with a programmable isolated upstream. emit is
// synchronized with server responses so the fixture itself never races writes.
func bridgeTransport(t *testing.T, b *bridge) (*websocket.Conn, func(...codex.RPCMessage)) {
	t.Helper()
	ready := make(chan struct{})
	var upstream *websocket.Conn
	var writes sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		upstream = conn
		close(ready)
		for {
			var request codex.RPCMessage
			if conn.ReadJSON(&request) != nil {
				return
			}
			if len(request.ID) > 0 {
				writes.Lock()
				err = conn.WriteJSON(codex.RPCMessage{ID: request.ID, Result: json.RawMessage(`{}`)})
				writes.Unlock()
				if err != nil {
					return
				}
			}
		}
	}))
	t.Cleanup(server.Close)
	b.endpoint = "ws" + strings.TrimPrefix(server.URL, "http")
	front := httptest.NewServer(http.HandlerFunc(b.serve))
	t.Cleanup(front.Close)
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(front.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	client.SetReadDeadline(time.Now().Add(4 * time.Second))
	if err := client.WriteJSON(codex.RPCMessage{ID: json.RawMessage(`"init"`), Method: "initialize", Params: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	var response codex.RPCMessage
	if err := client.ReadJSON(&response); err != nil || string(response.ID) != `"init"` || response.Error != nil {
		t.Fatalf("initialize response=%+v err=%v", response, err)
	}
	return client, func(events ...codex.RPCMessage) {
		<-ready
		writes.Lock()
		defer writes.Unlock()
		for _, event := range events {
			if err := upstream.WriteJSON(event); err != nil {
				t.Errorf("fixture notification write: %v", err)
				return
			}
		}
	}
}

func TestNativeBridgeRepeatedConnectionRequestDispatchesOnce(t *testing.T) {
	b, c := bridgeFixture(t)
	client, _ := bridgeTransport(t, b)
	request := codex.RPCMessage{ID: json.RawMessage(`"same-request"`), Method: "turn/start", Params: json.RawMessage(`{"threadId":"thread-owned","input":[{"type":"text","text":"once"}]}`)}
	var original json.RawMessage
	for i := 0; i < 2; i++ {
		if err := client.WriteJSON(request); err != nil {
			t.Fatal(err)
		}
		var response codex.RPCMessage
		if err := client.ReadJSON(&response); err != nil || response.Error != nil || string(response.ID) != string(request.ID) {
			t.Fatalf("response=%+v err=%v", response, err)
		}
		if i == 0 {
			original = response.Result
		} else if string(original) != string(response.Result) {
			t.Fatalf("replay changed receipt: original=%s now=%s", original, response.Result)
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.dispatches) != 1 {
		t.Fatalf("same connection/request ID dispatched %d times", len(c.dispatches))
	}
}

func TestNativeBridgeFastTerminalCannotPrecedeStartResponse(t *testing.T) {
	b, c := bridgeFixture(t)
	bridgeState(t, b, "task-previous", "turn-previous")
	if err := os.Remove(filepath.Join(filepath.Dir(b.statePath), "tasks", "task-owned.json")); err != nil {
		t.Fatal(err)
	}
	client, emit := bridgeTransport(t, b)
	// Dispatch may wake the Worker before the control API response is returned.
	// Produce an immediate terminal notification inside that window.
	c.mu.Lock()
	c.onDispatch = func() {
		bridgeState(t, b, "task-owned", "turn-owned")
		emit(codex.RPCMessage{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"thread-owned","turn":{"id":"turn-owned","status":"completed"}}`)})
		// Wait for the notification reader to run, not for a model or tool.
		time.Sleep(30 * time.Millisecond)
	}
	c.mu.Unlock()
	request := codex.RPCMessage{ID: json.RawMessage(`"fast-start"`), Method: "turn/start", Params: json.RawMessage(`{"threadId":"thread-owned","input":[{"type":"text","text":"fast work"}]}`)}
	if err := client.WriteJSON(request); err != nil {
		t.Fatal(err)
	}
	var first, second codex.RPCMessage
	if err := client.ReadJSON(&first); err != nil {
		t.Fatal(err)
	}
	if first.Method == "turn/completed" || string(first.ID) != string(request.ID) || first.Error != nil {
		t.Fatalf("terminal arrived before start response: %+v", first)
	}
	if err := client.ReadJSON(&second); err != nil || second.Method != "turn/completed" || !strings.Contains(string(second.Params), "turn-owned") {
		t.Fatalf("buffered terminal missing or wrong: %+v err=%v", second, err)
	}
}

func TestNativeBridgePendingQueuedStartPreservesCurrentTurnNotifications(t *testing.T) {
	b, c := bridgeFixture(t)
	bridgeState(t, b, "task-previous", "turn-previous")
	if err := os.Remove(filepath.Join(filepath.Dir(b.statePath), "tasks", "task-owned.json")); err != nil {
		t.Fatal(err)
	}
	dispatched := make(chan struct{})
	c.onDispatch = func() { close(dispatched) }
	client, emit := bridgeTransport(t, b)
	request := codex.RPCMessage{ID: json.RawMessage(`"queued-start"`), Method: "turn/start", Params: json.RawMessage(`{"threadId":"thread-owned","input":[{"type":"text","text":"queued work"}]}`)}
	if err := client.WriteJSON(request); err != nil {
		t.Fatal(err)
	}
	select {
	case <-dispatched:
	case <-time.After(time.Second):
		t.Fatal("queued start never reached control API")
	}
	emit(codex.RPCMessage{Method: "item/agentMessage/delta", Params: json.RawMessage(`{"threadId":"thread-owned","turnId":"turn-previous","delta":"CURRENT-TURN-STILL-VISIBLE"}`)})
	var event codex.RPCMessage
	if err := client.ReadJSON(&event); err != nil || !strings.Contains(string(event.Params), "CURRENT-TURN-STILL-VISIBLE") {
		t.Fatalf("pending request blocked current turn: %+v err=%v", event, err)
	}
	// Closing the native view only ends observation; it must not cancel the Task.
	client.Close()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.cancels) != 0 {
		t.Fatalf("closing view canceled background work: %+v", c.cancels)
	}
}
