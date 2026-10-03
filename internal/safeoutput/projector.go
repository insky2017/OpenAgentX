package safeoutput

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	openruntime "openagentx/internal/runtime"
)

const (
	MaxTextBytes = 4 << 10
	// Final replies are persisted outcomes, not event/diagnostic summaries.
	// Keep them bounded independently so ordinary complete answers stay complete.
	MaxResultBytes  = 32 << 10
	TruncatedMarker = " [TRUNCATED]"
)

var (
	urlCredentialPattern = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/@\s]+@`)
	headerSecretPattern  = regexp.MustCompile(`(?i)\b(authorization|cookie|set-cookie)\b\s*[:=]\s*[^\r\n]+`)
	secretValuePattern   = regexp.MustCompile(`(?i)(["']?\b(?:api[_-]?key|access[_-]?token|refresh[_-]?token|token|password|passwd|secret)\b["']?\s*[:=]\s*)(?:"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|[^\s,;]+)`)
	secretOptionPattern  = regexp.MustCompile(`(?i)(--(?:api[_-]?key|access[_-]?token|refresh[_-]?token|token|password|passwd|secret)(?:=|\s+))[^\s]+`)
	authSchemePattern    = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[a-z0-9._~+/=-]+`)
	privateKeyPattern    = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)
)

type TextProjection struct {
	Text      string
	Truncated bool
}

type OutcomeProjection struct {
	Result          *string
	Error           *string
	State           string
	ResultTruncated bool
	ErrorTruncated  bool
}

func ProjectText(value string) TextProjection {
	return projectText(value, MaxTextBytes)
}

func ProjectResultText(value string) TextProjection {
	return projectText(value, MaxResultBytes)
}

func projectText(value string, maxBytes int) TextProjection {
	alreadyTruncated := strings.HasSuffix(value, TruncatedMarker)
	if alreadyTruncated {
		value = strings.TrimSuffix(value, TruncatedMarker)
	}
	value = strings.ToValidUTF8(value, "?")
	value = privateKeyPattern.ReplaceAllString(value, "[REDACTED_PRIVATE_KEY]")
	value = urlCredentialPattern.ReplaceAllString(value, `${1}[REDACTED]@`)
	value = headerSecretPattern.ReplaceAllString(value, `${1}=[REDACTED]`)
	value = secretValuePattern.ReplaceAllString(value, `${1}[REDACTED]`)
	value = secretOptionPattern.ReplaceAllString(value, `${1}[REDACTED]`)
	value = authSchemePattern.ReplaceAllString(value, `${1} [REDACTED]`)
	if len(value) > maxBytes {
		limit := maxBytes
		for limit > 0 && !utf8.RuneStart(value[limit]) {
			limit--
		}
		value = value[:limit] + TruncatedMarker
		return TextProjection{Text: value, Truncated: true}
	}
	if alreadyTruncated {
		return TextProjection{Text: value + TruncatedMarker, Truncated: true}
	}
	return TextProjection{Text: value}
}

func RedactText(value string) string {
	return ProjectText(value).Text
}

func ProjectOutcome(terminal bool, result *string, resultError *string) OutcomeProjection {
	projection := OutcomeProjection{State: "pending"}
	if result != nil {
		value := ProjectResultText(*result)
		projection.Result = &value.Text
		projection.ResultTruncated = value.Truncated
	}
	if resultError != nil {
		value := ProjectText(*resultError)
		projection.Error = &value.Text
		projection.ErrorTruncated = value.Truncated
	}
	if !terminal {
		return projection
	}
	projection.State = "not_recorded"
	if projection.ResultTruncated || projection.ErrorTruncated {
		projection.State = "truncated"
	} else if (projection.Result != nil && *projection.Result != "") || (projection.Error != nil && *projection.Error != "") {
		projection.State = "available"
	}
	return projection
}

func SanitizeTurnResult(result openruntime.TurnResult) openruntime.TurnResult {
	body := ProjectResultText(result.Result)
	diagnostic := ProjectText(result.Error)
	result.Result = body.Text
	result.ResultTruncated = result.ResultTruncated || body.Truncated
	result.Error = diagnostic.Text
	result.ErrorTruncated = result.ErrorTruncated || diagnostic.Truncated
	if result.Status != openruntime.TurnResultSucceeded || strings.TrimSpace(result.Result) == "" ||
		result.ResultTruncated || result.Error != "" || result.ErrorTruncated {
		result.FinalReply = false
	}
	return result
}

func DecodeTurnResult(raw string) (*openruntime.TurnResult, string) {
	if strings.TrimSpace(raw) == "" {
		return nil, "not_recorded"
	}
	var result openruntime.TurnResult
	if json.Unmarshal([]byte(raw), &result) != nil || result.Validate() != nil {
		return nil, "invalid"
	}
	result = SanitizeTurnResult(result)
	state := "available"
	if result.Result == "" && result.Error == "" {
		state = "empty"
	} else if result.ResultTruncated || result.ErrorTruncated {
		state = "truncated"
	}
	return &result, state
}

type RuntimeProjection struct {
	Stage               string `json:"stage,omitempty"`
	Status              string `json:"status,omitempty"`
	Text                string `json:"text,omitempty"`
	TextTruncated       bool   `json:"text_truncated,omitempty"`
	Diagnostic          string `json:"diagnostic,omitempty"`
	DiagnosticTruncated bool   `json:"diagnostic_truncated,omitempty"`
	HasOutput           bool   `json:"has_output,omitempty"`
	HasError            bool   `json:"has_error,omitempty"`
}

// ProjectRuntimePayload accepts only documented public fields. Unknown keys,
// raw stderr, environment, credentials, and hidden-reasoning fields are never
// copied to the public event stream.
func ProjectRuntimePayload(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return nil, fmt.Errorf("Runtime output payload must be a JSON object")
	}
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
	if err := json.Unmarshal(trimmed, &input); err != nil {
		return nil, fmt.Errorf("decode Runtime output payload: %w", err)
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
	projectedText := ProjectText(text)
	projectedDiagnostic := ProjectText(diagnostic)
	projection := RuntimeProjection{
		Stage: RedactText(input.Stage), Status: RedactText(input.Status), Text: projectedText.Text,
		TextTruncated: projectedText.Truncated, Diagnostic: projectedDiagnostic.Text,
		DiagnosticTruncated: projectedDiagnostic.Truncated,
		HasOutput:           input.HasOutput || text != "",
		HasError:            input.HasError || diagnostic != "",
	}
	encoded, err := json.Marshal(projection)
	if err != nil {
		return nil, fmt.Errorf("encode Runtime output projection: %w", err)
	}
	return encoded, nil
}
