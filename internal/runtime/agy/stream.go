package agy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	openruntime "openagentx/internal/runtime"
)

const maxStreamLine = 4 << 20

type streamRecord struct {
	Type             string
	Status           string
	Text             string
	Result           string
	Error            string
	ConversationID   string
	SessionID        string
	Usage            json.RawMessage
	SideEffectsKnown *bool
}

func parseStreamJSON(reader io.Reader, sink openruntime.EventSink) (openruntime.TurnResult, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), maxStreamLine)
	var result openruntime.TurnResult
	var output bytes.Buffer
	seen := false
	terminal := false
	sideEffectsReported := false
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal(line, &raw); err != nil {
			return openruntime.TurnResult{}, fmt.Errorf("decode AGY stream-json line: %w", err)
		}
		seen = true
		record := decodeRecord(raw)
		if record.ConversationID != "" {
			result.ProviderSessionID = record.ConversationID
		} else if record.SessionID != "" && result.ProviderSessionID == "" {
			result.ProviderSessionID = record.SessionID
		}
		if record.Usage != nil && json.Valid(record.Usage) {
			result.UsageJSON = append([]byte(nil), record.Usage...)
		}
		if record.SideEffectsKnown != nil {
			result.SideEffectsKnown = *record.SideEffectsKnown
			sideEffectsReported = true
		}
		text := record.Result
		if text == "" {
			text = record.Text
		}
		if text != "" {
			output.WriteString(text)
		}
		if record.Status != "" {
			switch strings.ToLower(record.Status) {
			case "success", "succeeded", "completed", "done":
				result.Status = openruntime.TurnResultSucceeded
			case "cancelled", "canceled":
				result.Status = openruntime.TurnResultCanceled
			case "error", "failed", "failure":
				result.Status = openruntime.TurnResultFailed
			}
		}
		recordType := strings.ToLower(record.Type)
		if recordType == "result" {
			terminal = true
			if result.Status == "" {
				if record.Error != "" {
					result.Status = openruntime.TurnResultFailed
				} else {
					result.Status = openruntime.TurnResultSucceeded
				}
			}
		}
		if record.Error != "" {
			result.Error = record.Error
		}
		if recordType != "" && strings.Contains(recordType, "error") {
			result.Status = openruntime.TurnResultFailed
			terminal = true
		}
		if sink != nil {
			payload, _ := json.Marshal(raw)
			if err := sink.Emit(nilContext(), openruntime.RuntimeEvent{
				Type: normalizeEventType(record.Type), Payload: payload, OccurredAt: time.Now().UTC(),
			}); err != nil {
				return openruntime.TurnResult{}, fmt.Errorf("emit AGY Runtime Event: %w", err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return openruntime.TurnResult{}, fmt.Errorf("read AGY stream-json: %w", err)
	}
	if !seen {
		return openruntime.TurnResult{}, fmt.Errorf("AGY stream-json was empty")
	}
	result.Result = strings.TrimSpace(output.String())
	if !terminal {
		return result, fmt.Errorf("AGY stream-json ended without a terminal event")
	}
	if result.Status == openruntime.TurnResultSucceeded && !sideEffectsReported {
		known := true
		result.SideEffectsKnown = known
	}
	if result.Result == "" && result.Error == "" && result.Status == openruntime.TurnResultFailed {
		result.Error = "AGY reported a failed turn"
	}
	return result, nil
}

func decodeRecord(raw map[string]any) streamRecord {
	recordType := firstString(raw, "event", "type")
	payload := raw
	if nested, ok := raw[recordType].(map[string]any); ok {
		payload = nested
	}
	record := streamRecord{
		Type:           recordType,
		Status:         firstString(payload, "status"),
		Error:          firstString(payload, "error"),
		ConversationID: firstString(payload, "conversation_id", "conversationId"),
		SessionID:      firstString(payload, "session_id", "sessionId"),
	}
	if record.Type == "" {
		record.Type = firstString(payload, "event", "type")
	}
	if record.ConversationID == "" {
		record.ConversationID = firstString(raw, "conversation_id", "conversationId")
	}
	if record.SessionID == "" {
		record.SessionID = firstString(raw, "session_id", "sessionId")
	}
	for _, key := range []string{"response", "output", "text", "message", "content", "result"} {
		if value, ok := payload[key].(string); ok && value != "" {
			if key == "response" || key == "result" {
				record.Result = value
			} else if record.Text == "" {
				record.Text = value
			}
		}
	}
	if usage, ok := payload["usage"]; ok {
		if encoded, err := json.Marshal(usage); err == nil {
			record.Usage = encoded
		}
	}
	if known, ok := payload["side_effects_known"].(bool); ok {
		record.SideEffectsKnown = &known
	}
	return record
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func normalizeEventType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "agy.output"
	}
	var builder strings.Builder
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '_' || char == '-' {
			builder.WriteRune(char)
		} else {
			builder.WriteByte('_')
		}
	}
	return "agy." + builder.String()
}

func nilContext() context.Context { return context.Background() }
