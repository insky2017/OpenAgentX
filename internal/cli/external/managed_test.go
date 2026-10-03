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

	"openagentx/internal/domain"
)

func TestManagedInstructionsUseParticipantIdentityAndNeverPrintToken(t *testing.T) {
	const token = "private-communication-test-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("instructions must use participant credential")
		}
		switch r.URL.Path {
		case "/api/external/v1/status":
			_ = json.NewEncoder(w).Encode(domain.ExternalSessionBinding{AgentID: "app", Mode: "managed", ThreadID: "native-thread", AllowedPeerAgentIDs: []string{"pay"}})
		case "/api/external/v1/roles":
			_ = json.NewEncoder(w).Encode(domain.ExternalRoleCatalog{OrganizationID: "default", Revision: 2, Rules: []domain.ExternalRoleRule{{Scope: "payments", OwnerAgentID: "pay", Description: "payment contracts"}}})
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	defer server.Close()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
	}}
	defer transport.CloseIdleConnections()
	c := &client{http: &http.Client{Transport: transport}, token: token}
	var out bytes.Buffer
	if err := collaborationInstructions(context.Background(), c, options{agent: "app", sessionFile: "/private/app/credentials.json"}, "/private/oax.sock", &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), token) || !strings.Contains(out.String(), "pay") || !strings.Contains(out.String(), "不手动reply") {
		t.Fatalf("instructions leaked credentials or lost role/automatic-reply boundary: %s", out.String())
	}
	out.Reset()
	if err := collaborationInstructions(context.Background(), c, options{agent: "other"}, "/private/oax.sock", &out); err == nil || out.Len() != 0 {
		t.Fatal("mismatched participant produced actionable instructions")
	}
}

func TestManagedEntryCannotSilentlySelectExternalMode(t *testing.T) {
	for _, args := range [][]string{{"enable", "--agent", "app", "--mode", "external"}, {"enable", "--agent", "app", "--mode=external"}} {
		var out, errOut bytes.Buffer
		if code := ExecuteCollaboration(args, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "managed") {
			t.Fatalf("ambiguous host selection was accepted: %d %s", code, errOut.String())
		}
	}
}
