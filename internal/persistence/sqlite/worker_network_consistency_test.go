package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

func createPublishedNetworkBinding(t *testing.T, repository *Repository, fixture repositoryFixture) *domain.NetworkBinding {
	t.Helper()
	profile := &domain.ProxyProfile{
		ID: "proxy-worker", Version: 1, Status: domain.NetworkProfileDraft,
		Mode: "only_socks5", Host: "proxy.internal", Port: 28080,
		ConfigFile: "/etc/openagentx/proxy-worker.conf", CreatedBy: fixture.ownerPrincipal,
		CreatedAt: repositoryTestTime, UpdatedAt: repositoryTestTime,
	}
	if err := repository.CreateProxyProfile(context.Background(), profile); err != nil {
		t.Fatal(err)
	}
	published, err := repository.PublishProxyProfile(context.Background(), profile.ID, profile.Version,
		fixture.ownerPrincipal, repositoryTestTime.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	binding := &domain.NetworkBinding{
		AgentID: fixture.agentID, BackendID: "local", ProfileID: published.ID,
		ProfileVersion: published.Version, Version: 1, DesiredStatus: "pending",
		UpdatedAt: repositoryTestTime.Add(2 * time.Minute),
	}
	if err := repository.BindNetworkProfile(context.Background(), binding, 0); err != nil {
		t.Fatal(err)
	}
	return binding
}

func registerNetworkWorker(t *testing.T, repository *Repository, fixture repositoryFixture) (*domain.WorkerInstance, domain.WorkerWriteGuard, []domain.NetworkBinding) {
	t.Helper()
	descriptor := messageDescriptor(openruntime.SteerQueued)
	descriptor.NetworkModes = []string{"inherit", "named_profile"}
	registration := domain.WorkerRegistration{
		WorkerInstanceID: "worker-network", AgentID: fixture.agentID, Transport: domain.WorkerTransportUnix,
		PrincipalID: fixture.agentPrincipal, Capabilities: []string{"coding"},
		SessionTokenDigest: "network-session-token-digest", TokenExpiresAt: repositoryTestTime.Add(time.Hour),
		LeaseUntil: repositoryTestTime.Add(time.Hour),
	}
	worker, bindings, err := repository.RegisterWorker(context.Background(), registration,
		[]openruntime.BackendRegistration{{BackendID: "local", Descriptor: descriptor, Health: openruntime.BackendHealthy}},
		journalEvent("event-worker-network", "worker.registered", fixture.agentPrincipal, fixture.organizationID))
	if err != nil {
		t.Fatal(err)
	}
	guard := domain.WorkerWriteGuard{
		WorkerInstanceID: worker.ID, AgentID: worker.AgentID, PrincipalID: fixture.agentPrincipal,
		SessionTokenDigest: registration.SessionTokenDigest, Generation: worker.Generation,
		FencingToken: worker.FencingToken, CheckedAt: repositoryTestTime.Add(3 * time.Minute),
	}
	return worker, guard, bindings
}

func networkApplication(binding domain.NetworkBinding, state, eventID string) domain.NetworkBindingApplication {
	diagnostic := ""
	if state == "failed" {
		diagnostic = domain.NetworkApplyFailedDiagnostic
	}
	return domain.NetworkBindingApplication{
		BackendID: binding.BackendID, ProfileID: binding.ProfileID, ProfileVersion: binding.ProfileVersion,
		BindingRevision: binding.Version, State: state, Diagnostic: diagnostic,
		Event: journalEvent(eventID, "network_binding."+state, "agent-quote-principal", "org-main"),
	}
}

func TestRegisterWorkerRollsBackWhenInitialBindingLoadFails(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	createPublishedNetworkBinding(t, repository, fixture)
	if _, err := repository.db.Exec(`UPDATE network_profiles SET updated_at='not-a-time' WHERE profile_id='proxy-worker'`); err != nil {
		t.Fatal(err)
	}

	descriptor := messageDescriptor(openruntime.SteerQueued)
	registration := domain.WorkerRegistration{
		WorkerInstanceID: "worker-register-rollback", AgentID: fixture.agentID,
		Transport: domain.WorkerTransportUnix, PrincipalID: fixture.agentPrincipal,
		SessionTokenDigest: "register-rollback-token-digest", TokenExpiresAt: repositoryTestTime.Add(time.Hour),
		LeaseUntil: repositoryTestTime.Add(time.Hour),
	}
	if _, _, err := repository.RegisterWorker(context.Background(), registration,
		[]openruntime.BackendRegistration{{BackendID: "local", Descriptor: descriptor, Health: openruntime.BackendHealthy}},
		journalEvent("event-register-rollback", "worker.registered", fixture.agentPrincipal, fixture.organizationID)); err == nil {
		t.Fatal("registration unexpectedly survived binding load failure")
	}
	if _, err := repository.GetWorkerCredential(context.Background(), registration.WorkerInstanceID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("registration rollback left Worker credential: %v", err)
	}
}

func TestHeartbeatNetworkAckIsAtomicRevisionedAndJournaledOnce(t *testing.T) {
	var failCommit atomic.Bool
	repository, _ := openTestRepository(t, func(point FaultPoint) error {
		if failCommit.Load() && point == FaultBeforeCommit {
			return errors.New("forced heartbeat rollback")
		}
		return nil
	})
	fixture := seedRepository(t, repository)
	createPublishedNetworkBinding(t, repository, fixture)
	worker, guard, initial := registerNetworkWorker(t, repository, fixture)
	if len(initial) != 1 || initial[0].Version != 1 {
		t.Fatalf("initial bindings=%+v", initial)
	}
	application := networkApplication(initial[0], "applied", "event-network-applied")
	heartbeat := func(eventID string) error {
		_, err := repository.HeartbeatWorker(context.Background(), guard, domain.WorkerStatusOnline,
			map[string]openruntime.BackendHealth{"local": openruntime.BackendHealthy},
			guard.CheckedAt.Add(time.Hour), guard.CheckedAt.Add(time.Hour), []domain.NetworkBindingApplication{application},
			journalEvent(eventID, "worker.heartbeat", fixture.agentPrincipal, fixture.organizationID))
		return err
	}

	failCommit.Store(true)
	if err := heartbeat("event-heartbeat-rollback"); err == nil {
		t.Fatal("heartbeat unexpectedly committed through fault")
	}
	failCommit.Store(false)
	credential, err := repository.GetWorkerCredential(context.Background(), worker.ID)
	if err != nil || credential.Worker.Status != domain.WorkerStatusBootstrapping {
		t.Fatalf("heartbeat rollback Worker=%+v err=%v", credential, err)
	}
	bindings, err := repository.ListNetworkBindings(context.Background(), fixture.agentID)
	if err != nil || bindings[0].DesiredStatus != "pending" || bindings[0].AppliedBindingRevision != 0 {
		t.Fatalf("heartbeat rollback binding=%+v err=%v", bindings, err)
	}

	if err := heartbeat("event-heartbeat-applied"); err != nil {
		t.Fatal(err)
	}
	application.Event = journalEvent("event-network-applied-replay", "network_binding.applied", fixture.agentPrincipal, fixture.organizationID)
	if err := heartbeat("event-heartbeat-replay"); err != nil {
		t.Fatal(err)
	}
	events, err := repository.ListJournal(context.Background(), 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	networkEvents := 0
	for _, event := range events {
		if event.EventType == "network_binding.applied" {
			networkEvents++
		}
	}
	if networkEvents != 1 {
		t.Fatalf("network applied journal count=%d", networkEvents)
	}
	application = networkApplication(initial[0], "failed", "event-network-failed")
	if err := heartbeat("event-heartbeat-failed"); err != nil {
		t.Fatal(err)
	}
	application.Event = journalEvent("event-network-failed-replay", "network_binding.failed", fixture.agentPrincipal, fixture.organizationID)
	if err := heartbeat("event-heartbeat-failed-replay"); err != nil {
		t.Fatal(err)
	}
	bindings, err = repository.ListNetworkBindings(context.Background(), fixture.agentID)
	if err != nil || bindings[0].DesiredStatus != "failed" || bindings[0].AppliedWorkerID != worker.ID ||
		bindings[0].AppliedBindingRevision != 1 {
		t.Fatalf("failed acknowledgement overwrote applied fact=%+v err=%v", bindings, err)
	}
	events, err = repository.ListJournal(context.Background(), 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	failedEvents := 0
	for _, event := range events {
		if event.EventType == "network_binding.failed" {
			failedEvents++
		}
	}
	if failedEvents != 1 {
		t.Fatalf("network failed journal count=%d", failedEvents)
	}

	rebound := &domain.NetworkBinding{
		AgentID: fixture.agentID, BackendID: "local", ProfileID: initial[0].ProfileID,
		ProfileVersion: initial[0].ProfileVersion, Version: 1, DesiredStatus: "pending",
		UpdatedAt: guard.CheckedAt.Add(time.Minute),
	}
	if err := repository.BindNetworkProfile(context.Background(), rebound, 1); err != nil {
		t.Fatal(err)
	}
	application.Event = journalEvent("event-network-stale", "network_binding.applied", fixture.agentPrincipal, fixture.organizationID)
	if err := heartbeat("event-heartbeat-stale-ack"); err != nil {
		t.Fatalf("stale binding acknowledgement stopped heartbeat: %v", err)
	}
	bindings, err = repository.ListNetworkBindings(context.Background(), fixture.agentID)
	if err != nil || bindings[0].Version != 2 || bindings[0].DesiredStatus != "pending" || bindings[0].AppliedBindingRevision != 0 {
		t.Fatalf("stale acknowledgement changed rebound binding=%+v err=%v", bindings, err)
	}
}

func TestListWorkerBackendsRequiresCurrentGenerationAckAndPersistsExplicitPolicy(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	createPublishedNetworkBinding(t, repository, fixture)
	worker, guard, bindings := registerNetworkWorker(t, repository, fixture)
	listed, err := repository.ListWorkerBackends(context.Background(), worker.ID)
	if err != nil || len(listed) != 1 || listed[0].Health != openruntime.BackendUnavailable {
		t.Fatalf("pending binding scheduled Backend=%+v err=%v", listed, err)
	}
	application := networkApplication(bindings[0], "applied", "event-network-list-applied")
	if _, err := repository.HeartbeatWorker(context.Background(), guard, domain.WorkerStatusOnline,
		map[string]openruntime.BackendHealth{"local": openruntime.BackendHealthy}, guard.CheckedAt.Add(time.Hour),
		guard.CheckedAt.Add(time.Hour), []domain.NetworkBindingApplication{application},
		journalEvent("event-heartbeat-list-applied", "worker.heartbeat", fixture.agentPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	listed, err = repository.ListWorkerBackends(context.Background(), worker.ID)
	if err != nil || listed[0].Health != openruntime.BackendHealthy || listed[0].Network.BindingRevision != 1 ||
		listed[0].Network.ProfileID != bindings[0].ProfileID {
		t.Fatalf("acknowledged Backend=%+v err=%v", listed, err)
	}
	encoded, _ := json.Marshal(listed[0].Network)
	if string(encoded) == `{}` {
		t.Fatal("Worker Backend policy fell back to unknown zero value")
	}
	if _, err := repository.db.Exec(`UPDATE runtime_backend_registrations SET network_json='{}' WHERE worker_instance_id=?`, worker.ID); err != nil {
		t.Fatal(err)
	}
	listed, err = repository.ListWorkerBackends(context.Background(), worker.ID)
	if err != nil || listed[0].Health != openruntime.BackendUnavailable {
		t.Fatalf("legacy unknown network policy was schedulable: Backend=%+v err=%v", listed, err)
	}
}
