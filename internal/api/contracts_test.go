package api_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"openagentx/internal/api"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

func TestDecodeStrictJSON(t *testing.T) {
	t.Parallel()
	type payload struct {
		Name string `json:"name"`
	}

	var valid payload
	if err := api.DecodeStrictJSON(strings.NewReader(`{"name":"worker"}`), &valid); err != nil {
		t.Fatalf("valid JSON rejected: %v", err)
	}
	if valid.Name != "worker" {
		t.Fatalf("decoded name = %q", valid.Name)
	}

	var unknown payload
	if err := api.DecodeStrictJSON(strings.NewReader(`{"name":"worker","raw_argv":"rm -rf"}`), &unknown); err == nil {
		t.Fatal("unknown JSON field must be rejected")
	}

	var trailing payload
	if err := api.DecodeStrictJSON(strings.NewReader(`{"name":"worker"} {"name":"other"}`), &trailing); err == nil {
		t.Fatal("multiple JSON values must be rejected")
	}
}

func TestCommandMetaRequiresIdempotencyAndVersion(t *testing.T) {
	t.Parallel()
	if err := (api.CommandMeta{IdempotencyKey: "key-1", ExpectedVersion: 2}).Validate(true); err != nil {
		t.Fatalf("valid command meta rejected: %v", err)
	}
	if err := (api.CommandMeta{ExpectedVersion: 2}).Validate(true); err == nil {
		t.Fatal("missing idempotency key must be rejected")
	}
	if err := (api.CommandMeta{IdempotencyKey: "key-1"}).Validate(true); err == nil {
		t.Fatal("missing expected version must be rejected")
	}
}

func TestAPIBoundariesAreVersionedAndSeparated(t *testing.T) {
	t.Parallel()
	paths := []string{
		api.WorkerRegisterPath,
		api.ControlCreateTaskPath,
		api.ObserveOverviewPath,
		api.AdminWorkerStopPath,
		api.AuthLoginPath,
	}
	for _, path := range paths {
		if !strings.Contains(path, "/v1/") && !strings.HasSuffix(path, "/v1") {
			t.Fatalf("API path is not versioned: %s", path)
		}
	}
	if strings.HasPrefix(api.ControlCreateTaskPath, "/api/v1/workers") {
		t.Fatal("business control path must not be a Worker Control API path")
	}
}

func TestWorkerRequestsRejectStaleOrUnknownContractValues(t *testing.T) {
	t.Parallel()
	descriptor := openruntime.AdapterDescriptor{
		AdapterID: "fake", BackendType: "fake", Version: "1", LaunchProtocol: "inproc",
		Models: []string{"model-1"}, ReasoningModes: []domain.ReasoningMode{domain.ReasoningEffort},
		SessionModes: []domain.SessionMode{domain.SessionModeNew}, Steer: openruntime.SteerQueued,
		Approval: openruntime.ApprovalPreflight, Cancel: openruntime.CancelProcessSignal,
		BackendOptionsJSON: json.RawMessage(`{"type":"object"}`), MaxConcurrency: 1,
	}
	register := api.RegisterRequest{
		ContractVersion: api.ContractVersion, AgentID: "quote", WorkerInstanceID: "worker-1",
		Transport: domain.WorkerTransportUnix,
		Backends:  []openruntime.BackendRegistration{{BackendID: "local", Descriptor: descriptor, Health: openruntime.BackendHealthy}},
	}
	if err := register.Validate(); err != nil {
		t.Fatalf("valid register request rejected: %v", err)
	}
	register.ContractVersion = "v0"
	if err := register.Validate(); err == nil {
		t.Fatal("unsupported contract version must be rejected")
	}

	begin := api.BeginAttemptRequest{
		WorkerInstanceID: "worker-1", AgentID: "quote", Generation: 1, FencingToken: 2,
		ExpectedItemState: domain.MailboxStateClaimed, ExpectedTaskVersion: 3,
	}
	if err := begin.Validate(); err != nil {
		t.Fatalf("valid begin attempt request rejected: %v", err)
	}
	begin.ExpectedItemState = domain.MailboxStatePending
	if err := begin.Validate(); err == nil {
		t.Fatal("begin attempt must not bypass mailbox claim ownership")
	}

	finish := api.FinishRunRequest{
		WorkerInstanceID: "worker-1", Generation: 1, FencingToken: 2,
		ExpectedTaskVersion: 3, ExpectedRunVersion: 4,
		Result: openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, UsageJSON: json.RawMessage(`{}`)},
	}
	if err := finish.Validate(); err != nil {
		t.Fatalf("valid finish request rejected: %v", err)
	}
	finish.Result.Status = openruntime.TurnResultStatus("ignored")
	if err := finish.Validate(); err == nil {
		t.Fatal("unknown turn result status must be rejected")
	}
}

func TestControlRequestsRequireIdempotencyCASAndStructuredExecution(t *testing.T) {
	t.Parallel()
	create := api.CreateTaskRequest{
		Meta: api.CommandMeta{IdempotencyKey: "create-1"}, SenderPrincipalID: "human-1",
		TargetAgentID: "quote", OrganizationID: "org-1", DispatchMode: domain.DispatchModeDirect,
		Content: "implement quote API",
		Execution: &domain.ExecutionSpec{
			AdapterID: "agy", BackendID: "local", Model: "model-1",
			Reasoning: domain.ReasoningSpec{Mode: domain.ReasoningEffort, Value: "high"},
			Session:   domain.SessionSpec{Mode: domain.SessionModeNew}, Timeout: time.Minute,
		},
	}
	if err := create.Validate(); err != nil {
		t.Fatalf("valid create request rejected: %v", err)
	}

	message := api.CreateMessageRequest{
		Meta:              api.CommandMeta{IdempotencyKey: "message-1", ExpectedVersion: 2},
		SenderPrincipalID: "human-1", Content: "do not change the database",
	}
	if err := message.Validate(); err != nil {
		t.Fatalf("valid message request rejected: %v", err)
	}
	message.Meta.ExpectedVersion = 0
	if err := message.Validate(); err == nil {
		t.Fatal("message without Task CAS version must be rejected")
	}
}
