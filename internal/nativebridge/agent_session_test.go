package nativebridge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"openagentx/internal/domain"
)

type sessionControlFixture struct {
	bridgeControlFixture
	session domain.AgentSession
}

func (c *sessionControlFixture) ReadAgentSession(context.Context, string, string) (*domain.AgentSession, error) {
	return &c.session, nil
}

func TestOldNativeViewCannotWriteDuringOrAfterHandoff(t *testing.T) {
	for _, session := range []domain.AgentSession{
		{ThreadID: "old-thread", PendingTaskID: "handoff-task"},
		{ThreadID: "new-thread", Version: 1},
	} {
		c := &sessionControlFixture{session: session}
		b := &bridge{control: c, agentID: "agent", backendID: "codex", threadID: "old-thread"}
		for _, method := range []string{"turn/start", "turn/steer", "turn/interrupt", "thread/settings/update", "config/batchWrite"} {
			_, err := b.request(context.Background(), nil, method, json.RawMessage(`{"threadId":"old-thread"}`))
			if err == nil || (!strings.Contains(err.Error(), "交接") && !strings.Contains(err.Error(), "旧会话")) {
				t.Fatalf("%s: %v", method, err)
			}
		}
		if len(c.dispatches)+len(c.steers)+len(c.cancels)+len(c.updates) != 0 {
			t.Fatal("stale view issued a write")
		}
	}
}
