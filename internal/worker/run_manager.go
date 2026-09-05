package worker

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"openagentx/internal/api"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

type ControlPayloadResolver interface {
	ResolveMessage(context.Context, domain.MailboxItem) (domain.Message, error)
	ResolveApprovalDecision(context.Context, domain.MailboxItem) (domain.ApprovalDecision, error)
}

type waitResult struct {
	runID  string
	result openruntime.TurnResult
	err    error
}

type mailboxSubmission struct {
	item domain.MailboxItem
	done chan error
}

type activeTurn struct {
	request    openruntime.TurnRequest
	handle     openruntime.TurnHandle
	descriptor openruntime.AdapterDescriptor
}

type ActiveRunManager struct {
	client   api.WorkerControlClient
	backends *BackendPool
	resolver ControlPayloadResolver
	session  api.WorkerSession

	items       chan mailboxSubmission
	completions chan waitResult
	active      atomic.Bool
	capacity    chan struct{}
	draining    *atomic.Bool
	shutdown    time.Duration
}

func NewActiveRunManager(
	client api.WorkerControlClient,
	backends *BackendPool,
	resolver ControlPayloadResolver,
	session api.WorkerSession,
	draining *atomic.Bool,
	shutdownTimeout time.Duration,
) (*ActiveRunManager, error) {
	if client == nil || backends == nil || draining == nil {
		return nil, fmt.Errorf("Worker client, Backend pool, and draining state are required")
	}
	if shutdownTimeout <= 0 {
		return nil, fmt.Errorf("Worker shutdown timeout must be positive")
	}
	return &ActiveRunManager{
		client: client, backends: backends, resolver: resolver, session: session,
		items: make(chan mailboxSubmission), completions: make(chan waitResult, 1), capacity: make(chan struct{}, 1),
		draining: draining, shutdown: shutdownTimeout,
	}, nil
}

func (m *ActiveRunManager) CapacityAvailable() <-chan struct{} { return m.capacity }

func (m *ActiveRunManager) WorkCapacity() int {
	if m.draining.Load() || m.active.Load() || !m.backends.HasAvailableBackend() {
		return 0
	}
	return 1
}

func (m *ActiveRunManager) Submit(ctx context.Context, item domain.MailboxItem) error {
	submission := mailboxSubmission{item: item, done: make(chan error, 1)}
	select {
	case m.items <- submission:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-submission.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *ActiveRunManager) Run(ctx context.Context) error {
	var active *activeTurn
	for {
		select {
		case <-ctx.Done():
			return m.shutdownActive(active)
		case submission := <-m.items:
			var err error
			if submission.item.Lane == domain.MailboxLaneWork {
				active, err = m.startWork(ctx, active, submission.item)
			} else {
				err = m.applyControl(ctx, active, submission.item)
			}
			submission.done <- err
			if err != nil {
				return err
			}
		case completion := <-m.completions:
			if active == nil || completion.runID != active.request.RunAttempt.ID {
				continue
			}
			if err := m.finish(ctx, active, completion); err != nil {
				return err
			}
			active = nil
			m.releaseCapacity()
		}
	}
}

func (m *ActiveRunManager) startWork(ctx context.Context, active *activeTurn, item domain.MailboxItem) (*activeTurn, error) {
	if m.draining.Load() {
		return active, nil
	}
	if active != nil || !m.active.CompareAndSwap(false, true) {
		return active, fmt.Errorf("Run Manager received work without capacity")
	}
	request := api.BeginAttemptRequest{
		WorkerInstanceID: m.session.Worker.ID, AgentID: m.session.Worker.AgentID,
		Generation: m.session.Worker.Generation, FencingToken: m.session.Worker.FencingToken,
		ExpectedItemState: domain.MailboxStateClaimed,
	}
	begin, err := m.client.BeginAttempt(ctx, item.ID, request)
	if err != nil {
		m.releaseCapacity()
		if errors.Is(err, domain.ErrUnsupportedCapability) {
			return nil, nil
		}
		return nil, fmt.Errorf("begin RunAttempt: %w", err)
	}
	if begin.Turn.Execution.Spec.Network.IsZero() {
		return m.failStartedRun(ctx, begin.Turn, domain.ErrUnsupportedCapability)
	}
	adapter, err := m.backends.Resolve(ctx, begin.Turn.Execution.Spec.AdapterID, begin.Turn.Execution.Spec.BackendID)
	if err != nil {
		return m.failStartedRun(ctx, begin.Turn, err)
	}
	if err := adapter.Validate(ctx, begin.Turn.Execution.Spec); err != nil {
		return m.failStartedRun(ctx, begin.Turn, err)
	}
	descriptor, err := adapter.Descriptor(ctx)
	if err != nil {
		return m.failStartedRun(ctx, begin.Turn, err)
	}
	sink := openruntime.EventSinkFunc(func(eventContext context.Context, event openruntime.RuntimeEvent) error {
		return m.client.AppendRunEvents(eventContext, begin.Turn.RunAttempt.ID, api.EventBatch{
			WorkerInstanceID: m.session.Worker.ID, Generation: m.session.Worker.Generation,
			FencingToken:       m.session.Worker.FencingToken,
			ExpectedRunVersion: begin.Turn.RunAttempt.Version, Events: []openruntime.RuntimeEvent{event},
		})
	})
	handle, err := adapter.StartTurn(ctx, begin.Turn, sink)
	if err != nil {
		return m.failStartedRun(ctx, begin.Turn, err)
	}
	started := &activeTurn{request: begin.Turn, handle: handle, descriptor: descriptor}
	go func() {
		result, waitErr := handle.Wait(ctx)
		m.completions <- waitResult{runID: begin.Turn.RunAttempt.ID, result: result, err: waitErr}
	}()
	return started, nil
}

func (m *ActiveRunManager) failStartedRun(ctx context.Context, request openruntime.TurnRequest, cause error) (*activeTurn, error) {
	result := openruntime.TurnResult{
		Status: openruntime.TurnResultUncertain, Error: "Runtime Backend could not establish a controlled turn",
		SideEffectsKnown: false,
	}
	finishErr := m.client.FinishRun(ctx, request.RunAttempt.ID, api.FinishRunRequest{
		WorkerInstanceID: m.session.Worker.ID, Generation: m.session.Worker.Generation,
		FencingToken: m.session.Worker.FencingToken, ExpectedTaskVersion: request.Task.Version,
		ExpectedRunVersion: request.RunAttempt.Version, Result: result,
	})
	m.releaseCapacity()
	if finishErr != nil {
		return nil, errors.Join(fmt.Errorf("start Runtime Backend: %w", cause), fmt.Errorf("reconcile failed start: %w", finishErr))
	}
	return nil, nil
}

func (m *ActiveRunManager) releaseCapacity() {
	m.active.Store(false)
	select {
	case m.capacity <- struct{}{}:
	default:
	}
}

func (m *ActiveRunManager) applyControl(ctx context.Context, active *activeTurn, item domain.MailboxItem) error {
	outcome := domain.MailboxStateAccepted
	result := "applied"
	if active == nil || item.TargetRunID != active.request.RunAttempt.ID ||
		item.ExpectedRunVersion != active.request.RunAttempt.Version {
		outcome, result = domain.MailboxStateSuperseded, "target RunAttempt is no longer active"
	} else {
		var err error
		switch item.Kind {
		case domain.MailboxKindMessage:
			if active.descriptor.Steer != openruntime.SteerNative || m.resolver == nil {
				err = openruntime.ErrSteerUnsupported
			} else {
				var message domain.Message
				message, err = m.resolver.ResolveMessage(ctx, item)
				if err == nil {
					err = active.handle.Steer(ctx, message)
				}
			}
		case domain.MailboxKindApproval:
			if active.descriptor.Approval != openruntime.ApprovalNative || m.resolver == nil {
				err = openruntime.ErrApprovalUnsupported
			} else {
				var decision domain.ApprovalDecision
				decision, err = m.resolver.ResolveApprovalDecision(ctx, item)
				if err == nil {
					err = active.handle.DecideApproval(ctx, decision)
				}
			}
		case domain.MailboxKindCancel:
			if active.descriptor.Cancel == openruntime.CancelUnsupported {
				err = openruntime.ErrCancelUnsupported
			} else {
				err = active.handle.RequestCancel(ctx)
			}
		default:
			err = fmt.Errorf("unsupported control Mailbox kind")
		}
		if err != nil {
			outcome, result = domain.MailboxStateFailed, "Runtime Backend rejected control input"
		}
	}
	return m.client.AcceptMailboxItem(ctx, item.ID, api.AcceptRequest{
		WorkerInstanceID: m.session.Worker.ID, Generation: m.session.Worker.Generation,
		FencingToken: m.session.Worker.FencingToken, ExpectedItemState: domain.MailboxStateClaimed,
		Outcome: outcome, Result: result,
	})
}

func (m *ActiveRunManager) finish(ctx context.Context, active *activeTurn, completion waitResult) error {
	result := completion.result
	if completion.err != nil {
		result.Status = openruntime.TurnResultUncertain
		result.SideEffectsKnown = false
		if result.Error == "" {
			result.Error = "Runtime Backend ended without a verifiable result"
		}
	}
	if err := result.Validate(); err != nil {
		result = openruntime.TurnResult{
			Status: openruntime.TurnResultUncertain, Error: "Runtime Backend returned an invalid result",
			SideEffectsKnown: false,
		}
	}
	return m.client.FinishRun(ctx, active.request.RunAttempt.ID, api.FinishRunRequest{
		WorkerInstanceID: m.session.Worker.ID, Generation: m.session.Worker.Generation,
		FencingToken:        m.session.Worker.FencingToken,
		ExpectedTaskVersion: active.request.Task.Version,
		ExpectedRunVersion:  active.request.RunAttempt.Version, Result: result,
	})
}

func (m *ActiveRunManager) shutdownActive(active *activeTurn) error {
	if active == nil {
		return nil
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), m.shutdown)
	defer cancel()
	_ = active.handle.RequestCancel(shutdownContext)
	select {
	case completion := <-m.completions:
		if completion.runID != active.request.RunAttempt.ID {
			completion = waitResult{runID: active.request.RunAttempt.ID, err: context.Canceled}
		}
		return m.finish(shutdownContext, active, completion)
	case <-shutdownContext.Done():
		finishContext, finishCancel := context.WithTimeout(context.Background(), m.shutdown)
		defer finishCancel()
		return m.finish(finishContext, active, waitResult{
			runID: active.request.RunAttempt.ID, err: context.DeadlineExceeded,
		})
	}
}
