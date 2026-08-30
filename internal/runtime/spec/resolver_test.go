package spec

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"agentbus/internal/domain"
	openruntime "agentbus/internal/runtime"
)

func TestResolveRejectsModelOutsideWorkerDescriptor(t *testing.T) {
	descriptor := openruntime.AdapterDescriptor{AdapterID: "agy", BackendType: "batch", Version: "1", LaunchProtocol: "argv", Models: []string{"model-a"}, ReasoningModes: []domain.ReasoningMode{domain.ReasoningBackendDefault}, SessionModes: []domain.SessionMode{domain.SessionModeNew}, Steer: openruntime.SteerQueued, Approval: openruntime.ApprovalPreflight, Cancel: openruntime.CancelProcessSignal, MaxConcurrency: 1}
	registration := openruntime.BackendRegistration{BackendID: "local", Descriptor: descriptor, Health: openruntime.BackendHealthy}
	requested := domain.ExecutionSpec{AdapterID: "agy", BackendID: "local", Model: "model-b", Reasoning: domain.ReasoningSpec{Mode: domain.ReasoningBackendDefault}, Session: domain.SessionSpec{Mode: domain.SessionModeNew}, Timeout: time.Minute, BackendOptions: json.RawMessage(`{}`)}
	if _, err := Resolve(context.Background(), requested, domain.ExecutionSpec{}, registration, Policy{}); err == nil {
		t.Fatal("unknown model was accepted")
	}
}
