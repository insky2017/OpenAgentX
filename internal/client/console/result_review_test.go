package console

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	openapi "openagentx/internal/api"
	"openagentx/internal/domain"
)

func TestResultReviewUsesAuthenticatedControlAPI(t *testing.T) {
	calls := 0
	client := newUnixTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cliAuthResponse(w, r) {
			return
		}
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/api/control/v1/tasks/task-result/review" || r.Header.Get("Authorization") != "Bearer opaque-token-value" || r.Header.Get("Idempotency-Key") != "review-idem" {
			t.Errorf("invalid review transport: method=%s path=%s", r.Method, r.URL.Path)
			w.WriteHeader(400)
			return
		}
		var request openapi.ReviewTaskRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.RunID != "run-result" || request.RunVersion != 2 || request.Meta.ExpectedVersion != 5 || request.Decision != "accepted" {
			t.Errorf("request=%+v", request)
		}
		_ = json.NewEncoder(w).Encode(domain.TaskReview{ID: "review-result", TaskID: "task-result", RunID: request.RunID, RunVersion: 2, TaskVersion: 6, Decision: "accepted", Sequence: 10})
	}))
	if _, err := client.LoginCredential(context.Background(), "owner", "fixture"); err != nil {
		t.Fatal(err)
	}
	result, err := client.ReviewTaskResult(context.Background(), "task-result", openapi.ReviewTaskRequest{Meta: openapi.CommandMeta{IdempotencyKey: "review-idem", ExpectedVersion: 5}, RunID: "run-result", RunVersion: 2, Decision: "accepted"})
	if err != nil || calls != 1 || result.TaskVersion != 6 {
		t.Fatalf("review=%+v calls=%d err=%v", result, calls, err)
	}
}
