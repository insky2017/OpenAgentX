package nativebridge

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	consoleclient "openagentx/internal/client/console"
	"openagentx/internal/domain"
)

type agentSessionReader interface {
	ReadAgentSession(context.Context, string, string) (*domain.AgentSession, error)
}

func readAgentSession(ctx context.Context, source agentSessionReader, agent, backend string) (*domain.AgentSession, error) {
	current, err := source.ReadAgentSession(ctx, agent, backend)
	var apiError *consoleclient.APIError
	// A new CLI can temporarily coexist with the previously installed daemon.
	// That daemon has no session endpoint and cannot rotate an Agent. Never
	// hide auth, transport or server failures once the endpoint is available.
	if errors.As(err, &apiError) && apiError.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	return current, err
}

func (b *bridge) checkActiveSession(ctx context.Context) error {
	source, ok := b.control.(agentSessionReader)
	if !ok {
		return nil
	}
	current, err := readAgentSession(ctx, source, b.agentID, b.backendID)
	if err != nil {
		return err
	}
	if current == nil {
		return nil
	}
	if current.PendingTaskID != "" {
		return fmt.Errorf("Agent 正在交接新会话（Task %s），请等待结果后重新打开终端", current.PendingTaskID)
	}
	if current.ThreadID != "" && current.ThreadID != b.threadID {
		return fmt.Errorf("此终端仍连接旧会话；请退出后运行 openagentx agent open %s --native 连接当前会话", b.agentID)
	}
	return nil
}
