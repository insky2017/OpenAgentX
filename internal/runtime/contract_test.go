package runtime_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

func validDescriptor() openruntime.AdapterDescriptor {
	return openruntime.AdapterDescriptor{
		AdapterID: "fake-acp", BackendType: "acp", Version: "1.0.0", LaunchProtocol: "stdio",
		Models: []string{"model-1"}, ReasoningModes: []domain.ReasoningMode{domain.ReasoningEffort},
		SessionModes: []domain.SessionMode{domain.SessionModeNew, domain.SessionModeResume},
		Steer:        openruntime.SteerNative, Approval: openruntime.ApprovalNative, Cancel: openruntime.CancelNative,
		BackendOptionsJSON: json.RawMessage(`{"type":"object"}`), MaxConcurrency: 1,
	}
}

func TestAdapterDescriptorValidation(t *testing.T) {
	t.Parallel()
	descriptor := validDescriptor()
	if err := descriptor.Validate(); err != nil {
		t.Fatalf("valid descriptor rejected: %v", err)
	}
	descriptor.Models = append(descriptor.Models, "model-1")
	if err := descriptor.Validate(); err == nil {
		t.Fatal("duplicate model IDs must be rejected")
	}
	descriptor = validDescriptor()
	descriptor.Steer = openruntime.SteerMode("maybe")
	if err := descriptor.Validate(); err == nil {
		t.Fatal("unknown steer capability must be rejected")
	}
}

func TestRuntimeEventAndEventSink(t *testing.T) {
	t.Parallel()
	event := openruntime.RuntimeEvent{Type: "turn.output", Payload: json.RawMessage(`{"text":"ok"}`), OccurredAt: time.Now().UTC()}
	if err := event.Validate(); err != nil {
		t.Fatalf("valid runtime event rejected: %v", err)
	}
	called := false
	sink := openruntime.EventSinkFunc(func(_ context.Context, got openruntime.RuntimeEvent) error {
		called = got.Type == event.Type
		return nil
	})
	if err := sink.Emit(context.Background(), event); err != nil {
		t.Fatalf("emit failed: %v", err)
	}
	if !called {
		t.Fatal("event sink was not called")
	}
}

func TestExecutionSpecRejectsRawInvalidShape(t *testing.T) {
	t.Parallel()
	spec := domain.ExecutionSpec{
		AdapterID: "fake-acp", BackendID: "local", Model: "model-1",
		Reasoning: domain.ReasoningSpec{Mode: domain.ReasoningEffort, Value: "high"},
		Session:   domain.SessionSpec{Mode: domain.SessionModeNew},
		Timeout:   time.Minute, BackendOptions: json.RawMessage(`{"safe":true}`),
	}
	if err := spec.ValidateShape(); err != nil {
		t.Fatalf("valid execution spec rejected: %v", err)
	}
	spec.BackendOptions = json.RawMessage(`{"broken"`)
	if err := spec.ValidateShape(); err == nil {
		t.Fatal("invalid backend_options JSON must be rejected")
	}
}
