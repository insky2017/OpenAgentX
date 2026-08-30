package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	openapi "openagentx/internal/api"
	"openagentx/internal/auth/web"
	"openagentx/internal/controlplane"
	"openagentx/internal/domain"
)

type recordingAdminState struct {
	command *domain.WorkerCommand
}

func (s *recordingAdminState) CreateWorkerCommand(_ context.Context, command *domain.WorkerCommand, _ *domain.JournalEvent) (*domain.WorkerCommand, error) {
	copy := *command
	s.command = &copy
	return &copy, nil
}

func (s *recordingAdminState) RevokeWorkerLease(context.Context, string, int64, *domain.JournalEvent) (*domain.WorkerInstance, error) {
	return nil, domain.ErrNotFound
}

type adminFixture struct {
	handler *Handler
	session *web.Session
	cookie  *http.Cookie
	state   *recordingAdminState
}

func newAdminFixture(t *testing.T, role web.Role) adminFixture {
	t.Helper()
	digest, err := web.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	manager := web.NewManager(web.Config{})
	if err := manager.AddUser(web.User{
		ID: "human-owner", WebUserID: "web-owner", Username: "owner", Roles: []web.Role{role}, PasswordDigest: digest,
	}); err != nil {
		t.Fatal(err)
	}
	session, err := manager.Login("owner", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	cookieResponse := httptest.NewRecorder()
	web.SetSessionCookie(cookieResponse, session)
	state := &recordingAdminState{}
	service, err := controlplane.NewWorkerAdminService(state, time.Now, func(prefix string) string { return prefix + "-test" })
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(service, manager)
	if err != nil {
		t.Fatal(err)
	}
	return adminFixture{handler: handler, session: session, cookie: cookieResponse.Result().Cookies()[0], state: state}
}

func adminRequest(t *testing.T, fixture adminFixture, csrf, headerKey string) *http.Request {
	t.Helper()
	body, err := json.Marshal(openapi.WorkerAdminRequest{
		Meta:               openapi.CommandMeta{IdempotencyKey: "health-key", ExpectedVersion: 7},
		ExpectedGeneration: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/admin/v1/workers/worker-7/health-check", bytes.NewReader(body))
	request.AddCookie(fixture.cookie)
	request.Header.Set("X-CSRF-Token", csrf)
	request.Header.Set("Idempotency-Key", headerKey)
	return request
}

func TestHealthCheckRequiresOwnerCSRFAndMatchingIdempotencyKey(t *testing.T) {
	owner := newAdminFixture(t, web.RoleOwner)

	unauthenticated := httptest.NewRecorder()
	owner.handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodPost, "/api/admin/v1/workers/worker-7/health-check", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d", unauthenticated.Code)
	}

	operator := newAdminFixture(t, web.RoleOperator)
	operatorResponse := httptest.NewRecorder()
	operator.handler.ServeHTTP(operatorResponse, adminRequest(t, operator, operator.session.CSRFToken, "health-key"))
	if operatorResponse.Code != http.StatusForbidden {
		t.Fatalf("operator status=%d body=%s", operatorResponse.Code, operatorResponse.Body.String())
	}

	for name, testCase := range map[string]struct {
		csrf, header string
		status       int
	}{
		"missing csrf":      {header: "health-key", status: http.StatusForbidden},
		"incorrect csrf":    {csrf: "incorrect", header: "health-key", status: http.StatusForbidden},
		"missing header":    {csrf: owner.session.CSRFToken, status: http.StatusBadRequest},
		"mismatched header": {csrf: owner.session.CSRFToken, header: "other-key", status: http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			owner.handler.ServeHTTP(response, adminRequest(t, owner, testCase.csrf, testCase.header))
			if response.Code != testCase.status {
				t.Fatalf("status=%d want=%d body=%s", response.Code, testCase.status, response.Body.String())
			}
		})
	}

	response := httptest.NewRecorder()
	owner.handler.ServeHTTP(response, adminRequest(t, owner, owner.session.CSRFToken, "health-key"))
	if response.Code != http.StatusOK {
		t.Fatalf("owner status=%d body=%s", response.Code, response.Body.String())
	}
	if owner.state.command == nil || owner.state.command.Kind != domain.WorkerCommandHealthCheck ||
		owner.state.command.RequestedBy != "human-owner" || owner.state.command.Generation != 7 {
		t.Fatalf("persisted command=%+v", owner.state.command)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Admin cache policy=%q", response.Header().Get("Cache-Control"))
	}
}
