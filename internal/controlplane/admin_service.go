package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"openagentx/internal/api"
	"openagentx/internal/domain"
)

type WorkerAdminState interface {
	CreateWorkerCommand(context.Context, *domain.WorkerCommand, *domain.JournalEvent) (*domain.WorkerCommand, error)
	RevokeWorkerLease(context.Context, string, int64, *domain.JournalEvent) (*domain.WorkerInstance, error)
}

type WorkerAdminService struct {
	state  WorkerAdminState
	broker WakeupBroker
	now    func() time.Time
	newID  func(string) string
}

func NewWorkerAdminService(state WorkerAdminState, broker WakeupBroker, now func() time.Time, newID func(string) string) (*WorkerAdminService, error) {
	if state == nil {
		return nil, fmt.Errorf("Worker admin state is required")
	}
	if broker == nil {
		return nil, fmt.Errorf("shared Worker wakeup Broker is required")
	}
	if now == nil {
		now = time.Now
	}
	if newID == nil {
		newID = func(prefix string) string { return prefix + "-" + fmt.Sprint(now().UnixNano()) }
	}
	return &WorkerAdminService{state: state, broker: broker, now: now, newID: newID}, nil
}

func (s *WorkerAdminService) Command(ctx context.Context, principalID, workerID string, kind domain.WorkerCommandKind, request api.WorkerAdminRequest) (*domain.WorkerCommand, error) {
	if !kind.Valid() {
		return nil, domain.ErrInvalidInput("unsupported Worker command kind")
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if err := domain.ValidateOpaqueID("principal_id", principalID); err != nil {
		return nil, err
	}
	if err := domain.ValidateOpaqueID("worker_instance_id", workerID); err != nil {
		return nil, err
	}
	c := &domain.WorkerCommand{ID: s.newID("worker-command"), WorkerInstanceID: workerID, Generation: request.ExpectedGeneration, Kind: kind, RequestedBy: principalID, IdempotencyKey: request.Meta.IdempotencyKey, State: domain.WorkerCommandPending, CreatedAt: s.now().UTC()}
	payload, _ := json.Marshal(map[string]any{"kind": kind, "worker_instance_id": workerID, "generation": request.ExpectedGeneration})
	e := &domain.JournalEvent{ID: s.newID("event-worker-command"), AggregateType: "worker_command", AggregateID: c.ID, EventType: "worker_command.created", ActorPrincipalID: principalID, Payload: payload, CreatedAt: s.now().UTC()}
	created, err := s.state.CreateWorkerCommand(ctx, c, e)
	if err != nil {
		return nil, err
	}
	s.broker.Publish(WorkerControlTopic(workerID))
	return created, nil
}

func (s *WorkerAdminService) Revoke(ctx context.Context, principalID, workerID string, generation int64) (*domain.WorkerInstance, error) {
	if err := domain.ValidateOpaqueID("principal_id", principalID); err != nil {
		return nil, err
	}
	if err := domain.ValidateOpaqueID("worker_instance_id", workerID); err != nil {
		return nil, err
	}
	if generation <= 0 {
		return nil, domain.ErrInvalidInput("generation must be positive")
	}
	e := &domain.JournalEvent{ID: s.newID("event-worker-revoke"), AggregateType: "worker_instance", AggregateID: workerID, EventType: "worker.lease_revoked", ActorPrincipalID: principalID, Payload: json.RawMessage(`{}`), CreatedAt: s.now().UTC()}
	worker, err := s.state.RevokeWorkerLease(ctx, workerID, generation, e)
	if err != nil {
		return nil, err
	}
	s.broker.Publish(WorkerControlTopic(workerID))
	return worker, nil
}
