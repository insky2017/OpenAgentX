package safeoutput

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProjectionRedactsAndDropsUnsafeFields(t *testing.T) {
	raw := json.RawMessage(`{"stage":"step token=stage-secret","status":"authorization=state-secret","text":"ok token=top-secret https://user:pass@example.test","stderr":"raw failure","reasoning":"hidden","environment":{"SECRET":"x"}}`)
	projected := string(ProjectRuntimePayload(raw))
	for _, forbidden := range []string{"stage-secret", "state-secret", "top-secret", "user:pass", "raw failure", "hidden", "environment"} {
		if strings.Contains(projected, forbidden) {
			t.Fatalf("projection leaked %q: %s", forbidden, projected)
		}
	}
	if !strings.Contains(projected, "ok token=[REDACTED]") {
		t.Fatalf("projection lost safe output: %s", projected)
	}
}

func TestRedactTextLimitsOutput(t *testing.T) {
	projected := RedactText(strings.Repeat("x", MaxTextBytes+100))
	if len(projected) > MaxTextBytes+len(" [TRUNCATED]") || !strings.HasSuffix(projected, " [TRUNCATED]") {
		t.Fatalf("unexpected bounded projection length=%d", len(projected))
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
