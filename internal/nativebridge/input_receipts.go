package nativebridge

import (
	"bytes"
	"encoding/json"

	"openagentx/internal/runtime/codex"
)

// The Worker uses durable OAX Run/Message IDs for Codex submission identity.
// The TUI must see its own client ID again to retire pending input. Keep the
// mapping on the bridge (not the websocket) so reconnect/read recovery sees
// the same IDs. Never infer acknowledgement from equal text or an API receipt:
// only project actual upstream committed/queued input records.
// Caller holds inputMu.
func (b *bridge) rememberInputClientID(runtimeID, clientID string) {
	if runtimeID == "" || clientID == "" {
		return
	}
	if b.inputClientIDs == nil {
		b.inputClientIDs = make(map[string]string)
	}
	b.inputClientIDs[runtimeID] = clientID
}

func (b *bridge) projectInputReceipts(message codex.RPCMessage) codex.RPCMessage {
	project := func(raw json.RawMessage) json.RawMessage {
		var value any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if decoder.Decode(&value) != nil {
			return raw
		}
		changed := false
		var visit func(any)
		visit = func(value any) {
			switch v := value.(type) {
			case []any:
				for _, item := range v {
					visit(item)
				}
			case map[string]any:
				field := ""
				switch v["type"] {
				case "userMessage":
					field = "clientId"
				case "UserMessage", "user_message":
					field = "client_id"
				}
				if field != "" {
					if runtimeID, ok := v[field].(string); ok {
						b.inputMu.Lock()
						clientID := b.inputClientIDs[runtimeID]
						b.inputMu.Unlock()
						if clientID != "" && clientID != runtimeID {
							v[field] = clientID
							changed = true
						}
					}
				}
				for _, child := range v {
					visit(child)
				}
			}
		}
		visit(value)
		if !changed {
			return raw
		}
		projected, err := json.Marshal(value)
		if err != nil {
			return raw
		}
		return projected
	}
	message.Params = project(message.Params)
	message.Result = project(message.Result)
	return message
}
