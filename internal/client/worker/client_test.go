package worker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	openapi "openagentx/internal/api"
	"openagentx/internal/domain"
)

func TestBeginAttemptPreservesDomainErrorsAcrossHTTP(t *testing.T) {
	testCases := []struct {
		name       string
		statusCode int
		apiCode    string
		domainErrs []error
	}{
		{name: "unsupported", statusCode: http.StatusUnprocessableEntity, apiCode: openapi.ErrorUnsupportedCapability, domainErrs: []error{domain.ErrUnsupportedCapability}},
		{name: "unauthorized", statusCode: http.StatusUnauthorized, apiCode: openapi.ErrorUnauthorized, domainErrs: []error{domain.ErrUnauthorized}},
		{name: "lease", statusCode: http.StatusConflict, apiCode: openapi.ErrorLeaseExpired, domainErrs: []error{domain.ErrLeaseExpired}},
		{name: "fencing", statusCode: http.StatusConflict, apiCode: openapi.ErrorFencingRejected, domainErrs: []error{domain.ErrFencingRejected}},
		{name: "stale", statusCode: http.StatusConflict, apiCode: openapi.ErrorStaleVersion, domainErrs: []error{domain.ErrStaleVersion, domain.ErrSessionGenerationConflict}},
	}
	responses := make(map[string]struct {
		statusCode int
		apiCode    string
	}, len(testCases))
	for _, testCase := range testCases {
		responses[replacePath(openapi.MailboxBeginAttemptPath, "{item-id}", testCase.name)] = struct {
			statusCode int
			apiCode    string
		}{testCase.statusCode, testCase.apiCode}
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		failure, ok := responses[request.URL.Path]
		if !ok || request.Method != http.MethodPost || request.Header.Get("Authorization") != "Bearer worker-token" {
			http.Error(response, "unexpected request", http.StatusInternalServerError)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(failure.statusCode)
		_ = json.NewEncoder(response).Encode(openapi.ErrorResponse{Code: failure.apiCode, Message: "fixed diagnostic"})
	}))
	defer server.Close()
	client, err := NewHTTPWorkerClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.sessionToken = "worker-token"

	allDomainErrors := []error{
		domain.ErrUnsupportedCapability, domain.ErrUnauthorized, domain.ErrLeaseExpired,
		domain.ErrFencingRejected, domain.ErrStaleVersion, domain.ErrSessionGenerationConflict,
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, beginErr := client.BeginAttempt(context.Background(), testCase.name, openapi.BeginAttemptRequest{})
			var apiErr *APIError
			if !errors.As(beginErr, &apiErr) || apiErr.StatusCode != testCase.statusCode || apiErr.Code != testCase.apiCode {
				t.Fatalf("BeginAttempt error=%v", beginErr)
			}
			for _, target := range allDomainErrors {
				want := false
				for _, expected := range testCase.domainErrs {
					want = want || target == expected
				}
				if errors.Is(beginErr, target) != want {
					t.Fatalf("errors.Is(%v)=%v want=%v", target, errors.Is(beginErr, target), want)
				}
			}
		})
	}
}
