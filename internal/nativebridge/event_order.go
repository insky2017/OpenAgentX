package nativebridge

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"

	"openagentx/internal/runtime/codex"
)

const maxNativeReceipts = 256
const maxHeldNotifications = 2048
const maxHeldNotificationBytes = 8 << 20

type nativeReceipt struct {
	signature [32]byte
	done      chan struct{}
	response  codex.RPCMessage
}

func nativeWrite(method string) bool {
	return method == "turn/start" || method == "turn/steer" || method == "turn/interrupt" || method == "thread/settings/update" || method == "config/batchWrite"
}

func notificationIdentity(event codex.RPCMessage) (thread, turn string) {
	var p struct {
		ThreadID string          `json:"threadId"`
		TurnID   string          `json:"turnId"`
		ID       json.RawMessage `json:"id"`
		Thread   struct {
			ID string `json:"id"`
		} `json:"thread"`
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if json.Unmarshal(event.Params, &p) != nil {
		return "", ""
	}
	thread, turn = p.ThreadID, p.TurnID
	if thread == "" {
		thread = p.Thread.ID
	}
	if thread == "" && strings.HasPrefix(event.Method, "codex/event") {
		_ = json.Unmarshal(p.ID, &thread)
	}
	if turn == "" {
		turn = p.Turn.ID
	}
	return thread, turn
}

func notificationForThread(event codex.RPCMessage, threadID string) bool {
	var identity struct {
		ThreadID string `json:"threadId"`
		Thread   struct {
			ID string `json:"id"`
		} `json:"thread"`
		ID json.RawMessage `json:"id"`
	}
	if len(event.Params) > 0 && json.Unmarshal(event.Params, &identity) != nil {
		return false
	}
	thread := identity.ThreadID
	if thread == "" {
		thread = identity.Thread.ID
	}
	if strings.HasPrefix(event.Method, "codex/event") && len(identity.ID) > 0 {
		var legacy string
		if json.Unmarshal(identity.ID, &legacy) != nil {
			return false
		}
		if thread == "" {
			thread = legacy
		}
	}
	return thread == "" || thread == threadID
}

// turnEventOrder bridges the deliberate delay between durable OAX dispatch and
// the native turn/start response. Existing turns continue streaming; only new
// turn notifications which may belong to an unacknowledged dispatch are held.
type turnEventOrder struct {
	mu        sync.Mutex
	bridge    *bridge
	send      func(codex.RPCMessage) error
	pending   map[string]string // RPC request ID -> dispatched Task ID, or not yet known
	known     map[string]bool   // turns whose start was already visible to this terminal
	held      []codex.RPCMessage
	heldBytes int
}

func newTurnEventOrder(b *bridge, send func(codex.RPCMessage) error) *turnEventOrder {
	return &turnEventOrder{bridge: b, send: send, pending: map[string]string{}, known: map[string]bool{}}
}

func (o *turnEventOrder) begin(requestID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if state, err := readState(o.bridge.statePath); err == nil && state.ThreadID == o.bridge.threadID && state.TurnID != "" {
		// Do not reclassify a pending native dispatch as an already visible turn
		// when another turn/start arrives while the first is awaiting its receipt.
		pendingTask := false
		for _, task := range o.pending {
			if task == "" || task == state.TaskID {
				pendingTask = true
			}
		}
		if !pendingTask {
			o.known[state.TurnID] = true
		}
	}
	o.pending[requestID] = ""
}

func (o *turnEventOrder) dispatched(requestID, taskID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, ok := o.pending[requestID]; ok {
		o.pending[requestID] = taskID
	}
}

// shouldHold is called under mu. An unmatched turn is held only while one of
// the pending dispatches still lacks its Task/turn mapping. An unrelated active
// Task found in the shared state is allowed through immediately.
func (o *turnEventOrder) shouldHold(turnID string) bool {
	if turnID == "" || o.known[turnID] || len(o.pending) == 0 {
		return false
	}
	unmapped := false
	for _, taskID := range o.pending {
		if taskID == "" {
			unmapped = true
			continue
		}
		state, err := readState(filepath.Join(filepath.Dir(o.bridge.statePath), "tasks", taskID+".json"))
		if err != nil || state.TurnID == "" {
			unmapped = true
			continue
		}
		if state.ThreadID == o.bridge.threadID && state.TurnID == turnID {
			return true
		}
	}
	if unmapped {
		if state, err := readState(o.bridge.statePath); err == nil && state.ThreadID == o.bridge.threadID && state.TurnID == turnID {
			knownTask := true
			for _, taskID := range o.pending {
				if taskID == "" || taskID == state.TaskID {
					knownTask = false
				}
			}
			if knownTask {
				return false
			}
		}
	}
	return unmapped
}

func (o *turnEventOrder) notification(event codex.RPCMessage) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	_, turnID := notificationIdentity(event)
	if !o.shouldHold(turnID) {
		return o.send(event)
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if len(o.held) >= maxHeldNotifications || o.heldBytes+len(raw) > maxHeldNotificationBytes {
		return errors.New("native turn notification buffer exceeded; reopen the terminal to inspect durable work")
	}
	o.held = append(o.held, event)
	o.heldBytes += len(raw)
	return nil
}

func (o *turnEventOrder) reply(requestID string, response codex.RPCMessage) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	// Serialize the acknowledgement with every event release. A fast completed
	// turn must never appear to start again after its terminal notification.
	if err := o.send(response); err != nil {
		return err
	}
	var result struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if response.Error == nil && json.Unmarshal(response.Result, &result) == nil && result.Turn.ID != "" {
		o.known[result.Turn.ID] = true
	}
	delete(o.pending, requestID)
	held := o.held
	o.held = nil
	o.heldBytes = 0
	for _, event := range held {
		_, turnID := notificationIdentity(event)
		if o.shouldHold(turnID) {
			raw, _ := json.Marshal(event)
			o.held = append(o.held, event)
			o.heldBytes += len(raw)
			continue
		}
		if err := o.send(event); err != nil {
			return err
		}
	}
	return nil
}
