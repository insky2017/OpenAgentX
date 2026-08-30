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
		if record.Type != "" && strings.Contains(strings.ToLower(record.Type), "error") {
			result.Status = openruntime.TurnResultFailed
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
	if result.Status == "" {
		result.Status = openruntime.TurnResultSucceeded
	}
	if result.Status == openruntime.TurnResultSucceeded {
		known := true
		result.SideEffectsKnown = known
	}
	if result.Result == "" && result.Status == openruntime.TurnResultFailed {
		result.Error = "AGY reported a failed turn"
	}
	return result, nil
}

func decodeRecord(raw map[string]any) streamRecord {
	encoded, _ := json.Marshal(raw)
	var record streamRecord
	_ = json.Unmarshal(encoded, &record)
	for _, key := range []string{"conversation_id", "conversationId", "session_id", "sessionId"} {
		if value, ok := raw[key].(string); ok && value != "" {
			if strings.Contains(strings.ToLower(key), "conversation") {
				record.ConversationID = value
			} else {
				record.SessionID = value
			}
		}
	}
	for _, key := range []string{"output", "text", "message", "content", "result"} {
		if value, ok := raw[key].(string); ok && value != "" {
			if key == "result" {
				record.Result = value
			} else if record.Text == "" {
				record.Text = value
			}
		}
	}
	return record
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
