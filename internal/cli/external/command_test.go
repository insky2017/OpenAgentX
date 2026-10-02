package external

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"openagentx/internal/domain"
)

func TestRecoveryInboxPreservesAcknowledgedWorkAndCursor(t *testing.T) {
	now := time.Now()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("recover") != "true" || r.URL.Query().Get("after") != "7" {
			t.Errorf("query %s", r.URL.RawQuery)
		}
		if r.Header.Get("Authorization") != "Bearer isolated-test" {
			t.Error("missing agent token")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"messages": []domain.ExternalMessage{{Sequence: 12, ID: "acked-work", Kind: domain.ExternalMessageRequest, ProcessingState: "accepted", DeliveryState: "acknowledged", AcknowledgedAt: &now}}})
	}))
	defer server.Close()
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}}
	defer transport.CloseIdleConnections()
	c := &client{http: &http.Client{Transport: transport}, token: "isolated-test"}
	var out bytes.Buffer
	if err := inbox(context.Background(), c, options{recover: true, after: 7}, &out); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Messages []domain.ExternalMessage `json:"messages"`
		Next     int64                    `json:"next_after"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || len(result.Messages) != 1 || result.Next != 12 {
		t.Fatalf("recovery filtered acknowledged work: %s %v", out.String(), err)
	}
	if err := inbox(context.Background(), c, options{recover: true, watch: true}, &out); err == nil {
		t.Fatal("recovery watch can skip older changed processing states")
	}
}

func TestScopeCLIErrorIncludesCorrectOwner(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		_ = json.NewEncoder(w).Encode(domain.ExternalScopeError{Code: "OUT_OF_SCOPE", Scope: "app.db", OwnerAgentID: "application", Message: "target does not own the declared scope"})
	}))
	defer server.Close()
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}}
	defer transport.CloseIdleConnections()
	c := &client{http: &http.Client{Transport: transport}, token: "test-only"}
	err := c.call(context.Background(), "POST", "messages", "key", struct{}{}, nil)
	if err == nil || !strings.Contains(err.Error(), "OUT_OF_SCOPE") || !strings.Contains(err.Error(), "owner_agent_id=application") {
		t.Fatalf("CLI loses route correction: %v", err)
	}
}
