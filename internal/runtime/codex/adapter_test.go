package codex

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

type fakeServer struct {
	t            *testing.T
	server       *httptest.Server
	mu           sync.Mutex
	methods      []string
	params       []map[string]any
	onTurn       func(*websocket.Conn, RPCMessage)
	activeReads  int
	closeOnStart bool
}

func newFake(t *testing.T) *fakeServer {
	f := &fakeServer{t: t}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			var m RPCMessage
			if err = c.ReadJSON(&m); err != nil {
				return
			}
			if len(m.ID) == 0 || m.Method == "" {
				continue
			}
			var p map[string]any
			_ = json.Unmarshal(m.Params, &p)
			f.mu.Lock()
			f.methods = append(f.methods, m.Method)
			f.params = append(f.params, p)
			f.mu.Unlock()
			result := any(map[string]any{})
			switch m.Method {
			case "thread/start", "thread/resume", "thread/read":
				state := "idle"
				f.mu.Lock()
				if f.activeReads > 0 {
					state = "active"
					f.activeReads--
				}
				f.mu.Unlock()
				result = map[string]any{"thread": map[string]any{"id": "native-thread", "status": map[string]any{"type": state}}}
			case "turn/start":
				if f.closeOnStart {
					return
				}
				if f.onTurn != nil {
					f.onTurn(c, m)
					continue
				}
				// Notifications deliberately arrive before the start response.
				writeEvent(c, "item/completed", map[string]any{"threadId": "native-thread", "turnId": "other-turn", "item": map[string]any{"type": "agentMessage", "id": "wrong", "text": "WRONG", "phase": "final_answer"}})
				writeEvent(c, "item/completed", map[string]any{"threadId": "native-thread", "turnId": "native-turn", "item": map[string]any{"type": "agentMessage", "id": "answer", "text": "Complete authoritative reply", "phase": "final_answer"}})
				writeEvent(c, "turn/completed", map[string]any{"threadId": "native-thread", "turn": map[string]any{"id": "native-turn", "status": "completed", "items": []map[string]any{{"type": "agentMessage", "text": "truncated summary"}}}})
				result = map[string]any{"turn": map[string]any{"id": "native-turn", "status": "inProgress"}}
			}
			writeResult(c, m.ID, result)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}
func writeEvent(c *websocket.Conn, method string, params any) {
	raw, _ := json.Marshal(params)
	_ = c.WriteJSON(RPCMessage{Method: method, Params: raw})
}
func writeResult(c *websocket.Conn, id json.RawMessage, result any) {
	raw, _ := json.Marshal(result)
	_ = c.WriteJSON(RPCMessage{ID: id, Result: raw})
}
func (f *fakeServer) adapter(t *testing.T, config func(*Config)) *Adapter {
	cfg := Config{Binary: "/bin/true", Models: []string{"test-model"}, WorkingDir: t.TempDir(), StateDir: t.TempDir(), Endpoint: "ws" + strings.TrimPrefix(f.server.URL, "http")}
	if config != nil {
		config(&cfg)
	}
	a, err := NewAdapter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}
func fixtureRequest() openruntime.TurnRequest {
	return openruntime.TurnRequest{Task: domain.Task{ID: "task-probe", Content: "Say hello"}, RunAttempt: domain.RunAttempt{ID: "run-probe"}, Execution: domain.ResolvedExecutionSpec{Version: 1, Spec: domain.ExecutionSpec{AdapterID: AdapterID, BackendID: "local", Model: "test-model", Reasoning: domain.ReasoningSpec{Mode: domain.ReasoningBackendDefault}, Session: domain.SessionSpec{Mode: domain.SessionModeNew}, Timeout: time.Minute}}}
}
func successfulSink() openruntime.EventSink {
	return openruntime.EventSinkFunc(func(context.Context, openruntime.RuntimeEvent) error { return nil })
}

func TestEarlyBindingBeforeTurnAndAuthoritativeReply(t *testing.T) {
	f := newFake(t)
	a := f.adapter(t, nil)
	bound := false
	sink := openruntime.EventSinkFunc(func(_ context.Context, e openruntime.RuntimeEvent) error {
		if e.Type == "session.bound" {
			f.mu.Lock()
			defer f.mu.Unlock()
			for _, method := range f.methods {
				if method == "turn/start" {
					t.Error("execution started before durable binding")
				}
			}
			var p map[string]string
			_ = json.Unmarshal(e.Payload, &p)
			if p["provider_session_id"] != "native-thread" || p["source"] != "new" {
				t.Errorf("bad binding: %s", e.Payload)
			}
			bound = true
		}
		return nil
	})
	h, err := a.StartTurn(context.Background(), fixtureRequest(), sink)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := h.Wait(ctx)
	if err != nil || !bound || result.Status != openruntime.TurnResultSucceeded || !result.FinalReply || result.Result != "Complete authoritative reply" {
		t.Fatalf("result=%+v err=%v bound=%v", result, err, bound)
	}
	raw, err := os.ReadFile(filepath.Join(a.config.StateDir, "tasks", "task-probe.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s SessionState
	_ = json.Unmarshal(raw, &s)
	if s.TurnID != "native-turn" || s.RunID != "run-probe" {
		t.Fatalf("mapping=%s", raw)
	}
}

func TestBindingFailureNeverDispatches(t *testing.T) {
	f := newFake(t)
	a := f.adapter(t, nil)
	_, err := a.StartTurn(context.Background(), fixtureRequest(), openruntime.EventSinkFunc(func(context.Context, openruntime.RuntimeEvent) error { return errors.New("fence rejected") }))
	if err == nil {
		t.Fatal("wanted binding failure")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.methods {
		if m == "turn/start" {
			t.Fatal("model dispatched after binding rejection")
		}
	}
}

func TestLostStartAckDoesNotRetry(t *testing.T) {
	f := newFake(t)
	f.closeOnStart = true
	a := f.adapter(t, nil)
	h, err := a.StartTurn(context.Background(), fixtureRequest(), successfulSink())
	if err != nil {
		t.Fatal(err)
	}
	r, err := h.Wait(context.Background())
	if err == nil || r.Status != openruntime.TurnResultUncertain || r.FinalReply {
		t.Fatalf("result=%+v err=%v", r, err)
	}
	if _, err = a.StartTurn(context.Background(), fixtureRequest(), successfulSink()); err == nil {
		t.Fatal("unknown prior turn allowed new execution")
	}
	if err = a.ApplyNetworkPolicy(domain.NetworkPolicy{Mode: domain.NetworkDirect}); err == nil {
		t.Fatal("unknown prior turn allowed network change")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, m := range f.methods {
		if m == "turn/start" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("dispatch count=%d", n)
	}
}

func TestMalformedCompletionFailsClosed(t *testing.T) {
	for _, variant := range []string{"missing-turn-id", "missing-item-id", "terminal-error", "no-final", "missing-start-id"} {
		t.Run(variant, func(t *testing.T) {
			f := newFake(t)
			f.onTurn = func(c *websocket.Conn, m RPCMessage) {
				turnID := "native-turn"
				itemID := "answer"
				if variant == "missing-turn-id" {
					turnID = ""
				}
				if variant == "missing-item-id" {
					itemID = ""
				}
				if variant != "no-final" {
					writeEvent(c, "item/completed", map[string]any{"threadId": "native-thread", "turnId": turnID, "item": map[string]any{"id": itemID, "type": "agentMessage", "phase": "final_answer", "text": "looks successful"}})
				}
				terminal := map[string]any{"id": "native-turn", "status": "completed"}
				if variant == "terminal-error" {
					terminal["error"] = map[string]any{"message": "provider failed"}
				}
				writeEvent(c, "turn/completed", map[string]any{"threadId": "native-thread", "turn": terminal})
				startID := "native-turn"
				if variant == "missing-start-id" {
					startID = ""
				}
				writeResult(c, m.ID, map[string]any{"turn": map[string]any{"id": startID}})
			}
			a := f.adapter(t, nil)
			h, err := a.StartTurn(context.Background(), fixtureRequest(), successfulSink())
			if err != nil {
				t.Fatal(err)
			}
			r, err := h.Wait(context.Background())
			if err == nil || r.Status != openruntime.TurnResultUncertain || r.FinalReply {
				t.Fatalf("%s: %+v %v", variant, r, err)
			}
			if _, err = a.StartTurn(context.Background(), fixtureRequest(), successfulSink()); err == nil {
				t.Fatal("uncertain host not quarantined")
			}
		})
	}
}

func TestNativeApprovalUsesPersistedApprovalIdentity(t *testing.T) {
	f := newFake(t)
	f.onTurn = func(c *websocket.Conn, m RPCMessage) {
		writeResult(c, m.ID, map[string]any{"turn": map[string]any{"id": "native-turn"}})
		params, _ := json.Marshal(map[string]any{"threadId": "native-thread", "turnId": "native-turn", "itemId": "command-1", "command": "touch proof"})
		_ = c.WriteJSON(RPCMessage{ID: json.RawMessage("42"), Method: "item/commandExecution/requestApproval", Params: params})
		var reply RPCMessage
		if c.ReadJSON(&reply) != nil {
			return
		}
		if string(reply.ID) != "42" || string(reply.Result) != `{"decision":"accept"}` {
			t.Errorf("approval reply=%+v", reply)
		}
		writeEvent(c, "item/completed", map[string]any{"threadId": "native-thread", "turnId": "native-turn", "item": map[string]any{"id": "answer", "type": "agentMessage", "phase": "final_answer", "text": "approved"}})
		writeEvent(c, "turn/completed", map[string]any{"threadId": "native-thread", "turn": map[string]any{"id": "native-turn", "status": "completed"}})
	}
	a := f.adapter(t, nil)
	approval := make(chan string, 1)
	sink := openruntime.EventSinkFunc(func(_ context.Context, e openruntime.RuntimeEvent) error {
		if e.Type == "approval.requested" {
			var p map[string]any
			_ = json.Unmarshal(e.Payload, &p)
			approval <- p["approval_request_id"].(string)
		}
		return nil
	})
	h, err := a.StartTurn(context.Background(), fixtureRequest(), sink)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-approval:
		if err = h.DecideApproval(context.Background(), domain.ApprovalDecision{ApprovalRequestID: id, Decision: domain.ApprovalDecisionApprove}); err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("missing persisted approval")
	}
	if _, err = h.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestExternalActiveThreadWaitsInsteadOfSteering(t *testing.T) {
	f := newFake(t)
	f.activeReads = 1
	a := f.adapter(t, func(c *Config) { c.ThreadID = "native-thread" })
	h, err := a.StartTurn(context.Background(), fixtureRequest(), successfulSink())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	read, start := -1, -1
	for i, m := range f.methods {
		if m == "thread/read" {
			read = i
		}
		if m == "turn/start" {
			start = i
		}
	}
	if read < 0 || start < read {
		t.Fatalf("methods=%v", f.methods)
	}
}

func TestJoinedBusyDoesNotBlockHealthOrCancelOldWork(t *testing.T) {
	f := newFake(t)
	f.activeReads = 100
	a := f.adapter(t, func(c *Config) { c.ThreadID = "native-thread" })
	if err := a.Health(context.Background()); err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("busy health=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := a.StartTurn(ctx, fixtureRequest(), successfulSink()); done <- err }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		a.mu.Lock()
		starting := a.starting
		a.mu.Unlock()
		if starting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("start did not enter admission")
		}
		time.Sleep(time.Millisecond)
	}
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer probeCancel()
	if err := a.Health(probeCtx); err != nil {
		t.Fatalf("heartbeat health blocked by waiting admission: %v", err)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled admission succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("admission cancellation blocked")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.methods {
		if m == "turn/start" || m == "turn/interrupt" {
			t.Fatalf("waiting admission modified old native work: %s", m)
		}
	}
}

func TestHandoffNeverOverwrittenAndFrozenRoleInjected(t *testing.T) {
	f := newFake(t)
	handoff := filepath.Join(t.TempDir(), "handoff.md")
	_ = os.WriteFile(handoff, []byte("PREVIOUS_CONTEXT"), 0600)
	a := f.adapter(t, func(c *Config) { c.HandoffFile = handoff })
	req := fixtureRequest()
	req.Execution.AgentInput = &domain.AgentExecutionInput{ProfileVersion: 2, WorkspaceRoot: a.config.WorkingDir, InstructionsContent: "FROZEN_ROLE", InstructionsSHA256: "abc"}
	h, err := a.StartTurn(context.Background(), req, successfulSink())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = h.Wait(context.Background())
	data, _ := os.ReadFile(handoff)
	if string(data) != "PREVIOUS_CONTEXT" {
		t.Fatal("handoff overwritten")
	}
	if a.StatePath() == handoff {
		t.Fatal("handoff used as state")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, m := range f.methods {
		if m == "turn/start" {
			raw, _ := json.Marshal(f.params[i])
			if !strings.Contains(string(raw), "PREVIOUS_CONTEXT") || !strings.Contains(string(raw), "FROZEN_ROLE") {
				t.Fatalf("prompt=%s", raw)
			}
		}
	}
}

func TestCancellationWaitsForExactTerminal(t *testing.T) {
	f := newFake(t)
	f.onTurn = func(c *websocket.Conn, m RPCMessage) {
		writeResult(c, m.ID, map[string]any{"turn": map[string]any{"id": "native-turn", "status": "inProgress"}})
		var cancel RPCMessage
		if c.ReadJSON(&cancel) != nil {
			return
		}
		if cancel.Method != "turn/interrupt" {
			t.Errorf("method=%s", cancel.Method)
		}
		var p map[string]string
		_ = json.Unmarshal(cancel.Params, &p)
		if p["turnId"] != "native-turn" {
			t.Error("wrong cancel target")
		}
		writeResult(c, cancel.ID, map[string]any{})
		writeEvent(c, "turn/completed", map[string]any{"threadId": "native-thread", "turn": map[string]any{"id": "native-turn", "status": "interrupted"}})
	}
	a := f.adapter(t, nil)
	h, err := a.StartTurn(context.Background(), fixtureRequest(), successfulSink())
	if err != nil {
		t.Fatal(err)
	}
	if err = h.RequestCancel(context.Background()); err != nil {
		t.Fatal(err)
	}
	r, err := h.Wait(context.Background())
	if err != nil || r.Status != openruntime.TurnResultCanceled || r.FinalReply {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestInterruptedUnfinishedOtherToolsRemainUncertain(t *testing.T) {
	for _, kind := range []string{"fileChange", "mcpToolCall", "dynamicToolCall"} {
		t.Run(kind, func(t *testing.T) {
			f := newFake(t)
			f.onTurn = func(c *websocket.Conn, m RPCMessage) {
				writeResult(c, m.ID, map[string]any{"turn": map[string]any{"id": "native-turn", "status": "inProgress"}})
				writeEvent(c, "item/started", map[string]any{"threadId": "native-thread", "turnId": "native-turn", "item": map[string]any{"id": "tool", "type": kind}})
				writeEvent(c, "turn/completed", map[string]any{"threadId": "native-thread", "turn": map[string]any{"id": "native-turn", "status": "interrupted"}})
			}
			a := f.adapter(t, nil)
			h, err := a.StartTurn(context.Background(), fixtureRequest(), successfulSink())
			if err != nil {
				t.Fatal(err)
			}
			r, err := h.Wait(context.Background())
			if err == nil || r.Status != openruntime.TurnResultUncertain || !a.unresolved {
				t.Fatalf("unfinished tool was reported as stopped: %+v %v", r, err)
			}
		})
	}
}

func TestExternalNetworkCannotBePretendedChanged(t *testing.T) {
	f := newFake(t)
	a := f.adapter(t, nil)
	if err := a.ApplyNetworkPolicy(domain.NetworkPolicy{Mode: domain.NetworkDirect}); err == nil {
		t.Fatal("external host must reject environment change")
	}
}

func TestExternalNetworkRegistrationMetadataDoesNotRestartHost(t *testing.T) {
	f := newFake(t)
	a := f.adapter(t, nil)
	if err := a.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := a.client
	if err := a.ApplyNetworkPolicy(domain.NetworkPolicy{Mode: domain.NetworkInherit}); err != nil {
		t.Fatal(err)
	}
	if a.client != before {
		t.Fatal("metadata update restarted host")
	}
}

func TestExpiredFrozenDeadlinePreventsDispatch(t *testing.T) {
	f := newFake(t)
	a := f.adapter(t, nil)
	r := fixtureRequest()
	r.Execution.DeadlineAt = time.Now().Add(-time.Second)
	if _, err := a.StartTurn(context.Background(), r, successfulSink()); err == nil {
		t.Fatal("expired deadline accepted")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.methods {
		if m == "turn/start" {
			t.Fatal("dispatch after deadline")
		}
	}
}
