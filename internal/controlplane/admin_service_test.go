package controlplane

import (
	"context"
	"errors"
	"testing"
	"time"

	"openagentx/internal/api"
	"openagentx/internal/domain"
)

type workerAdminStateStub struct {
	createErr error
	revokeErr error
}

func (s workerAdminStateStub) CreateWorkerCommand(_ context.Context, command *domain.WorkerCommand, _ *domain.JournalEvent) (*domain.WorkerCommand, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	return command, nil
}

func (s workerAdminStateStub) RevokeWorkerLease(_ context.Context, workerID string, generation int64, _ *domain.JournalEvent) (*domain.WorkerInstance, error) {
	if s.revokeErr != nil {
		return nil, s.revokeErr
	}
	return &domain.WorkerInstance{ID: workerID, Generation: generation}, nil
}

type recordingWakeupBroker struct {
	published []string
}

func (*recordingWakeupBroker) Subscribe(string) (<-chan struct{}, func()) {
	return make(chan struct{}), func() {}
}

func (b *recordingWakeupBroker) Publish(topic string) {
	b.published = append(b.published, topic)
}

func TestWorkerAdminServiceRequiresSharedBroker(t *testing.T) {
	if _, err := NewWorkerAdminService(workerAdminStateStub{}, nil, time.Now, nil); err == nil {
		t.Fatal("Worker Admin service accepted a missing shared Broker")
	}
}

func TestWorkerAdminServicePublishesOnlyAfterSuccessfulStateCommit(t *testing.T) {
	stateFailure := errors.New("state commit failed")
	request := api.WorkerAdminRequest{
		Meta:        api.CommandMeta{IdempotencyKey: "admin-publish", ExpectedVersion: 1},
		RequestedBy: "human-owner", ExpectedGeneration: 1,
	}
	for _, test := range []struct {
		name      string
		state     workerAdminStateStub
		operation func(*WorkerAdminService) error
		wantCount int
	}{
		{name: "command commit", operation: func(service *WorkerAdminService) error {
			_, err := service.Command(context.Background(), "human-owner", "worker-1", domain.WorkerCommandHealthCheck, request)
			return err
		}, wantCount: 1},
		{name: "command rollback", state: workerAdminStateStub{createErr: stateFailure}, operation: func(service *WorkerAdminService) error {
			_, err := service.Command(context.Background(), "human-owner", "worker-1", domain.WorkerCommandHealthCheck, request)
			return err
		}},
		{name: "revoke commit", operation: func(service *WorkerAdminService) error {
			_, err := service.Revoke(context.Background(), "human-owner", "worker-1", 1)
			return err
		}, wantCount: 1},
		{name: "revoke rollback", state: workerAdminStateStub{revokeErr: stateFailure}, operation: func(service *WorkerAdminService) error {
			_, err := service.Revoke(context.Background(), "human-owner", "worker-1", 1)
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			broker := &recordingWakeupBroker{}
			service, err := NewWorkerAdminService(test.state, broker, time.Now, func(prefix string) string { return prefix + "-test" })
			if err != nil {
				t.Fatal(err)
			}
			err = test.operation(service)
			if test.wantCount == 0 && !errors.Is(err, stateFailure) {
				t.Fatalf("operation error=%v", err)
			}
			if len(broker.published) != test.wantCount {
				t.Fatalf("published=%v want count=%d", broker.published, test.wantCount)
			}
			if test.wantCount == 1 && broker.published[0] != WorkerControlTopic("worker-1") {
				t.Fatalf("published topic=%q", broker.published[0])
			}
		})
	}
}
