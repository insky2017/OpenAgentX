package codex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Exercise the actual WebSocket transport: long-lived threads can return more
// than 32 MiB of history before either a Worker or a native TUI starts a turn.
func TestRPCReceivesLargeThreadHistory(t *testing.T) {
	history := strings.Repeat("h", 33<<20)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var request RPCMessage
		if err = conn.ReadJSON(&request); err != nil {
			return
		}
		writeResult(conn, request.ID, map[string]any{"thread": map[string]any{"id": "preserved-thread", "history": history}})
		// Keep the peer alive until the client has consumed the response.
		_, _, _ = conn.ReadMessage()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, err := DialRPC(ctx, "ws"+strings.TrimPrefix(server.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var result struct {
		Thread struct{ ID, History string }
	}
	if err = client.Call(ctx, "thread/resume", map[string]any{"threadId": "preserved-thread"}, &result); err != nil {
		t.Fatal(err)
	}
	if result.Thread.ID != "preserved-thread" || result.Thread.History != history {
		t.Fatal("thread history was truncated or replaced")
	}
}
