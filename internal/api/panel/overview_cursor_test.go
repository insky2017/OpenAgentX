package panel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	openapi "openagentx/internal/api"
	"openagentx/internal/auth/web"
	"openagentx/internal/domain"
)

type overviewCursorState struct {
	*testPanelState
	afterTaskRead func()
	cursorError   error
	reads         []string
}

func (s *overviewCursorState) LatestJournalSequence(ctx context.Context) (int64, error) {
	s.reads = append(s.reads, "cursor")
	if s.cursorError != nil {
		return 0, s.cursorError
	}
	return s.testPanelState.LatestJournalSequence(ctx)
}

func (s *overviewCursorState) ListAgents(ctx context.Context, limit int) ([]domain.AgentIdentity, error) {
	s.reads = append(s.reads, "agents")
	return s.testPanelState.ListAgents(ctx, limit)
}

func (s *overviewCursorState) ListTasks(ctx context.Context, agent string, limit int) ([]domain.Task, error) {
	tasks, err := s.testPanelState.ListTasks(ctx, agent, limit)
	copy := append([]domain.Task(nil), tasks...)
	if s.afterTaskRead != nil {
		s.afterTaskRead()
		s.afterTaskRead = nil
	}
	return copy, err
}

func TestOverviewBootstrapCursorPrecedesProjectionsAndReplaysConcurrentUpdate(t *testing.T) {
	const historicalTail = 267814
	task := validSSETask("task-bootstrap", "quote")
	task.Status = domain.TaskStatusQueued
	state := &overviewCursorState{testPanelState: &testPanelState{
		tasks: []domain.Task{task},
		// Historical rows may no longer have a readable projection. Bootstrap
		// must not attempt to reconstruct this entire history from sequence 0.
		journal: []domain.JournalEvent{{Sequence: historicalTail, ID: "old-worker-event", AggregateType: "worker_instance", AggregateID: "missing-old-worker", EventType: "worker.heartbeat"}},
	}}
	state.afterTaskRead = func() {
		state.tasks[0].Status = domain.TaskStatusRunning
		state.tasks[0].Version++
		state.journal = append(state.journal, domain.JournalEvent{Sequence: historicalTail + 1, ID: "concurrent-update", AggregateType: "task", AggregateID: task.ID, EventType: "task.running"})
	}
	panel := newAuthenticatedPanel(t, web.RoleOwner, state)
	request := httptest.NewRequest(http.MethodGet, openapi.ObserveOverviewPath, nil)
	request.AddCookie(panel.cookie)
	response := httptest.NewRecorder()
	panel.handler.ServeHTTP(response, request)
	var overview struct {
		LiveAfterSequence int64         `json:"live_after_sequence"`
		LatestSequence    int64         `json:"latest_sequence"`
		Tasks             []domain.Task `json:"tasks"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &overview); err != nil || response.Code != http.StatusOK {
		t.Fatalf("overview status=%d err=%v body=%s", response.Code, err, response.Body.String())
	}
	if overview.LiveAfterSequence != historicalTail || overview.LatestSequence != historicalTail+1 || len(overview.Tasks) != 1 || overview.Tasks[0].Status != domain.TaskStatusQueued {
		t.Fatalf("bootstrap cursor skipped a concurrent update: %+v", overview)
	}
	if len(state.reads) < 3 || state.reads[0] != "cursor" || state.reads[1] != "agents" {
		t.Fatalf("bootstrap cursor was not captured before projection reads: %v", state.reads)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := httptest.NewRequest(http.MethodGet, fmt.Sprintf("%s?mode=normal&after_sequence=%d", openapi.ObserveEventsStreamPath, overview.LiveAfterSequence), nil).WithContext(ctx)
	stream.AddCookie(panel.cookie)
	streamResponse := &cancelingRecorder{ResponseRecorder: httptest.NewRecorder(), cancel: cancel}
	panel.handler.ServeHTTP(streamResponse, stream)
	if !strings.Contains(streamResponse.Body.String(), "id: 267815\n") || !strings.Contains(streamResponse.Body.String(), "task.running") || strings.Contains(streamResponse.Body.String(), "old-worker-event") {
		t.Fatalf("bootstrap replay lost concurrent event or replayed old history: %s", streamResponse.Body.String())
	}
}

func TestOverviewBootstrapCursorFailureDoesNotInventZero(t *testing.T) {
	state := &overviewCursorState{testPanelState: &testPanelState{}, cursorError: errors.New("cursor unavailable")}
	panel := newAuthenticatedPanel(t, web.RoleOwner, state)
	request := httptest.NewRequest(http.MethodGet, openapi.ObserveOverviewPath, nil)
	request.AddCookie(panel.cookie)
	response := httptest.NewRecorder()
	panel.handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "live_after_sequence") || len(state.reads) != 1 {
		t.Fatalf("failed cursor lookup produced usable snapshot: status=%d reads=%v body=%s", response.Code, state.reads, response.Body.String())
	}
}

type bootstrapFlushRecorder struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
}

func (r *bootstrapFlushRecorder) Flush() {
	r.ResponseRecorder.Flush()
	r.cancel()
}

func TestSSEEmptyBootstrapFlushesWithoutWaitingForKeepalive(t *testing.T) {
	panel := newAuthenticatedPanel(t, web.RoleOwner, &testPanelState{journal: []domain.JournalEvent{{Sequence: 267814}}})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, openapi.ObserveEventsStreamPath+"?mode=normal&after_sequence=267814", nil).WithContext(ctx)
	request.AddCookie(panel.cookie)
	response := &bootstrapFlushRecorder{ResponseRecorder: httptest.NewRecorder(), cancel: cancel}
	// The recorder cancels as soon as the first flush occurs, so this test
	// proves that an empty first page flushes without waiting for an event.
	panel.handler.ServeHTTP(response, request)
	if !response.Flushed || response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/event-stream" || response.Body.Len() != 0 {
		t.Fatalf("empty bootstrap was not ready: status=%d body=%s", response.Code, response.Body.String())
	}
}
