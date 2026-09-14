package console

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	"openagentx/internal/domain"
)

func newUnixTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	socketPath := filepath.Join(t.TempDir(), "console.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Shutdown(context.Background())
		_ = listener.Close()
	})
	client, err := NewUnixClient(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func loginResponse(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: "openagentx_session", Value: "session-value", Path: "/"})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(openapi.WebSessionResponse{CSRFToken: "csrf-value"})
}

func TestFollowReconnectsFromLastSequenceAndRefreshesGeneration(t *testing.T) {
	var mu sync.Mutex
	attachCount := 0
	var afterValues []int64
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case openapi.AuthLoginPath:
			loginResponse(w)
		case consoleapi.AttachPath:
			mu.Lock()
			attachCount++
			generation := attachCount
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(consoleapi.AttachResponse{AgentID: "quote", Generation: int64(generation), WorkerInstanceID: "worker-" + strconv.Itoa(generation)})
		case openapi.ObserveEventsStreamPath:
			after, _ := strconv.ParseInt(r.URL.Query().Get("after_sequence"), 10, 64)
			mu.Lock()
			afterValues = append(afterValues, after)
			connection := len(afterValues)
			mu.Unlock()
			sequence := int64(4 + connection)
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("id: " + strconv.FormatInt(sequence, 10) + "\ndata: {\"sequence\":" + strconv.FormatInt(sequence, 10) + ",\"event_id\":\"event\"}\n\n"))
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			if connection > 1 {
				<-r.Context().Done()
			}
		default:
			http.NotFound(w, r)
		}
	})
	client := newUnixTestClient(t, handler)
	client.reconnectDelay = time.Millisecond
	if err := client.Login(ctx, "owner", "password"); err != nil {
		t.Fatal(err)
	}
	var generations, sequences []int64
	err := client.Follow(ctx, "quote", consoleapi.ModeNormal, 0,
		func(attached consoleapi.AttachResponse) error {
			generations = append(generations, attached.Generation)
			return nil
		},
		func(event openapi.JournalEventReadModel) error {
			sequences = append(sequences, event.Sequence)
			if event.Sequence == 6 {
				cancel()
			}
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(generations, []int64{1, 2}) || !reflect.DeepEqual(sequences, []int64{5, 6}) || !reflect.DeepEqual(afterValues, []int64{0, 5}) {
		t.Fatalf("reconnect generations=%v sequences=%v cursors=%v", generations, sequences, afterValues)
	}
}

func TestControlMethodsUseAuthenticatedOfficialAPIs(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == openapi.AuthLoginPath {
			loginResponse(w)
			return
		}
		if cookie, err := r.Cookie("openagentx_session"); err != nil || cookie.Value != "session-value" {
			http.Error(w, "missing session", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet && r.Header.Get("X-CSRF-Token") != "csrf-value" {
			http.Error(w, "missing csrf", http.StatusForbidden)
			return
		}
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	})
	client := newUnixTestClient(t, handler)
	ctx := context.Background()
	if err := client.Login(ctx, "owner", "password"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Dispatch(ctx, openapi.CreateTaskRequest{Meta: openapi.CommandMeta{IdempotencyKey: "dispatch"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Steer(ctx, "task-1", openapi.CreateMessageRequest{Meta: openapi.CommandMeta{IdempotencyKey: "steer"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Cancel(ctx, "task-1", openapi.CancelTaskRequest{Meta: openapi.CommandMeta{IdempotencyKey: "cancel"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.DecideApproval(ctx, "approval-1", openapi.DecideApprovalRequest{Meta: openapi.CommandMeta{IdempotencyKey: "approval"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.WorkerCommand(ctx, "worker-1", 3, domain.WorkerCommandStop, "stop", false); err != nil {
		t.Fatal(err)
	}
	if _, err := client.WorkerCommand(ctx, "worker-1", 3, domain.WorkerCommandForceStop, "force", true); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"POST " + openapi.ControlCreateTaskPath,
		"POST /api/control/v1/tasks/task-1/messages",
		"POST /api/control/v1/tasks/task-1/cancel",
		"POST /api/control/v1/approvals/approval-1/decisions",
		"POST /api/admin/v1/workers/worker-1/stop",
		"POST /api/admin/v1/workers/worker-1/force-stop",
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("official API paths=%v want=%v", paths, want)
	}
}

func TestEventStreamFailsClosedOnMissingIDOrSequenceMismatch(t *testing.T) {
	for name, stream := range map[string]string{
		"missing id":        "data: {\"sequence\":1}\n\n",
		"sequence mismatch": "id: 2\ndata: {\"sequence\":1}\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := readEventStream(strings.NewReader(stream), 0, func(openapi.JournalEventReadModel) error { return nil }); err == nil {
				t.Fatal("unsafe SSE frame was accepted")
			}
		})
	}
}
