package workercli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
	residentworker "openagentx/internal/worker"
)

func TestOpenAgentXWorkerRunExitCodes(t *testing.T) {
	if code := ExecuteOpenAgentX([]string{"worker", "run", "--config", "agent.yaml"},
		func(context.Context, string) error { return nil }); code != 0 {
		t.Fatalf("controlled Worker stop exit code=%d want=0", code)
	}
	if code := ExecuteOpenAgentX([]string{"worker", "run", "--config", "agent.yaml"},
		func(context.Context, string) error { return errors.New("lease lost") }); code == 0 {
		t.Fatal("abnormal Worker stop must return nonzero")
	}
	if code := ExecuteOpenAgentX([]string{"worker", "run"}, func(context.Context, string) error { return nil }); code == 0 {
		t.Fatal("missing Worker config must return nonzero")
	}
}

func TestAGYConfigFromOptions(t *testing.T) {
	configDir := t.TempDir()
	workingDir := filepath.Join(configDir, "workspace")
	if err := os.Mkdir(workingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	config, err := agyConfigFromOptions(map[string]any{
		"binary":      "agy-graft",
		"models":      []any{"model-one", "model-two"},
		"working_dir": "workspace",
	}, configDir)
	if err != nil || config.Binary != "agy-graft" || config.WorkingDir != workingDir || !reflect.DeepEqual(config.Models, []string{"model-one", "model-two"}) {
		t.Fatalf("config=%+v err=%v", config, err)
	}
	for _, options := range []map[string]any{
		{"binary": " "},
		{"working_dir": " "},
		{"working_dir": "missing"},
		{"models": []any{}},
		{"models": []any{"model-one", "model-one"}},
		{"models": []any{"model-one", 42}},
		{"unexpected": true},
	} {
		if _, err := agyConfigFromOptions(options, configDir); err == nil {
			t.Fatalf("options=%#v unexpectedly accepted", options)
		}
	}
}

func TestAssembleFakeAdapterScriptedResultsAndResumeCapability(t *testing.T) {
	adapter, err := assembleM1Adapter(residentworker.RuntimeBackendConfig{
		BackendID: "primary", AdapterID: "fake",
		Options: map[string]any{
			"model":                  "fake-multiturn-model",
			"result":                 "need follow-up",
			"result_status_sequence": []any{"waiting_input", "succeeded"},
			"provider_session_id":    "fake-session-1",
		},
	}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := adapter.Descriptor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !hasSessionMode(descriptor.SessionModes, domain.SessionModeNew) || !hasSessionMode(descriptor.SessionModes, domain.SessionModeResume) {
		t.Fatalf("fake session modes=%v; want new and resume", descriptor.SessionModes)
	}
	first, err := adapter.StartTurn(context.Background(), fakeAssemblyTurnRequest(domain.SessionModeNew, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	firstResult, err := first.Wait(context.Background())
	if err != nil || firstResult.Status != openruntime.TurnResultWaitingInput || firstResult.ProviderSessionID != "fake-session-1" || firstResult.Result != "need follow-up" {
		t.Fatalf("first result=%+v err=%v", firstResult, err)
	}
	second, err := adapter.StartTurn(context.Background(), fakeAssemblyTurnRequest(domain.SessionModeResume, &domain.SessionBinding{
		ID: "binding-1", ContextID: "context-1", AgentID: "test-fake-multiturn", BackendID: "primary",
		ProviderSessionID: firstResult.ProviderSessionID, State: domain.SessionBindingActive, Version: 1,
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	secondResult, err := second.Wait(context.Background())
	if err != nil || secondResult.Status != openruntime.TurnResultSucceeded || secondResult.ProviderSessionID != "fake-session-1" {
		t.Fatalf("second result=%+v err=%v", secondResult, err)
	}
	if _, err := adapter.StartTurn(context.Background(), fakeAssemblyTurnRequest(domain.SessionModeResume, nil), nil); err == nil || !strings.Contains(err.Error(), "script exhausted") {
		t.Fatalf("exhausted scripted fake result error=%v; want fail closed", err)
	}
}

func TestAssembleFakeAdapterSupportsAllContractResultStatuses(t *testing.T) {
	for _, status := range []openruntime.TurnResultStatus{
		openruntime.TurnResultSucceeded,
		openruntime.TurnResultWaitingInput,
		openruntime.TurnResultFailed,
		openruntime.TurnResultUncertain,
		openruntime.TurnResultCanceled,
	} {
		t.Run(string(status), func(t *testing.T) {
			adapter, err := assembleM1Adapter(residentworker.RuntimeBackendConfig{
				BackendID: "primary", AdapterID: "fake", Options: map[string]any{"result_status": string(status)},
			}, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			handle, err := adapter.StartTurn(context.Background(), fakeAssemblyTurnRequest(domain.SessionModeNew, nil), nil)
			if err != nil {
				t.Fatal(err)
			}
			result, err := handle.Wait(context.Background())
			if err != nil || result.Status != status {
				t.Fatalf("result=%+v err=%v want status=%s", result, err, status)
			}
		})
	}
}

func TestAssembleFakeAdapterRejectsInvalidOptions(t *testing.T) {
	validSequence := []any{"waiting_input", "succeeded"}
	cases := map[string]map[string]any{
		"unknown option":             {"unexpected": true},
		"model wrong type":           {"model": 7},
		"model blank":                {"model": "  "},
		"result wrong type":          {"result": 7},
		"provider wrong type":        {"provider_session_id": 7},
		"provider blank":             {"provider_session_id": "  "},
		"provider control character": {"provider_session_id": "bad\nidentifier"},
		"status wrong type":          {"result_status": 7},
		"status blank":               {"result_status": "  "},
		"status unsupported":         {"result_status": "running"},
		"sequence wrong type":        {"result_status_sequence": "succeeded"},
		"sequence empty":             {"result_status_sequence": []any{}},
		"sequence invalid entry":     {"result_status_sequence": []any{"waiting_input", "running"}},
		"sequence non string entry":  {"result_status_sequence": []any{"waiting_input", 7}},
		"status sequence conflict":   {"result_status": "succeeded", "result_status_sequence": validSequence},
	}
	for name, options := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := assembleM1Adapter(residentworker.RuntimeBackendConfig{
				BackendID: "primary", AdapterID: "fake", Options: options,
			}, t.TempDir()); err == nil {
				t.Fatalf("options=%#v unexpectedly accepted", options)
			}
		})
	}
}

func TestFakeMultiTurnFixtureLoads(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", ".."))
	config, err := residentworker.LoadProcessConfig(filepath.Join(root, "agents", "test-fake-multiturn", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if config.AgentID != "test-fake-multiturn" || len(config.RuntimeBackendConfig) != 1 {
		t.Fatalf("fixture config=%+v", config)
	}
	options := config.RuntimeBackendConfig[0].Options
	sequence, ok := options["result_status_sequence"].([]any)
	if !ok || !reflect.DeepEqual(sequence, []any{"waiting_input", "succeeded"}) || options["provider_session_id"] != "fake-multiturn-session-1" {
		t.Fatalf("fixture options=%#v", options)
	}
}

func fakeAssemblyTurnRequest(mode domain.SessionMode, binding *domain.SessionBinding) openruntime.TurnRequest {
	return openruntime.TurnRequest{
		Task:           domain.Task{ID: "task-fake-assembly", Content: "test fake assembly"},
		SessionBinding: binding,
		Execution: domain.ResolvedExecutionSpec{Version: 1, Spec: domain.ExecutionSpec{
			AdapterID: "fake", BackendID: "primary", Model: "fake-multiturn-model",
			Reasoning: domain.ReasoningSpec{Mode: domain.ReasoningBackendDefault},
			Session:   domain.SessionSpec{Mode: mode, ContextID: "context-1"},
			Timeout:   time.Minute,
		}},
	}
}

func hasSessionMode(modes []domain.SessionMode, target domain.SessionMode) bool {
	for _, mode := range modes {
		if mode == target {
			return true
		}
	}
	return false
}
