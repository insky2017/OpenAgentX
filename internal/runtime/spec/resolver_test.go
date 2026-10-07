package spec

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

func TestResolveRejectsModelOutsideWorkerDescriptor(t *testing.T) {
	descriptor := openruntime.AdapterDescriptor{AdapterID: "agy", BackendType: "batch", Version: "1", LaunchProtocol: "argv", Models: []string{"model-a"}, ReasoningModes: []domain.ReasoningMode{domain.ReasoningBackendDefault}, SessionModes: []domain.SessionMode{domain.SessionModeNew}, Steer: openruntime.SteerQueued, Approval: openruntime.ApprovalPreflight, Cancel: openruntime.CancelProcessSignal, MaxConcurrency: 1}
	registration := openruntime.BackendRegistration{BackendID: "local", Descriptor: descriptor, Health: openruntime.BackendHealthy}
	requested := domain.ExecutionSpec{AdapterID: "agy", BackendID: "local", Model: "model-b", Reasoning: domain.ReasoningSpec{Mode: domain.ReasoningBackendDefault}, Session: domain.SessionSpec{Mode: domain.SessionModeNew}, Timeout: time.Minute, BackendOptions: json.RawMessage(`{}`)}
	if _, err := Resolve(context.Background(), requested, domain.ExecutionSpec{}, registration, Policy{}); err == nil {
		t.Fatal("unknown model was accepted")
	}
}

func TestResolveRejectsUnregisteredNetworkOverride(t *testing.T) {
	descriptor := openruntime.AdapterDescriptor{AdapterID: "agy", BackendType: "batch", Version: "1", LaunchProtocol: "argv", Models: []string{"model-a"}, ReasoningModes: []domain.ReasoningMode{domain.ReasoningBackendDefault}, SessionModes: []domain.SessionMode{domain.SessionModeNew}, Steer: openruntime.SteerQueued, Approval: openruntime.ApprovalPreflight, Cancel: openruntime.CancelProcessSignal, MaxConcurrency: 1}
	registration := openruntime.BackendRegistration{BackendID: "local", Descriptor: descriptor, Health: openruntime.BackendHealthy}
	requested := domain.ExecutionSpec{AdapterID: "agy", BackendID: "local", Model: "model-a", Reasoning: domain.ReasoningSpec{Mode: domain.ReasoningBackendDefault}, Session: domain.SessionSpec{Mode: domain.SessionModeNew}, Timeout: time.Minute, Network: domain.NetworkPolicy{Mode: domain.NetworkNamedProfile, ProfileID: "unregistered", ProfileVersion: 1, ConfigFile: "/run/profile.conf"}}
	if _, err := Resolve(context.Background(), requested, domain.ExecutionSpec{}, registration, Policy{}); err == nil {
		t.Fatal("unregistered network profile was accepted")
	}
}

func TestResolveAcceptsOnlyMatchingBackendNetworkProfile(t *testing.T) {
	descriptor := openruntime.AdapterDescriptor{AdapterID: "agy", BackendType: "batch", Version: "1", LaunchProtocol: "argv", Models: []string{"model-a"}, ReasoningModes: []domain.ReasoningMode{domain.ReasoningBackendDefault}, SessionModes: []domain.SessionMode{domain.SessionModeNew}, Steer: openruntime.SteerQueued, Approval: openruntime.ApprovalPreflight, Cancel: openruntime.CancelProcessSignal, MaxConcurrency: 1}
	profile := domain.NetworkPolicy{Mode: domain.NetworkNamedProfile, ProfileID: "registered", ProfileVersion: 2, ConfigFile: "/run/profile.conf"}
	registration := openruntime.BackendRegistration{BackendID: "local", Descriptor: descriptor, Health: openruntime.BackendHealthy, Network: profile}
	requested := domain.ExecutionSpec{AdapterID: "agy", BackendID: "local", Model: "model-a", Reasoning: domain.ReasoningSpec{Mode: domain.ReasoningBackendDefault}, Session: domain.SessionSpec{Mode: domain.SessionModeNew}, Timeout: time.Minute, Network: profile}
	resolved, err := Resolve(context.Background(), requested, domain.ExecutionSpec{Network: profile}, registration, Policy{})
	if err != nil || resolved.Spec.Network.ProfileID != "registered" {
		t.Fatalf("matching network profile rejected: resolved=%+v err=%v", resolved, err)
	}
}

func TestResolveCodexUnlimitedDefaultHonorsPolicyLimit(t *testing.T) {
	registration := openruntime.BackendRegistration{BackendID: "local", Descriptor: openruntime.AdapterDescriptor{
		AdapterID: "codex-app-server", Models: []string{"model-a"},
		ReasoningModes: []domain.ReasoningMode{domain.ReasoningBackendDefault},
		SessionModes:   []domain.SessionMode{domain.SessionModeNew},
	}}
	resolved, err := Resolve(context.Background(), domain.ExecutionSpec{}, domain.ExecutionSpec{}, registration, Policy{})
	if err != nil || resolved.Spec.Timeout != 0 || !resolved.DeadlineAt.IsZero() {
		t.Fatalf("unlimited Codex default changed: %+v err=%v", resolved, err)
	}
	if _, err := Resolve(context.Background(), domain.ExecutionSpec{}, domain.ExecutionSpec{}, registration, Policy{MaxTimeout: time.Hour}); err == nil {
		t.Fatal("unlimited execution bypassed the finite policy limit")
	}
	resolved, err = Resolve(context.Background(), domain.ExecutionSpec{Timeout: 47 * time.Second}, domain.ExecutionSpec{}, registration, Policy{MaxTimeout: time.Hour})
	if err != nil || resolved.Spec.Timeout != 47*time.Second {
		t.Fatalf("explicit finite timeout changed: %+v err=%v", resolved, err)
	}
}
