package nativebridge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestManagedModelWritesPersistOnlyAgentPreference(t *testing.T) {
	b, c := bridgeFixture(t)
	ctx := context.Background()
	// A nil upstream proves neither method writes Codex's live thread or global file.
	got, err := b.request(ctx, nil, "thread/settings/update", json.RawMessage(`{"threadId":"thread-owned","model":"model-other","effort":"max","sandboxPolicy":null}`))
	if err != nil || string(got) != "{}" {
		t.Fatalf("settings response=%s error=%v", got, err)
	}
	if len(c.updates) != 1 || c.updates[0].ExpectedVersion != 0 || c.settings.Model != "model-other" || c.settings.Effort != "max" {
		t.Fatalf("updates=%+v settings=%+v", c.updates, c.settings)
	}
	got, err = b.request(ctx, nil, "config/batchWrite", json.RawMessage(`{"edits":[{"keyPath":"model","value":"model-other","mergeStrategy":"replace"},{"keyPath":"model_reasoning_effort","value":"max","mergeStrategy":"replace"}],"reloadUserConfig":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.updates) != 1 {
		t.Fatal("same selection performed another write")
	}
	var response map[string]any
	if json.Unmarshal(got, &response) != nil || response["status"] != "ok" || response["version"] != "oax-agent-model:1" || !strings.HasSuffix(response["filePath"].(string), "/data/openagentx.db") {
		t.Fatalf("config response=%s", got)
	}
	// A stale TUI turn must use the currently saved Agent setting at execution time.
	if _, err = b.request(ctx, nil, "turn/start", json.RawMessage(`{"threadId":"thread-owned","model":"stale","effort":"low","input":[{"type":"text","text":"proof"}]}`)); err != nil {
		t.Fatal(err)
	}
	if len(c.updates) != 1 || len(c.dispatches) != 1 {
		t.Fatal("turn overwrote settings or lost task")
	}
}

func TestManagedModelWritesRejectMixedUnsafeAndStaleRequestsAtomically(t *testing.T) {
	b, c := bridgeFixture(t)
	cases := []struct{ method, params string }{
		{"thread/settings/update", `{"threadId":"thread-other","model":"model-other"}`},
		{"thread/settings/update", `{"model":"model-other"}`},
		{"thread/settings/update", `{"threadId":"thread-owned","model":"model-other","approvalPolicy":"never"}`},
		{"thread/settings/update", `{"threadId":"thread-owned","model":"unsupported"}`},
		{"thread/settings/update", `{"threadId":"thread-owned","effort":"unsupported"}`},
		{"config/batchWrite", `{"filePath":"/tmp/config.toml","edits":[{"keyPath":"model","value":"model-other","mergeStrategy":"replace"}]}`},
		{"config/batchWrite", `{"expectedVersion":"oax-agent-model:99","edits":[{"keyPath":"model","value":"model-other","mergeStrategy":"replace"}]}`},
		{"config/batchWrite", `{"edits":[{"keyPath":"model","value":"model-other","mergeStrategy":"replace"},{"keyPath":"approval_policy","value":"never","mergeStrategy":"replace"}]}`},
		{"config/batchWrite", `{"edits":[{"keyPath":"model","value":"model-other","mergeStrategy":"append"}]}`},
		{"config/batchWrite", `{"edits":[{"keyPath":"model","value":"model-other","mergeStrategy":"replace"},{"keyPath":"models.new_thread.model","value":"model-default","mergeStrategy":"replace"}]}`},
	}
	for _, tc := range cases {
		if _, err := b.request(context.Background(), nil, tc.method, json.RawMessage(tc.params)); err == nil {
			t.Fatalf("accepted %s %s", tc.method, tc.params)
		}
	}
	if len(c.updates) != 0 || len(c.dispatches) != 0 || c.settings.Version != 0 {
		t.Fatal("rejected write changed authoritative state")
	}
}

func TestManagedModelReadsProjectAuthoritativePreference(t *testing.T) {
	b, c := bridgeFixture(t)
	c.settings.Model = "model-other"
	c.settings.Effort = "max"
	c.settings.Version = 8
	cases := []struct {
		method, input string
		check         func(*testing.T, map[string]any)
	}{
		{"thread/resume", `{"model":"model-default","reasoningEffort":"high","cwd":"/preserved","collaborationMode":{"mode":"default","settings":{"model":"model-default","reasoning_effort":"high"}}}`, func(t *testing.T, d map[string]any) {
			if d["model"] != "model-other" || d["reasoningEffort"] != "max" || d["cwd"] != "/preserved" || d["collaborationMode"].(map[string]any)["settings"].(map[string]any)["model"] != "model-other" {
				t.Fatalf("resume=%v", d)
			}
		}},
		{"config/read", `{"config":{"model":"old","model_reasoning_effort":"low","approval_policy":"on-request","models":{"new_thread":{"model":"old","model_reasoning_effort":"low"}}},"origins":{},"layers":null}`, func(t *testing.T, d map[string]any) {
			s := d["config"].(map[string]any)
			if s["model"] != "model-other" || s["model_reasoning_effort"] != "max" || s["approval_policy"] != "on-request" || s["models"].(map[string]any)["new_thread"].(map[string]any)["model"] != "model-other" {
				t.Fatalf("config=%v", d)
			}
		}},
		{"model/list", `{"data":[{"model":"hidden-unregistered","isDefault":true},{"model":"model-other","isDefault":false,"supportedReasoningEfforts":[{"reasoningEffort":"high"},{"reasoningEffort":"max"},{"reasoningEffort":"unsupported"}]}],"nextCursor":null}`, func(t *testing.T, d map[string]any) {
			a := d["data"].([]any)
			if len(a) != 1 {
				t.Fatalf("catalog=%v", d)
			}
			m := a[0].(map[string]any)
			if m["isDefault"] != true || m["defaultReasoningEffort"] != "max" || len(m["supportedReasoningEfforts"].([]any)) != 2 {
				t.Fatalf("model=%v", m)
			}
		}},
		{"thread/settings/updated", `{"threadId":"thread-owned","threadSettings":{"model":"old","effort":"low","approvalPolicy":"never"}}`, func(t *testing.T, d map[string]any) {
			s := d["threadSettings"].(map[string]any)
			if s["model"] != "model-other" || s["effort"] != "max" || s["approvalPolicy"] != "never" {
				t.Fatalf("notification=%v", d)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			raw, err := b.projectSettings(context.Background(), tc.method, json.RawMessage(tc.input))
			if err != nil {
				t.Fatal(err)
			}
			var d map[string]any
			if err = json.Unmarshal(raw, &d); err != nil {
				t.Fatal(err)
			}
			tc.check(t, d)
		})
	}
}

func TestNativeModelMenuGoldenCollaborationFields(t *testing.T) {
	b, c := bridgeFixture(t)
	// Reduced, non-secret real Codex 0.160.1 /model request from fixture01.
	request := json.RawMessage(`{"threadId":"thread-owned","disabledPluginIds":null,"cwd":null,"approvalPolicy":null,"approvalsReviewer":null,"sandboxPolicy":null,"permissions":null,"model":null,"effort":"high","summary":null,"collaborationMode":{"mode":"default","settings":{"model":"model-other","reasoning_effort":"high","developer_instructions":null}},"multiAgentMode":null,"personality":null}`)
	if _, err := b.request(context.Background(), nil, "thread/settings/update", request); err != nil {
		t.Fatal(err)
	}
	if c.settings.Model != "model-other" || c.settings.Effort != "high" || len(c.updates) != 1 {
		t.Fatalf("settings=%+v", c.settings)
	}
	for _, request := range []string{
		`{"threadId":"thread-owned","effort":"high","collaborationMode":{"mode":"plan","settings":{"model":"model-other","reasoning_effort":"high","developer_instructions":null}}}`,
		`{"threadId":"thread-owned","effort":"high","collaborationMode":{"mode":"default","settings":{"model":"model-other","reasoning_effort":"high","developer_instructions":"replace role"}}}`,
		`{"threadId":"thread-owned","effort":"max","collaborationMode":{"mode":"default","settings":{"model":"model-other","reasoning_effort":"high","developer_instructions":null}}}`,
	} {
		if _, err := b.request(context.Background(), nil, "thread/settings/update", json.RawMessage(request)); err == nil {
			t.Fatalf("unsafe collaboration accepted: %s", request)
		}
	}
	if len(c.updates) != 1 {
		t.Fatal("rejected collaboration changed state")
	}
}
