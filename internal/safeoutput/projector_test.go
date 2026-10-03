package safeoutput

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	openruntime "openagentx/internal/runtime"
)

func TestProjectionRedactsAndDropsUnsafeFields(t *testing.T) {
	raw := json.RawMessage(`{"stage":"step token=stage-secret","status":"authorization=state-secret","text":"ok token=top-secret https://user:pass@example.test","stderr":"raw failure","reasoning":"hidden","environment":{"SECRET":"x"}}`)
	projection, err := ProjectRuntimePayload(raw)
	if err != nil {
		t.Fatal(err)
	}
	projected := string(projection)
	for _, forbidden := range []string{"stage-secret", "state-secret", "top-secret", "user:pass", "raw failure", "hidden", "environment"} {
		if strings.Contains(projected, forbidden) {
			t.Fatalf("projection leaked %q: %s", forbidden, projected)
		}
	}
	if !strings.Contains(projected, "ok token=[REDACTED]") {
		t.Fatalf("projection lost safe output: %s", projected)
	}
}

func TestRuntimeProjectionRejectsNonObjectPayload(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`null`), json.RawMessage(`"text"`), json.RawMessage(`[]`)} {
		if _, err := ProjectRuntimePayload(raw); err == nil {
			t.Fatalf("payload %q was accepted", raw)
		}
	}
}

func TestRedactTextLimitsOutput(t *testing.T) {
	projected := RedactText(strings.Repeat("x", MaxTextBytes+100))
	if len(projected) > MaxTextBytes+len(" [TRUNCATED]") || !strings.HasSuffix(projected, " [TRUNCATED]") {
		t.Fatalf("unexpected bounded projection length=%d", len(projected))
	}
	reprojected := ProjectText(projected)
	if !reprojected.Truncated || reprojected.Text != projected {
		t.Fatalf("truncated projection was not idempotent: first_len=%d second_len=%d", len(projected), len(reprojected.Text))
	}
}

func TestTurnResultProjectionSharesRedactionAndStructuredTruncation(t *testing.T) {
	result := SanitizeTurnResult(openruntime.TurnResult{
		Status: openruntime.TurnResultSucceeded,
		Result: "token=reply-secret " + strings.Repeat("x", MaxResultBytes+1),
		Error:  "Authorization: Bearer error-secret",
	})
	if !result.ResultTruncated || result.ErrorTruncated || !strings.HasSuffix(result.Result, TruncatedMarker) {
		t.Fatalf("projected result=%+v", result)
	}
	for _, secret := range []string{"reply-secret", "error-secret"} {
		if strings.Contains(result.Result+result.Error, secret) {
			t.Fatalf("projected result leaked %q", secret)
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	decoded, state := DecodeTurnResult(string(encoded))
	if state != "truncated" || decoded == nil || !decoded.ResultTruncated {
		t.Fatalf("decoded=%+v state=%q", decoded, state)
	}
}

func TestFinalReplyLimitIsIndependentFromEventSummaries(t *testing.T) {
	for _, extra := range []string{"", "界"} {
		body := strings.Repeat("x", MaxResultBytes) + extra
		result := SanitizeTurnResult(openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, FinalReply: true, Result: body})
		if result.FinalReply != (extra == "") || result.ResultTruncated != (extra != "") || !utf8.ValidString(result.Result) || len(result.Result) > MaxResultBytes+len(TruncatedMarker) {
			t.Fatalf("incorrect final result boundary: complete=%t truncated=%t bytes=%d", result.FinalReply, result.ResultTruncated, len(result.Result))
		}
		payload, _ := json.Marshal(map[string]string{"text": body})
		projected, err := ProjectRuntimePayload(payload)
		var event RuntimeProjection
		if err != nil || json.Unmarshal(projected, &event) != nil || !event.TextTruncated || len(event.Text) > MaxTextBytes+len(TruncatedMarker) {
			t.Fatal("final reply limit widened public event summaries")
		}
	}
}

func TestOutcomeProjectionDistinguishesPendingMissingAndTruncated(t *testing.T) {
	secret := "token=task-secret " + strings.Repeat("x", MaxResultBytes+1)
	if state := ProjectOutcome(false, nil, nil).State; state != "pending" {
		t.Fatalf("pending state=%q", state)
	}
	if state := ProjectOutcome(true, nil, nil).State; state != "not_recorded" {
		t.Fatalf("missing state=%q", state)
	}
	projected := ProjectOutcome(true, &secret, nil)
	if projected.State != "truncated" || !projected.ResultTruncated || projected.Result == nil || strings.Contains(*projected.Result, "task-secret") {
		t.Fatalf("projected outcome=%+v", projected)
	}
}

func TestRedactTextCoversStructuredHeadersAndCLIOptions(t *testing.T) {
	input := `{"token":"json-secret","password": "quoted value"} Authorization: Bearer header-secret
Cookie: session=cookie-secret; other=still-secret
command --api-key cli-secret --refresh_token=refresh-secret`
	projected := RedactText(input)
	for _, forbidden := range []string{"json-secret", "quoted value", "header-secret", "cookie-secret", "still-secret", "cli-secret", "refresh-secret"} {
		if strings.Contains(projected, forbidden) {
			t.Fatalf("redaction leaked %q: %s", forbidden, projected)
		}
	}
}
