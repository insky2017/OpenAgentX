package testkit

import (
	"context"
	"sync"

	"agentbus/internal/domain"
	openruntime "agentbus/internal/runtime"
)

type turnOutcome struct {
	result openruntime.TurnResult
	err    error
}

type FakeTurnHandle struct {
	descriptor openruntime.AdapterDescriptor
	outcome    chan turnOutcome
	once       sync.Once

	mu        sync.Mutex
	messages  []domain.Message
	decisions []domain.ApprovalDecision
	cancels   int

	steerCh    chan domain.Message
	approvalCh chan domain.ApprovalDecision
	cancelCh   chan struct{}
}

func NewFakeTurnHandle(descriptor openruntime.AdapterDescriptor) *FakeTurnHandle {
	return &FakeTurnHandle{
		descriptor: descriptor,
		outcome:    make(chan turnOutcome, 1),
		steerCh:    make(chan domain.Message, 16),
		approvalCh: make(chan domain.ApprovalDecision, 16),
		cancelCh:   make(chan struct{}, 16),
	}
}

func (h *FakeTurnHandle) Wait(ctx context.Context) (openruntime.TurnResult, error) {
	select {
	case outcome := <-h.outcome:
		return outcome.result, outcome.err
	case <-ctx.Done():
		return openruntime.TurnResult{}, ctx.Err()
	}
}

func (h *FakeTurnHandle) Steer(_ context.Context, message domain.Message) error {
	if h.descriptor.Steer != openruntime.SteerNative {
		return openruntime.ErrSteerUnsupported
	}
	h.mu.Lock()
	h.messages = append(h.messages, message)
	h.mu.Unlock()
	h.steerCh <- message
	return nil
}

func (h *FakeTurnHandle) DecideApproval(_ context.Context, decision domain.ApprovalDecision) error {
	if h.descriptor.Approval != openruntime.ApprovalNative {
		return openruntime.ErrApprovalUnsupported
	}
	h.mu.Lock()
	h.decisions = append(h.decisions, decision)
	h.mu.Unlock()
	h.approvalCh <- decision
	return nil
}

func (h *FakeTurnHandle) RequestCancel(context.Context) error {
	if h.descriptor.Cancel == openruntime.CancelUnsupported {
		return openruntime.ErrCancelUnsupported
	}
	h.mu.Lock()
	h.cancels++
	h.mu.Unlock()
	h.cancelCh <- struct{}{}
	return nil
}

func (h *FakeTurnHandle) Complete(result openruntime.TurnResult) {
	h.once.Do(func() { h.outcome <- turnOutcome{result: result} })
}

func (h *FakeTurnHandle) Crash(err error) {
	h.once.Do(func() { h.outcome <- turnOutcome{err: err} })
}

func (h *FakeTurnHandle) Steers() <-chan domain.Message {
	return h.steerCh
}

func (h *FakeTurnHandle) Approvals() <-chan domain.ApprovalDecision {
	return h.approvalCh
}

func (h *FakeTurnHandle) Cancels() <-chan struct{} {
	return h.cancelCh
}

type StartedTurn struct {
	Request openruntime.TurnRequest
	Sink    openruntime.EventSink
	Handle  *FakeTurnHandle
}

type FakeAdapter struct {
	mu          sync.Mutex
	descriptor  openruntime.AdapterDescriptor
	healthError error
	startError  error
	started     chan StartedTurn
}

func NewFakeAdapter(descriptor openruntime.AdapterDescriptor) *FakeAdapter {
	return &FakeAdapter{descriptor: descriptor, started: make(chan StartedTurn, 16)}
}

func (a *FakeAdapter) Descriptor(context.Context) (openruntime.AdapterDescriptor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.descriptor, nil
}

func (a *FakeAdapter) Validate(_ context.Context, spec domain.ExecutionSpec) error {
	return spec.ValidateShape()
}

func (a *FakeAdapter) Health(context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.healthError
}

func (a *FakeAdapter) StartTurn(_ context.Context, request openruntime.TurnRequest, sink openruntime.EventSink) (openruntime.TurnHandle, error) {
	a.mu.Lock()
	startError := a.startError
	descriptor := a.descriptor
	a.mu.Unlock()
	if startError != nil {
		return nil, startError
	}
	handle := NewFakeTurnHandle(descriptor)
	a.started <- StartedTurn{Request: request, Sink: sink, Handle: handle}
	return handle, nil
}

func (a *FakeAdapter) SetHealthError(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.healthError = err
}

func (a *FakeAdapter) SetStartError(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.startError = err
}

func (a *FakeAdapter) NextStartedTurn(ctx context.Context) (StartedTurn, error) {
	select {
	case turn := <-a.started:
		return turn, nil
	case <-ctx.Done():
		return StartedTurn{}, ctx.Err()
	}
}

var _ openruntime.AgentRuntimeAdapter = (*FakeAdapter)(nil)
var _ openruntime.TurnHandle = (*FakeTurnHandle)(nil)
