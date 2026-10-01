package panel

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	openapi "openagentx/internal/api"
	"openagentx/internal/auth/web"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

// This tests the HTTP projection against scheduler-filtered backend results.
// Worker networking/application itself is covered by repository tests and R/I.
func TestPanelReadinessLeaseGenerationRecoveryAndBusyQueue(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	old := domain.WorkerInstance{ID: "worker-old", AgentID: "quote", Generation: 1, Status: domain.WorkerStatusOnline, LeaseUntil: now.Add(time.Hour)}
	latest := old
	latest.ID = "worker-new"
	latest.Generation = 2
	state := &backendPanelState{testPanelState: &testPanelState{agents: []domain.AgentIdentity{{ID: "quote", DisplayName: "Quote", Version: 1, Status: domain.AgentIdentityActive}}}, backends: map[string][]openruntime.BackendRegistration{
		old.ID:    {{BackendID: "local", Health: openruntime.BackendHealthy}},
		latest.ID: {{BackendID: "local", Health: openruntime.BackendUnavailable}},
	}}
	panel := newAuthenticatedPanel(t, web.RoleOwner, state)
	panel.handler.now = func() time.Time { return now }
	check := func(wantReady, wantStart bool, reason string) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, openapi.ObserveOverviewPath, nil)
		request.AddCookie(panel.cookie)
		response := httptest.NewRecorder()
		panel.handler.ServeHTTP(response, request)
		var overview struct {
			Agents []readyAgent `json:"agents"`
		}
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &overview) != nil || len(overview.Agents) != 1 {
			t.Fatalf("overview status=%d body=%s", response.Code, response.Body.String())
		}
		got := overview.Agents[0].Readiness
		if got.Ready != wantReady || got.CanStartNow != wantStart || !strings.Contains(got.Reason, reason) {
			t.Fatalf("readiness=%+v, want ready=%t start=%t reason=%s", got, wantReady, wantStart, reason)
		}
		if !wantReady && got.NextAction == "" {
			t.Fatal("blocked readiness lacks recovery action")
		}
	}
	state.workers = []domain.WorkerInstance{old}
	check(true, true, "可开始")
	// Exact lease boundary must fail closed even with a healthy backend.
	state.workers[0].LeaseUntil = now
	check(false, false, "过期")
	// Latest generation must not inherit the old generation's applied backend,
	// even when the old row appears later in the list and still looks healthy.
	state.workers = []domain.WorkerInstance{latest, old}
	check(false, false, "尚未就绪")
	state.backends[latest.ID][0].Health = openruntime.BackendHealthy
	check(true, true, "可开始")
	for _, status := range []domain.TaskStatus{domain.TaskStatusRunning, domain.TaskStatusWaitingApproval, domain.TaskStatusCancelRequested} {
		state.tasks = []domain.Task{{ID: "busy-task", TargetAgentID: "quote", Status: status}}
		check(true, false, "排队")
	}
	state.tasks = []domain.Task{{ID: "old-task", TargetAgentID: "quote", Status: domain.TaskStatusUncertain}, {ID: "other-task", TargetAgentID: "other", Status: domain.TaskStatusRunning}}
	check(true, true, "可开始")
	state.backendErr = errors.New("backend snapshot unavailable")
	check(false, false, "尚未就绪")
	state.backendErr = nil
	state.workers[0].Status = domain.WorkerStatusDraining
	check(false, false, "暂停")
}

func TestPanelReadinessOfficialRepositoryRejectsUnappliedAndExpiredWorker(t *testing.T) {
	fixture := newPanelIntegrationFixture(t, false)
	panel := reviewPanel(t, fixture)
	// This fixture registers a Worker without a proven applied network policy.
	// Presence and an active Task therefore cannot make the Agent ready.
	for _, tc := range []struct {
		now    time.Time
		reason string
	}{
		{fixture.guard.CheckedAt, "尚未就绪"},
		{fixture.worker.LeaseUntil, "过期"},
	} {
		panel.handler.now = func() time.Time { return tc.now }
		request := httptest.NewRequest(http.MethodGet, openapi.ObserveOverviewPath, nil)
		request.AddCookie(panel.cookie)
		response := httptest.NewRecorder()
		panel.handler.ServeHTTP(response, request)
		var overview struct {
			Agents []readyAgent `json:"agents"`
		}
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &overview) != nil || len(overview.Agents) != 1 {
			t.Fatalf("repository readiness status=%d body=%s", response.Code, response.Body.String())
		}
		got := overview.Agents[0].Readiness
		if got.Ready || got.CanStartNow || !strings.Contains(got.Reason, tc.reason) {
			t.Fatalf("repository readiness=%+v want reason=%s", got, tc.reason)
		}
	}
}
