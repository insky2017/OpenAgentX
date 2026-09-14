package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	openapi "openagentx/internal/api"
	cliauth "openagentx/internal/auth/cli"
	"openagentx/internal/domain"
)

func TestRoleHierarchy(t *testing.T) {
	operator := Principal{Roles: []domain.WebRole{domain.WebRoleOperator}}
	if !operator.HasRole(domain.WebRoleViewer) || !operator.HasRole(domain.WebRoleOperator) || operator.HasRole(domain.WebRoleOwner) {
		t.Fatalf("operator hierarchy mismatch")
	}
	owner := Principal{Roles: []domain.WebRole{domain.WebRoleOwner}}
	if !owner.HasRole(domain.WebRoleViewer) || !owner.HasRole(domain.WebRoleOperator) || !owner.HasRole(domain.WebRoleOwner) {
		t.Fatalf("owner hierarchy mismatch")
	}
}

func TestCLIAuthorizerWritesStableAuthenticationErrors(t *testing.T) {
	authorizer, err := NewCLIAuthorizer(new(cliauth.Service))
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		err    error
		status int
		code   string
	}{
		{err: cliauth.ErrUnauthenticated, status: http.StatusUnauthorized, code: openapi.ErrorCLIUnauthenticated},
		{err: cliauth.ErrForbidden, status: http.StatusForbidden, code: openapi.ErrorCLIForbidden},
	} {
		response := httptest.NewRecorder()
		authorizer.WriteFailure(response, testCase.err)
		var body openapi.ErrorResponse
		if response.Code != testCase.status || json.Unmarshal(response.Body.Bytes(), &body) != nil || body.Code != testCase.code {
			t.Fatalf("auth failure status=%d body=%s", response.Code, response.Body.String())
		}
	}
}
