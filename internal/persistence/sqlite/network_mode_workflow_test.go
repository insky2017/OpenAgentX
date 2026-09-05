package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openagentx/internal/api"
	"openagentx/internal/controlplane"
	"openagentx/internal/domain"
	"openagentx/internal/network/secretstore"
	openruntime "openagentx/internal/runtime"
)

func TestNetworkModeWorkflowPreservesAppliedSnapshotAcrossPendingSwitch(t *testing.T) {
	ctx := context.Background()
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	descriptor := messageDescriptor(openruntime.SteerQueued)
	descriptor.NetworkModes = []string{"inherit", "direct", "named_profile"}
	descriptor.RuntimeIdentity = domain.RuntimeIdentity{AdapterID: descriptor.AdapterID, AdapterVersion: descriptor.Version}
	worker, guard := registerModeWorker(t, repository, fixture, descriptor)
	secrets, err := secretstore.Open(filepath.Join(t.TempDir(), "secrets"))
	if err != nil {
		t.Fatal(err)
	}
	now := repositoryTestTime.Add(10 * time.Minute)
	workflow, err := controlplane.NewNetworkWorkflowService(repository, secrets, nil, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	createRequest := api.CreateNetworkProfileRequest{
		Meta: api.CommandMeta{IdempotencyKey: "create-named"}, ProfileID: "named", Mode: "only_http_proxy", Host: "proxy.internal", Port: 8080,
	}
	createResult, err := workflow.CreateDraft(ctx, fixture.ownerPrincipal, createRequest)
	if err != nil {
		t.Fatal(err)
	}
	namedTestRequest := api.TestNetworkProfileRequest{
		Meta: api.CommandMeta{IdempotencyKey: "test-named", ExpectedVersion: 1}, WorkerInstanceID: worker.ID, Generation: worker.Generation, BackendID: "local",
	}
	namedTestResult, err := workflow.StartTest(ctx, fixture.ownerPrincipal, "named", namedTestRequest)
	if err != nil {
		t.Fatal(err)
	}
	namedTest := pullNetworkWork(t, workflow, guard)
	if namedTest.Work.Mode != domain.NetworkNamedProfile || namedTest.Work.Content == nil {
		t.Fatalf("named test work=%+v", namedTest.Work)
	}
	ackNetworkWork(t, workflow, guard, namedTest.Work.ID, api.NetworkWorkAckRequest{State: "succeeded", ProbeResults: successfulProbeResults()})
	replayResult, replayErr := workflow.CreateDraft(ctx, fixture.ownerPrincipal, createRequest)
	assertNetworkReplay(t, createResult, replayResult, replayErr)
	conflictingCreate := createRequest
	conflictingCreate.Port++
	if _, err := workflow.CreateDraft(ctx, fixture.ownerPrincipal, conflictingCreate); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("same-key different create error=%v", err)
	}
	replayResult, replayErr = workflow.StartTest(ctx, fixture.ownerPrincipal, "named", namedTestRequest)
	assertNetworkReplay(t, namedTestResult, replayResult, replayErr)
	conflictingNamedTest := namedTestRequest
	conflictingNamedTest.Generation++
	if _, err := workflow.StartTest(ctx, fixture.ownerPrincipal, "named", conflictingNamedTest); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("same-key different profile test error=%v", err)
	}
	publishRequest := api.PublishNetworkProfileRequest{
		Meta: api.CommandMeta{IdempotencyKey: "publish-named", ExpectedVersion: 3},
	}
	publishResult, err := workflow.Publish(ctx, fixture.ownerPrincipal, "named", publishRequest)
	if err != nil {
		t.Fatal(err)
	}
	bindRequest := api.BindNetworkProfileRequest{
		Meta: api.CommandMeta{IdempotencyKey: "bind-named"}, AgentID: fixture.agentID, BackendID: "local",
		ProfileID: "named", ProfileVersion: 1, WorkerInstanceID: worker.ID, Generation: worker.Generation,
	}
	bindResult, err := workflow.Bind(ctx, fixture.ownerPrincipal, bindRequest)
	if err != nil {
		t.Fatal(err)
	}
	namedApply := pullNetworkWork(t, workflow, guard)
	namedPolicy := policyForWork(namedApply.Work)
	ackNetworkWork(t, workflow, guard, namedApply.Work.ID, api.NetworkWorkAckRequest{State: "succeeded", Policy: &namedPolicy})
	replayResult, replayErr = workflow.Publish(ctx, fixture.ownerPrincipal, "named", publishRequest)
	assertNetworkReplay(t, publishResult, replayResult, replayErr)
	conflictingPublish := publishRequest
	conflictingPublish.Meta.ExpectedVersion++
	if _, err := workflow.Publish(ctx, fixture.ownerPrincipal, "named", conflictingPublish); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("same-key different publish error=%v", err)
	}
	replayResult, replayErr = workflow.Bind(ctx, fixture.ownerPrincipal, bindRequest)
	assertNetworkReplay(t, bindResult, replayResult, replayErr)
	conflictingBind := bindRequest
	conflictingBind.Generation++
	if _, err := workflow.Bind(ctx, fixture.ownerPrincipal, conflictingBind); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("same-key different bind error=%v", err)
	}
	binding, err := repository.GetNetworkBinding(ctx, fixture.agentID, "local")
	if err != nil || binding.Mode != domain.NetworkNamedProfile || binding.AppliedMode != domain.NetworkNamedProfile ||
		binding.AppliedProfileID != "named" || binding.AppliedProfileVersion != 1 || binding.AppliedBindingRevision != 1 {
		t.Fatalf("named binding=%+v err=%v", binding, err)
	}
	backends, err := repository.ListWorkerBackends(ctx, worker.ID)
	if err != nil || len(backends) != 1 || backends[0].Health != openruntime.BackendHealthy || backends[0].Network.Mode != domain.NetworkNamedProfile {
		t.Fatalf("named Backend=%+v err=%v", backends, err)
	}
	oldSnapshot := backends[0].Network
	rollbackRequest := api.RollbackNetworkBindingRequest{
		Meta: api.CommandMeta{IdempotencyKey: "rollback-named", ExpectedVersion: 1}, AgentID: fixture.agentID, BackendID: "local",
		TargetContentVersion: 1, WorkerInstanceID: worker.ID, Generation: worker.Generation,
	}
	rollbackResult, err := workflow.Rollback(ctx, fixture.ownerPrincipal, rollbackRequest)
	if err != nil {
		t.Fatal(err)
	}
	replayResult, replayErr = workflow.Rollback(ctx, fixture.ownerPrincipal, rollbackRequest)
	assertNetworkReplay(t, rollbackResult, replayResult, replayErr)
	conflictingRollback := rollbackRequest
	conflictingRollback.TargetContentVersion = 2
	if _, err := workflow.Rollback(ctx, fixture.ownerPrincipal, conflictingRollback); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("same-key different rollback error=%v", err)
	}
	rollbackTest := pullNetworkWork(t, workflow, guard)
	ackNetworkWork(t, workflow, guard, rollbackTest.Work.ID, api.NetworkWorkAckRequest{State: "succeeded", ProbeResults: successfulProbeResults()})
	created, claimed := claimNetworkTask(t, repository, fixture, guard, "before-direct")

	if _, err := workflow.StartModeTest(ctx, fixture.ownerPrincipal, api.TestNetworkModeRequest{
		Meta: api.CommandMeta{IdempotencyKey: "stale-mode-test", ExpectedVersion: 0}, AgentID: fixture.agentID, BackendID: "local",
		Mode: domain.NetworkDirect, WorkerInstanceID: worker.ID, Generation: worker.Generation,
	}); !errors.Is(err, domain.ErrStaleVersion) {
		t.Fatalf("stale mode test error=%v", err)
	}
	directModeRequest := api.TestNetworkModeRequest{
		Meta: api.CommandMeta{IdempotencyKey: "test-direct", ExpectedVersion: 1}, AgentID: fixture.agentID, BackendID: "local",
		Mode: domain.NetworkDirect, WorkerInstanceID: worker.ID, Generation: worker.Generation,
	}
	modeResult, err := workflow.StartModeTest(ctx, fixture.ownerPrincipal, directModeRequest)
	if err != nil {
		t.Fatal(err)
	}
	var modeReceipt struct {
		TestID string `json:"test_id"`
	}
	if err := json.Unmarshal(modeResult, &modeReceipt); err != nil || modeReceipt.TestID == "" {
		t.Fatalf("mode receipt=%s err=%v", modeResult, err)
	}
	unchanged, err := repository.GetNetworkBinding(ctx, fixture.agentID, "local")
	if err != nil || unchanged.Version != 1 || unchanged.DesiredStatus != "applied" {
		t.Fatalf("mode test mutated binding=%+v err=%v", unchanged, err)
	}
	directTest := pullNetworkWork(t, workflow, guard)
	if directTest.Work.Mode != domain.NetworkDirect || directTest.Work.ProfileID != "" || directTest.Work.PolicyVersion == 0 {
		t.Fatalf("direct test work=%+v", directTest.Work)
	}
	ackNetworkWork(t, workflow, guard, directTest.Work.ID, api.NetworkWorkAckRequest{State: "succeeded", ProbeResults: successfulProbeResults()})
	directPublishRequest := api.PublishNetworkModeRequest{
		Meta: api.CommandMeta{IdempotencyKey: "publish-direct", ExpectedVersion: 1}, TestID: modeReceipt.TestID,
		WorkerInstanceID: worker.ID, Generation: worker.Generation,
	}
	directPublishResult, err := workflow.PublishMode(ctx, fixture.ownerPrincipal, directPublishRequest)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := repository.GetNetworkBinding(ctx, fixture.agentID, "local")
	if err != nil || pending.Mode != domain.NetworkDirect || pending.Version != 2 || pending.DesiredStatus != "pending" ||
		pending.AppliedMode != domain.NetworkNamedProfile || pending.AppliedProfileID != "named" ||
		pending.AppliedProfileVersion != 1 || pending.AppliedBindingRevision != 1 {
		t.Fatalf("pending direct binding lost applied named fact=%+v err=%v", pending, err)
	}
	backends, err = repository.ListWorkerBackends(ctx, worker.ID)
	if err != nil || backends[0].Health != openruntime.BackendUnavailable {
		t.Fatalf("pending direct binding was schedulable=%+v err=%v", backends, err)
	}
	oldRun := beginClaimedNetworkRun(t, repository, fixture, worker, guard, descriptor, created, claimed, "old", oldSnapshot)

	directApply := pullNetworkWork(t, workflow, guard)
	directPolicy := policyForWork(directApply.Work)
	ackNetworkWork(t, workflow, guard, directApply.Work.ID, api.NetworkWorkAckRequest{State: "succeeded", Policy: &directPolicy})
	replayResult, replayErr = workflow.StartModeTest(ctx, fixture.ownerPrincipal, directModeRequest)
	assertNetworkReplay(t, modeResult, replayResult, replayErr)
	replayResult, replayErr = workflow.PublishMode(ctx, fixture.ownerPrincipal, directPublishRequest)
	assertNetworkReplay(t, directPublishResult, replayResult, replayErr)
	conflictingModePublish := directPublishRequest
	conflictingModePublish.Generation++
	if _, err := workflow.PublishMode(ctx, fixture.ownerPrincipal, conflictingModePublish); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("same-key different mode publish error=%v", err)
	}
	conflictingModeRequest := directModeRequest
	conflictingModeRequest.Mode = domain.NetworkInherit
	if _, err := workflow.StartModeTest(ctx, fixture.ownerPrincipal, conflictingModeRequest); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("same-key different mode error=%v", err)
	}
	applied, err := repository.GetNetworkBinding(ctx, fixture.agentID, "local")
	if err != nil || applied.DesiredStatus != "applied" || applied.AppliedMode != domain.NetworkDirect ||
		applied.AppliedPolicyVersion != applied.PolicyVersion || applied.AppliedProfileID != "" ||
		applied.AppliedProfileVersion != 0 || applied.AppliedBindingRevision != 2 {
		t.Fatalf("applied direct binding=%+v err=%v", applied, err)
	}
	backends, err = repository.ListWorkerBackends(ctx, worker.ID)
	if err != nil || backends[0].Health != openruntime.BackendHealthy || backends[0].Network.Mode != domain.NetworkDirect ||
		backends[0].Network.PolicyVersion != applied.PolicyVersion || backends[0].Network.BindingRevision != 2 {
		t.Fatalf("direct Backend=%+v err=%v", backends, err)
	}
	if err := repository.FinishRun(ctx, guard, oldRun.ID, 2, oldRun.Version,
		openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "old snapshot completed", SideEffectsKnown: true},
		nil, 0, nil,
		journalEvent("event-task-mode-old-finished", "task.settled", fixture.agentPrincipal, fixture.organizationID),
		journalEvent("event-run-mode-old-finished", "run_attempt.finished", fixture.agentPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	newTask, newClaim := claimNetworkTask(t, repository, fixture, guard, "after-direct")
	beginClaimedNetworkRun(t, repository, fixture, worker, guard, descriptor, newTask, newClaim, "new", backends[0].Network)

	inheritResult, err := workflow.StartModeTest(ctx, fixture.ownerPrincipal, api.TestNetworkModeRequest{
		Meta: api.CommandMeta{IdempotencyKey: "test-inherit", ExpectedVersion: 2}, AgentID: fixture.agentID, BackendID: "local",
		Mode: domain.NetworkInherit, WorkerInstanceID: worker.ID, Generation: worker.Generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	var inheritReceipt struct {
		TestID string `json:"test_id"`
	}
	if err := json.Unmarshal(inheritResult, &inheritReceipt); err != nil {
		t.Fatal(err)
	}
	inheritTest := pullNetworkWork(t, workflow, guard)
	ackNetworkWork(t, workflow, guard, inheritTest.Work.ID, api.NetworkWorkAckRequest{State: "succeeded", ProbeResults: successfulProbeResults()})
	if _, err := workflow.PublishMode(ctx, fixture.ownerPrincipal, api.PublishNetworkModeRequest{
		Meta: api.CommandMeta{IdempotencyKey: "publish-inherit", ExpectedVersion: 2}, TestID: inheritReceipt.TestID,
		WorkerInstanceID: worker.ID, Generation: worker.Generation,
	}); err != nil {
		t.Fatal(err)
	}
	inheritApply := pullNetworkWork(t, workflow, guard)
	ackNetworkWork(t, workflow, guard, inheritApply.Work.ID, api.NetworkWorkAckRequest{State: "failed", DiagnosticCode: domain.NetworkDiagnosticRuntime})
	failed, err := repository.GetNetworkBinding(ctx, fixture.agentID, "local")
	if err != nil || failed.DesiredStatus != "failed" || failed.Mode != domain.NetworkInherit ||
		failed.AppliedMode != domain.NetworkDirect || failed.AppliedPolicyVersion != applied.PolicyVersion ||
		failed.AppliedBindingRevision != 2 {
		t.Fatalf("failed inherit apply overwrote direct applied fact=%+v err=%v", failed, err)
	}
	backends, err = repository.ListWorkerBackends(ctx, worker.ID)
	if err != nil || backends[0].Health != openruntime.BackendUnavailable {
		t.Fatalf("failed desired mode remained schedulable=%+v err=%v", backends, err)
	}
	importRequest := api.ImportNetworkProfileRequest{
		Meta: api.CommandMeta{IdempotencyKey: "import-replay"}, ProfileID: "imported-replay",
		WorkerInstanceID: worker.ID, Generation: worker.Generation, BackendID: "local",
	}
	importResult, err := workflow.StartImport(ctx, fixture.ownerPrincipal, importRequest)
	if err != nil {
		t.Fatal(err)
	}
	replayResult, replayErr = workflow.StartImport(ctx, fixture.ownerPrincipal, importRequest)
	assertNetworkReplay(t, importResult, replayResult, replayErr)
	conflictingImport := importRequest
	conflictingImport.ProfileID = "imported-other"
	if _, err := workflow.StartImport(ctx, fixture.ownerPrincipal, conflictingImport); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("same-key different import error=%v", err)
	}
}

func TestNetworkModeTestRejectsUndeclaredDirectCapability(t *testing.T) {
	repository, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, repository)
	descriptor := messageDescriptor(openruntime.SteerQueued)
	descriptor.NetworkModes = []string{"inherit", "named_profile"}
	descriptor.RuntimeIdentity = domain.RuntimeIdentity{AdapterID: descriptor.AdapterID, AdapterVersion: descriptor.Version}
	worker, _ := registerModeWorker(t, repository, fixture, descriptor)
	secrets, err := secretstore.Open(filepath.Join(t.TempDir(), "secrets"))
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := controlplane.NewNetworkWorkflowService(repository, secrets, nil, func() time.Time { return repositoryTestTime })
	if err != nil {
		t.Fatal(err)
	}
	_, err = workflow.StartModeTest(context.Background(), fixture.ownerPrincipal, api.TestNetworkModeRequest{
		Meta: api.CommandMeta{IdempotencyKey: "unsupported-direct"}, AgentID: fixture.agentID, BackendID: "local",
		Mode: domain.NetworkDirect, WorkerInstanceID: worker.ID, Generation: worker.Generation,
	})
	if !errors.Is(err, domain.ErrUnsupportedCapability) {
		t.Fatalf("undeclared direct mode error=%v", err)
	}
}

func TestReplaceNetworkSecretCleansFailedMetadataAndRetriesDeterministically(t *testing.T) {
	failCommit := false
	repository, _ := openTestRepository(t, func(point FaultPoint) error {
		if failCommit && point == FaultBeforeCommit {
			return errors.New("metadata commit failed")
		}
		return nil
	})
	fixture := seedRepository(t, repository)
	secretRoot := filepath.Join(t.TempDir(), "secrets")
	secrets, err := secretstore.Open(secretRoot)
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := controlplane.NewNetworkWorkflowService(repository, secrets, nil, func() time.Time { return repositoryTestTime })
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := workflow.CreateDraft(ctx, fixture.ownerPrincipal, api.CreateNetworkProfileRequest{
		Meta: api.CommandMeta{IdempotencyKey: "create-secret-cleanup"}, ProfileID: "secret-cleanup",
		Mode: "only_socks5", Host: "proxy.internal", Port: 1080,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.CreateDraft(ctx, fixture.ownerPrincipal, api.CreateNetworkProfileRequest{
		Meta: api.CommandMeta{IdempotencyKey: "create-edit-replay"}, ProfileID: "edit-replay",
		Mode: "only_socks5", Host: "proxy.internal", Port: 1080,
	}); err != nil {
		t.Fatal(err)
	}
	editRequest := api.EditNetworkProfileRequest{
		Meta: api.CommandMeta{IdempotencyKey: "edit-replay", ExpectedVersion: 1},
		Mode: "only_socks5", Host: "proxy-edited.internal", Port: 1081,
	}
	editResult, err := workflow.EditDraft(ctx, fixture.ownerPrincipal, "edit-replay", editRequest)
	if err != nil {
		t.Fatal(err)
	}
	replayResult, replayErr := workflow.EditDraft(ctx, fixture.ownerPrincipal, "edit-replay", editRequest)
	assertNetworkReplay(t, editResult, replayResult, replayErr)
	conflictingEdit := editRequest
	conflictingEdit.Port = 1082
	if _, err := workflow.EditDraft(ctx, fixture.ownerPrincipal, "edit-replay", conflictingEdit); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("same-key different edit error=%v", err)
	}
	request := api.ReplaceNetworkSecretRequest{
		Meta:     api.CommandMeta{IdempotencyKey: "replace-secret-cleanup", ExpectedVersion: 1},
		Username: "user", Password: "sensitive-value",
	}
	failCommit = true
	if _, err := workflow.ReplaceSecret(ctx, fixture.ownerPrincipal, "secret-cleanup", request); err == nil {
		t.Fatal("fault-injected metadata commit succeeded")
	}
	entries, err := os.ReadDir(secretRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".secret") {
			t.Fatalf("failed metadata commit left orphan secret %q", entry.Name())
		}
	}
	failCommit = false
	replaceResult, err := workflow.ReplaceSecret(ctx, fixture.ownerPrincipal, "secret-cleanup", request)
	if err != nil {
		t.Fatalf("deterministic retry failed: %v", err)
	}
	replayResult, replayErr = workflow.ReplaceSecret(ctx, fixture.ownerPrincipal, "secret-cleanup", request)
	assertNetworkReplay(t, replaceResult, replayResult, replayErr)
	conflictingReplace := request
	conflictingReplace.Password = "different-sensitive-value"
	if _, err := workflow.ReplaceSecret(ctx, fixture.ownerPrincipal, "secret-cleanup", conflictingReplace); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("same-key different secret error=%v", err)
	}
	entries, err = os.ReadDir(secretRoot)
	if err != nil {
		t.Fatal(err)
	}
	secretFiles := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".secret") {
			secretFiles++
		}
	}
	if secretFiles != 1 {
		t.Fatalf("successful retry secret files=%d want=1", secretFiles)
	}
}

func registerModeWorker(t *testing.T, repository *Repository, fixture repositoryFixture, descriptor openruntime.AdapterDescriptor) (*domain.WorkerInstance, domain.WorkerWriteGuard) {
	t.Helper()
	registration := domain.WorkerRegistration{
		WorkerInstanceID: "worker-mode", AgentID: fixture.agentID, Transport: domain.WorkerTransportUnix,
		PrincipalID: fixture.agentPrincipal, Capabilities: []string{"coding"}, SessionTokenDigest: "mode-token-digest",
		TokenExpiresAt: repositoryTestTime.Add(time.Hour), LeaseUntil: repositoryTestTime.Add(time.Hour),
	}
	worker, _, err := repository.RegisterWorker(context.Background(), registration, []openruntime.BackendRegistration{{
		BackendID: "local", Descriptor: descriptor, Health: openruntime.BackendHealthy, Network: domain.NetworkPolicy{Mode: domain.NetworkInherit},
	}}, journalEvent("event-worker-mode", "worker.registered", fixture.ownerPrincipal, fixture.organizationID))
	if err != nil {
		t.Fatal(err)
	}
	guard := domain.WorkerWriteGuard{WorkerInstanceID: worker.ID, AgentID: worker.AgentID, PrincipalID: fixture.agentPrincipal,
		SessionTokenDigest: registration.SessionTokenDigest, Generation: worker.Generation, FencingToken: worker.FencingToken,
		CheckedAt: repositoryTestTime.Add(15 * time.Minute)}
	if _, err := repository.HeartbeatWorker(context.Background(), guard, domain.WorkerStatusOnline,
		map[string]openruntime.BackendHealth{"local": openruntime.BackendHealthy}, guard.CheckedAt.Add(time.Hour), guard.CheckedAt.Add(time.Hour), nil,
		journalEvent("event-worker-mode-online", "worker.heartbeat", fixture.ownerPrincipal, fixture.organizationID)); err != nil {
		t.Fatal(err)
	}
	return worker, guard
}

func pullNetworkWork(t *testing.T, workflow *controlplane.NetworkWorkflowService, guard domain.WorkerWriteGuard) *api.NetworkWorkEnvelope {
	t.Helper()
	work, err := workflow.PullWork(context.Background(), guard)
	if err != nil || work == nil {
		t.Fatalf("pull network work=%+v err=%v", work, err)
	}
	return work
}

func ackNetworkWork(t *testing.T, workflow *controlplane.NetworkWorkflowService, guard domain.WorkerWriteGuard, workID string, result api.NetworkWorkAckRequest) {
	t.Helper()
	result.WorkerInstanceID = guard.WorkerInstanceID
	result.Generation = guard.Generation
	result.FencingToken = guard.FencingToken
	if err := workflow.AcknowledgeWork(context.Background(), guard, workID, result); err != nil {
		t.Fatal(err)
	}
}

func successfulProbeResults() []domain.NetworkProbeResult {
	return []domain.NetworkProbeResult{
		{Layer: domain.NetworkProbeConfiguration, State: domain.NetworkProbePassed},
		{Layer: domain.NetworkProbeSecret, State: domain.NetworkProbeNotApplicable},
		{Layer: domain.NetworkProbeEndpoint, State: domain.NetworkProbeNotApplicable},
		{Layer: domain.NetworkProbeDirectRules, State: domain.NetworkProbeNotApplicable},
		{Layer: domain.NetworkProbeRuntimeHealth, State: domain.NetworkProbePassed},
		{Layer: domain.NetworkProbeNetworkEffect, State: domain.NetworkProbeNotVerified, DiagnosticCode: domain.NetworkDiagnosticNotVerified},
		{Layer: domain.NetworkProbeModelCall, State: domain.NetworkProbeNotVerified, DiagnosticCode: domain.NetworkDiagnosticNotVerified},
	}
}

func assertNetworkReplay(t *testing.T, want json.RawMessage, got json.RawMessage, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("idempotent replay failed: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("idempotent replay=%s want=%s", got, want)
	}
}

func policyForWork(work domain.NetworkWork) domain.NetworkPolicy {
	policy := domain.NetworkPolicy{Mode: work.Mode, PolicyVersion: work.PolicyVersion, BindingRevision: work.BindingRevision,
		ManifestDigest: work.ManifestDigest, RuntimeIdentity: work.RuntimeIdentity}
	if work.Content != nil {
		policy.ProfileID = work.Content.ProfileID
		policy.ProfileVersion = work.Content.ContentVersion
		policy.ProxyMode = work.Content.Mode
		policy.DirectDestinations = append([]string(nil), work.Content.DirectIPs...)
		policy.SecretVersion = work.Content.SecretVersion
		policy.MaterializationDigest = strings.Repeat("d", 64)
	}
	return policy
}

func claimNetworkTask(t *testing.T, repository *Repository, fixture repositoryFixture, guard domain.WorkerWriteGuard, suffix string) (*CreateTaskResult, *domain.MailboxItem) {
	t.Helper()
	created := createTask(t, repository, fixture, suffix)
	claimed, err := repository.TryClaimMailbox(context.Background(), guard, 1, guard.CheckedAt.Add(time.Hour),
		journalEvent("event-claim-"+suffix, "mailbox.claimed", fixture.agentPrincipal, fixture.organizationID))
	if err != nil || claimed == nil {
		t.Fatalf("claim network task=%+v err=%v", claimed, err)
	}
	return created, claimed
}

func beginClaimedNetworkRun(t *testing.T, repository *Repository, fixture repositoryFixture, worker *domain.WorkerInstance, guard domain.WorkerWriteGuard, descriptor openruntime.AdapterDescriptor, created *CreateTaskResult, claimed *domain.MailboxItem, suffix string, policy domain.NetworkPolicy) *domain.RunAttempt {
	t.Helper()
	resolved, err := json.Marshal(domain.ResolvedExecutionSpec{Version: 1, Spec: domain.ExecutionSpec{
		AdapterID: descriptor.AdapterID, BackendID: "local", Model: descriptor.Models[0],
		Reasoning: domain.ReasoningSpec{Mode: domain.ReasoningBackendDefault},
		Session:   domain.SessionSpec{Mode: domain.SessionModeNew, ContextID: created.Task.ID}, Timeout: time.Minute, Network: policy,
	}})
	if err != nil {
		t.Fatal(err)
	}
	run := &domain.RunAttempt{ID: "run-mode-" + suffix, TaskID: created.Task.ID, AgentID: fixture.agentID, Version: 1,
		Status: domain.RunAttemptStarting, WorkerInstanceID: worker.ID, FencingToken: worker.FencingToken,
		LeaseUntil: guard.CheckedAt.Add(time.Hour), ExecutionSpecVersion: 1, RequestedExecutionJSON: `{}`,
		ResolvedExecutionJSON: string(resolved), AdapterID: descriptor.AdapterID, BackendID: "local", Model: descriptor.Models[0],
		ReasoningMode: domain.ReasoningBackendDefault}
	if _, _, err := repository.BeginClaimedRunAttempt(context.Background(), guard, claimed.ID, created.Task.Version, run, "", nil,
		journalEvent("event-task-mode-"+suffix, "task.running", fixture.agentPrincipal, fixture.organizationID),
		journalEvent("event-run-mode-"+suffix, "run_attempt.started", fixture.agentPrincipal, fixture.organizationID),
		journalEvent("event-mailbox-mode-"+suffix, "mailbox.accepted", fixture.agentPrincipal, fixture.organizationID)); err != nil {
		t.Fatalf("begin claimed network run: %v", err)
	}
	return run
}
