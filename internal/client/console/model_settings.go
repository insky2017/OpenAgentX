package console

import (
	"context"
	"net/http"
	"net/url"
	"openagentx/internal/domain"
)

func (c *Client) GetAgentModelSettings(ctx context.Context, agentID, backendID string) (domain.AgentModelSettings, error) {
	var result domain.AgentModelSettings
	_, err := c.do(ctx, http.MethodGet, "/api/console/v1/agents/"+url.PathEscape(agentID)+"/model-settings", url.Values{"backend_id": {backendID}}, nil, &result, true, "", false)
	return result, err
}

func (c *Client) SetAgentModelSettings(ctx context.Context, agentID string, request domain.AgentModelSettingsUpdate) (domain.AgentModelSettings, error) {
	var result domain.AgentModelSettings
	_, err := c.do(ctx, http.MethodPut, "/api/console/v1/agents/"+url.PathEscape(agentID)+"/model-settings", nil, request, &result, true, "", false)
	return result, err
}
