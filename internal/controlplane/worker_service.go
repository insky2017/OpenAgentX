package controlplane

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"agentbus/internal/api"
	"agentbus/internal/domain"
	openruntime "agentbus/internal/runtime"
	"github.com/google/uuid"
)

type WorkerState interface {
	ReconcileExpired(context.Context) error
	RegisterWorker(context.Context, domain.WorkerRegistration, []openruntime.BackendRegistration, *domain.JournalEvent) (*domain.WorkerInstance, error)
	GetWorkerCredential(context.Context, string) (*domain.WorkerCredential, error)
	HeartbeatWorker(context.Context, domain.WorkerWriteGuard, domain.WorkerStatus, map[string]openruntime.BackendHealth, time.Time, time.Time, *domain.JournalEvent) (*domain.WorkerInstance, error)
	TryClaimMailbox(context.Context, domain.WorkerWriteGuard, int, time.Time, *domain.JournalEvent) (*domain.MailboxItem, error)
	AcceptMailboxItem(context.Context, domain.WorkerWriteGuard, string, domain.MailboxState, domain.MailboxState, *domain.JournalEvent) error
	GetMailboxItem(context.Context, string) (*domain.MailboxItem, error)
	GetTask(context.Context, string) (*domain.Task, error)
	ListMessages(context.Context, string) ([]domain.Message, error)
	ListWorkerBackends(context.Context, string) ([]openruntime.BackendRegistration, error)
	BeginClaimedRunAttempt(context.Context, domain.WorkerWriteGuard, string, int64, *domain.RunAttempt, *domain.JournalEvent, *domain.JournalEvent, *domain.JournalEvent) (*domain.Task, *domain.MailboxItem, error)
	AppendRunEvents(context.Context, domain.WorkerWriteGuard, string, int64, []*domain.JournalEvent) error
	FinishRun(context.Context, domain.WorkerWriteGuard, string, int64, int64, openruntime.TurnResult, *domain.JournalEvent, *domain.JournalEvent) error
}

type TurnPlan struct {
	Execution      domain.ResolvedExecutionSpec
	SessionBinding *domain.SessionBinding
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
		options.Planner = M1TurnPlanner{}
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
			if !timer.Stop() {
				<-timer.C
			}
			unsubscribe()
			return nil, ctx.Err()
		case <-wakeup:
			if !timer.Stop() {
				<-timer.C
			}
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
	updatedTask, acceptedItem, err := s.state.BeginClaimedRunAttempt(ctx, guard, itemID,
		request.ExpectedTaskVersion, run, taskEvent, runEvent, mailboxEvent)
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
	// The repository binds the Task aggregate ID from the RunAttempt and rejects
	// mismatches; leave it empty here so the transaction supplies the authority.
	return s.state.FinishRun(ctx, guard, runID, request.ExpectedTaskVersion,
		request.ExpectedRunVersion, request.Result, taskEvent, runEvent)
}

func (s *WorkerService) guard(ctx context.Context, principalID string, token string, workerID string, requestedAgentID string, generation int64, fencingToken int64) (domain.WorkerWriteGuard, error) {
	if strings.TrimSpace(token) == "" {
		return domain.WorkerWriteGuard{}, domain.ErrUnauthorized
	}
	credential, err := s.state.GetWorkerCredential(ctx, workerID)
	if err != nil {
		return domain.WorkerWriteGuard{}, domain.ErrUnauthorized
	}
	agentID := credential.Worker.AgentID
	if requestedAgentID != "" && requestedAgentID != agentID {
		return domain.WorkerWriteGuard{}, domain.ErrForbidden("Worker is not bound to requested Agent")
	}
	return domain.WorkerWriteGuard{
		WorkerInstanceID: workerID, AgentID: agentID, PrincipalID: principalID,
		SessionTokenDigest: workerTokenDigest(token), Generation: generation,
		FencingToken: fencingToken, CheckedAt: s.now().UTC(),
	}, nil
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

type M1TurnPlanner struct{}

func (M1TurnPlanner) Plan(_ context.Context, task domain.Task, _ []domain.Message, backends []openruntime.BackendRegistration) (TurnPlan, error) {
	available := append([]openruntime.BackendRegistration(nil), backends...)
	sort.Slice(available, func(i, j int) bool { return available[i].BackendID < available[j].BackendID })
	for _, backend := range available {
		if backend.Health == openruntime.BackendUnavailable || len(backend.Descriptor.Models) == 0 {
			continue
		}
		reasoning := domain.ReasoningSpec{Mode: domain.ReasoningBackendDefault}
		if !containsReasoning(backend.Descriptor.ReasoningModes, reasoning.Mode) {
			if len(backend.Descriptor.ReasoningModes) == 0 {
				continue
			}
			reasoning.Mode = backend.Descriptor.ReasoningModes[0]
			switch reasoning.Mode {
			case domain.ReasoningEffort:
				reasoning.Value = "medium"
			case domain.ReasoningBudgetTokens:
				reasoning.Value = "1024"
			}
		}
		sessionMode := domain.SessionModeNew
		if !containsSession(backend.Descriptor.SessionModes, sessionMode) {
			if len(backend.Descriptor.SessionModes) == 0 {
				continue
			}
			sessionMode = backend.Descriptor.SessionModes[0]
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
		}}, nil
	}
	return TurnPlan{}, domain.ErrUnsupportedCapability
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
