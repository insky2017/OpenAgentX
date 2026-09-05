package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"openagentx/internal/api"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
	"openagentx/internal/runtime/fake"
)

type finishRecord struct {
	runID   string
	request api.FinishRunRequest
}

type acceptRecord struct {
	itemID  string
	request api.AcceptRequest
}

type commandAckRecord struct {
	commandID string
	request   api.ControlAckRequest
}

type releaseRecord struct{ request api.WorkerReleaseRequest }

type fakeWorkerClient struct {
	mu sync.Mutex

	session         api.WorkerSession
	registerRequest api.RegisterRequest
	mailbox         []domain.MailboxItem
	commands        []domain.WorkerCommand
	claims          []api.ClaimRequest
	heartbeats      []api.HeartbeatRequest
	finishes        []finishRecord
	accepts         []acceptRecord
	commandAcks     []commandAckRecord
	releases        []releaseRecord
	eventLog        []string
	runtimeEvents   []openruntime.RuntimeEvent
	networkWorks    []*api.NetworkWorkEnvelope
	networkAcks     []api.NetworkWorkAckRequest
	networkAckErr   error
	runCounter      int
	heartbeatFail   int

	wakeup        chan struct{}
	commandWake   chan struct{}
	heartbeatHit  chan struct{}
	finishHit     chan struct{}
	acceptHit     chan struct{}
	ackHit        chan struct{}
	networkAckHit chan struct{}
}

func newFakeWorkerClient() *fakeWorkerClient {
	return &fakeWorkerClient{
		wakeup: make(chan struct{}, 1), commandWake: make(chan struct{}, 1),
		heartbeatHit: make(chan struct{}, 64), finishHit: make(chan struct{}, 64),
		acceptHit: make(chan struct{}, 64), ackHit: make(chan struct{}, 64),
		networkAckHit: make(chan struct{}, 64),
	}
}

func (c *fakeWorkerClient) RegisterWorker(_ context.Context, request api.RegisterRequest) (*api.WorkerSession, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.registerRequest = request
	c.session = api.WorkerSession{Worker: domain.WorkerInstance{
		ID: request.WorkerInstanceID, AgentID: request.AgentID, Generation: 1,
		Transport: request.Transport, AuthenticatedPrincipal: "worker-principal",
		Capabilities: request.Capabilities, Status: domain.WorkerStatusBootstrapping,
		FencingToken: 1, LastHeartbeatAt: time.Now(), LeaseUntil: time.Now().Add(time.Minute),
		StartedAt: time.Now(), UpdatedAt: time.Now(),
	}, SessionToken: "fake-worker-session-token", TokenExpiresAt: time.Now().Add(time.Hour)}
	session := c.session
	return &session, nil
}

func (c *fakeWorkerClient) Heartbeat(_ context.Context, request api.HeartbeatRequest) error {
	c.mu.Lock()
	c.heartbeats = append(c.heartbeats, request)
	count := len(c.heartbeats)
	failAt := c.heartbeatFail
	c.mu.Unlock()
	signal(c.heartbeatHit)
	if failAt > 0 && count >= failAt {
		return domain.ErrLeaseExpired
	}
	return nil
}

func (c *fakeWorkerClient) PullNetworkWork(context.Context, api.NetworkWorkPullRequest) (*api.NetworkWorkEnvelope, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.networkWorks) == 0 {
		return nil, nil
	}
	work := c.networkWorks[0]
	c.networkWorks = c.networkWorks[1:]
	return work, nil
}

func (c *fakeWorkerClient) AcknowledgeNetworkWork(_ context.Context, _ string, ack api.NetworkWorkAckRequest) error {
	c.mu.Lock()
	c.networkAcks = append(c.networkAcks, ack)
	err := c.networkAckErr
	c.mu.Unlock()
	signal(c.networkAckHit)
	return err
}

func (c *fakeWorkerClient) ClaimMailbox(ctx context.Context, request api.ClaimRequest) (*domain.MailboxItem, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.claims = append(c.claims, request)
	index := c.nextMailboxIndex(request.WorkCapacity)
	if index >= 0 {
		item := c.mailbox[index]
		c.mailbox = append(c.mailbox[:index], c.mailbox[index+1:]...)
		c.mu.Unlock()
		return &item, nil
	}
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.wakeup:
	case <-time.After(10 * time.Millisecond):
	}
	return nil, nil
}

func (c *fakeWorkerClient) nextMailboxIndex(workCapacity int) int {
	indexes := make([]int, 0, len(c.mailbox))
	for index, item := range c.mailbox {
		if item.Lane == domain.MailboxLaneControl || workCapacity > 0 {
			indexes = append(indexes, index)
		}
	}
	if len(indexes) == 0 {
		return -1
	}
	sort.Slice(indexes, func(i, j int) bool {
		left, right := c.mailbox[indexes[i]], c.mailbox[indexes[j]]
		if left.Lane.Priority() != right.Lane.Priority() {
			return left.Lane.Priority() < right.Lane.Priority()
		}
		return left.Sequence < right.Sequence
	})
	return indexes[0]
}

func (c *fakeWorkerClient) BeginAttempt(_ context.Context, itemID string, request api.BeginAttemptRequest) (*api.BeginAttemptResponse, error) {
	c.mu.Lock()
	c.runCounter++
	runID := fmt.Sprintf("run-%d", c.runCounter)
	c.mu.Unlock()
	now := time.Now().UTC()
	task := domain.Task{
		ID: "task-" + itemID, TargetAgentID: request.AgentID, Version: 2,
		Status: domain.TaskStatusRunning, DispatchMode: domain.DispatchModeDirect,
		SenderPrincipalID: "human-owner", OrganizationID: "org-main", Content: "work",
	}
	run := domain.RunAttempt{
		ID: runID, TaskID: task.ID, AgentID: request.AgentID, Version: 1,
		Status: domain.RunAttemptStarting, WorkerInstanceID: request.WorkerInstanceID,
		FencingToken: request.FencingToken, LeaseUntil: now.Add(time.Minute),
		ExecutionSpecVersion: 1, RequestedExecutionJSON: `{}`, ResolvedExecutionJSON: `{}`,
		AdapterID: "fake", BackendID: "local", Model: "model-1",
		ReasoningMode: domain.ReasoningBackendDefault, StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	spec := domain.ExecutionSpec{
		AdapterID: "fake", BackendID: "local", Model: "model-1",
		Reasoning: domain.ReasoningSpec{Mode: domain.ReasoningBackendDefault},
		Session:   domain.SessionSpec{Mode: domain.SessionModeNew, ContextID: task.ID},
		Timeout:   time.Minute, BackendOptions: json.RawMessage(`{}`),
		Network: domain.NetworkPolicy{Mode: domain.NetworkInherit},
	}
	return &api.BeginAttemptResponse{
		MailboxItem: domain.MailboxItem{ID: itemID, State: domain.MailboxStateAccepted},
		Turn: openruntime.TurnRequest{Task: task, RunAttempt: run,
			Execution: domain.ResolvedExecutionSpec{Version: 1, Spec: spec}},
	}, nil
}

func (c *fakeWorkerClient) ResolveMailboxPayload(context.Context, string, api.MailboxPayloadRequest) (*api.MailboxPayloadResponse, error) {
	return nil, errors.New("fake Worker client does not resolve payloads")
}

func (c *fakeWorkerClient) AcceptMailboxItem(_ context.Context, itemID string, request api.AcceptRequest) error {
	c.mu.Lock()
	c.accepts = append(c.accepts, acceptRecord{itemID: itemID, request: request})
	c.mu.Unlock()
	signal(c.acceptHit)
	return nil
}

func (c *fakeWorkerClient) ClaimWorkerCommand(ctx context.Context, request api.ControlClaimRequest) (*domain.WorkerCommand, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	if len(c.commands) != 0 {
		command := c.commands[0]
		c.commands = c.commands[1:]
		c.mu.Unlock()
		return &command, nil
	}
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.commandWake:
	case <-time.After(10 * time.Millisecond):
	}
	return nil, nil
}

func (c *fakeWorkerClient) AcknowledgeWorkerCommand(_ context.Context, commandID string, request api.ControlAckRequest) error {
	c.mu.Lock()
	c.commandAcks = append(c.commandAcks, commandAckRecord{commandID: commandID, request: request})
	c.eventLog = append(c.eventLog, "ack")
	c.mu.Unlock()
	signal(c.ackHit)
	return nil
}

func (c *fakeWorkerClient) ReleaseWorker(_ context.Context, request api.WorkerReleaseRequest) error {
	c.mu.Lock()
	c.releases = append(c.releases, releaseRecord{request: request})
	c.eventLog = append(c.eventLog, "release")
	c.mu.Unlock()
	return nil
}

func (c *fakeWorkerClient) AcknowledgeReleasedWorkerCommand(_ context.Context, commandID string, request api.ControlAckRequest) error {
	c.mu.Lock()
	c.commandAcks = append(c.commandAcks, commandAckRecord{commandID: commandID, request: request})
	c.eventLog = append(c.eventLog, "released-ack")
	c.mu.Unlock()
	signal(c.ackHit)
	return nil
}

func (c *fakeWorkerClient) AppendRunEvents(_ context.Context, _ string, batch api.EventBatch) error {
	c.mu.Lock()
	c.runtimeEvents = append(c.runtimeEvents, batch.Events...)
	c.mu.Unlock()
	return nil
}

func (c *fakeWorkerClient) FinishRun(_ context.Context, runID string, request api.FinishRunRequest) error {
	c.mu.Lock()
	c.finishes = append(c.finishes, finishRecord{runID: runID, request: request})
	c.mu.Unlock()
	signal(c.finishHit)
	return nil
}

func (c *fakeWorkerClient) enqueueMailbox(items ...domain.MailboxItem) {
	c.mu.Lock()
	c.mailbox = append(c.mailbox, items...)
	c.mu.Unlock()
	signal(c.wakeup)
}

func (c *fakeWorkerClient) enqueueCommand(command domain.WorkerCommand) {
	c.mu.Lock()
	c.commands = append(c.commands, command)
	c.mu.Unlock()
	signal(c.commandWake)
}

func (c *fakeWorkerClient) hasPendingMailbox(itemID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, item := range c.mailbox {
		if item.ID == itemID {
			return true
		}
	}
	return false
}

type fakePayloadResolver struct {
	messages  map[string]domain.Message
	decisions map[string]domain.ApprovalDecision
}

func (r fakePayloadResolver) ResolveMessage(_ context.Context, item domain.MailboxItem) (domain.Message, error) {
	message, ok := r.messages[item.MessageID]
	if !ok {
		return domain.Message{}, domain.ErrNotFound
	}
	return message, nil
}

func (r fakePayloadResolver) ResolveApprovalDecision(_ context.Context, item domain.MailboxItem) (domain.ApprovalDecision, error) {
	decision, ok := r.decisions[item.ApprovalDecisionID]
	if !ok {
		return domain.ApprovalDecision{}, domain.ErrNotFound
	}
	return decision, nil
}

func newRunnerFixture(t *testing.T, enableControl bool) (*Runner, *fakeWorkerClient, *fake.Adapter) {
	t.Helper()
	descriptor := openruntime.AdapterDescriptor{
		AdapterID: "fake", BackendType: "fake", Version: "1", LaunchProtocol: "inproc",
		Models: []string{"model-1"}, ReasoningModes: []domain.ReasoningMode{domain.ReasoningBackendDefault},
		SessionModes: []domain.SessionMode{domain.SessionModeNew}, Steer: openruntime.SteerNative,
		Approval: openruntime.ApprovalNative, Cancel: openruntime.CancelNative,
		BackendOptionsJSON: json.RawMessage(`{"type":"object"}`), MaxConcurrency: 1,
	}
	adapter, err := fake.NewAdapter(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := NewBackendPool([]RuntimeBackend{{ID: "local", Adapter: adapter}})
	if err != nil {
		t.Fatal(err)
	}
	client := newFakeWorkerClient()
	resolver := fakePayloadResolver{
		messages: map[string]domain.Message{"message-control": {
			ID: "message-control", Version: 1, Sequence: 1, TaskID: "task-mailbox-work-1",
			SenderPrincipalID: "human-owner", TargetAgentID: "quote", Kind: domain.MessageKindSupplement,
			Content: "change direction", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		}},
		decisions: map[string]domain.ApprovalDecision{"decision-control": {
			ID: "decision-control", ApprovalRequestID: "approval-control", DecidedBy: "human-owner",
			Decision: domain.ApprovalDecisionApprove, State: domain.ApprovalDecisionPersisted,
			IdempotencyKey: "approval-idem", CreatedAt: time.Now().UTC(),
		}},
	}
	runner, err := NewRunner(Config{
		AgentID: "quote", WorkerInstanceID: "worker-1", Transport: domain.WorkerTransportUnix,
		Capabilities: []string{"coding"}, HeartbeatInterval: 10 * time.Millisecond,
		MailboxWait: time.Second, ControlWait: time.Second, ShutdownTimeout: time.Second,
		EnableControlLoop: enableControl,
	}, client, pool, resolver)
	if err != nil {
		t.Fatal(err)
	}
	return runner, client, adapter
}

func TestResidentWorkerWaitDoesNotBlockHeartbeatOrActiveControlAndContinuesToNextTask(t *testing.T) {
	runner, client, adapter := newRunnerFixture(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- runner.Run(ctx) }()
	waitSignal(t, client.heartbeatHit, "initial heartbeat")
	client.enqueueMailbox(workItem("mailbox-work-1", 1))
	handle, err := adapter.NextHandle(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	drainSignals(client.heartbeatHit)
	if err := handle.Emit(testContext(t), openruntime.RuntimeEvent{
		Type: "turn.output", Payload: json.RawMessage(`{"text":"working"}`), OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("emit Runtime Event while Wait is blocked: %v", err)
	}

	client.enqueueMailbox(
		workItem("mailbox-work-2", 2),
		controlItem("mailbox-message", 3, domain.MailboxKindMessage, "run-1", "message-control", ""),
		controlItem("mailbox-approval", 4, domain.MailboxKindApproval, "run-1", "", "decision-control"),
		controlItem("mailbox-cancel", 5, domain.MailboxKindCancel, "run-1", "", ""),
	)
	if _, err := handle.NextSteer(testContext(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := handle.NextApproval(testContext(t)); err != nil {
		t.Fatal(err)
	}
	if err := handle.NextCancel(testContext(t)); err != nil {
		t.Fatal(err)
	}
	waitN(t, client.acceptHit, 3, "control acknowledgements")
	if !client.hasPendingMailbox("mailbox-work-2") {
		t.Fatal("Worker claimed a second Task while the first turn was active")
	}
	waitSignal(t, client.heartbeatHit, "heartbeat while TurnHandle.Wait is blocked")
	handle.Complete(openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "first", SideEffectsKnown: true})
	waitSignal(t, client.finishHit, "first turn finish")
	second, err := adapter.NextHandle(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	second.Complete(openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "second", SideEffectsKnown: true})
	waitSignal(t, client.finishHit, "second turn finish")
	cancel()
	if err := waitWorkerExit(t, result); err != nil {
		t.Fatalf("Resident Worker stopped with error: %v", err)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.finishes) != 2 {
		t.Fatalf("finish count=%d want=2", len(client.finishes))
	}
	if len(client.releases) != 1 {
		t.Fatalf("graceful shutdown release count=%d want=1", len(client.releases))
	}
	foundZeroCapacity := false
	for _, claim := range client.claims {
		if claim.WorkCapacity == 0 {
			foundZeroCapacity = true
			break
		}
	}
	if !foundZeroCapacity {
		t.Fatal("Mailbox Pump never advertised zero work capacity during active turn")
	}
}

func TestSlowBackendHealthDoesNotBlockHeartbeatOrControl(t *testing.T) {
	descriptor := runnerTestDescriptor([]string{"inherit"})
	base, err := fake.NewAdapter(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	adapter := &slowHealthAdapter{AgentRuntimeAdapter: base, blocked: make(chan struct{})}
	pool, err := NewBackendPool([]RuntimeBackend{{ID: "local", Adapter: adapter}})
	if err != nil {
		t.Fatal(err)
	}
	client := newFakeWorkerClient()
	runner := newTestRunner(t, client, pool, true)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- runner.Run(ctx) }()
	waitSignal(t, client.heartbeatHit, "initial heartbeat")
	waitSignal(t, adapter.blocked, "slow periodic Health")
	drainSignals(client.heartbeatHit)
	client.enqueueCommand(domain.WorkerCommand{
		ID: "command-health", WorkerInstanceID: "worker-1", Generation: 1,
		Kind: domain.WorkerCommandHealthCheck, State: domain.WorkerCommandClaimed,
		RequestedBy: "human-owner", IdempotencyKey: "health-idem", Attempts: 1, CreatedAt: time.Now(),
	})
	waitSignal(t, client.ackHit, "control acknowledgement during slow Health")
	waitSignal(t, client.heartbeatHit, "heartbeat during slow Health")
	cancel()
	if err := waitWorkerExit(t, result); err != nil {
		t.Fatalf("Worker stopped after slow Health: %v", err)
	}
}

func TestSlowNetworkProbeDoesNotBlockHeartbeatOrControl(t *testing.T) {
	descriptor := runnerTestDescriptor([]string{"inherit", "direct"})
	adapter := &probeBlockingAdapter{descriptor: descriptor, blocked: make(chan struct{}), release: make(chan struct{})}
	pool, err := NewBackendPool([]RuntimeBackend{{ID: "local", Adapter: adapter}})
	if err != nil {
		t.Fatal(err)
	}
	client := newFakeWorkerClient()
	client.networkWorks = []*api.NetworkWorkEnvelope{{Work: domain.NetworkWork{
		ID: "network-test", Kind: domain.NetworkWorkTest, AgentID: "quote", BackendID: "local",
		WorkerInstanceID: "worker-1", Generation: 1, Mode: domain.NetworkDirect,
		PolicyVersion: 1, ManifestDigest: strings.Repeat("a", 64), RuntimeIdentity: descriptor.RuntimeIdentity, State: "claimed",
	}}}
	runner := newTestRunner(t, client, pool, true)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- runner.Run(ctx) }()
	waitSignal(t, client.heartbeatHit, "initial heartbeat")
	waitSignal(t, adapter.blocked, "slow network probe")
	drainSignals(client.heartbeatHit)
	client.enqueueCommand(domain.WorkerCommand{
		ID: "command-health", WorkerInstanceID: "worker-1", Generation: 1,
		Kind: domain.WorkerCommandHealthCheck, State: domain.WorkerCommandClaimed,
		RequestedBy: "human-owner", IdempotencyKey: "health-idem", Attempts: 1, CreatedAt: time.Now(),
	})
	waitSignal(t, client.ackHit, "control acknowledgement during slow network probe")
	waitSignal(t, client.heartbeatHit, "heartbeat during slow network probe")
	close(adapter.release)
	waitSignal(t, client.networkAckHit, "network probe acknowledgement")
	cancel()
	if err := waitWorkerExit(t, result); err != nil {
		t.Fatalf("Worker stopped after slow network probe: %v", err)
	}
}

func TestStaleNetworkWorkAckKeepsValidWorkerControlSession(t *testing.T) {
	runner, client, _ := newRunnerFixture(t, true)
	client.networkAckErr = domain.ErrStaleVersion
	client.networkWorks = []*api.NetworkWorkEnvelope{{Work: domain.NetworkWork{
		ID: "stale-work", Kind: domain.NetworkWorkTest, AgentID: "quote", BackendID: "missing",
		WorkerInstanceID: "worker-1", Generation: 1, Mode: domain.NetworkInherit, State: "claimed",
	}}}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- runner.Run(ctx) }()
	waitSignal(t, client.heartbeatHit, "initial heartbeat")
	waitSignal(t, client.networkAckHit, "stale network work acknowledgement")
	drainSignals(client.heartbeatHit)
	client.enqueueCommand(domain.WorkerCommand{
		ID: "command-health", WorkerInstanceID: "worker-1", Generation: 1,
		Kind: domain.WorkerCommandHealthCheck, State: domain.WorkerCommandClaimed,
		RequestedBy: "human-owner", IdempotencyKey: "health-idem", Attempts: 1, CreatedAt: time.Now(),
	})
	waitSignal(t, client.ackHit, "control after stale network work")
	waitSignal(t, client.heartbeatHit, "heartbeat after stale network work")
	cancel()
	if err := waitWorkerExit(t, result); err != nil {
		t.Fatalf("stale work terminated valid Worker session: %v", err)
	}
}

func TestFencedNetworkWorkAckTerminatesWorkerSession(t *testing.T) {
	runner, client, _ := newRunnerFixture(t, false)
	client.networkAckErr = domain.ErrFencingRejected
	client.networkWorks = []*api.NetworkWorkEnvelope{{Work: domain.NetworkWork{
		ID: "fenced-work", Kind: domain.NetworkWorkTest, AgentID: "quote", BackendID: "missing",
		WorkerInstanceID: "worker-1", Generation: 1, Mode: domain.NetworkInherit, State: "claimed",
	}}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := runner.Run(ctx); !errors.Is(err, domain.ErrFencingRejected) {
		t.Fatalf("fencing loss error=%v", err)
	}
}

type slowHealthAdapter struct {
	openruntime.AgentRuntimeAdapter
	calls   atomic.Int32
	blocked chan struct{}
	once    sync.Once
}

func (a *slowHealthAdapter) Health(ctx context.Context) error {
	if a.calls.Add(1) == 1 {
		return nil
	}
	a.once.Do(func() { close(a.blocked) })
	<-ctx.Done()
	return ctx.Err()
}

type probeBlockingAdapter struct {
	descriptor  openruntime.AdapterDescriptor
	blocked     chan struct{}
	release     chan struct{}
	blockHealth bool
	once        sync.Once
}

func (a *probeBlockingAdapter) Descriptor(context.Context) (openruntime.AdapterDescriptor, error) {
	return a.descriptor, nil
}
func (a *probeBlockingAdapter) Validate(context.Context, domain.ExecutionSpec) error { return nil }
func (a *probeBlockingAdapter) Health(ctx context.Context) error {
	if !a.blockHealth {
		return nil
	}
	a.once.Do(func() { close(a.blocked) })
	select {
	case <-a.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (a *probeBlockingAdapter) StartTurn(context.Context, openruntime.TurnRequest, openruntime.EventSink) (openruntime.TurnHandle, error) {
	return nil, errors.New("unused")
}
func (a *probeBlockingAdapter) CloneForNetworkProbe(policy domain.NetworkPolicy) (openruntime.AgentRuntimeAdapter, error) {
	clone := *a
	clone.blockHealth = true
	return &clone, nil
}
func (a *probeBlockingAdapter) ApplyNetworkPolicy(domain.NetworkPolicy) error { return nil }

func runnerTestDescriptor(networkModes []string) openruntime.AdapterDescriptor {
	return openruntime.AdapterDescriptor{
		AdapterID: "fake", BackendType: "fake", Version: "1", LaunchProtocol: "inproc",
		Models: []string{"model-1"}, ReasoningModes: []domain.ReasoningMode{domain.ReasoningBackendDefault},
		SessionModes: []domain.SessionMode{domain.SessionModeNew}, Steer: openruntime.SteerNative,
		Approval: openruntime.ApprovalNative, Cancel: openruntime.CancelNative,
		BackendOptionsJSON: json.RawMessage(`{"type":"object"}`), MaxConcurrency: 1,
		NetworkModes: networkModes, RuntimeIdentity: domain.RuntimeIdentity{AdapterID: "fake", AdapterVersion: "1"},
	}
}

func newTestRunner(t *testing.T, client *fakeWorkerClient, pool *BackendPool, enableControl bool) *Runner {
	t.Helper()
	runner, err := NewRunner(Config{
		AgentID: "quote", WorkerInstanceID: "worker-1", Transport: domain.WorkerTransportUnix,
		Capabilities: []string{"coding"}, HeartbeatInterval: 10 * time.Millisecond,
		MailboxWait: time.Second, ControlWait: time.Second, ShutdownTimeout: time.Second,
		EnableControlLoop: enableControl,
	}, client, pool, fakePayloadResolver{})
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func TestTurnCompletionRacesAllControlOperationsWithoutDeadlockOrDuplicateFinish(t *testing.T) {
	runner, client, adapter := newRunnerFixture(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- runner.Run(ctx) }()
	waitSignal(t, client.heartbeatHit, "initial heartbeat")
	client.enqueueMailbox(workItem("mailbox-race", 1))
	handle, err := adapter.NextHandle(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var group sync.WaitGroup
	group.Add(2)
	go func() {
		defer group.Done()
		<-start
		client.enqueueMailbox(
			controlItem("mailbox-race-message", 2, domain.MailboxKindMessage, "run-1", "message-control", ""),
			controlItem("mailbox-race-approval", 3, domain.MailboxKindApproval, "run-1", "", "decision-control"),
			controlItem("mailbox-race-cancel", 4, domain.MailboxKindCancel, "run-1", "", ""),
		)
	}()
	go func() {
		defer group.Done()
		<-start
		handle.Complete(openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "race", SideEffectsKnown: true})
	}()
	close(start)
	group.Wait()
	waitSignal(t, client.finishHit, "racing turn finish")
	waitN(t, client.acceptHit, 3, "racing control settlement")
	cancel()
	if err := waitWorkerExit(t, result); err != nil {
		t.Fatalf("Worker race run failed: %v", err)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.finishes) != 1 {
		t.Fatalf("finish count=%d want=1", len(client.finishes))
	}
	if len(client.accepts) != 3 {
		t.Fatalf("control settlement count=%d want=3", len(client.accepts))
	}
	for _, accepted := range client.accepts {
		if accepted.request.Outcome != domain.MailboxStateAccepted && accepted.request.Outcome != domain.MailboxStateSuperseded {
			t.Fatalf("race produced invalid control outcome: %+v", accepted)
		}
	}
}

func TestControlledStopCancelsAndReconcilesBeforeAcknowledgement(t *testing.T) {
	runner, client, adapter := newRunnerFixture(t, true)
	result := make(chan error, 1)
	go func() { result <- runner.Run(context.Background()) }()
	waitSignal(t, client.heartbeatHit, "initial heartbeat")
	client.enqueueMailbox(workItem("mailbox-stop", 1))
	handle, err := adapter.NextHandle(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	client.enqueueCommand(domain.WorkerCommand{
		ID: "command-stop", WorkerInstanceID: "worker-1", Generation: 1,
		Kind: domain.WorkerCommandStop, State: domain.WorkerCommandClaimed,
		RequestedBy: "human-owner", IdempotencyKey: "stop-idem", Attempts: 1, CreatedAt: time.Now(),
	})
	if err := handle.NextCancel(testContext(t)); err != nil {
		t.Fatal(err)
	}
	waitSignal(t, client.finishHit, "shutdown reconciliation")
	if err := waitWorkerExit(t, result); err != nil {
		t.Fatalf("controlled stop must return success: %v", err)
	}
	waitSignal(t, client.ackHit, "stop acknowledgement")
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.commandAcks) != 1 || client.commandAcks[0].request.State != domain.WorkerCommandApplied {
		t.Fatalf("stop acknowledgements=%+v", client.commandAcks)
	}
	if len(client.releases) != 1 || client.commandAcks[0].request.FencingToken != 2 {
		t.Fatalf("release=%+v ack=%+v", client.releases, client.commandAcks)
	}
	if len(client.eventLog) < 2 || client.eventLog[len(client.eventLog)-2] != "release" || client.eventLog[len(client.eventLog)-1] != "released-ack" {
		t.Fatalf("stop release/ack order=%v", client.eventLog)
	}
	if len(client.finishes) != 1 || client.finishes[0].request.Result.Status != openruntime.TurnResultUncertain {
		t.Fatalf("shutdown reconciliation=%+v", client.finishes)
	}
}

func TestRunManagerPreservesAdapterDiagnosticWhenWaitReturnsError(t *testing.T) {
	client := newFakeWorkerClient()
	manager := &ActiveRunManager{client: client, session: api.WorkerSession{Worker: domain.WorkerInstance{
		ID: "worker-1", Generation: 1, FencingToken: 1,
	}}}
	active := &activeTurn{request: openruntime.TurnRequest{
		Task:       domain.Task{ID: "task-1", Version: 2},
		RunAttempt: domain.RunAttempt{ID: "run-1", Version: 1},
	}}
	diagnostic := "AGY stream-json ended without a terminal event; AGY stderr: provider unavailable"
	err := manager.finish(context.Background(), active, waitResult{
		runID:  "run-1",
		result: openruntime.TurnResult{Status: openruntime.TurnResultUncertain, Error: diagnostic},
		err:    errors.New("AGY stream parse failed"),
	})
	if err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.finishes) != 1 || client.finishes[0].request.Result.Error != diagnostic ||
		client.finishes[0].request.Result.Status != openruntime.TurnResultUncertain || client.finishes[0].request.Result.SideEffectsKnown {
		t.Fatalf("finish=%+v", client.finishes)
	}
}

func TestDrainStopsNewWorkWhileMailboxPumpKeepsRunningAtZeroCapacity(t *testing.T) {
	runner, client, adapter := newRunnerFixture(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- runner.Run(ctx) }()
	waitSignal(t, client.heartbeatHit, "initial heartbeat")
	client.enqueueCommand(domain.WorkerCommand{
		ID: "command-drain", WorkerInstanceID: "worker-1", Generation: 1,
		Kind: domain.WorkerCommandDrain, State: domain.WorkerCommandClaimed,
		RequestedBy: "human-owner", IdempotencyKey: "drain-idem", Attempts: 1, CreatedAt: time.Now(),
	})
	waitSignal(t, client.ackHit, "drain acknowledgement")
	client.enqueueMailbox(workItem("mailbox-after-drain", 1))
	shortContext, shortCancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer shortCancel()
	if _, err := adapter.NextHandle(shortContext); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("draining Worker unexpectedly started new work: %v", err)
	}
	cancel()
	if err := waitWorkerExit(t, result); err != nil {
		t.Fatalf("stop drained Worker: %v", err)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	foundDrainingHeartbeat, foundZeroCapacity := false, false
	for _, heartbeat := range client.heartbeats {
		foundDrainingHeartbeat = foundDrainingHeartbeat || heartbeat.Status == domain.WorkerStatusDraining
	}
	for _, claim := range client.claims {
		foundZeroCapacity = foundZeroCapacity || claim.WorkCapacity == 0
	}
	if !foundDrainingHeartbeat || !foundZeroCapacity {
		t.Fatalf("draining heartbeat=%v zero capacity=%v", foundDrainingHeartbeat, foundZeroCapacity)
	}
}

func TestLeaseLossIsFatalAndReturnsNonZeroSemantics(t *testing.T) {
	runner, client, _ := newRunnerFixture(t, false)
	client.heartbeatFail = 2
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := runner.Run(ctx)
	if !errors.Is(err, domain.ErrLeaseExpired) {
		t.Fatalf("Worker lease loss error=%v", err)
	}
}

func TestUnavailableBackendStillRegistersAndKeepsControlConnection(t *testing.T) {
	runner, client, adapter := newRunnerFixture(t, false)
	adapter.SetHealthError(errors.New("proxy credential unavailable: token=must-not-leak"))
	client.enqueueMailbox(workItem("queued-while-unavailable", 1))
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- runner.Run(ctx) }()
	waitSignal(t, client.heartbeatHit, "degraded initial heartbeat")
	client.mu.Lock()
	registered := client.registerRequest
	heartbeats := append([]api.HeartbeatRequest(nil), client.heartbeats...)
	client.mu.Unlock()
	if len(registered.Backends) != 1 || registered.Backends[0].Health != openruntime.BackendUnavailable {
		t.Fatalf("unavailable Backend was not registered: %+v", registered.Backends)
	}
	if len(heartbeats) == 0 || heartbeats[0].Status != domain.WorkerStatusDegraded {
		t.Fatalf("unavailable Backend heartbeat=%+v", heartbeats)
	}
	shortContext, shortCancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	if _, err := adapter.NextHandle(shortContext); !errors.Is(err, context.DeadlineExceeded) {
		shortCancel()
		t.Fatalf("unavailable Backend claimed queued Task: %v", err)
	}
	shortCancel()
	adapter.SetHealthError(nil)
	handle, err := adapter.NextHandle(testContext(t))
	if err != nil {
		t.Fatalf("recovered Backend did not claim queued Task: %v", err)
	}
	handle.Complete(openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "recovered", SideEffectsKnown: true})
	waitSignal(t, client.finishHit, "recovered Backend turn finish")
	cancel()
	if err := waitWorkerExit(t, result); err != nil {
		t.Fatalf("degraded Worker did not keep a controllable lifecycle: %v", err)
	}
}

func workItem(id string, sequence int64) domain.MailboxItem {
	return domain.MailboxItem{
		ID: id, Sequence: sequence, TargetAgentID: "quote", Kind: domain.MailboxKindTask,
		Lane: domain.MailboxLaneWork, TaskID: "task-" + id, State: domain.MailboxStateClaimed,
		WorkerInstanceID: "worker-1", FencingToken: 1, Attempts: 1, CreatedAt: time.Now(),
	}
}

func controlItem(id string, sequence int64, kind domain.MailboxKind, runID string, messageID string, decisionID string) domain.MailboxItem {
	return domain.MailboxItem{
		ID: id, Sequence: sequence, TargetAgentID: "quote", Kind: kind, Lane: domain.MailboxLaneControl,
		TaskID: "task-mailbox-work-1", MessageID: messageID, ApprovalDecisionID: decisionID,
		TargetRunID: runID, ExpectedRunVersion: 1, State: domain.MailboxStateClaimed,
		WorkerInstanceID: "worker-1", FencingToken: 1, Attempts: 1, CreatedAt: time.Now(),
	}
}

func signal(channel chan struct{}) {
	select {
	case channel <- struct{}{}:
	default:
	}
}

func drainSignals(channel <-chan struct{}) {
	for {
		select {
		case <-channel:
		default:
			return
		}
	}
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func waitSignal(t *testing.T, channel <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-channel:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func waitN(t *testing.T, channel <-chan struct{}, count int, description string) {
	t.Helper()
	for index := 0; index < count; index++ {
		waitSignal(t, channel, description)
	}
}

func waitWorkerExit(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Worker exit")
		return nil
	}
}

var _ api.WorkerControlClient = (*fakeWorkerClient)(nil)
