package safeoutput

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"
)

const MaxTextBytes = 4 << 10

var (
	urlCredentialPattern = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/@\s]+@`)
	headerSecretPattern  = regexp.MustCompile(`(?i)\b(authorization|cookie|set-cookie)\b\s*[:=]\s*[^\r\n]+`)
	secretValuePattern   = regexp.MustCompile(`(?i)(["']?\b(?:api[_-]?key|access[_-]?token|refresh[_-]?token|token|password|passwd|secret)\b["']?\s*[:=]\s*)(?:"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|[^\s,;]+)`)
	secretOptionPattern  = regexp.MustCompile(`(?i)(--(?:api[_-]?key|access[_-]?token|refresh[_-]?token|token|password|passwd|secret)(?:=|\s+))[^\s]+`)
	authSchemePattern    = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[a-z0-9._~+/=-]+`)
	privateKeyPattern    = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)
)

func RedactText(value string) string {
	value = strings.ToValidUTF8(value, "?")
	value = privateKeyPattern.ReplaceAllString(value, "[REDACTED_PRIVATE_KEY]")
	value = urlCredentialPattern.ReplaceAllString(value, `${1}[REDACTED]@`)
	value = headerSecretPattern.ReplaceAllString(value, `${1}=[REDACTED]`)
	value = secretValuePattern.ReplaceAllString(value, `${1}[REDACTED]`)
	value = secretOptionPattern.ReplaceAllString(value, `${1}[REDACTED]`)
	value = authSchemePattern.ReplaceAllString(value, `${1} [REDACTED]`)
	if len(value) > MaxTextBytes {
		limit := MaxTextBytes
		for limit > 0 && !utf8.RuneStart(value[limit]) {
			limit--
		}
		value = value[:limit] + " [TRUNCATED]"
	}
	return value
}

type RuntimeProjection struct {
	Stage      string `json:"stage,omitempty"`
	Status     string `json:"status,omitempty"`
	Text       string `json:"text,omitempty"`
	Diagnostic string `json:"diagnostic,omitempty"`
	HasOutput  bool   `json:"has_output,omitempty"`
	HasError   bool   `json:"has_error,omitempty"`
}

// ProjectRuntimePayload accepts only documented public fields. Unknown keys,
// raw stderr, environment, credentials, and hidden-reasoning fields are never
// copied to the public event stream.
func ProjectRuntimePayload(raw json.RawMessage) json.RawMessage {
	var input struct {
		Stage      string `json:"stage"`
		Status     string `json:"status"`
		Text       string `json:"text"`
		Output     string `json:"output"`
		Result     string `json:"result"`
		Diagnostic string `json:"diagnostic"`
		Error      string `json:"error"`
		HasOutput  bool   `json:"has_output"`
		HasError   bool   `json:"has_error"`
	}
	if json.Unmarshal(raw, &input) != nil {
		return nil
	}
	text := input.Text
	if text == "" {
		text = input.Output
	}
	if text == "" {
		text = input.Result
	}
	diagnostic := input.Diagnostic
	if diagnostic == "" {
		diagnostic = input.Error
	}
	projection := RuntimeProjection{
		Stage: RedactText(input.Stage), Status: RedactText(input.Status), Text: RedactText(text), Diagnostic: RedactText(diagnostic),
		HasOutput: input.HasOutput || text != "", HasError: input.HasError || diagnostic != "",
	}
	encoded, _ := json.Marshal(projection)
	return encoded
}
