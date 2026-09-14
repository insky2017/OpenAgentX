package console

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
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
	directory, err := os.MkdirTemp("", "oax-uds-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	socketPath := filepath.Join(directory, "s")
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

func cliAuthResponse(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case openapi.CLIInstallationProbePath:
		_ = json.NewEncoder(w).Encode(openapi.CLIInstallationResponse{InstallationID: "installation-test"})
		return true
	case openapi.CLIAuthLoginPath:
		_ = json.NewEncoder(w).Encode(openapi.CLILoginResponse{CLISessionResponse: openapi.CLISessionResponse{
			Principal: openapi.CLIPrincipal{TokenID: "token-id", Username: "owner"}, InstallationID: "installation-test",
			AbsoluteExpiresAt: time.Now().Add(time.Hour)}, Token: "opaque-token-value"})
		return true
	default:
		return false
	}
}

func TestFollowStartsAtAttachCursorAndReconnectsFromLastAppliedSequence(t *testing.T) {
	var mu sync.Mutex
	attachCount := 0
	var afterValues []int64
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cliAuthResponse(w, r) {
			return
		}
		switch r.URL.Path {
		case consoleapi.AttachPath:
			mu.Lock()
			attachCount++
			generation := attachCount
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(consoleapi.AttachResponse{AgentID: "quote", Generation: int64(generation), WorkerInstanceID: "worker-" + strconv.Itoa(generation), SnapshotSequence: 4})
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
	if _, err := client.LoginCredential(ctx, "owner", "password"); err != nil {
		t.Fatal(err)
	}
	var generations, sequences []int64
	err := client.Follow(ctx, "quote", consoleapi.ModeNormal,
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
	if !reflect.DeepEqual(generations, []int64{1}) || !reflect.DeepEqual(sequences, []int64{5, 6}) || !reflect.DeepEqual(afterValues, []int64{4, 5}) {
		t.Fatalf("reconnect generations=%v sequences=%v cursors=%v", generations, sequences, afterValues)
	}
}

func TestFollowReattachesOnlyAfterStructuredRetentionGap(t *testing.T) {
	var mu sync.Mutex
	attachCount := 0
	var afterValues []int64
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cliAuthResponse(w, r) {
			return
		}
		switch r.URL.Path {
		case consoleapi.AttachPath:
			mu.Lock()
			attachCount++
			count := attachCount
			mu.Unlock()
			sequence := int64(4)
			if count > 1 {
				sequence = 20
			}
			_ = json.NewEncoder(w).Encode(consoleapi.AttachResponse{AgentID: "quote", WorkerInstanceID: "worker-current", Generation: 3, SnapshotSequence: sequence})
		case openapi.ObserveEventsStreamPath:
			after, _ := strconv.ParseInt(r.URL.Query().Get("after_sequence"), 10, 64)
			mu.Lock()
			afterValues = append(afterValues, after)
			requestCount := len(afterValues)
			mu.Unlock()
			if requestCount == 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(openapi.ErrorResponse{Code: openapi.ErrorEventCursorExpired, Message: "expired"})
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("id: 21\ndata: {\"sequence\":21,\"event_id\":\"event-21\"}\n\n"))
		default:
			http.NotFound(w, r)
		}
	})
	client := newUnixTestClient(t, handler)
	client.reconnectDelay = time.Millisecond
	if _, err := client.LoginCredential(ctx, "owner", "password"); err != nil {
		t.Fatal(err)
	}
	var snapshots, events []int64
	err := client.Follow(ctx, "quote", consoleapi.ModeNormal,
		func(attached consoleapi.AttachResponse) error {
			snapshots = append(snapshots, attached.SnapshotSequence)
			return nil
		}, func(event openapi.JournalEventReadModel) error {
			events = append(events, event.Sequence)
			cancel()
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(snapshots, []int64{4, 20}) || !reflect.DeepEqual(afterValues, []int64{4, 20}) || !reflect.DeepEqual(events, []int64{21}) {
		t.Fatalf("gap recovery snapshots=%v cursors=%v events=%v", snapshots, afterValues, events)
	}
}

func TestControlMethodsUseAuthenticatedOfficialAPIs(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cliAuthResponse(w, r) {
			return
		}
		if r.Header.Get("Authorization") != "Bearer opaque-token-value" {
			http.Error(w, "missing session", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("X-CSRF-Token") != "" {
			http.Error(w, "unexpected csrf", http.StatusForbidden)
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
	if _, err := client.LoginCredential(ctx, "owner", "password"); err != nil {
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

func TestStoredCredentialIsNotSentAfterSocketInstallationReplacement(t *testing.T) {
	authenticatedRequests := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == openapi.CLIInstallationProbePath {
			_ = json.NewEncoder(w).Encode(openapi.CLIInstallationResponse{InstallationID: "replacement-installation"})
			return
		}
		if r.Header.Get("Authorization") != "" {
			authenticatedRequests++
		}
		http.NotFound(w, r)
	})
	client := newUnixTestClient(t, handler)
	if err := client.UseCredential(context.Background(), "original-installation", "stored-secret-token"); err == nil {
		t.Fatal("credential for replaced installation was accepted")
	}
	if authenticatedRequests != 0 || client.bearerToken != "" {
		t.Fatalf("credential crossed installation boundary requests=%d retained=%v", authenticatedRequests, client.bearerToken != "")
	}
}

func TestSessionValidatesStoredBearerAndReturnsStructuredUnauthenticated(t *testing.T) {
	const token = "stored-secret-token"
	sessionCalls := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case openapi.CLIInstallationProbePath:
			_ = json.NewEncoder(w).Encode(openapi.CLIInstallationResponse{InstallationID: "installation-test"})
		case openapi.CLIAuthSessionPath:
			sessionCalls++
			if r.Header.Get("Authorization") != "Bearer "+token {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(openapi.ErrorResponse{Code: openapi.ErrorCLIUnauthenticated, Message: "CLI authentication required"})
				return
			}
			_ = json.NewEncoder(w).Encode(openapi.CLISessionResponse{Principal: openapi.CLIPrincipal{TokenID: "token-id", Username: "owner"},
				InstallationID: "installation-test", AbsoluteExpiresAt: time.Now().Add(time.Hour)})
		default:
			http.NotFound(w, r)
		}
	})
	client := newUnixTestClient(t, handler)
	if err := client.UseCredential(context.Background(), "installation-test", token); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Session(context.Background()); err != nil {
		t.Fatal(err)
	}
	client.bearerToken = "rejected-token"
	_, err := client.Session(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized || apiErr.Code != openapi.ErrorCLIUnauthenticated {
		t.Fatalf("structured Session error=%v", err)
	}
	if sessionCalls != 2 {
		t.Fatalf("Session calls=%d", sessionCalls)
	}
}

func TestLegacyDirectPasswordLoginFailsBeforeNetwork(t *testing.T) {
	requests := 0
	client := newUnixTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.NotFound(w, r)
	}))
	if err := client.Login(context.Background(), "owner", "must-not-be-sent"); err == nil {
		t.Fatal("legacy direct password login was accepted")
	}
	if requests != 0 {
		t.Fatalf("legacy password login reached UDS requests=%d", requests)
	}
}

func TestEventStreamFailsClosedOnMissingIDOrSequenceMismatch(t *testing.T) {
	for name, stream := range map[string]string{
		"missing id":        "data: {\"sequence\":1}\n\n",
		"sequence mismatch": "id: 2\ndata: {\"sequence\":1}\n\n",
		"backward sequence": "id: 4\ndata: {\"sequence\":4}\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			after := int64(0)
			if name == "backward sequence" {
				after = 5
			}
			if _, err := readEventStream(strings.NewReader(stream), after, func(openapi.JournalEventReadModel) error { return nil }); err == nil {
				t.Fatal("unsafe SSE frame was accepted")
			}
		})
	}
}

func TestEventStreamAdvancesOnlyAfterSuccessfulApplyAndDeduplicates(t *testing.T) {
	applied := 0
	stream := "id: 5\ndata: {\"sequence\":5,\"event_id\":\"duplicate\"}\n\n" +
		"id: 6\ndata: {\"sequence\":6,\"event_id\":\"new\"}\n\n"
	cursor, err := readEventStream(strings.NewReader(stream), 5, func(openapi.JournalEventReadModel) error {
		applied++
		return errors.New("apply failed")
	})
	if err == nil || cursor != 5 || applied != 1 {
		t.Fatalf("failed apply cursor=%d applied=%d err=%v", cursor, applied, err)
	}
}

func TestNewUnixClientDistinguishesMissingAndNonSocketPaths(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.sock")
	if _, err := NewUnixClient(missing); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing socket error=%v", err)
	}
	regular := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(regular, []byte("not a socket"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewUnixClient(regular); err == nil || !strings.Contains(err.Error(), "not a Unix socket") {
		t.Fatalf("non-socket error=%v", err)
	}
}
