package worker

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	openapi "openagentx/internal/api"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
	"openagentx/internal/runtime/agy"
	runtimenetwork "openagentx/internal/runtime/network"
)

func TestBackendPoolAppliesPublishedNetworkBinding(t *testing.T) {
	profilePath := filepath.Join(t.TempDir(), "proxy.conf")
	if err := os.WriteFile(profilePath, []byte("proxy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	adapter := agy.NewAdapterForTest(agy.Config{Binary: "agy-graft", Models: []string{"model"}})
	var err error
	pool, err := NewBackendPool([]RuntimeBackend{{ID: "backend", Adapter: adapter}})
	if err != nil {
		t.Fatal(err)
	}
	binding := domain.NetworkBinding{AgentID: "agent", BackendID: "backend", ProfileID: "proxy", ProfileVersion: 2, Version: 1, DesiredStatus: "pending", Profile: &domain.ProxyProfile{ID: "proxy", Version: 2, Status: domain.NetworkProfilePublished, Mode: "only_socks5", Host: "proxy.internal", Port: 28080, ConfigFile: profilePath, CreatedBy: "owner", CreatedAt: nowForTest(), UpdatedAt: nowForTest()}, UpdatedAt: nowForTest()}
	if err := pool.ApplyNetworkBinding(binding); err != nil {
		t.Fatal(err)
	}
	pool.mu.RLock()
	applied := pool.backends["backend"].Network
	pool.mu.RUnlock()
	if applied.ProfileID != "proxy" || applied.ProfileVersion != 2 {
		t.Fatalf("applied=%+v", applied)
	}
}

func TestBackendPoolBlocksWorkAfterNetworkApplyFailureUntilRepair(t *testing.T) {
	adapter := agy.NewAdapterForTest(agy.Config{Binary: "agy-graft", Models: []string{"model"}})
	pool, err := NewBackendPool([]RuntimeBackend{{ID: "backend", Adapter: adapter}})
	if err != nil {
		t.Fatal(err)
	}
	now := nowForTest()
	binding := domain.NetworkBinding{
		AgentID: "agent", BackendID: "backend", ProfileID: "proxy", ProfileVersion: 2,
		Version: 1, DesiredStatus: "pending", UpdatedAt: now,
		Profile: &domain.ProxyProfile{
			ID: "proxy", Version: 2, Status: domain.NetworkProfilePublished, Mode: "only_socks5",
			Host: "proxy.internal", Port: 28080, ConfigFile: filepath.Join(t.TempDir(), "missing.conf"),
			CreatedBy: "owner", CreatedAt: now, UpdatedAt: now,
		},
	}
	if err := pool.ApplyNetworkBinding(binding); err == nil || pool.HasAvailableBackend() {
		t.Fatalf("failed network apply did not block work: err=%v available=%v", err, pool.HasAvailableBackend())
	}
	if err := os.WriteFile(binding.Profile.ConfigFile, []byte("proxy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := pool.ApplyNetworkBinding(binding); err != nil || !pool.HasAvailableBackend() {
		t.Fatalf("repaired network apply did not restore work: err=%v available=%v", err, pool.HasAvailableBackend())
	}
}

func TestBackendPoolTestsAndAppliesDeclaredDirectMode(t *testing.T) {
	identity := domain.RuntimeIdentity{AdapterID: "mode-adapter", AdapterVersion: "1"}
	adapter := &networkModeAdapter{descriptor: openruntime.AdapterDescriptor{
		AdapterID: "mode-adapter", BackendType: "test", Version: "1", LaunchProtocol: "test",
		Models: []string{"model"}, ReasoningModes: []domain.ReasoningMode{domain.ReasoningBackendDefault},
		SessionModes: []domain.SessionMode{domain.SessionModeNew}, Steer: openruntime.SteerUnsupported,
		Approval: openruntime.ApprovalUnsupported, Cancel: openruntime.CancelUnsupported,
		NetworkModes: []string{"inherit", "direct"}, MaxConcurrency: 1, RuntimeIdentity: identity,
	}}
	pool, err := NewBackendPool([]RuntimeBackend{{ID: "backend", Adapter: adapter}})
	if err != nil {
		t.Fatal(err)
	}
	work := domain.NetworkWork{ID: "mode-test", Kind: domain.NetworkWorkTest, AgentID: "agent", BackendID: "backend",
		WorkerInstanceID: "worker", Generation: 1, Mode: domain.NetworkDirect, PolicyVersion: 4,
		ManifestDigest: strings.Repeat("a", 64), RuntimeIdentity: identity, State: "claimed"}
	testAck := pool.ProcessNetworkWork(context.Background(), &openapi.NetworkWorkEnvelope{Work: work})
	if testAck.State != "succeeded" || testAck.Policy != nil {
		t.Fatalf("direct mode test ack=%+v", testAck)
	}
	work.ID = "mode-apply"
	work.Kind = domain.NetworkWorkApply
	work.BindingRevision = 7
	applyAck := pool.ProcessNetworkWork(context.Background(), &openapi.NetworkWorkEnvelope{Work: work})
	if applyAck.State != "succeeded" || applyAck.Policy == nil || applyAck.Policy.Mode != domain.NetworkDirect ||
		applyAck.Policy.PolicyVersion != 4 || applyAck.Policy.BindingRevision != 7 {
		t.Fatalf("direct mode apply ack=%+v", applyAck)
	}
	if adapter.applied.Mode != domain.NetworkDirect || adapter.applied.PolicyVersion != 4 {
		t.Fatalf("adapter applied policy=%+v", adapter.applied)
	}
}

func TestBackendPoolRejectsUndeclaredDirectMode(t *testing.T) {
	identity := domain.RuntimeIdentity{AdapterID: "agy-batch", AdapterVersion: "1"}
	adapter := &networkModeAdapter{descriptor: openruntime.AdapterDescriptor{
		AdapterID: "agy-batch", BackendType: "agy", Version: "1", LaunchProtocol: "argv",
		Models: []string{"model"}, ReasoningModes: []domain.ReasoningMode{domain.ReasoningBackendDefault},
		SessionModes: []domain.SessionMode{domain.SessionModeNew}, Steer: openruntime.SteerQueued,
		Approval: openruntime.ApprovalPreflight, Cancel: openruntime.CancelProcessSignal,
		NetworkModes: []string{"inherit", "named_profile"}, MaxConcurrency: 1, RuntimeIdentity: identity,
	}}
	pool, err := NewBackendPool([]RuntimeBackend{{ID: "backend", Adapter: adapter}})
	if err != nil {
		t.Fatal(err)
	}
	ack := pool.ProcessNetworkWork(context.Background(), &openapi.NetworkWorkEnvelope{Work: domain.NetworkWork{
		ID: "direct", Kind: domain.NetworkWorkTest, AgentID: "agent", BackendID: "backend", WorkerInstanceID: "worker",
		Generation: 1, Mode: domain.NetworkDirect, PolicyVersion: 1, ManifestDigest: strings.Repeat("b", 64), RuntimeIdentity: identity,
	}})
	if ack.State != "failed" || ack.DiagnosticCode != domain.NetworkDiagnosticUnsupported {
		t.Fatalf("undeclared direct ack=%+v", ack)
	}
}

func TestBackendPoolNetworkTestRejectsUnreachableNamedProfileBeforeRuntimeHealth(t *testing.T) {
	identity := domain.RuntimeIdentity{AdapterID: "mode-adapter", AdapterVersion: "1"}
	healthCalls := 0
	adapter := &networkModeAdapter{healthCalls: &healthCalls, descriptor: openruntime.AdapterDescriptor{
		AdapterID: "mode-adapter", BackendType: "test", Version: "1", LaunchProtocol: "test",
		Models: []string{"model"}, ReasoningModes: []domain.ReasoningMode{domain.ReasoningBackendDefault},
		SessionModes: []domain.SessionMode{domain.SessionModeNew}, Steer: openruntime.SteerUnsupported,
		Approval: openruntime.ApprovalUnsupported, Cancel: openruntime.CancelUnsupported,
		NetworkModes: []string{"inherit", "named_profile"}, MaxConcurrency: 1, RuntimeIdentity: identity,
	}}
	pool, err := NewBackendPool([]RuntimeBackend{{ID: "backend", Adapter: adapter}})
	if err != nil {
		t.Fatal(err)
	}
	materializer, err := runtimenetwork.NewMaterializer(filepath.Join(t.TempDir(), "network"))
	if err != nil {
		t.Fatal(err)
	}
	pool.SetNetworkMaterializer(materializer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	content := domain.NetworkProfileContent{
		ProfileID: "proxy", ContentVersion: 1, Mode: "only_socks5", Host: "127.0.0.1", Port: port,
		CreatedBy: "owner", CreatedAt: time.Now().UTC(),
	}
	content.ManifestDigest = content.ComputeManifestDigest()
	ack := pool.ProcessNetworkWork(context.Background(), &openapi.NetworkWorkEnvelope{Work: domain.NetworkWork{
		ID: "named-test", Kind: domain.NetworkWorkTest, ProfileID: content.ProfileID, ContentVersion: 1,
		AgentID: "agent", BackendID: "backend", WorkerInstanceID: "worker", Generation: 1,
		Mode: domain.NetworkNamedProfile, ManifestDigest: content.ManifestDigest, RuntimeIdentity: identity, Content: &content,
	}})
	if ack.State != "failed" || ack.DiagnosticCode != domain.NetworkDiagnosticEndpoint {
		t.Fatalf("unreachable endpoint ack=%+v", ack)
	}
	if healthCalls != 0 {
		t.Fatalf("runtime Health ran despite endpoint failure: calls=%d", healthCalls)
	}
	if err := domain.ValidateNetworkProbeResults(ack.State, ack.ProbeResults); err != nil {
		t.Fatalf("invalid layered evidence: %v (%+v)", err, ack.ProbeResults)
	}
}

func TestBackendPoolReportsHTTPProxyAuthenticationRequirement(t *testing.T) {
	identity := domain.RuntimeIdentity{AdapterID: "mode-adapter", AdapterVersion: "1"}
	adapter := &networkModeAdapter{descriptor: openruntime.AdapterDescriptor{
		AdapterID: "mode-adapter", BackendType: "test", Version: "1", LaunchProtocol: "test",
		Models: []string{"model"}, ReasoningModes: []domain.ReasoningMode{domain.ReasoningBackendDefault},
		SessionModes: []domain.SessionMode{domain.SessionModeNew}, Steer: openruntime.SteerUnsupported,
		Approval: openruntime.ApprovalUnsupported, Cancel: openruntime.CancelUnsupported,
		NetworkModes: []string{"inherit", "named_profile"}, MaxConcurrency: 1, RuntimeIdentity: identity,
	}}
	pool, err := NewBackendPool([]RuntimeBackend{{ID: "backend", Adapter: adapter}})
	if err != nil {
		t.Fatal(err)
	}
	materializer, err := runtimenetwork.NewMaterializer(filepath.Join(t.TempDir(), "network"))
	if err != nil {
		t.Fatal(err)
	}
	pool.SetNetworkMaterializer(materializer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		buffer := make([]byte, 4096)
		_, _ = conn.Read(buffer)
		_, _ = conn.Write([]byte("HTTP/1.1 407 Proxy Authentication Required\r\nContent-Length: 0\r\n\r\n"))
	}()
	address := listener.Addr().(*net.TCPAddr)
	content := domain.NetworkProfileContent{
		ProfileID: "http-proxy", ContentVersion: 1, Mode: "only_http_proxy", Host: "127.0.0.1", Port: address.Port,
		CreatedBy: "owner", CreatedAt: time.Now().UTC(),
	}
	content.ManifestDigest = content.ComputeManifestDigest()
	ack := pool.ProcessNetworkWork(context.Background(), &openapi.NetworkWorkEnvelope{Work: domain.NetworkWork{
		ID: "http-auth-test", Kind: domain.NetworkWorkTest, ProfileID: content.ProfileID, ContentVersion: 1,
		AgentID: "agent", BackendID: "backend", WorkerInstanceID: "worker", Generation: 1,
		Mode: domain.NetworkNamedProfile, ManifestDigest: content.ManifestDigest, RuntimeIdentity: identity, Content: &content,
	}})
	if ack.State != "failed" || ack.DiagnosticCode != domain.NetworkDiagnosticProxyAuth {
		t.Fatalf("HTTP 407 ack=%+v", ack)
	}
	if err := domain.ValidateNetworkProbeResults(ack.State, ack.ProbeResults); err != nil {
		t.Fatalf("invalid HTTP 407 layered evidence: %v (%+v)", err, ack.ProbeResults)
	}
}

type networkModeAdapter struct {
	descriptor  openruntime.AdapterDescriptor
	applied     domain.NetworkPolicy
	healthErr   error
	healthCalls *int
}

func (a *networkModeAdapter) Descriptor(context.Context) (openruntime.AdapterDescriptor, error) {
	return a.descriptor, nil
}
func (a *networkModeAdapter) Validate(context.Context, domain.ExecutionSpec) error { return nil }
func (a *networkModeAdapter) Health(context.Context) error {
	if a.healthCalls != nil {
		*a.healthCalls++
	}
	return a.healthErr
}
func (a *networkModeAdapter) StartTurn(context.Context, openruntime.TurnRequest, openruntime.EventSink) (openruntime.TurnHandle, error) {
	return nil, errors.New("unused")
}
func (a *networkModeAdapter) CloneForNetworkProbe(policy domain.NetworkPolicy) (openruntime.AgentRuntimeAdapter, error) {
	clone := *a
	clone.applied = policy
	return &clone, nil
}
func (a *networkModeAdapter) ApplyNetworkPolicy(policy domain.NetworkPolicy) error {
	a.applied = policy
	return nil
}

func nowForTest() time.Time { return time.Now().UTC() }
