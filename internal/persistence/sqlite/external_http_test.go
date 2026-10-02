package sqlite

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	externalapi "openagentx/internal/api/external"
	cliauth "openagentx/internal/auth/cli"
	"openagentx/internal/domain"
)

func TestExternalHTTPRolesScopeReceiptsAndRecovery(t *testing.T) {
	r, f, s := externalFixture(t)
	ctx := context.Background()
	_, quote := externalBind(t, s, f.ownerPrincipal, f.agentID, "pay")
	_, pay := externalBind(t, s, f.ownerPrincipal, "pay", f.agentID)
	if _, err := s.ApplyRoles(ctx, f.ownerPrincipal, externalRules(f)); err != nil {
		t.Fatal(err)
	}
	auth, err := cliauth.NewService(r, cliauth.Config{Now: func() time.Time { return repositoryTestTime }})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := externalapi.NewHandler(s, auth)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, token string, input any) (int, []byte) {
		t.Helper()
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, externalapi.Prefix+path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+token)
		if in, ok := input.(domain.SendExternalMessageInput); ok {
			req.Header.Set("Idempotency-Key", in.IdempotencyKey)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response.Code, response.Body.Bytes()
	}
	code, raw := call("GET", "roles", pay, nil)
	if code != 200 {
		t.Fatalf("roles=%d %s", code, raw)
	}
	var catalog domain.ExternalRoleCatalog
	if err = json.Unmarshal(raw, &catalog); err != nil || catalog.Revision != 1 || len(catalog.Rules) != 2 {
		t.Fatalf("roles=%s", raw)
	}
	code, _ = call("PUT", "roles", pay, externalRules(f))
	if code/100 == 2 {
		t.Fatal("external token modified roles")
	}
	in := domain.SendExternalMessageInput{TargetAgentID: "pay", Kind: domain.ExternalMessageRequest, Scope: "quote.data", Content: "inspect", IdempotencyKey: "http-wrong"}
	code, raw = call("POST", "messages", quote, in)
	if code != 422 {
		t.Fatalf("route=%d %s", code, raw)
	}
	var wrong domain.ExternalScopeError
	if err = json.Unmarshal(raw, &wrong); err != nil || wrong.Code != "OUT_OF_SCOPE" || wrong.OwnerAgentID != f.agentID {
		t.Fatalf("wrong owner=%s", raw)
	}
	in.Scope = "pay.api"
	in.IdempotencyKey = "http-correct"
	code, raw = call("POST", "messages", quote, in)
	if code != 200 {
		t.Fatalf("send=%d %s", code, raw)
	}
	var m domain.ExternalMessage
	if err = json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	code, raw = call("POST", "messages/"+m.ID+"/ack", pay, struct{}{})
	if code != 200 {
		t.Fatalf("ack=%d %s", code, raw)
	}
	code, raw = call("POST", "messages/"+m.ID+"/receipt", pay, domain.ExternalReceiptInput{State: "accepted", Note: "inspect only"})
	if code != 200 {
		t.Fatalf("receipt=%d %s", code, raw)
	}
	code, raw = call("GET", "inbox?recover=true", pay, nil)
	if code != 200 {
		t.Fatalf("recover=%d %s", code, raw)
	}
	var inbox struct {
		Messages []domain.ExternalMessage `json:"messages"`
	}
	if err = json.Unmarshal(raw, &inbox); err != nil || len(inbox.Messages) != 1 || inbox.Messages[0].ProcessingState != "accepted" || inbox.Messages[0].DeliveryState != "acknowledged" {
		t.Fatalf("recover=%s", raw)
	}
	code, raw = call("POST", "messages/"+m.ID+"/receipt", "invalid", domain.ExternalReceiptInput{State: "bad"})
	if code != http.StatusUnauthorized {
		t.Fatalf("auth ordering=%d %s", code, raw)
	}
	code, raw = call("POST", "messages", pay, domain.SendExternalMessageInput{Kind: domain.ExternalMessageResult, ReplyToMessageID: m.ID, Content: "read evidence", IdempotencyKey: "http-result"})
	if code != 200 {
		t.Fatalf("result=%d %s", code, raw)
	}
	code, raw = call("GET", "inbox?recover=true", pay, nil)
	if code != 200 {
		t.Fatalf("recover=%d %s", code, raw)
	}
	if err = json.Unmarshal(raw, &inbox); err != nil || len(inbox.Messages) != 0 {
		t.Fatalf("terminal recovery=%s", raw)
	}
}
