package domain

import (
	"encoding/json"
	"testing"
)

func TestTaskIntentJSONRejectsExplicitInvalidValuesAndDefaultsGoZero(t *testing.T) {
	for _, raw := range []string{`null`, `""`, `"unknown"`, `false`} {
		var intent TaskIntent
		if err := json.Unmarshal([]byte(raw), &intent); err == nil {
			t.Fatalf("intent %s was accepted", raw)
		}
	}
	for _, raw := range []string{`"mutation"`, `"query"`} {
		var intent TaskIntent
		if err := json.Unmarshal([]byte(raw), &intent); err != nil || !intent.Valid() {
			t.Fatalf("intent %s parsed as %q, err=%v", raw, intent, err)
		}
	}
	encoded, err := json.Marshal(TaskIntent(""))
	if err != nil || string(encoded) != `"mutation"` {
		t.Fatalf("zero intent JSON=%s err=%v", encoded, err)
	}
	if got, err := NormalizeTaskIntent(""); err != nil || got != TaskIntentMutation {
		t.Fatalf("normalized zero intent=%q err=%v", got, err)
	}
}
