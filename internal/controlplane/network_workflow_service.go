package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"openagentx/internal/api"
	"openagentx/internal/domain"
	"openagentx/internal/network/secretstore"
)

type NetworkWorkflowState interface {
	ExecuteNetworkCommand(context.Context, domain.NetworkCommandMutation) (*domain.NetworkCommandReceipt, error)
	GetNetworkCommandReceipt(context.Context, string, string, string, string) (*domain.NetworkCommandReceipt, error)
	GetNetworkProfileHead(context.Context, string) (*domain.NetworkProfileHead, error)
	GetNetworkProfileContent(context.Context, string, int64) (*domain.NetworkProfileContent, error)
	ListNetworkProfileContents(context.Context, int) ([]domain.NetworkProfileContent, error)
	ListNetworkProfileHeads(context.Context, int) ([]domain.NetworkProfileHead, error)
	GetNetworkPublication(context.Context, string, int64) (*domain.NetworkPublication, error)
	ListNetworkPublications(context.Context, int) ([]domain.NetworkPublication, error)
	GetNetworkTest(context.Context, string) (*domain.NetworkTest, error)
	ListNetworkTests(context.Context, string, int) ([]domain.NetworkTest, error)
	GetWorkerRuntimeTarget(context.Context, string, string, domain.NetworkMode) (domain.WorkerInstance, domain.RuntimeIdentity, error)
	NextNetworkModePolicyVersion(context.Context, string, string) (int64, error)
	GetNetworkModePolicy(context.Context, string, string, int64) (*domain.NetworkModePolicy, error)
	GetNetworkModeTest(context.Context, string) (*domain.NetworkModeTest, error)
	ListNetworkModeTests(context.Context, int) ([]domain.NetworkModeTest, error)
	GetNetworkBinding(context.Context, string, string) (*domain.NetworkBinding, error)
	ListNetworkBindings(context.Context, string) ([]domain.NetworkBinding, error)
	ListActiveNetworkRuns(context.Context, string, int) ([]domain.NetworkActiveRunSnapshot, error)
	NetworkSecretReferenced(context.Context, string) (bool, error)
	ClaimNetworkWork(context.Context, domain.WorkerWriteGuard) (*domain.NetworkWork, error)
	GetNetworkWorkForAck(context.Context, domain.WorkerWriteGuard, string) (*domain.NetworkWork, error)
	AcknowledgeNetworkWork(context.Context, domain.WorkerWriteGuard, string, string, string, int64, []domain.NetworkProbeResult, *domain.NetworkPolicy, *domain.NetworkProfileContent, string, *domain.JournalEvent) error
}

func (s *NetworkWorkflowService) PullWork(ctx context.Context, guard domain.WorkerWriteGuard) (*api.NetworkWorkEnvelope, error) {
	// Reconciliation is maintenance. Uncertain reference checks preserve files
	// and are retried on the next pull without taking the Worker loop offline.
	_ = s.secrets.ReconcileOrphans(ctx, s.state.NetworkSecretReferenced)
	work, err := s.state.ClaimNetworkWork(ctx, guard)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	envelope := &api.NetworkWorkEnvelope{Work: *work}
	if work.SecretVersion != "" {
		payload, getErr := s.secrets.GetReferenced(ctx, work.SecretVersion, s.state.NetworkSecretReferenced)
		if getErr != nil {
			return envelope, nil
		}
		var secret api.NetworkSecretPayload
		if err := json.Unmarshal(payload, &secret); err != nil {
			return envelope, nil
		}
		envelope.Secret = &secret
	}
	return envelope, nil
}

func (s *NetworkWorkflowService) AcknowledgeWork(ctx context.Context, guard domain.WorkerWriteGuard, workID string, req api.NetworkWorkAckRequest) error {
	var imported *domain.NetworkProfileContent
	var sourceIdentity string
	var secretVersion string
	var secretPayload []byte
	work, err := s.state.GetNetworkWorkForAck(ctx, guard, workID)
	if err != nil {
		return err
	}
	if work.State == req.State {
		return nil
	}
	if work.State != "claimed" {
		return domain.ErrStaleVersion
	}
	if work.Kind == domain.NetworkWorkTest {
		if err := domain.ValidateNetworkProbeResults(req.State, req.ProbeResults); err != nil {
			return err
		}
	} else if len(req.ProbeResults) != 0 {
		return domain.ErrInvalidInput("probe results require network test work")
	}
	if work.Kind == domain.NetworkWorkImport {
		if req.State == "succeeded" && req.Imported == nil {
			return domain.ErrInvalidInput("successful network import requires parsed content")
		}
		if req.Imported != nil {
			if err := req.Imported.Validate(); err != nil {
				return err
			}
			sourceIdentity = req.Imported.SourceIdentity
			if req.Imported.Secret != nil {
				secretVersion = "secret-" + uuid.NewSHA1(uuid.NameSpaceURL, []byte("openagentx:network-import:"+work.ID+":"+sourceIdentity)).String()
				secretPayload = mustJSON(req.Imported.Secret)
			}
			content := &domain.NetworkProfileContent{
				ProfileID: work.ProfileID, ContentVersion: 1, Mode: req.Imported.Mode,
				Host: req.Imported.Host, Port: req.Imported.Port,
				DirectIPs: append([]string(nil), req.Imported.DirectIPs...), SecretVersion: secretVersion,
				CreatedBy: guard.PrincipalID, CreatedAt: guard.CheckedAt,
			}
			content.ManifestDigest = content.ComputeManifestDigest()
			if err := content.Validate(); err != nil {
				return err
			}
			imported = content
		}
	} else if req.Imported != nil {
		return domain.ErrInvalidInput("non-import network work cannot include imported content")
	}
	eventType := "network.work_" + req.State
	event := networkEvent(workID, eventType, guard.PrincipalID, "network_work", map[string]any{"worker_instance_id": guard.WorkerInstanceID, "generation": guard.Generation, "state": req.State, "diagnostic_code": req.DiagnosticCode, "duration_ms": req.DurationMS}, guard.CheckedAt)
	commit := func() error {
		return s.state.AcknowledgeNetworkWork(ctx, guard, workID, req.State, req.DiagnosticCode, req.DurationMS, req.ProbeResults, req.Policy, imported, sourceIdentity, event)
	}
	if secretVersion != "" {
		return s.secrets.CommitImmutable(ctx, secretVersion, secretPayload, commit, s.state.NetworkSecretReferenced)
	}
	return commit()
}

type NetworkWorkflowService struct {
	state   NetworkWorkflowState
	secrets secretstore.Store
	broker  WakeupBroker
	now     func() time.Time
}

func NewNetworkWorkflowService(state NetworkWorkflowState, secrets secretstore.Store, broker WakeupBroker, now func() time.Time) (*NetworkWorkflowService, error) {
	if state == nil || secrets == nil {
		return nil, fmt.Errorf("network workflow state and secret store are required")
	}
	if broker == nil {
		broker = NewMemoryWakeupBroker()
	}
	if now == nil {
		now = time.Now
	}
	// Startup reconciliation is conservative: any uncertain reference check
	// leaves the file in place, and the next worker pull retries the scan.
	_ = secrets.ReconcileOrphans(context.Background(), state.NetworkSecretReferenced)
	return &NetworkWorkflowService{state: state, secrets: secrets, broker: broker, now: now}, nil
}

func (s *NetworkWorkflowService) List(ctx context.Context) ([]domain.NetworkProfileHead, []domain.NetworkTest, error) {
	h, err := s.state.ListNetworkProfileHeads(ctx, 200)
	if err != nil {
		return nil, nil, err
	}
	t, err := s.state.ListNetworkTests(ctx, "", 100)
	return h, t, err
}

func (s *NetworkWorkflowService) Observe(ctx context.Context, agentID string) (*api.NetworkOverviewResponse, error) {
	heads, err := s.state.ListNetworkProfileHeads(ctx, 200)
	if err != nil {
		return nil, err
	}
	contents, err := s.state.ListNetworkProfileContents(ctx, 1000)
	if err != nil {
		return nil, err
	}
	tests, err := s.state.ListNetworkTests(ctx, "", 100)
	if err != nil {
		return nil, err
	}
	modeTests, err := s.state.ListNetworkModeTests(ctx, 100)
	if err != nil {
		return nil, err
	}
	bindings, err := s.state.ListNetworkBindings(ctx, agentID)
	if err != nil {
		return nil, err
	}
	activeRuns, err := s.state.ListActiveNetworkRuns(ctx, agentID, 200)
	if err != nil {
		return nil, err
	}
	publications, err := s.state.ListNetworkPublications(ctx, 1000)
	if err != nil {
		return nil, err
	}
	published := make(map[string]domain.NetworkPublication, len(publications))
	for _, publication := range publications {
		published[fmt.Sprintf("%s:%d", publication.ProfileID, publication.ContentVersion)] = publication
	}
	current := make(map[string]domain.NetworkProfileHead, len(heads))
	response := &api.NetworkOverviewResponse{
		Profiles: make([]api.NetworkProfileSummary, 0, len(heads)),
		Versions: make([]api.NetworkProfileVersionSummary, 0, len(contents)),
		Tests:    make([]api.NetworkTestSummary, 0, len(tests)), ModeTests: modeTests, Bindings: bindings, ActiveRuns: activeRuns,
	}
	for _, head := range heads {
		current[head.ProfileID] = head
		if head.Content == nil {
			continue
		}
		response.Profiles = append(response.Profiles, api.NetworkProfileSummary{
			ProfileID: head.ProfileID, CurrentContentVersion: head.CurrentContentVersion,
			State: head.State, StateRevision: head.StateRevision, ReadyTestID: head.ReadyTestID,
			PublishedContentVersion: head.PublishedContentVersion, UpdatedAt: head.UpdatedAt,
			Mode: head.Content.Mode, Host: head.Content.Host, Port: head.Content.Port,
			DirectIPs: append([]string(nil), head.Content.DirectIPs...), SecretPresent: head.Content.SecretVersion != "",
			ManifestDigest: head.Content.ManifestDigest,
		})
	}
	for _, content := range contents {
		key := fmt.Sprintf("%s:%d", content.ProfileID, content.ContentVersion)
		publication, wasPublished := published[key]
		head := current[content.ProfileID]
		version := api.NetworkProfileVersionSummary{
			ProfileID: content.ProfileID, ContentVersion: content.ContentVersion,
			Mode: content.Mode, Host: content.Host, Port: content.Port,
			DirectIPs: append([]string(nil), content.DirectIPs...), ManifestDigest: content.ManifestDigest,
			CreatedBy: content.CreatedBy, CreatedAt: content.CreatedAt, SecretPresent: content.SecretVersion != "",
			Published:        wasPublished,
			CurrentPublished: wasPublished && head.State == domain.NetworkStatePublished && head.CurrentContentVersion == content.ContentVersion,
		}
		if wasPublished {
			publishedAt := publication.PublishedAt
			version.PublishedAt = &publishedAt
		}
		response.Versions = append(response.Versions, version)
	}
	for _, test := range tests {
		response.Tests = append(response.Tests, api.NetworkTestSummary{
			ID: test.ID, ProfileID: test.ProfileID, ContentVersion: test.ContentVersion,
			WorkerInstanceID: test.WorkerInstanceID, Generation: test.Generation,
			BackendID: test.BackendID, RuntimeIdentity: test.RuntimeIdentity,
			State: test.State, DiagnosticCode: test.DiagnosticCode, DurationMS: test.DurationMS, ProbeResults: test.ProbeResults,
			CreatedBy: test.CreatedBy, CreatedAt: test.CreatedAt, FinishedAt: test.FinishedAt,
		})
	}
	for i := range response.Bindings {
		if response.Bindings[i].Profile != nil {
			profile := *response.Bindings[i].Profile
			profile.SecretRef = ""
			profile.ConfigFile = ""
			response.Bindings[i].Profile = &profile
		}
	}
	return response, nil
}

func (s *NetworkWorkflowService) StartModeTest(ctx context.Context, actor string, req api.TestNetworkModeRequest) (json.RawMessage, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	digest := standardDigest(req)
	if replay, found, err := s.replay(ctx, "mode_test", actor, req.Meta.IdempotencyKey, digest); found || err != nil {
		return replay, err
	}
	worker, identity, err := s.state.GetWorkerRuntimeTarget(ctx, req.WorkerInstanceID, req.BackendID, req.Mode)
	if err != nil {
		return nil, err
	}
	if worker.AgentID != req.AgentID || worker.Generation != req.Generation || worker.Status == domain.WorkerStatusOffline {
		return nil, domain.ErrStaleVersion
	}
	if binding, getErr := s.state.GetNetworkBinding(ctx, req.AgentID, req.BackendID); getErr == nil {
		if binding.Version != req.Meta.ExpectedVersion {
			return nil, domain.ErrStaleVersion
		}
	} else if !errors.Is(getErr, domain.ErrNotFound) {
		return nil, getErr
	} else if req.Meta.ExpectedVersion != 0 {
		return nil, domain.ErrStaleVersion
	}
	policyVersion, err := s.state.NextNetworkModePolicyVersion(ctx, req.AgentID, req.BackendID)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	policy := &domain.NetworkModePolicy{AgentID: req.AgentID, BackendID: req.BackendID, PolicyVersion: policyVersion, Mode: req.Mode, CreatedBy: actor, CreatedAt: now}
	policy.ManifestDigest = policy.ComputeManifestDigest()
	id := "network-mode-test-" + uuid.NewString()
	test := &domain.NetworkModeTest{ID: id, AgentID: req.AgentID, BackendID: req.BackendID, PolicyVersion: policyVersion, Mode: req.Mode, ManifestDigest: policy.ManifestDigest, WorkerInstanceID: worker.ID, Generation: worker.Generation, RuntimeIdentity: identity, BindingRevision: req.Meta.ExpectedVersion, State: "pending", CreatedBy: actor, CreatedAt: now}
	work := &domain.NetworkWork{ID: id, Kind: domain.NetworkWorkTest, AgentID: req.AgentID, BackendID: req.BackendID, WorkerInstanceID: worker.ID, Generation: worker.Generation, BindingRevision: req.Meta.ExpectedVersion, Mode: req.Mode, PolicyVersion: policyVersion, ManifestDigest: policy.ManifestDigest, RuntimeIdentity: identity, State: "pending", CreatedAt: now}
	result := mustJSON(map[string]any{"test_id": id, "agent_id": req.AgentID, "backend_id": req.BackendID, "mode": req.Mode, "policy_version": policyVersion, "manifest_digest": policy.ManifestDigest, "state": "pending", "binding_revision": req.Meta.ExpectedVersion})
	mutation := domain.NetworkCommandMutation{ModePolicy: policy, ModeTest: test, Work: work, CheckBindingRevision: true, BindingAgentID: req.AgentID, BindingBackendID: req.BackendID, ExpectedBindingRevision: req.Meta.ExpectedVersion, Event: networkEvent(id, "network.mode_test_requested", actor, "network_mode_test", map[string]any{"agent_id": req.AgentID, "backend_id": req.BackendID, "mode": req.Mode, "policy_version": policyVersion, "worker_instance_id": worker.ID, "generation": worker.Generation, "binding_revision": req.Meta.ExpectedVersion}, now)}
	out, err := s.execute(ctx, "mode_test", actor, req.Meta.IdempotencyKey, digest, result, mutation)
	if err == nil {
		s.broker.Publish(WorkerControlTopic(worker.ID))
	}
	return out, err
}

func (s *NetworkWorkflowService) PublishMode(ctx context.Context, actor string, req api.PublishNetworkModeRequest) (json.RawMessage, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	digest := standardDigest(req)
	if replay, found, err := s.replay(ctx, "mode_publish", actor, req.Meta.IdempotencyKey, digest); found || err != nil {
		return replay, err
	}
	test, err := s.state.GetNetworkModeTest(ctx, req.TestID)
	if err != nil {
		return nil, err
	}
	if test.State != "succeeded" || test.BindingRevision != req.Meta.ExpectedVersion ||
		test.WorkerInstanceID != req.WorkerInstanceID || test.Generation != req.Generation {
		return nil, domain.ErrStaleVersion
	}
	policy, err := s.state.GetNetworkModePolicy(ctx, test.AgentID, test.BackendID, test.PolicyVersion)
	if err != nil {
		return nil, err
	}
	if policy.Mode != test.Mode || policy.ManifestDigest != test.ManifestDigest {
		return nil, domain.ErrStaleVersion
	}
	worker, identity, err := s.state.GetWorkerRuntimeTarget(ctx, req.WorkerInstanceID, test.BackendID, test.Mode)
	if err != nil {
		return nil, err
	}
	if worker.AgentID != test.AgentID || worker.Generation != req.Generation || worker.Status == domain.WorkerStatusOffline || identity != test.RuntimeIdentity {
		return nil, domain.ErrStaleVersion
	}
	now := s.now().UTC()
	revision := req.Meta.ExpectedVersion + 1
	binding := &domain.NetworkBinding{AgentID: test.AgentID, BackendID: test.BackendID, Mode: test.Mode, PolicyVersion: test.PolicyVersion, TestID: test.ID, ManifestDigest: test.ManifestDigest, RuntimeIdentity: identity, Version: revision, DesiredStatus: "pending", UpdatedAt: now}
	work := &domain.NetworkWork{ID: "network-mode-apply-" + uuid.NewString(), Kind: domain.NetworkWorkApply, AgentID: test.AgentID, BackendID: test.BackendID, WorkerInstanceID: worker.ID, Generation: worker.Generation, BindingRevision: revision, Mode: test.Mode, PolicyVersion: test.PolicyVersion, ManifestDigest: test.ManifestDigest, RuntimeIdentity: identity, State: "pending", CreatedAt: now}
	result := mustJSON(map[string]any{"agent_id": test.AgentID, "backend_id": test.BackendID, "mode": test.Mode, "policy_version": test.PolicyVersion, "binding_revision": revision, "state": "pending", "work_id": work.ID})
	mutation := domain.NetworkCommandMutation{Binding: binding, ExpectedBindingRevision: req.Meta.ExpectedVersion, Work: work, Event: networkEvent(test.AgentID+":"+test.BackendID, "network.mode_binding_pending", actor, "network_binding", map[string]any{"mode": test.Mode, "policy_version": test.PolicyVersion, "test_id": test.ID, "binding_revision": revision, "worker_instance_id": worker.ID, "generation": worker.Generation}, now)}
	out, err := s.execute(ctx, "mode_publish", actor, req.Meta.IdempotencyKey, digest, result, mutation)
	if err == nil {
		s.broker.Publish(WorkerControlTopic(worker.ID))
	}
	return out, err
}

func (s *NetworkWorkflowService) CreateDraft(ctx context.Context, actor string, req api.CreateNetworkProfileRequest) (json.RawMessage, error) {
	p, err := req.Validate(actor)
	if err != nil {
		return nil, err
	}
	digest := standardDigest(req)
	if replay, found, err := s.replay(ctx, "create", actor, req.Meta.IdempotencyKey, digest); found || err != nil {
		return replay, err
	}
	now := s.now().UTC()
	c := &domain.NetworkProfileContent{ProfileID: p.ID, ContentVersion: 1, Mode: p.Mode, Host: p.Host, Port: p.Port, DirectIPs: append([]string(nil), p.DirectIPs...), CreatedBy: actor, CreatedAt: now}
	c.ManifestDigest = c.ComputeManifestDigest()
	h := &domain.NetworkProfileHead{ProfileID: p.ID, CurrentContentVersion: 1, State: domain.NetworkStateDraft, StateRevision: 1, UpdatedAt: now}
	result := mustJSON(map[string]any{"profile_id": p.ID, "content_version": int64(1), "state_revision": int64(1), "state": h.State})
	return s.execute(ctx, "create", actor, req.Meta.IdempotencyKey, digest, result, domain.NetworkCommandMutation{Content: c, Head: h, ExpectedStateRevision: 0, Event: networkEvent("profile-"+p.ID, "network.profile_created", actor, "network_profile", map[string]any{"profile_id": p.ID, "content_version": 1}, now)})
}

func (s *NetworkWorkflowService) EditDraft(ctx context.Context, actor, profileID string, req api.EditNetworkProfileRequest) (json.RawMessage, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	digest := standardDigest(struct {
		Profile string
		Request api.EditNetworkProfileRequest
	}{profileID, req})
	if replay, found, err := s.replay(ctx, "edit", actor, req.Meta.IdempotencyKey, digest); found || err != nil {
		return replay, err
	}
	h, err := s.requireHead(ctx, profileID, req.Meta.ExpectedVersion)
	if err != nil {
		return nil, err
	}
	if h.State == domain.NetworkStateTesting {
		return nil, domain.ErrConflict("network profile test is in progress")
	}
	now := s.now().UTC()
	c := &domain.NetworkProfileContent{ProfileID: profileID, ContentVersion: h.CurrentContentVersion + 1, Mode: req.Mode, Host: req.Host, Port: req.Port, DirectIPs: append([]string(nil), req.DirectIPs...), SecretVersion: h.Content.SecretVersion, CreatedBy: actor, CreatedAt: now}
	c.ManifestDigest = c.ComputeManifestDigest()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	next := &domain.NetworkProfileHead{ProfileID: profileID, CurrentContentVersion: c.ContentVersion, State: domain.NetworkStateDraft, StateRevision: h.StateRevision + 1, UpdatedAt: now}
	result := mustJSON(map[string]any{"profile_id": profileID, "content_version": c.ContentVersion, "state_revision": next.StateRevision, "state": next.State})
	return s.execute(ctx, "edit", actor, req.Meta.IdempotencyKey, digest, result, domain.NetworkCommandMutation{Content: c, Head: next, ExpectedStateRevision: h.StateRevision, Event: networkEvent("profile-"+profileID, "network.profile_edited", actor, "network_profile", map[string]any{"content_version": c.ContentVersion}, now)})
}

func (s *NetworkWorkflowService) ReplaceSecret(ctx context.Context, actor, profileID string, req api.ReplaceNetworkSecretRequest) (json.RawMessage, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	digest := s.secrets.Fingerprint(mustJSON(struct {
		Profile string
		Request api.ReplaceNetworkSecretRequest
	}{profileID, req}))
	if replay, found, err := s.replay(ctx, "replace_secret", actor, req.Meta.IdempotencyKey, digest); found || err != nil {
		return replay, err
	}
	h, err := s.requireHead(ctx, profileID, req.Meta.ExpectedVersion)
	if err != nil {
		return nil, err
	}
	if h.Content.Mode == "only_http_proxy" && (req.Username != "" || req.Password != "") {
		return nil, domain.ErrUnsupportedCapability
	}
	payload := mustJSON(api.NetworkSecretPayload{Username: req.Username, Password: req.Password})
	version := "secret-" + uuid.NewSHA1(uuid.NameSpaceURL, []byte("openagentx:network-secret:"+actor+":"+req.Meta.IdempotencyKey)).String()
	now := s.now().UTC()
	c := *h.Content
	c.ContentVersion++
	c.SecretVersion = version
	c.CreatedBy = actor
	c.CreatedAt = now
	c.LegacySecretStale = false
	c.ManifestDigest = c.ComputeManifestDigest()
	next := &domain.NetworkProfileHead{ProfileID: profileID, CurrentContentVersion: c.ContentVersion, State: domain.NetworkStateDraft, StateRevision: h.StateRevision + 1, UpdatedAt: now}
	result := mustJSON(map[string]any{"profile_id": profileID, "content_version": c.ContentVersion, "state_revision": next.StateRevision, "secret_version": version, "secret_present": true})
	mutation := domain.NetworkCommandMutation{Content: &c, Head: next, ExpectedStateRevision: h.StateRevision, Event: networkEvent("profile-"+profileID, "network.secret_replaced", actor, "network_profile", map[string]any{"content_version": c.ContentVersion, "secret_version": version}, now)}
	var out json.RawMessage
	commit := func() error {
		var commitErr error
		out, commitErr = s.execute(ctx, "replace_secret", actor, req.Meta.IdempotencyKey, digest, result, mutation)
		return commitErr
	}
	if err := s.secrets.CommitImmutable(ctx, version, payload, commit, s.state.NetworkSecretReferenced); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *NetworkWorkflowService) StartTest(ctx context.Context, actor, profileID string, req api.TestNetworkProfileRequest) (json.RawMessage, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	digest := standardDigest(struct {
		Profile string
		Request api.TestNetworkProfileRequest
	}{profileID, req})
	if replay, found, err := s.replay(ctx, "test", actor, req.Meta.IdempotencyKey, digest); found || err != nil {
		return replay, err
	}
	h, err := s.requireHead(ctx, profileID, req.Meta.ExpectedVersion)
	if err != nil {
		return nil, err
	}
	worker, identity, err := s.state.GetWorkerRuntimeTarget(ctx, req.WorkerInstanceID, req.BackendID, domain.NetworkNamedProfile)
	if err != nil {
		return nil, err
	}
	if worker.Generation != req.Generation || worker.Status == domain.WorkerStatusOffline {
		return nil, domain.ErrStaleVersion
	}
	if identity.IsZero() {
		return nil, domain.ErrUnsupportedCapability
	}
	now := s.now().UTC()
	id := "network-test-" + uuid.NewString()
	test := &domain.NetworkTest{ID: id, ProfileID: profileID, ContentVersion: h.CurrentContentVersion, SecretVersion: h.Content.SecretVersion, WorkerInstanceID: worker.ID, Generation: worker.Generation, BackendID: req.BackendID, RuntimeIdentity: identity, State: "pending", CreatedBy: actor, CreatedAt: now}
	work := &domain.NetworkWork{ID: id, Kind: domain.NetworkWorkTest, ProfileID: profileID, ContentVersion: h.CurrentContentVersion, SecretVersion: h.Content.SecretVersion, AgentID: worker.AgentID, BackendID: req.BackendID, WorkerInstanceID: worker.ID, Generation: worker.Generation, Mode: domain.NetworkNamedProfile, ManifestDigest: h.Content.ManifestDigest, RuntimeIdentity: identity, State: "pending", CreatedAt: now}
	next := &domain.NetworkProfileHead{ProfileID: profileID, CurrentContentVersion: h.CurrentContentVersion, State: domain.NetworkStateTesting, StateRevision: h.StateRevision + 1, UpdatedAt: now}
	result := mustJSON(map[string]any{"test_id": id, "state": "pending", "state_revision": next.StateRevision})
	out, err := s.execute(ctx, "test", actor, req.Meta.IdempotencyKey, digest, result, domain.NetworkCommandMutation{Head: next, ExpectedStateRevision: h.StateRevision, Test: test, Work: work, Event: networkEvent(id, "network.test_requested", actor, "network_test", map[string]any{"profile_id": profileID, "content_version": h.CurrentContentVersion, "worker_instance_id": worker.ID, "generation": worker.Generation, "backend_id": req.BackendID}, now)})
	if err == nil {
		s.broker.Publish(WorkerControlTopic(worker.ID))
	}
	return out, err
}

func (s *NetworkWorkflowService) Publish(ctx context.Context, actor, profileID string, req api.PublishNetworkProfileRequest) (json.RawMessage, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	digest := standardDigest(struct {
		Profile string
		Request api.PublishNetworkProfileRequest
	}{profileID, req})
	if replay, found, err := s.replay(ctx, "publish", actor, req.Meta.IdempotencyKey, digest); found || err != nil {
		return replay, err
	}
	h, err := s.requireHead(ctx, profileID, req.Meta.ExpectedVersion)
	if err != nil {
		return nil, err
	}
	if h.State != domain.NetworkStateReady || h.ReadyTestID == "" {
		return nil, domain.ErrConflict("network profile is not ready")
	}
	test, err := s.state.GetNetworkTest(ctx, h.ReadyTestID)
	if err != nil {
		return nil, err
	}
	if test.State != "succeeded" || test.ContentVersion != h.CurrentContentVersion || test.SecretVersion != h.Content.SecretVersion {
		return nil, domain.ErrStaleVersion
	}
	now := s.now().UTC()
	next := &domain.NetworkProfileHead{ProfileID: profileID, CurrentContentVersion: h.CurrentContentVersion, State: domain.NetworkStatePublished, StateRevision: h.StateRevision + 1, ReadyTestID: h.ReadyTestID, PublishedContentVersion: h.CurrentContentVersion, UpdatedAt: now}
	publication := &domain.NetworkPublication{ProfileID: profileID, ContentVersion: h.CurrentContentVersion, TestID: test.ID, RuntimeIdentity: test.RuntimeIdentity, PublishedBy: actor, PublishedAt: now}
	result := mustJSON(map[string]any{"profile_id": profileID, "content_version": h.CurrentContentVersion, "state_revision": next.StateRevision, "state": next.State})
	return s.execute(ctx, "publish", actor, req.Meta.IdempotencyKey, digest, result, domain.NetworkCommandMutation{Head: next, ExpectedStateRevision: h.StateRevision, Publication: publication, Event: networkEvent("profile-"+profileID, "network.profile_published", actor, "network_profile", map[string]any{"content_version": h.CurrentContentVersion, "test_id": h.ReadyTestID, "manifest_digest": h.Content.ManifestDigest, "secret_version": h.Content.SecretVersion, "runtime_identity": test.RuntimeIdentity}, now)})
}

func (s *NetworkWorkflowService) Bind(ctx context.Context, actor string, req api.BindNetworkProfileRequest) (json.RawMessage, error) {
	if _, err := req.Validate(s.now()); err != nil {
		return nil, err
	}
	digest := standardDigest(req)
	if replay, found, err := s.replay(ctx, "bind", actor, req.Meta.IdempotencyKey, digest); found || err != nil {
		return replay, err
	}
	h, err := s.state.GetNetworkProfileHead(ctx, req.ProfileID)
	if err != nil {
		return nil, err
	}
	if h.State != domain.NetworkStatePublished || h.PublishedContentVersion != req.ProfileVersion {
		return nil, domain.ErrConflict("profile content is not published")
	}
	test, err := s.state.GetNetworkTest(ctx, h.ReadyTestID)
	if err != nil {
		return nil, err
	}
	worker, identity, err := s.state.GetWorkerRuntimeTarget(ctx, req.WorkerInstanceID, req.BackendID, domain.NetworkNamedProfile)
	if err != nil {
		return nil, err
	}
	if worker.AgentID != req.AgentID || worker.Generation != req.Generation || test.WorkerInstanceID != worker.ID || test.Generation != worker.Generation || test.BackendID != req.BackendID || test.RuntimeIdentity != identity {
		return nil, domain.ErrStaleVersion
	}
	now := s.now().UTC()
	profile := &domain.ProxyProfile{ID: req.ProfileID, Version: req.ProfileVersion, Status: domain.NetworkProfilePublished, Mode: h.Content.Mode, Host: h.Content.Host, Port: h.Content.Port, SecretRef: h.Content.SecretVersion, DirectIPs: h.Content.DirectIPs, ManifestDigest: h.Content.ManifestDigest, RuntimeIdentity: identity, CreatedBy: h.Content.CreatedBy, CreatedAt: h.Content.CreatedAt, UpdatedAt: now}
	binding := &domain.NetworkBinding{AgentID: req.AgentID, BackendID: req.BackendID, Mode: domain.NetworkNamedProfile, ProfileID: req.ProfileID, ProfileVersion: req.ProfileVersion, ManifestDigest: h.Content.ManifestDigest, RuntimeIdentity: identity, DesiredStatus: "pending", UpdatedAt: now, Profile: profile}
	revision := req.Meta.ExpectedVersion + 1
	if req.Meta.ExpectedVersion == 0 {
		revision = 1
	}
	binding.Version = revision
	work := &domain.NetworkWork{ID: "network-apply-" + uuid.NewString(), Kind: domain.NetworkWorkApply, ProfileID: req.ProfileID, ContentVersion: req.ProfileVersion, SecretVersion: h.Content.SecretVersion, AgentID: req.AgentID, BackendID: req.BackendID, WorkerInstanceID: worker.ID, Generation: worker.Generation, BindingRevision: revision, Mode: domain.NetworkNamedProfile, ManifestDigest: h.Content.ManifestDigest, RuntimeIdentity: identity, State: "pending", CreatedAt: now}
	result := mustJSON(map[string]any{"agent_id": req.AgentID, "backend_id": req.BackendID, "binding_revision": revision, "state": "pending", "work_id": work.ID})
	out, err := s.execute(ctx, "bind", actor, req.Meta.IdempotencyKey, digest, result, domain.NetworkCommandMutation{Binding: binding, ExpectedBindingRevision: req.Meta.ExpectedVersion, Work: work, Event: networkEvent(req.AgentID+":"+req.BackendID, "network.binding_pending", actor, "network_binding", map[string]any{"profile_id": req.ProfileID, "content_version": req.ProfileVersion, "binding_revision": revision, "worker_instance_id": worker.ID, "generation": worker.Generation}, now)})
	if err == nil {
		s.broker.Publish(WorkerControlTopic(worker.ID))
	}
	return out, err
}

func (s *NetworkWorkflowService) Rollback(ctx context.Context, actor string, req api.RollbackNetworkBindingRequest) (json.RawMessage, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	digest := standardDigest(req)
	if replay, found, err := s.replay(ctx, "rollback", actor, req.Meta.IdempotencyKey, digest); found || err != nil {
		return replay, err
	}
	binding, err := s.state.GetNetworkBinding(ctx, req.AgentID, req.BackendID)
	if err != nil {
		return nil, err
	}
	if binding.Version != req.Meta.ExpectedVersion {
		return nil, domain.ErrStaleVersion
	}
	source, err := s.state.GetNetworkProfileContent(ctx, binding.ProfileID, req.TargetContentVersion)
	if err != nil {
		return nil, err
	}
	if _, err := s.state.GetNetworkPublication(ctx, binding.ProfileID, req.TargetContentVersion); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrConflict("rollback target was never published")
		}
		return nil, err
	}
	head, err := s.state.GetNetworkProfileHead(ctx, binding.ProfileID)
	if err != nil {
		return nil, err
	}
	worker, identity, err := s.state.GetWorkerRuntimeTarget(ctx, req.WorkerInstanceID, req.BackendID, domain.NetworkNamedProfile)
	if err != nil {
		return nil, err
	}
	if worker.AgentID != req.AgentID || worker.Generation != req.Generation {
		return nil, domain.ErrStaleVersion
	}
	now := s.now().UTC()
	copy := *source
	copy.ContentVersion = head.CurrentContentVersion + 1
	copy.CreatedBy = actor
	copy.CreatedAt = now
	copy.ManifestDigest = copy.ComputeManifestDigest()
	id := "network-test-" + uuid.NewString()
	next := &domain.NetworkProfileHead{ProfileID: copy.ProfileID, CurrentContentVersion: copy.ContentVersion, State: domain.NetworkStateTesting, StateRevision: head.StateRevision + 1, UpdatedAt: now}
	test := &domain.NetworkTest{ID: id, ProfileID: copy.ProfileID, ContentVersion: copy.ContentVersion, SecretVersion: copy.SecretVersion, WorkerInstanceID: worker.ID, Generation: worker.Generation, BackendID: req.BackendID, RuntimeIdentity: identity, State: "pending", CreatedBy: actor, CreatedAt: now}
	work := &domain.NetworkWork{ID: id, Kind: domain.NetworkWorkTest, ProfileID: copy.ProfileID, ContentVersion: copy.ContentVersion, SecretVersion: copy.SecretVersion, AgentID: req.AgentID, BackendID: req.BackendID, WorkerInstanceID: worker.ID, Generation: worker.Generation, Mode: domain.NetworkNamedProfile, ManifestDigest: copy.ManifestDigest, RuntimeIdentity: identity, State: "pending", CreatedAt: now}
	result := mustJSON(map[string]any{"profile_id": copy.ProfileID, "content_version": copy.ContentVersion, "state_revision": next.StateRevision, "test_id": id, "rollback_of": req.TargetContentVersion})
	out, err := s.execute(ctx, "rollback", actor, req.Meta.IdempotencyKey, digest, result, domain.NetworkCommandMutation{Content: &copy, Head: next, ExpectedStateRevision: head.StateRevision, Test: test, Work: work, Event: networkEvent("profile-"+copy.ProfileID, "network.rollback_created", actor, "network_profile", map[string]any{"content_version": copy.ContentVersion, "source_content_version": req.TargetContentVersion, "test_id": id}, now)})
	if err == nil {
		s.broker.Publish(WorkerControlTopic(worker.ID))
	}
	return out, err
}

func (s *NetworkWorkflowService) StartImport(ctx context.Context, actor string, req api.ImportNetworkProfileRequest) (json.RawMessage, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	digest := standardDigest(req)
	if replay, found, err := s.replay(ctx, "import", actor, req.Meta.IdempotencyKey, digest); found || err != nil {
		return replay, err
	}
	worker, identity, err := s.state.GetWorkerRuntimeTarget(ctx, req.WorkerInstanceID, req.BackendID, domain.NetworkNamedProfile)
	if err != nil {
		return nil, err
	}
	if worker.Generation != req.Generation {
		return nil, domain.ErrStaleVersion
	}
	now := s.now().UTC()
	id := "network-import-" + uuid.NewString()
	source := standardDigest(struct{ WorkID string }{id})
	work := &domain.NetworkWork{ID: id, Kind: domain.NetworkWorkImport, ProfileID: req.ProfileID, AgentID: worker.AgentID, BackendID: req.BackendID, WorkerInstanceID: worker.ID, Generation: worker.Generation, RuntimeIdentity: identity, State: "pending", CreatedAt: now}
	imp := &domain.NetworkImport{WorkerInstanceID: worker.ID, Generation: worker.Generation, BackendID: req.BackendID, SourceIdentity: source, WorkID: id, State: "pending", CreatedAt: now}
	result := mustJSON(map[string]any{"import_id": id, "state": "pending", "source_identity": source})
	out, err := s.execute(ctx, "import", actor, req.Meta.IdempotencyKey, digest, result, domain.NetworkCommandMutation{Work: work, Import: imp, Event: networkEvent(id, "network.import_requested", actor, "network_import", map[string]any{"worker_instance_id": worker.ID, "generation": worker.Generation, "backend_id": req.BackendID, "profile_id": req.ProfileID, "source_identity": source}, now)})
	if err == nil {
		s.broker.Publish(WorkerControlTopic(worker.ID))
	}
	return out, err
}

func (s *NetworkWorkflowService) requireHead(ctx context.Context, profileID string, expected int64) (*domain.NetworkProfileHead, error) {
	h, err := s.state.GetNetworkProfileHead(ctx, profileID)
	if err != nil {
		return nil, err
	}
	if h.StateRevision != expected {
		return nil, domain.ErrStaleVersion
	}
	if h.Content == nil || h.Content.LegacySecretStale {
		return nil, domain.ErrConflict("legacy network profile requires explicit import")
	}
	return h, nil
}

func (s *NetworkWorkflowService) execute(ctx context.Context, op, actor, key, digest string, result json.RawMessage, m domain.NetworkCommandMutation) (json.RawMessage, error) {
	m.Receipt = domain.NetworkCommandReceipt{Actor: actor, Operation: op, IdempotencyKey: key, RequestDigest: digest, ResultJSON: result, CreatedAt: s.now().UTC()}
	receipt, err := s.state.ExecuteNetworkCommand(ctx, m)
	if err != nil {
		return nil, err
	}
	return append(json.RawMessage(nil), receipt.ResultJSON...), nil
}

func (s *NetworkWorkflowService) replay(ctx context.Context, op, actor, key, digest string) (json.RawMessage, bool, error) {
	receipt, err := s.state.GetNetworkCommandReceipt(ctx, actor, op, key, digest)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return append(json.RawMessage(nil), receipt.ResultJSON...), true, nil
}
func standardDigest(value any) string {
	b, _ := json.Marshal(value)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func mustJSON(value any) json.RawMessage { b, _ := json.Marshal(value); return b }
func networkEvent(id, typ, actor, aggregate string, payload any, now time.Time) *domain.JournalEvent {
	return &domain.JournalEvent{ID: "event-" + uuid.NewString(), AggregateType: aggregate, AggregateID: id, EventType: typ, ActorPrincipalID: actor, Payload: mustJSON(payload), CreatedAt: now}
}
