package nativebridge

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"openagentx/internal/runtime/codex"
)

// Golden protocol integration, not real-model E2E. The userMessage shape is
// from the installed Codex 0.160.1 read-only thread/items/list response.
func TestNativeBridgeCommittedSteerPreservesSubmissionIdentity(t *testing.T) {
	b, c := bridgeFixture(t)
	client, emit := bridgeTransport(t, b)
	raw, err := os.ReadFile("testdata/codex-0.160.1-user-message.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden map[string]any
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	golden["threadId"] = b.threadID
	params, _ := json.Marshal(golden)
	committed := codex.RPCMessage{Method: "item/completed", Params: params}
	c.onSteer = func() {
		// Commit before the Control API returns its Message ID. This is the
		// window in which a notification-only mapping loses the acknowledgement.
		emit(committed)
		time.Sleep(30 * time.Millisecond)
	}
	request := codex.RPCMessage{ID: json.RawMessage(`"steer-once"`), Method: "turn/steer", Params: json.RawMessage(`{"threadId":"thread-owned","expectedTurnId":"turn-owned","clientUserMessageId":"tui-submission","input":[{"type":"text","text":"follow up"}]}`)}
	if err := client.WriteJSON(request); err != nil {
		t.Fatal(err)
	}
	responseSeen, committedSeen := false, false
	for !responseSeen || !committedSeen {
		var event codex.RPCMessage
		if err := client.ReadJSON(&event); err != nil {
			t.Fatal(err)
		}
		if event.Method == "item/completed" {
			if !strings.Contains(string(event.Params), `"clientId":"tui-submission"`) || !strings.Contains(string(event.Params), `"id":"item-owned"`) {
				t.Fatalf("commit lost identity or changed the upstream item ID: %s", event.Params)
			}
			committedSeen = true
		} else if string(event.ID) == string(request.ID) {
			if event.Error != nil {
				t.Fatal(event.Error)
			}
			responseSeen = true
		}
	}
	if err := client.WriteJSON(request); err != nil {
		t.Fatal(err)
	}
	var replay codex.RPCMessage
	if err := client.ReadJSON(&replay); err != nil || replay.Error != nil || string(replay.ID) != string(request.ID) {
		t.Fatalf("retry: %+v, %v", replay, err)
	}
	c.mu.Lock()
	if len(c.steers) != 1 {
		t.Fatalf("retry created %d messages", len(c.steers))
	}
	c.mu.Unlock()
	// Reconnect recovery reads must correlate the same committed item. No
	// acknowledgement is manufactured merely because a message was enqueued.
	read := b.projectInputReceipts(codex.RPCMessage{Result: json.RawMessage(`{"data":[` + string(raw) + `],"nextCursor":null}`)})
	if !strings.Contains(string(read.Result), `"clientId":"tui-submission"`) {
		t.Fatalf("read recovery lost identity: %s", read.Result)
	}
	other := codex.RPCMessage{Method: "item/completed", Params: json.RawMessage(`{"item":{"type":"userMessage","id":"different-item","clientId":"different-submission","content":[{"type":"text","text":"follow up"}]}}`)}
	if got := b.projectInputReceipts(other); string(got.Params) != string(other.Params) {
		t.Fatal("equal text was incorrectly treated as duplicate input")
	}
}

func TestNativeBridgeLegacyInputReceiptsPreserveClientID(t *testing.T) {
	b, _ := bridgeFixture(t)
	b.rememberInputClientID("message-owned", "tui-submission")
	for _, kind := range []string{"UserMessage", "user_message"} {
		event := codex.RPCMessage{Method: "codex/event/item_completed", Params: json.RawMessage(`{"id":"thread-owned","msg":{"type":"item_completed","item":{"type":"` + kind + `","client_id":"message-owned","id":"item-owned"}}}`)}
		if got := b.projectInputReceipts(event); !strings.Contains(string(got.Params), `"client_id":"tui-submission"`) {
			t.Fatalf("legacy input not projected: %s", got.Params)
		}
	}
}

func TestNativeBridgePendingReceiptDoesNotBlockWorkOrInventCommit(t *testing.T) {
	b, c := bridgeFixture(t)
	client, emit := bridgeTransport(t, b)
	release := make(chan struct{})
	c.onSteer = func() {
		emit(codex.RPCMessage{Method: "item/agentMessage/delta", Params: json.RawMessage(`{"threadId":"thread-owned","turnId":"turn-owned","delta":"still working"}`)})
		<-release
	}
	request := codex.RPCMessage{ID: json.RawMessage(`"queued"`), Method: "turn/steer", Params: json.RawMessage(`{"threadId":"thread-owned","expectedTurnId":"turn-owned","clientUserMessageId":"tui-queued","input":[{"type":"text","text":"follow up"}]}`)}
	if err := client.WriteJSON(request); err != nil {
		close(release)
		t.Fatal(err)
	}
	var event codex.RPCMessage
	err := client.ReadJSON(&event)
	close(release)
	if err != nil || event.Method != "item/agentMessage/delta" {
		t.Fatalf("waiting for an input receipt blocked work output: %+v, %v", event, err)
	}
	if err := client.ReadJSON(&event); err != nil || string(event.ID) != string(request.ID) || event.Error != nil {
		t.Fatalf("API receipt invented an input commit: %+v, %v", event, err)
	}
	emit(codex.RPCMessage{Method: "item/completed", Params: json.RawMessage(`{"threadId":"thread-owned","turnId":"turn-owned","item":{"type":"userMessage","id":"item-owned","clientId":"message-owned"}}`)})
	if err := client.ReadJSON(&event); err != nil || !strings.Contains(string(event.Params), `"clientId":"tui-queued"`) {
		t.Fatalf("late commit: %+v, %v", event, err)
	}
}
