package workerapi

import (
	"errors"
	"net/http"
	"testing"

	openapi "openagentx/internal/api"
	"openagentx/internal/domain"
)

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
