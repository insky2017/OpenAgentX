package controlplane

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"openagentx/internal/api"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

type WorkerState interface {
	ReconcileExpired(context.Context) error
	RegisterWorker(context.Context, domain.WorkerRegistration, []openruntime.BackendRegistration, *domain.JournalEvent) (*domain.WorkerInstance, error)
	GetWorkerCredential(context.Context, string) (*domain.WorkerCredential, error)
	HeartbeatWorker(context.Context, domain.WorkerWriteGuard, domain.WorkerStatus, map[string]openruntime.BackendHealth, time.Time, time.Time, *domain.JournalEvent) (*domain.WorkerInstance, error)
	TryClaimMailbox(context.Context, domain.WorkerWriteGuard, int, time.Time, *domain.JournalEvent) (*domain.MailboxItem, error)
	AcceptMailboxItem(context.Context, domain.WorkerWriteGuard, string, domain.MailboxState, domain.MailboxState, *domain.JournalEvent) error
	ResolveMailboxPayload(context.Context, domain.WorkerWriteGuard, string) (*domain.MailboxPayload, error)
	GetMailboxItem(context.Context, string) (*domain.MailboxItem, error)
	GetTask(context.Context, string) (*domain.Task, error)
	GetRunAttempt(context.Context, string) (*domain.RunAttempt, error)
	GetSessionBinding(context.Context, string, string, string) (*domain.SessionBinding, error)
	ListMessages(context.Context, string) ([]domain.Message, error)
	ListWorkerBackends(context.Context, string) ([]openruntime.BackendRegistration, error)
	BeginClaimedRunAttempt(context.Context, domain.WorkerWriteGuard, string, int64, *domain.RunAttempt, string, *domain.JournalEvent, *domain.JournalEvent, *domain.JournalEvent, *domain.JournalEvent) (*domain.Task, *domain.MailboxItem, error)
	AppendRunEvents(context.Context, domain.WorkerWriteGuard, string, int64, []*domain.JournalEvent) error
	FinishRun(context.Context, domain.WorkerWriteGuard, string, int64, int64, openruntime.TurnResult, *domain.SessionBinding, int64, *domain.JournalEvent, *domain.JournalEvent, *domain.JournalEvent) error
	ClaimWorkerCommand(context.Context, domain.WorkerWriteGuard, time.Time, *domain.JournalEvent) (*domain.WorkerCommand, error)
	AcknowledgeWorkerCommand(context.Context, domain.WorkerWriteGuard, string, domain.WorkerCommandState, string, *domain.JournalEvent) error
}

type TurnPlan struct {
	Execution            domain.ResolvedExecutionSpec
	SessionBinding       *domain.SessionBinding
	PreflightScopeDigest string
}

type TurnPlanner interface {
	Plan(context.Context, domain.Task, []domain.Message, []openruntime.BackendRegistration) (TurnPlan, error)
}

type WorkerServiceOptions struct {
	Now             func() time.Time
	NewID           func(prefix string) string
	NewSessionToken func() (string, error)
	WorkerLease     time.Duration
	TokenLifetime   time.Duration
	MailboxLease    time.Duration
	RunLease        time.Duration
	Planner         TurnPlanner
}

type WorkerService struct {
	state           WorkerState
	broker          WakeupBroker
	now             func() time.Time
	newID           func(string) string
	newSessionToken func() (string, error)
	workerLease     time.Duration
	tokenLifetime   time.Duration
	mailboxLease    time.Duration
	runLease        time.Duration
	planner         TurnPlanner
}

// Reconcile runs daemon-start recovery before Workers are allowed to claim
// new work. It is intentionally explicit so a daemon can fail startup when
// recovery cannot establish a consistent state.
func (s *WorkerService) Reconcile(ctx context.Context) error {
	return s.state.ReconcileExpired(ctx)
}

func NewWorkerService(state WorkerState, broker WakeupBroker, options WorkerServiceOptions) (*WorkerService, error) {
	if state == nil {
		return nil, fmt.Errorf("WorkerState is required")
	}
	if broker == nil {
		broker = NewMemoryWakeupBroker()
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.NewID == nil {
		options.NewID = func(prefix string) string { return prefix + "-" + uuid.NewString() }
	}
	if options.NewSessionToken == nil {
		options.NewSessionToken = secureWorkerToken
	}
	if options.WorkerLease <= 0 {
		options.WorkerLease = 30 * time.Second
	}
	if options.TokenLifetime <= 0 {
		options.TokenLifetime = 15 * time.Minute
	}
	if options.MailboxLease <= 0 {
		options.MailboxLease = 30 * time.Second
	}
	if options.RunLease <= 0 {
		options.RunLease = 5 * time.Minute
	}
	if options.Planner == nil {
		options.Planner = M1TurnPlanner{Bindings: state}
	}
	return &WorkerService{
		state: state, broker: broker, now: options.Now, newID: options.NewID,
		newSessionToken: options.NewSessionToken, workerLease: options.WorkerLease,
		tokenLifetime: options.TokenLifetime, mailboxLease: options.MailboxLease,
		runLease: options.RunLease, planner: options.Planner,
	}, nil
}

func secureWorkerToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate Worker Session Token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func workerTokenDigest(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func (s *WorkerService) Register(ctx context.Context, principalID string, request api.RegisterRequest) (*api.WorkerSession, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if err := domain.ValidateOpaqueID("authenticated principal", principalID); err != nil {
		return nil, err
	}
	token, err := s.newSessionToken()
	if err != nil {
		return nil, err
	}
	if len(token) < 32 {
		return nil, fmt.Errorf("Worker Session Token generator returned insufficient entropy")
	}
	now := s.now().UTC()
	registration := domain.WorkerRegistration{
		WorkerInstanceID: request.WorkerInstanceID, AgentID: request.AgentID,
		Transport: request.Transport, PrincipalID: principalID, Capabilities: request.Capabilities,
		SessionTokenDigest: workerTokenDigest(token), TokenExpiresAt: now.Add(s.tokenLifetime),
		LeaseUntil: now.Add(s.workerLease),
	}
	event := s.event("worker", "worker.registered", principalID, "", request.WorkerInstanceID, map[string]any{
		"agent_id": request.AgentID, "transport": request.Transport,
	})
	worker, err := s.state.RegisterWorker(ctx, registration, request.Backends, event)
	if err != nil {
		return nil, err
	}
	return &api.WorkerSession{Worker: *worker, SessionToken: token, TokenExpiresAt: registration.TokenExpiresAt}, nil
}

func (s *WorkerService) Heartbeat(ctx context.Context, principalID string, token string, request api.HeartbeatRequest) error {
	if err := request.Validate(); err != nil {
		return err
	}
	guard, err := s.guard(ctx, principalID, token, request.WorkerInstanceID, "", request.Generation, request.FencingToken)
	if err != nil {
		return err
	}
	event := s.event("heartbeat", "worker.heartbeat", principalID, "", request.WorkerInstanceID, map[string]any{
		"status": request.Status, "backend_health": request.BackendHealth,
	})
	_, err = s.state.HeartbeatWorker(ctx, guard, request.Status, request.BackendHealth,
		guard.CheckedAt.Add(s.workerLease), guard.CheckedAt.Add(s.tokenLifetime), event)
	return err
}

func (s *WorkerService) ClaimMailbox(ctx context.Context, principalID string, token string, request api.ClaimRequest) (*domain.MailboxItem, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	guard, err := s.guard(ctx, principalID, token, request.WorkerInstanceID, request.AgentID, request.Generation, request.FencingToken)
	if err != nil {
		return nil, err
	}
	wait := time.Duration(request.WaitSeconds) * time.Second
	deadline := time.Now().Add(wait)
	for {
		now := s.now().UTC()
		guard.CheckedAt = now
		event := s.event("mailbox", "mailbox.claimed", principalID, "", "", map[string]any{
			"worker_instance_id": guard.WorkerInstanceID,
		})
		item, err := s.state.TryClaimMailbox(ctx, guard, request.WorkCapacity, now.Add(s.mailboxLease), event)
		if err != nil || item != nil || wait == 0 {
			return item, err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, nil
		}
		wakeup, unsubscribe := s.broker.Subscribe(AgentMailboxTopic(guard.AgentID))
		// Recheck after subscribing so a commit between the previous query and
		// subscription cannot be lost.
		guard.CheckedAt = s.now().UTC()
		item, err = s.state.TryClaimMailbox(ctx, guard, request.WorkCapacity,
			guard.CheckedAt.Add(s.mailboxLease), s.event("mailbox", "mailbox.claimed", principalID, "", "", nil))
		if err != nil || item != nil {
			unsubscribe()
			return item, err
		}
		timer := time.NewTimer(remaining)
		select {
		case <-ctx.Done():
			stopAndDrainTimer(timer)
			unsubscribe()
			return nil, ctx.Err()
		case <-wakeup:
			stopAndDrainTimer(timer)
			unsubscribe()
			continue
		case <-timer.C:
			unsubscribe()
			guard.CheckedAt = s.now().UTC()
			return s.state.TryClaimMailbox(ctx, guard, request.WorkCapacity,
				guard.CheckedAt.Add(s.mailboxLease), s.event("mailbox", "mailbox.claimed", principalID, "", "", nil))
		}
	}
}

func stopAndDrainTimer(timer *time.Timer) {
	if timer.Stop() {
		return
	}
	select {
	case <-timer.C:
	default:
	}
}

func (s *WorkerService) AcceptMailbox(ctx context.Context, principalID string, token string, itemID string, request api.AcceptRequest) error {
	if err := request.Validate(); err != nil {
		return err
	}
	guard, err := s.guard(ctx, principalID, token, request.WorkerInstanceID, "", request.Generation, request.FencingToken)
	if err != nil {
		return err
	}
	event := s.event("mailbox", "mailbox."+string(request.Outcome), principalID, "", itemID,
		map[string]any{"result": request.Result})
	return s.state.AcceptMailboxItem(ctx, guard, itemID, request.ExpectedItemState, request.Outcome, event)
}

func (s *WorkerService) ResolveMailboxPayload(ctx context.Context, principalID string, token string, itemID string, request api.MailboxPayloadRequest) (*api.MailboxPayloadResponse, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	guard, err := s.guard(ctx, principalID, token, request.WorkerInstanceID, "", request.Generation, request.FencingToken)
	if err != nil {
		return nil, err
	}
	payload, err := s.state.ResolveMailboxPayload(ctx, guard, itemID)
	if err != nil {
		return nil, err
	}
	return &api.MailboxPayloadResponse{
		Message: payload.Message, ApprovalDecision: payload.ApprovalDecision,
	}, nil
}

func (s *WorkerService) BeginAttempt(ctx context.Context, principalID string, token string, itemID string, request api.BeginAttemptRequest) (*api.BeginAttemptResponse, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	guard, err := s.guard(ctx, principalID, token, request.WorkerInstanceID, request.AgentID, request.Generation, request.FencingToken)
	if err != nil {
		return nil, err
	}
	item, err := s.state.GetMailboxItem(ctx, itemID)
	if err != nil {
		return nil, err
	}
	if item.TaskID == "" {
		return nil, domain.ErrInvalidInput("work item has no Task")
	}
	task, err := s.state.GetTask(ctx, item.TaskID)
	if err != nil {
		return nil, err
	}
	messages, err := s.state.ListMessages(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	backends, err := s.state.ListWorkerBackends(ctx, guard.WorkerInstanceID)
	if err != nil {
		return nil, err
	}
	plan, err := s.planner.Plan(ctx, *task, messages, backends)
	if err != nil {
		return nil, err
	}
	if plan.PreflightScopeDigest != "" {
		if err := domain.ValidateOpaqueID("preflight_scope_digest", plan.PreflightScopeDigest); err != nil {
			return nil, err
		}
		preflightCapable := false
		for _, backend := range backends {
			if backend.BackendID == plan.Execution.Spec.BackendID &&
				backend.Descriptor.AdapterID == plan.Execution.Spec.AdapterID &&
				backend.Descriptor.Approval == openruntime.ApprovalPreflight {
				preflightCapable = true
				break
			}
		}
		if !preflightCapable {
			return nil, domain.ErrUnsupportedCapability
		}
	}
	requestedJSON, err := json.Marshal(plan.Execution.Spec)
	if err != nil {
		return nil, err
	}
	resolvedJSON, err := json.Marshal(plan.Execution)
	if err != nil {
		return nil, err
	}
	run := &domain.RunAttempt{
		ID: s.newID("run"), TaskID: task.ID, AgentID: guard.AgentID, Version: 1,
		Status: domain.RunAttemptStarting, WorkerInstanceID: guard.WorkerInstanceID,
		FencingToken: guard.FencingToken, LeaseUntil: guard.CheckedAt.Add(s.runLease),
		ExecutionSpecVersion: plan.Execution.Version, RequestedExecutionJSON: string(requestedJSON),
		ResolvedExecutionJSON: string(resolvedJSON), AdapterID: plan.Execution.Spec.AdapterID,
		BackendID: plan.Execution.Spec.BackendID, Model: plan.Execution.Spec.Model,
		ReasoningMode: plan.Execution.Spec.Reasoning.Mode, ReasoningValue: plan.Execution.Spec.Reasoning.Value,
		StartedAt: guard.CheckedAt,
	}
	taskEvent := s.event("task", "task.running", principalID, task.OrganizationID, task.ID, map[string]any{"run_id": run.ID})
	runEvent := s.event("run", "run_attempt.started", principalID, task.OrganizationID, run.ID, map[string]any{"task_id": task.ID})
	mailboxEvent := s.event("mailbox", "mailbox.accepted", principalID, task.OrganizationID, itemID, map[string]any{"run_id": run.ID})
	var preflightEvent *domain.JournalEvent
	if plan.PreflightScopeDigest != "" {
		preflightEvent = s.event("approval", "approval.consumed", principalID, task.OrganizationID, "", map[string]any{
			"task_id": task.ID, "scope_digest": plan.PreflightScopeDigest, "run_id": run.ID,
		})
	}
	updatedTask, acceptedItem, err := s.state.BeginClaimedRunAttempt(ctx, guard, itemID,
		task.Version, run, plan.PreflightScopeDigest, preflightEvent, taskEvent, runEvent, mailboxEvent)
	if err != nil {
		return nil, err
	}
	return &api.BeginAttemptResponse{
		MailboxItem: *acceptedItem,
		Turn: openruntime.TurnRequest{
			Task: *updatedTask, RunAttempt: *run, SessionBinding: plan.SessionBinding,
			Messages: messages, Execution: plan.Execution,
		},
	}, nil
}

func (s *WorkerService) AppendEvents(ctx context.Context, principalID string, token string, runID string, batch api.EventBatch) error {
	if err := batch.Validate(); err != nil {
		return err
	}
	guard, err := s.guard(ctx, principalID, token, batch.WorkerInstanceID, "", batch.Generation, batch.FencingToken)
	if err != nil {
		return err
	}
	events := make([]*domain.JournalEvent, 0, len(batch.Events))
	for _, runtimeEvent := range batch.Events {
		if runtimeEvent.Type == "approval.requested" {
			if _, err := decodeNativeApprovalPayload(runtimeEvent.Payload, guard.CheckedAt); err != nil {
				return err
			}
		}
		payload, err := json.Marshal(map[string]any{
			"runtime_event_type": runtimeEvent.Type, "payload": runtimeEvent.Payload,
			"occurred_at": runtimeEvent.OccurredAt,
		})
		if err != nil {
			return err
		}
		events = append(events, s.event("runtime", "runtime."+runtimeEvent.Type, principalID,
			"", runID, json.RawMessage(payload)))
	}
	return s.state.AppendRunEvents(ctx, guard, runID, batch.ExpectedRunVersion, events)
}

// nativeApprovalPayload is the only RuntimeEvent payload that has control
// plane semantics.  The task/run binding is deliberately not accepted from
// the runtime: it is derived from the authenticated active RunAttempt inside
// the repository transaction.
type nativeApprovalPayload struct {
	ApprovalRequestID string    `json:"approval_request_id"`
	ScopeDigest       string    `json:"scope_digest"`
	ExpiresAt         time.Time `json:"expires_at"`
}

func decodeNativeApprovalPayload(raw json.RawMessage, now time.Time) (nativeApprovalPayload, error) {
	var payload nativeApprovalPayload
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return nativeApprovalPayload{}, domain.ErrInvalidInput("approval.requested payload must be strict JSON")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nativeApprovalPayload{}, domain.ErrInvalidInput("approval.requested payload must contain one JSON object")
	}
	if err := domain.ValidateOpaqueID("approval_request_id", payload.ApprovalRequestID); err != nil {
		return nativeApprovalPayload{}, err
	}
	if payload.ApprovalRequestID != strings.TrimSpace(payload.ApprovalRequestID) {
		return nativeApprovalPayload{}, domain.ErrInvalidInput("approval_request_id cannot contain surrounding whitespace")
	}
	if err := domain.ValidateOpaqueID("scope_digest", payload.ScopeDigest); err != nil {
		return nativeApprovalPayload{}, err
	}
	if payload.ScopeDigest != strings.TrimSpace(payload.ScopeDigest) {
		return nativeApprovalPayload{}, domain.ErrInvalidInput("scope_digest cannot contain surrounding whitespace")
	}
	if payload.ExpiresAt.IsZero() || !payload.ExpiresAt.After(now) {
		return nativeApprovalPayload{}, domain.ErrInvalidInput("approval expires_at must be in the future")
	}
	return payload, nil
}

func (s *WorkerService) Finish(ctx context.Context, principalID string, token string, runID string, request api.FinishRunRequest) error {
	if err := request.Validate(); err != nil {
		return err
	}
	guard, err := s.guard(ctx, principalID, token, request.WorkerInstanceID, "", request.Generation, request.FencingToken)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(request.Result)
	if err != nil {
		return err
	}
	runEvent := s.event("run", "run_attempt.finished", principalID, "", runID, json.RawMessage(payload))
	taskEvent := s.event("task", "task.settled", principalID, "", "", json.RawMessage(payload))
	var binding *domain.SessionBinding
	var expectedBindingVersion int64
	var bindingEvent *domain.JournalEvent
	if request.Result.ProviderSessionID != "" {
		run, loadErr := s.state.GetRunAttempt(ctx, runID)
		if loadErr != nil {
			return loadErr
		}
		var resolved domain.ResolvedExecutionSpec
		if err := json.Unmarshal([]byte(run.ResolvedExecutionJSON), &resolved); err != nil {
			return fmt.Errorf("decode RunAttempt resolved execution for SessionBinding: %w", err)
		}
		contextID := resolved.Spec.Session.ContextID
		if err := domain.ValidateOpaqueID("session context_id", contextID); err != nil {
			return err
		}
		existing, lookupErr := s.state.GetSessionBinding(ctx, contextID, run.AgentID, run.BackendID)
		switch {
		case lookupErr == nil:
			binding = existing
			expectedBindingVersion = existing.Version
			binding.ProviderSessionID = request.Result.ProviderSessionID
			binding.State = domain.SessionBindingActive
			binding.Version = existing.Version + 1
		case errors.Is(lookupErr, domain.ErrNotFound):
			binding = &domain.SessionBinding{
				ID: s.newID("binding"), ContextID: contextID, AgentID: run.AgentID,
				BackendID: run.BackendID, ProviderSessionID: request.Result.ProviderSessionID,
				State: domain.SessionBindingActive, Version: 1,
			}
		default:
			return lookupErr
		}
		bindingEvent = s.event("session-binding", "session_binding.saved", principalID, "", binding.ID,
			map[string]any{"context_id": contextID, "backend_id": run.BackendID, "run_id": runID})
	}
	// The repository binds the Task aggregate ID from the RunAttempt and rejects
	// mismatches; leave it empty here so the transaction supplies the authority.
	return s.state.FinishRun(ctx, guard, runID, request.ExpectedTaskVersion,
		request.ExpectedRunVersion, request.Result, binding, expectedBindingVersion, bindingEvent, taskEvent, runEvent)
}

func (s *WorkerService) ClaimWorkerCommand(ctx context.Context, principalID string, token string, request api.ControlClaimRequest) (*domain.WorkerCommand, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	guard, err := s.guard(ctx, principalID, token, request.WorkerInstanceID, "", request.Generation, request.FencingToken)
	if err != nil {
		return nil, err
	}
	wait := time.Duration(request.WaitSeconds) * time.Second
	deadline := time.Now().Add(wait)
	claim := func() (*domain.WorkerCommand, error) {
		guard.CheckedAt = s.now().UTC()
		return s.state.ClaimWorkerCommand(ctx, guard, guard.CheckedAt.Add(s.mailboxLease),
			s.event("worker_command", "worker_command.claimed", principalID, "", "", nil))
	}
	for {
		command, err := claim()
		if err != nil || command != nil || wait == 0 {
			return command, err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, nil
		}
		wakeup, unsubscribe := s.broker.Subscribe(WorkerControlTopic(guard.WorkerInstanceID))
		// Recheck after subscribing so a commit between the previous query and
		// subscription cannot be lost.
		command, err = claim()
		if err != nil || command != nil {
			unsubscribe()
			return command, err
		}
		timer := time.NewTimer(remaining)
		select {
		case <-ctx.Done():
			stopAndDrainTimer(timer)
			unsubscribe()
			return nil, ctx.Err()
		case <-wakeup:
			stopAndDrainTimer(timer)
			unsubscribe()
			continue
		case <-timer.C:
			unsubscribe()
			return claim()
		}
	}
}

func (s *WorkerService) AcknowledgeWorkerCommand(ctx context.Context, principalID string, token string, commandID string, request api.ControlAckRequest) error {
	if err := request.Validate(); err != nil {
		return err
	}
	guard, err := s.guard(ctx, principalID, token, request.WorkerInstanceID, "", request.Generation, request.FencingToken)
	if err != nil {
		return err
	}
	return s.state.AcknowledgeWorkerCommand(ctx, guard, commandID, request.State, request.Result, s.event("worker_command", "worker_command.acknowledged", principalID, "", commandID, map[string]any{"state": request.State}))
}

func (s *WorkerService) guard(ctx context.Context, principalID string, token string, workerID string, requestedAgentID string, generation int64, fencingToken int64) (domain.WorkerWriteGuard, error) {
	if strings.TrimSpace(token) == "" {
		return domain.WorkerWriteGuard{}, domain.ErrUnauthorized
	}
	credential, err := s.state.GetWorkerCredential(ctx, workerID)
	if err != nil {
		return domain.WorkerWriteGuard{}, domain.ErrUnauthorized
	}
	guard := domain.WorkerWriteGuard{
		WorkerInstanceID: workerID, AgentID: credential.Worker.AgentID, PrincipalID: principalID,
		SessionTokenDigest: workerTokenDigest(token), Generation: generation,
		FencingToken: fencingToken, CheckedAt: s.now().UTC(),
	}
	if err := credential.Authorize(guard); err != nil {
		return domain.WorkerWriteGuard{}, err
	}
	if requestedAgentID != "" && requestedAgentID != guard.AgentID {
		return domain.WorkerWriteGuard{}, domain.ErrForbidden("Worker is not bound to requested Agent")
	}
	return guard, nil
}

func (s *WorkerService) event(prefix string, eventType string, actor string, organization string, aggregateID string, payload any) *domain.JournalEvent {
	payloadJSON := json.RawMessage(`{}`)
	if payload != nil {
		if raw, ok := payload.(json.RawMessage); ok {
			payloadJSON = raw
		} else if encoded, err := json.Marshal(payload); err == nil {
			payloadJSON = encoded
		}
	}
	return &domain.JournalEvent{
		ID: s.newID("event-" + prefix), OrganizationID: organization,
		AggregateID: aggregateID, EventType: eventType, ActorPrincipalID: actor,
		Payload: payloadJSON, CreatedAt: s.now().UTC(),
	}
}

type SessionBindingReader interface {
	GetSessionBinding(context.Context, string, string, string) (*domain.SessionBinding, error)
}

type M1TurnPlanner struct {
	Bindings SessionBindingReader
}

func (p M1TurnPlanner) Plan(ctx context.Context, task domain.Task, _ []domain.Message, backends []openruntime.BackendRegistration) (TurnPlan, error) {
	available := append([]openruntime.BackendRegistration(nil), backends...)
	sort.Slice(available, func(i, j int) bool { return available[i].BackendID < available[j].BackendID })
	// Preserve provider context whenever the currently available Backend can
	// resume an active binding. Backend ordering is only a deterministic
	// fallback for a genuinely new session.
	if p.Bindings != nil {
		for _, backend := range available {
			if !m1BackendUsable(backend) || !containsSession(backend.Descriptor.SessionModes, domain.SessionModeResume) {
				continue
			}
			binding, err := p.Bindings.GetSessionBinding(ctx, task.ID, task.TargetAgentID, backend.BackendID)
			if err != nil && !errors.Is(err, domain.ErrNotFound) {
				return TurnPlan{}, err
			}
			if err == nil && binding.State == domain.SessionBindingActive {
				return m1PlanForBackend(task, backend, binding)
			}
		}
	}
	for _, backend := range available {
		if !m1BackendUsable(backend) || !containsSession(backend.Descriptor.SessionModes, domain.SessionModeNew) {
			continue
		}
		return m1PlanForBackend(task, backend, nil)
	}
	return TurnPlan{}, domain.ErrUnsupportedCapability
}

func m1BackendUsable(backend openruntime.BackendRegistration) bool {
	return (backend.Health == openruntime.BackendHealthy || backend.Health == openruntime.BackendDegraded) &&
		len(backend.Descriptor.Models) > 0 &&
		len(backend.Descriptor.ReasoningModes) > 0
}

func m1PlanForBackend(task domain.Task, backend openruntime.BackendRegistration, binding *domain.SessionBinding) (TurnPlan, error) {
	reasoning := domain.ReasoningSpec{Mode: domain.ReasoningBackendDefault}
	if !containsReasoning(backend.Descriptor.ReasoningModes, reasoning.Mode) {
		reasoning.Mode = backend.Descriptor.ReasoningModes[0]
		switch reasoning.Mode {
		case domain.ReasoningEffort:
			reasoning.Value = "medium"
		case domain.ReasoningBudgetTokens:
			reasoning.Value = "1024"
		}
	}
	sessionMode := domain.SessionModeNew
	if binding != nil {
		sessionMode = domain.SessionModeResume
	}
	spec := domain.ExecutionSpec{
		AdapterID: backend.Descriptor.AdapterID, BackendID: backend.BackendID,
		Model: backend.Descriptor.Models[0], Reasoning: reasoning,
		Session: domain.SessionSpec{Mode: sessionMode, ContextID: task.ID},
		Timeout: 30 * time.Minute, BackendOptions: json.RawMessage(`{}`),
	}
	if err := spec.ValidateShape(); err != nil {
		return TurnPlan{}, err
	}
	return TurnPlan{Execution: domain.ResolvedExecutionSpec{
		Version: 1, Spec: spec,
		Sources: map[string]string{"adapter": "worker_descriptor", "backend": "worker_descriptor", "model": "m1_default"},
	}, SessionBinding: binding}, nil
}

func containsReasoning(values []domain.ReasoningMode, target domain.ReasoningMode) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsSession(values []domain.SessionMode, target domain.SessionMode) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
