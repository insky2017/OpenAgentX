package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agentbus/internal/connector"
	"agentbus/internal/domain"
	"agentbus/internal/server"
	"agentbus/internal/service"
	"agentbus/internal/store"
)

type mockRunner struct{}

func (m *mockRunner) Run(ctx context.Context, stdin string, name string, args ...string) (string, error) {
	return "%51 0\n", nil
}

func setupTestServer(t *testing.T) (*http.Client, string, *store.SQLiteStore, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "agentbus-server-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(dir, "server_test.db")
	socketPath := filepath.Join(dir, "agentbus.sock")

	s, err := store.OpenSQLite(dbPath)
	if err != nil {
		os.RemoveAll(dir)
		t.Fatalf("failed to open store: %v", err)
	}

	conn := connector.NewTmuxConnector(&mockRunner{})
	svc := service.NewService(s, conn, nil)
	srv := server.NewServer(svc, socketPath, nil)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	// Wait for socket to be ready
	for i := 0; i < 50; i++ {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	cleanup := func() {
		cancel()
		<-errCh
		s.Close()
		os.RemoveAll(dir)
	}

	return client, socketPath, s, cleanup
}

func TestServerE2EUnixSocket(t *testing.T) {
	client, socketPath, _, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. Register coordinator
	coordBody, _ := json.Marshal(map[string]any{
		"id":        "coordinator",
		"role":      "coordinator",
		"connector": "tmux",
		"address":   "%50",
	})
	resp, err := client.Post("http://unix/api/v1/agents", "application/json", bytes.NewReader(coordBody))
	if err != nil {
		t.Fatalf("Register coordinator failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	// 2. Register quote
	quoteBody, _ := json.Marshal(map[string]any{
		"id":        "quote",
		"role":      "quote",
		"connector": "tmux",
		"address":   "%51",
	})
	resp, err = client.Post("http://unix/api/v1/agents", "application/json", bytes.NewReader(quoteBody))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("Register quote failed: %v, status: %d", err, resp.StatusCode)
	}

	// 3. List agents
	resp, err = client.Get("http://unix/api/v1/agents")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("List agents failed: %v", err)
	}
	var agentsResp struct {
		Agents []*domain.Agent `json:"agents"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&agentsResp)
	if len(agentsResp.Agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(agentsResp.Agents))
	}

	// 4. Submit task
	submitBody, _ := json.Marshal(map[string]any{
		"sender_agent_id": "coordinator",
		"target_agent_id": "quote",
		"idempotency_key": "e2e-demo-1",
		"content":         "E2E test task",
	})
	resp, err = client.Post("http://unix/api/v1/tasks", "application/json", bytes.NewReader(submitBody))
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("Submit task failed: %v, status: %d", err, resp.StatusCode)
	}
	var submitResp struct {
		Task        *domain.Task `json:"task"`
		IsDuplicate bool         `json:"is_duplicate"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&submitResp)
	taskID := submitResp.Task.ID

	// 5. Get task as quote
	resp, err = client.Get("http://unix/api/v1/tasks/" + taskID + "?agent=quote")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("Get task failed: %v", err)
	}
	var getTaskResp struct {
		Task     *domain.Task      `json:"task"`
		Messages []*domain.Message `json:"messages"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&getTaskResp)
	if getTaskResp.Task.Status != domain.TaskStatusQueued || len(getTaskResp.Messages) != 1 {
		t.Fatalf("unexpected get task response: %+v", getTaskResp)
	}

	// 6. Ack task as quote
	ackBody, _ := json.Marshal(map[string]any{"agent": "quote"})
	resp, err = client.Post("http://unix/api/v1/tasks/"+taskID+"/ack", "application/json", bytes.NewReader(ackBody))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("Ack task failed: %v", err)
	}

	// 7. Update status as quote
	statBody, _ := json.Marshal(map[string]any{"agent": "quote", "message": "processing step 1"})
	resp, err = client.Post("http://unix/api/v1/tasks/"+taskID+"/status", "application/json", bytes.NewReader(statBody))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("Status update failed: %v", err)
	}

	// 8. Send supplemental message as coordinator
	sendMsgBody, _ := json.Marshal(map[string]any{"from": "coordinator", "content": "supplemental input"})
	resp, err = client.Post("http://unix/api/v1/tasks/"+taskID+"/send", "application/json", bytes.NewReader(sendMsgBody))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("Send supplemental msg failed: %v", err)
	}

	// 8b. Read back task and verify supplemental message is present
	resp, err = client.Get("http://unix/api/v1/tasks/" + taskID + "?agent=quote")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("Get task after send failed: %v", err)
	}
	var getTaskAfterSend struct {
		Task     *domain.Task      `json:"task"`
		Messages []*domain.Message `json:"messages"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&getTaskAfterSend)
	if len(getTaskAfterSend.Messages) < 3 {
		t.Fatalf("expected at least 3 messages (instruction + status + supplement), got %d", len(getTaskAfterSend.Messages))
	}
	var foundSupplement bool
	for _, m := range getTaskAfterSend.Messages {
		if m.Kind == domain.MessageKindSupplement && m.Content == "supplemental input" {
			foundSupplement = true
			break
		}
	}
	if !foundSupplement {
		t.Fatalf("supplemental message not found in messages: %+v", getTaskAfterSend.Messages)
	}

	// 9. Complete task as quote
	compBody, _ := json.Marshal(map[string]any{"agent": "quote", "result": "all operations succeeded"})
	resp, err = client.Post("http://unix/api/v1/tasks/"+taskID+"/complete", "application/json", bytes.NewReader(compBody))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("Complete task failed: %v", err)
	}

	// 10. Query events with caller agent
	resp, err = client.Get("http://unix/api/v1/tasks/" + taskID + "/events?agent=coordinator&after=0")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("Get events failed: %v", err)
	}
	var eventsResp struct {
		Events []*domain.Event `json:"events"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&eventsResp)
	if len(eventsResp.Events) < 5 {
		t.Fatalf("expected at least 5 events, got %d", len(eventsResp.Events))
	}

	// Verify socket file exists
	if _, err := os.Stat(socketPath); err != nil {
		t.Fatalf("socket file missing while running: %v", err)
	}
}

func TestServerLiveSocketRefusal(t *testing.T) {
	_, socketPath, storeRef, cleanup := setupTestServer(t)
	defer cleanup()

	// Try starting a second server on the same live socket
	svc2 := service.NewService(storeRef, nil, nil)
	srv2 := server.NewServer(svc2, socketPath, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := srv2.Start(ctx)
	if err == nil {
		t.Fatalf("expected error when starting on an already live socket, got nil")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Fatalf("expected error message to mention 'already running', got: %v", err)
	}
}

func TestServerStaleSocketRecovery(t *testing.T) {
	dir, err := os.MkdirTemp("", "agentbus-stale-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	dbPath := filepath.Join(dir, "db.sqlite")
	socketPath := filepath.Join(dir, "stale.sock")

	// Create a dummy dead unix socket by listening and closing it
	dummyListener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to create dummy socket: %v", err)
	}
	dummyListener.Close() // closed, but file remains on disk!

	s, _ := store.OpenSQLite(dbPath)
	defer s.Close()

	svc := service.NewService(s, nil, nil)
	srv := server.NewServer(svc, socketPath, nil)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	// Wait for socket to be revived
	var ready bool
	for i := 0; i < 50; i++ {
		conn, err := net.Dial("unix", socketPath)
		if err == nil {
			conn.Close()
			ready = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if !ready {
		t.Fatalf("server failed to recover stale socket and listen")
	}

	cancel()
	<-errCh
}

func TestServerCleanSocketRecreate(t *testing.T) {
	dir, err := os.MkdirTemp("", "agentbus-recreate-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	dbPath := filepath.Join(dir, "db.sqlite")
	socketPath := filepath.Join(dir, "agentbus.sock")

	s, _ := store.OpenSQLite(dbPath)
	defer s.Close()

	svc := service.NewService(s, nil, nil)
	srv := server.NewServer(svc, socketPath, nil)

	// Create a dummy file that is NOT a socket
	if err := os.WriteFile(socketPath, []byte("regular file"), 0644); err != nil {
		t.Fatalf("failed to write dummy file: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = srv.Start(ctx)
	if err == nil {
		t.Fatalf("expected error when socket path is a regular file, got nil")
	}
}

func TestServerInvalidQueryParams(t *testing.T) {
	client, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. Missing agent query param on /events -> 400
	resp, err := client.Get("http://unix/api/v1/tasks/task-123/events")
	if err != nil {
		t.Fatalf("Get events failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 when missing agent, got %d", resp.StatusCode)
	}

	// 2. Invalid after parameter -> 400
	resp, err = client.Get("http://unix/api/v1/tasks/task-123/events?agent=coord&after=invalid_seq")
	if err != nil {
		t.Fatalf("Get events failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid after param, got %d", resp.StatusCode)
	}

	// 3. Invalid timeout parameter -> 400
	resp, err = client.Get("http://unix/api/v1/tasks/task-123/events?agent=coord&timeout=invalid_timeout")
	if err != nil {
		t.Fatalf("Get events failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid timeout param, got %d", resp.StatusCode)
	}
}

func TestServerSocketCleanupOwnershipRace(t *testing.T) {
	dir, err := os.MkdirTemp("", "agentbus-race-sock-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	dbPath := filepath.Join(dir, "db.sqlite")
	socketPath := filepath.Join(dir, "agentbus.sock")

	s, _ := store.OpenSQLite(dbPath)
	defer s.Close()

	svc := service.NewService(s, nil, nil)
	srv := server.NewServer(svc, socketPath, nil)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	// Wait for Server A socket to be ready
	for i := 0; i < 50; i++ {
		if conn, err := net.Dial("unix", socketPath); err == nil {
			conn.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Unlink Server A socket directly (simulating external unlink)
	if err := os.Remove(socketPath); err != nil {
		t.Fatalf("failed to unlink Server A socket: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	// Server B creates new socket listener at the same path
	listenerB, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to start listener B at socket path: %v", err)
	}
	defer func() {
		listenerB.Close()
		_ = os.Remove(socketPath)
	}()

	// Stop Server A
	cancel()
	<-errCh

	// Assert Server B's socket still exists on disk and was NOT deleted by Server A
	if _, err := os.Stat(socketPath); err != nil {
		t.Fatalf("Server B socket was unexpectedly removed by Server A: %v", err)
	}

	// Assert Server B is still connectable
	connB, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to connect to Server B socket after Server A shutdown: %v", err)
	}
	connB.Close()
}
