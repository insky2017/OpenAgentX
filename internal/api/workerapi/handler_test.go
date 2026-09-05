package workerapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	openapi "openagentx/internal/api"
	"openagentx/internal/domain"
)

type recordingControlClaimService struct {
	Service
	request *openapi.ControlClaimRequest
}

func (s *recordingControlClaimService) ClaimWorkerCommand(_ context.Context, _ string, _ string, request openapi.ControlClaimRequest) (*domain.WorkerCommand, error) {
	s.request = &request
	return nil, nil
}

func TestClassifyErrorDoesNotExposeInternalDetails(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "unauthorized", err: domain.ErrUnauthorized, wantStatus: http.StatusUnauthorized, wantCode: openapi.ErrorUnauthorized},
		{name: "stale", err: domain.ErrStaleVersion, wantStatus: http.StatusConflict, wantCode: openapi.ErrorStaleVersion},
		{name: "lease", err: domain.ErrLeaseExpired, wantStatus: http.StatusConflict, wantCode: openapi.ErrorLeaseExpired},
		{name: "fencing", err: domain.ErrFencingRejected, wantStatus: http.StatusConflict, wantCode: openapi.ErrorFencingRejected},
		{name: "bootstrapping", err: domain.ErrAgentNotReady, wantStatus: http.StatusConflict, wantCode: openapi.ErrorConflict},
		{name: "missing Agent", err: domain.ErrAgentNotFound, wantStatus: http.StatusNotFound, wantCode: openapi.ErrorNotFound},
		{name: "internal", err: errors.New("database secret detail"), wantStatus: http.StatusInternalServerError, wantCode: openapi.ErrorInternal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, code, message := classifyError(test.err)
			if status != test.wantStatus || code != test.wantCode {
				t.Fatalf("status=%d code=%s want status=%d code=%s", status, code, test.wantStatus, test.wantCode)
			}
			if test.name == "internal" && message == test.err.Error() {
				t.Fatal("internal error detail leaked to Worker API response")
			}
		})
	}
}

func TestApplyWaitQueryUsesWholeSecondsAndEnforcesLimit(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		initial int
		want    int
		wantErr bool
	}{
		{name: "missing preserves body", initial: 7, want: 7},
		{name: "query overrides body", query: "?wait=30s", initial: 7, want: 30},
		{name: "zero", query: "?wait=0s", initial: 7, want: 0},
		{name: "fraction rejected", query: "?wait=500ms", wantErr: true},
		{name: "negative rejected", query: "?wait=-1s", wantErr: true},
		{name: "over limit rejected", query: "?wait=31s", wantErr: true},
		{name: "invalid rejected", query: "?wait=forever", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/control/claim"+test.query, nil)
			waitSeconds := test.initial
			err := applyWaitQuery(request, &waitSeconds)
			if (err != nil) != test.wantErr {
				t.Fatalf("apply wait error=%v wantErr=%v", err, test.wantErr)
			}
			if err == nil && waitSeconds != test.want {
				t.Fatalf("waitSeconds=%d want=%d", waitSeconds, test.want)
			}
		})
	}
}

func TestControlClaimHandlerAppliesQueryWaitBeforeService(t *testing.T) {
	service := &recordingControlClaimService{}
	handler, err := NewHandler(service, StaticPrincipal("worker-principal"))
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(openapi.ControlClaimRequest{
		WorkerInstanceID: "worker-1", Generation: 1, FencingToken: 1, WaitSeconds: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/workers/worker-1/control/claim?wait=9s", strings.NewReader(string(body)))
	request.Header.Set("Authorization", "Bearer worker-session-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.request == nil || service.request.WaitSeconds != 9 {
		t.Fatalf("status=%d request=%+v body=%s", response.Code, service.request, response.Body.String())
	}

	service.request = nil
	invalid := httptest.NewRequest(http.MethodPost, "/api/v1/workers/worker-1/control/claim?wait=31s", strings.NewReader(string(body)))
	invalid.Header.Set("Authorization", "Bearer worker-session-token")
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusBadRequest || service.request != nil {
		t.Fatalf("invalid status=%d request=%+v body=%s", invalidResponse.Code, service.request, invalidResponse.Body.String())
	}
}

type networkPullService struct {
	Service
	called *openapi.NetworkBindingPullRequest
}

func (s *networkPullService) PullNetworkBindings(_ context.Context, _ string, _ string, request openapi.NetworkBindingPullRequest) ([]domain.NetworkBinding, error) {
	s.called = &request
	return []domain.NetworkBinding{}, nil
}

func TestNetworkBindingPullUsesWorkerPathAndFencingRequest(t *testing.T) {
	service := &networkPullService{}
	handler, err := NewHandler(service, StaticPrincipal("worker-principal"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(openapi.NetworkBindingPullRequest{WorkerInstanceID: "worker-1", Generation: 2, FencingToken: 3})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/workers/worker-1/network-bindings/pull", strings.NewReader(string(body)))
	request.Header.Set("Authorization", "Bearer worker-session-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.called == nil || service.called.Generation != 2 || service.called.FencingToken != 3 {
		t.Fatalf("status=%d request=%+v body=%s", response.Code, service.called, response.Body.String())
	}
}
