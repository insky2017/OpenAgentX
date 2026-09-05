package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openagentx/internal/api"
	"openagentx/internal/api/workerapi"
	workerclient "openagentx/internal/client/worker"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
	"openagentx/internal/transport/unixhttp"
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
	environment.backend.Descriptor.Steer = openruntime.SteerNative
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
	environment.bootstrapInherit(t, session)
	created := environment.createTask(t, "uds")
	item, err := client.ClaimMailbox(context.Background(), claimRequest(session, 1))
	if err != nil || item == nil || item.ID != created.MailboxItem.ID {
		t.Fatalf("UDS claim item=%+v err=%v", item, err)
	}
	begin, err := client.BeginAttempt(context.Background(), item.ID, api.BeginAttemptRequest{
		WorkerInstanceID: session.Worker.ID, AgentID: environment.agentID,
		Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken,
		ExpectedItemState: domain.MailboxStateClaimed,
	})
	if err != nil {
		t.Fatal(err)
	}
	commandService, err := NewCommandService(environment.repository, environment.broker, environment.clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	messageResponse, err := commandService.CreateMessage(context.Background(), environment.ownerID, created.Task.ID, api.CreateMessageRequest{
		Meta:              api.CommandMeta{IdempotencyKey: "uds-native-message", ExpectedVersion: begin.Turn.Task.Version},
		SenderPrincipalID: environment.ownerID, Content: "steer the active UDS turn",
	})
	if err != nil {
		t.Fatal(err)
	}
	controlItem, err := client.ClaimMailbox(context.Background(), claimRequest(session, 0))
	if err != nil || controlItem == nil || controlItem.MessageID != messageResponse.MessageID || controlItem.Lane != domain.MailboxLaneControl {
		t.Fatalf("UDS native Message claim=%+v response=%+v err=%v", controlItem, messageResponse, err)
	}
	_, err = client.ResolveMailboxPayload(context.Background(), controlItem.ID, api.MailboxPayloadRequest{
		WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation,
		FencingToken: session.Worker.FencingToken + 1,
	})
	var apiErr *workerclient.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != api.ErrorFencingRejected {
		t.Fatalf("stale fencing payload error=%v", err)
	}
	message, err := client.ResolveMessage(context.Background(), *controlItem)
	if err != nil || message.ID != messageResponse.MessageID || message.Content != "steer the active UDS turn" {
		t.Fatalf("UDS Message payload=%+v err=%v", message, err)
	}
	if err := client.AcceptMailboxItem(context.Background(), controlItem.ID, api.AcceptRequest{
		WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation,
		FencingToken: session.Worker.FencingToken, ExpectedItemState: domain.MailboxStateClaimed,
		Outcome: domain.MailboxStateAccepted,
	}); err != nil {
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
	if err := client.ReleaseWorker(context.Background(), api.WorkerReleaseRequest{WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken}); err != nil {
		t.Fatalf("release Worker lease: %v", err)
	}
	if err := client.ReleaseWorker(context.Background(), api.WorkerReleaseRequest{WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken}); err == nil {
		t.Fatal("duplicate release unexpectedly succeeded")
	}
	if _, err := client.RegisterWorker(context.Background(), api.RegisterRequest{
		ContractVersion: api.ContractVersion, AgentID: environment.agentID, WorkerInstanceID: "worker-uds-replacement",
		Transport: domain.WorkerTransportUnix, Capabilities: []string{"coding"}, Backends: []openruntime.BackendRegistration{environment.backend},
	}); err != nil {
		t.Fatalf("replacement Worker registration after release: %v", err)
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

func TestUnixHTTPWorkerControlClaimWaitsAndWakesThroughSharedBroker(t *testing.T) {
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
	t.Cleanup(func() {
		cancelServer()
		select {
		case <-serverResult:
		case <-time.After(5 * time.Second):
		}
	})
	client, err := workerclient.NewUnixHTTPWorkerClient(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	session, err := client.RegisterWorker(context.Background(), api.RegisterRequest{
		ContractVersion: api.ContractVersion, AgentID: environment.agentID, WorkerInstanceID: "worker-uds-control",
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

	started := time.Now()
	command, err := client.ClaimWorkerCommand(context.Background(), controlClaimRequest(session, 1))
	if err != nil || command != nil {
		t.Fatalf("UDS control timeout command=%+v err=%v", command, err)
	}
	if elapsed := time.Since(started); elapsed < 900*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("UDS control timeout elapsed=%s", elapsed)
	}

	result := make(chan *domain.WorkerCommand, 1)
	errorsChannel := make(chan error, 1)
	go func() {
		claimed, claimErr := client.ClaimWorkerCommand(context.Background(), controlClaimRequest(session, 5))
		result <- claimed
		errorsChannel <- claimErr
	}()
	time.Sleep(100 * time.Millisecond)
	created := createWorkerCommand(t, newWorkerAdminTestService(t, environment, environment.broker), environment, session, "uds-wakeup")
	select {
	case claimed := <-result:
		if err := <-errorsChannel; err != nil || claimed == nil || claimed.ID != created.ID {
			t.Fatalf("UDS woken command=%+v err=%v", claimed, err)
		}
	case <-time.After(time.Second):
		t.Fatal("UDS control claim was not woken by shared Broker")
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
