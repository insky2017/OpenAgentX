package controlplane

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agentbus/internal/api"
	"agentbus/internal/api/workerapi"
	workerclient "agentbus/internal/client/worker"
	"agentbus/internal/domain"
	openruntime "agentbus/internal/runtime"
	"agentbus/internal/transport/unixhttp"
)

func TestWorkerAPIRejectsUnknownFieldsAndMissingBearerToken(t *testing.T) {
	environment := newWorkerTestEnvironment(t, nil)
	handler, err := workerapi.NewHandler(environment.service, workerapi.StaticPrincipal(environment.workerID))
	if err != nil {
		t.Fatal(err)
	}
	unknown := httptest.NewRequest(http.MethodPost, api.WorkerRegisterPath,
		strings.NewReader(`{"contract_version":"openagentx.worker.v1","unexpected":true}`))
	unknown.Header.Set("Content-Type", "application/json")
	unknownResponse := httptest.NewRecorder()
	handler.ServeHTTP(unknownResponse, unknown)
	if unknownResponse.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status=%d body=%s", unknownResponse.Code, unknownResponse.Body.String())
	}
	var apiError api.ErrorResponse
	if err := json.Unmarshal(unknownResponse.Body.Bytes(), &apiError); err != nil || apiError.Code != api.ErrorInvalidRequest {
		t.Fatalf("unknown field response=%+v err=%v", apiError, err)
	}

	missingToken := httptest.NewRequest(http.MethodPost, "/api/v1/workers/worker-1/heartbeat", strings.NewReader(`{}`))
	missingTokenResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingTokenResponse, missingToken)
	if missingTokenResponse.Code != http.StatusUnauthorized {
		t.Fatalf("missing bearer status=%d body=%s", missingTokenResponse.Code, missingTokenResponse.Body.String())
	}
}

func TestUnixHTTPWorkerAPIEndToEnd(t *testing.T) {
	environment := newWorkerTestEnvironment(t, nil)
	handler, err := workerapi.NewHandler(environment.service, workerapi.StaticPrincipal(environment.workerID))
	if err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(t.TempDir(), "run", "openagentx.sock")
	server, err := unixhttp.NewServer(socketPath, handler, nil)
	if err != nil {
		t.Fatal(err)
	}
	serverContext, cancelServer := context.WithCancel(context.Background())
	serverResult := make(chan error, 1)
	go func() { serverResult <- server.Start(serverContext) }()
	waitForSocket(t, socketPath)
	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("Unix socket mode=%#o want=0600", info.Mode().Perm())
	}
	client, err := workerclient.NewUnixHTTPWorkerClient(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	session, err := client.RegisterWorker(context.Background(), api.RegisterRequest{
		ContractVersion: api.ContractVersion, AgentID: environment.agentID, WorkerInstanceID: "worker-uds",
		Transport: domain.WorkerTransportUnix, Capabilities: []string{"coding"},
		Backends: []openruntime.BackendRegistration{environment.backend},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Heartbeat(context.Background(), api.HeartbeatRequest{
		WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation,
		FencingToken: session.Worker.FencingToken, Status: domain.WorkerStatusOnline,
		BackendHealth: map[string]openruntime.BackendHealth{"local": openruntime.BackendHealthy},
	}); err != nil {
		t.Fatal(err)
	}
	created := environment.createTask(t, "uds")
	item, err := client.ClaimMailbox(context.Background(), claimRequest(session, 1))
	if err != nil || item == nil || item.ID != created.MailboxItem.ID {
		t.Fatalf("UDS claim item=%+v err=%v", item, err)
	}
	begin, err := client.BeginAttempt(context.Background(), item.ID, api.BeginAttemptRequest{
		WorkerInstanceID: session.Worker.ID, AgentID: environment.agentID,
		Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken,
		ExpectedItemState: domain.MailboxStateClaimed, ExpectedTaskVersion: created.Task.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.AppendRunEvents(context.Background(), begin.Turn.RunAttempt.ID, api.EventBatch{
		WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation,
		FencingToken: session.Worker.FencingToken, ExpectedRunVersion: begin.Turn.RunAttempt.Version,
		Events: []openruntime.RuntimeEvent{{Type: "turn.output", OccurredAt: environment.clock.Now()}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := client.FinishRun(context.Background(), begin.Turn.RunAttempt.ID, api.FinishRunRequest{
		WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation,
		FencingToken: session.Worker.FencingToken, ExpectedTaskVersion: begin.Turn.Task.Version,
		ExpectedRunVersion: begin.Turn.RunAttempt.Version,
		Result:             openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "uds-done", SideEffectsKnown: true},
	}); err != nil {
		t.Fatal(err)
	}
	cancelServer()
	select {
	case err := <-serverResult:
		if err != nil {
			t.Fatalf("stop UDS server: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("UDS server did not stop")
	}
	if _, err := os.Lstat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("owned Unix socket was not removed: %v", err)
	}
}

func waitForSocket(t *testing.T, socketPath string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(socketPath); err == nil && info.Mode()&os.ModeSocket != 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Unix socket was not created: %s", socketPath)
}
