package console

import (
	"context"
	"net/http"
	"net/url"

	"openagentx/internal/domain"
)

func (c *Client) ReadAgentSession(ctx context.Context, agentID, backendID string) (*domain.AgentSession, error) {
	var result domain.AgentSession
	_, err := c.do(ctx, http.MethodGet, "/api/console/v1/agents/"+url.PathEscape(agentID)+"/session", url.Values{"backend_id": {backendID}}, nil, &result, true, "", false)
	return &result, err
}
