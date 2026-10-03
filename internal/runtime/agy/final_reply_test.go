package agy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openagentx/internal/safeoutput"
)

func TestFinalReplyRequiresTerminalBodyAndConsistentStream(t *testing.T) {
	terminal := `{"event":"result","result":{"status":"SUCCESS","response":"final"}}`
	for _, tc := range []struct {
		name, input string
		want        bool
	}{
		{"final", terminal, true},
		{"incremental then final", `{"event":"step_update","step_update":{"text":"draft"}}` + "\n" + terminal, true},
		{"incremental without final body", `{"event":"step_update","step_update":{"text":"draft"}}` + "\n" + `{"event":"result","result":{"status":"SUCCESS"}}`, false},
		{"empty", `{"event":"result","result":{"status":"SUCCESS","response":"   "}}`, false},
		{"missing terminal", `{"event":"step_update","step_update":{"text":"draft"}}`, false},
		{"duplicate terminal", terminal + "\n" + terminal, false},
		{"status after terminal", `{"event":"result","result":{"status":"failed","response":"partial"}}` + "\n" + `{"event":"step_update","step_update":{"status":"SUCCESS"}}`, false},
		{"invalid trailing json", terminal + "\n" + "invalid", false},
		{"conflicting error", `{"event":"result","result":{"status":"SUCCESS","response":"final","error":"failed"}}`, false},
		{"public truncation", `{"event":"result","result":{"status":"SUCCESS","response":"` + strings.Repeat("x", safeoutput.MaxResultBytes+1) + `"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, _ := parseStreamJSON(strings.NewReader(tc.input), nil)
			if result.FinalReply != tc.want {
				t.Fatalf("final reply=%t want=%t", result.FinalReply, tc.want)
			}
			if strings.Contains(result.Result, "draft") {
				t.Fatal("incremental output became final result")
			}
		})
	}
}

func TestFinalReplyDoesNotIgnoreStderrAtExitZero(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "agy-fixture")
	script := "#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' '{\"event\":\"result\",\"result\":{\"status\":\"SUCCESS\",\"response\":\"answer\"}}'\nprintf '%s\\n' 'provider execution error' >&2\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	adapter := NewAdapterForTest(Config{Binary: binary, WorkingDir: dir})
	handle, err := adapter.StartTurn(context.Background(), testTurnRequest(validSpec()), nil)
	if err != nil {
		t.Fatal(err)
	}
	result, _ := handle.Wait(context.Background())
	if result.FinalReply || result.Error == "" {
		t.Fatal("stderr diagnostic was accepted as complete delivery")
	}
}
