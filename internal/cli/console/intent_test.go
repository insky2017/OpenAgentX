package console

import (
	"strings"
	"testing"

	"openagentx/internal/consolemodel"
	"openagentx/internal/domain"
)

func TestDispatchIntentUsesOfficialAPIWithoutChangingContent(t *testing.T) {
	tests := []struct {
		line    string
		intent  domain.TaskIntent
		content string
	}{
		{"/dispatch tell me the time", domain.TaskIntentMutation, "tell me the time"},
		{"/dispatch --intent query tell me the time", domain.TaskIntentQuery, "tell me the time"},
		{"/dispatch --intent mutation update the file", domain.TaskIntentMutation, "update the file"},
		{"/dispatch --intent query -- --intent is literal text", domain.TaskIntentQuery, "--intent is literal text"},
		{"/dispatch -- --intent query is literal text", domain.TaskIntentMutation, "--intent query is literal text"},
	}
	for _, tc := range tests {
		t.Run(tc.line, func(t *testing.T) {
			application, client, _, _ := applicationFixture(t, true)
			application.prepared[9] = preparedAttach{client: client, agents: map[string]domain.ConsoleAgentOption{
				"quote": {AgentID: "quote", OrganizationID: "org-main"},
			}}
			request, err := parseControlInput(tc.line, "quote", consolemodel.State{})
			if err != nil {
				t.Fatal(err)
			}
			result := application.controlCmd(9, request)().(controlResultMsg)
			if result.Err != nil || len(client.dispatches) != 1 {
				t.Fatalf("dispatch count=%d err=%v", len(client.dispatches), result.Err)
			}
			got := client.dispatches[0]
			if got.Intent != tc.intent || got.Content != tc.content || got.TargetAgentID != "quote" || got.Meta.IdempotencyKey == "" {
				t.Fatal("official dispatch lost explicit intent, content, identity, or idempotency")
			}
		})
	}
}

func TestInvalidDispatchIntentPreservesDraftAndDoesNotSend(t *testing.T) {
	for _, line := range []string{"/dispatch --intent", "/dispatch --intent query", "/dispatch --intent QUERY work", "/dispatch --intent invalid work", "/dispatch --intent query --intent mutation work", "/dispatch --unknown work"} {
		t.Run(line, func(t *testing.T) {
			actions := &fakeTUIActions{}
			model := attachedModel(t, actions)
			model, command := executeLine(t, model, line)
			if command != nil || len(actions.controls) != 0 || model.input.Value() != line || !strings.Contains(model.timeline.String(), "usage:") {
				t.Fatal("invalid intent was sent, lost its draft, or lacked a safe usage error")
			}
		})
	}
}
