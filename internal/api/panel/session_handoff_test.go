package panel

import (
	"net/http"
	openapi "openagentx/internal/api"
	web "openagentx/internal/auth/web"
	"openagentx/internal/domain"
	"testing"
)

func TestSessionHandoffHTTPRequiresOwner(t *testing.T) {
	panel := newAuthenticatedPanel(t, web.RoleOperator, &testPanelState{})
	request := openapi.CreateTaskRequest{Meta: openapi.CommandMeta{IdempotencyKey: "operator-handoff"}, TargetAgentID: "quote", OrganizationID: "org-main", DispatchMode: domain.DispatchModeDirect, Intent: domain.TaskIntentQuery, Content: "handoff", NewSession: &domain.NewSessionRequest{BackendID: "local"}}
	response := postContinueTask(t, panel, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("operator handoff=%d %s", response.Code, response.Body.String())
	}
}
