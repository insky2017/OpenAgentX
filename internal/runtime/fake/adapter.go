package fake

import (
	"context"
	"errors"
	"sync"

	"agentbus/internal/domain"
	openruntime "agentbus/internal/runtime"
)

type Adapter struct {
	descriptor openruntime.AdapterDescriptor

	mu        sync.RWMutex
	healthErr error
	started   chan *Handle
	auto      *openruntime.TurnResult
}

func NewAdapter(descriptor openruntime.AdapterDescriptor) (*Adapter, error) {
	if err := descriptor.Validate(); err != nil {
		return nil, err
	}
	return &Adapter{descriptor: descriptor, started: make(chan *Handle, 16)}, nil
}

func NewAutoAdapter(descriptor openruntime.AdapterDescriptor, result openruntime.TurnResult) (*Adapter, error) {
	if err := descriptor.Validate(); err != nil {
		return nil, err
	}
	if err := result.Validate(); err != nil {
		return nil, err
	}
	return &Adapter{descriptor: descriptor, auto: &result}, nil
}

func (a *Adapter) Descriptor(context.Context) (openruntime.AdapterDescriptor, error) {
	return a.descriptor, nil
}

func (a *Adapter) Validate(_ context.Context, spec domain.ExecutionSpec) error {
	if err := spec.ValidateShape(); err != nil {
		return err
	}
	if spec.AdapterID != a.descriptor.AdapterID {
		return domain.ErrUnsupportedCapability
	}
	for _, model := range a.descriptor.Models {
		if spec.Model == model {
			return nil
		}
	}
	return domain.ErrUnsupportedCapability
}

func (a *Adapter) Health(context.Context) error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.healthErr
}

func (a *Adapter) SetHealthError(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.healthErr = err
}

func (a *Adapter) StartTurn(ctx context.Context, request openruntime.TurnRequest, sink openruntime.EventSink) (openruntime.TurnHandle, error) {
	handle := &Handle{
		request: request, sink: sink, result: make(chan completion, 1),
		steers: make(chan domain.Message, 16), approvals: make(chan domain.ApprovalDecision, 16),
		cancels: make(chan struct{}, 16),
	}
	if a.started != nil {
		select {
		case a.started <- handle:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if a.auto != nil {
		result := *a.auto
		go handle.Complete(result)
	}
	return handle, nil
}

func (a *Adapter) NextHandle(ctx context.Context) (*Handle, error) {
	if a.started == nil {
		return nil, errors.New("auto fake Adapter does not expose handles")
	}
	select {
	case handle := <-a.started:
		return handle, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type completion struct {
	result openruntime.TurnResult
	err    error
}

type Handle struct {
	request   openruntime.TurnRequest
	sink      openruntime.EventSink
	result    chan completion
	steers    chan domain.Message
	approvals chan domain.ApprovalDecision
	cancels   chan struct{}
	once      sync.Once
}

func (h *Handle) Request() openruntime.TurnRequest { return h.request }

func (h *Handle) Wait(ctx context.Context) (openruntime.TurnResult, error) {
	select {
	case completion := <-h.result:
		return completion.result, completion.err
	case <-ctx.Done():
		return openruntime.TurnResult{}, ctx.Err()
	}
}

func (h *Handle) Steer(ctx context.Context, message domain.Message) error {
	select {
	case h.steers <- message:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Handle) DecideApproval(ctx context.Context, decision domain.ApprovalDecision) error {
	select {
	case h.approvals <- decision:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Handle) RequestCancel(ctx context.Context) error {
	select {
	case h.cancels <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Handle) Emit(ctx context.Context, event openruntime.RuntimeEvent) error {
	if h.sink == nil {
		return errors.New("fake Runtime EventSink is unavailable")
	}
	return h.sink.Emit(ctx, event)
}

func (h *Handle) Complete(result openruntime.TurnResult) {
	h.once.Do(func() { h.result <- completion{result: result} })
}

func (h *Handle) Crash(err error) {
	if err == nil {
		err = openruntime.ErrBackendCrashed
	}
	h.once.Do(func() { h.result <- completion{err: err} })
}

func (h *Handle) NextSteer(ctx context.Context) (domain.Message, error) {
	select {
	case message := <-h.steers:
		return message, nil
	case <-ctx.Done():
		return domain.Message{}, ctx.Err()
	}
}

func (h *Handle) NextApproval(ctx context.Context) (domain.ApprovalDecision, error) {
	select {
	case decision := <-h.approvals:
		return decision, nil
	case <-ctx.Done():
		return domain.ApprovalDecision{}, ctx.Err()
	}
}

func (h *Handle) NextCancel(ctx context.Context) error {
	select {
	case <-h.cancels:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

var _ openruntime.AgentRuntimeAdapter = (*Adapter)(nil)
var _ openruntime.TurnHandle = (*Handle)(nil)
