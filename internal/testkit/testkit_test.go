package testkit_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
	"openagentx/internal/testkit"
)

func descriptor() openruntime.AdapterDescriptor {
	return openruntime.AdapterDescriptor{
		AdapterID: "fake", BackendType: "fake", Version: "1", LaunchProtocol: "inproc",
		Models: []string{"fake-model"}, ReasoningModes: []domain.ReasoningMode{domain.ReasoningEffort},
		SessionModes: []domain.SessionMode{domain.SessionModeNew}, Steer: openruntime.SteerNative,
		Approval: openruntime.ApprovalNative, Cancel: openruntime.CancelNative,
		BackendOptionsJSON: json.RawMessage(`{"type":"object"}`), MaxConcurrency: 1,
	}
}

func TestFakeClockAndDroppedBrokerWakeup(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)
	clock := testkit.NewFakeClock(start)
	timer := clock.After(time.Minute)
	clock.Advance(59 * time.Second)
	select {
	case <-timer:
		t.Fatal("timer fired too early")
	default:
	}
	clock.Advance(time.Second)
	if fired := <-timer; !fired.Equal(start.Add(time.Minute)) {
		t.Fatalf("timer fired at %s", fired)
	}

	broker := testkit.NewFakeBroker()
	wakeup, unsubscribe := broker.Subscribe("agent:quote")
	defer unsubscribe()
	broker.SetDropWakeups(true)
	if delivered := broker.Publish("agent:quote"); delivered != 0 {
		t.Fatalf("dropped publish delivered %d wakeups", delivered)
	}
	select {
	case <-wakeup:
		t.Fatal("dropped wakeup reached subscriber")
	default:
	}
	broker.SetDropWakeups(false)
	if delivered := broker.Publish("agent:quote"); delivered != 1 {
		t.Fatalf("publish delivered %d wakeups", delivered)
	}
}

func TestBarrierCoordinatesConcurrentActors(t *testing.T) {
	t.Parallel()
	barrier := testkit.NewBarrier(2)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var group sync.WaitGroup
	group.Add(2)
	errorsCh := make(chan error, 2)
	for range 2 {
		go func() {
			defer group.Done()
			errorsCh <- barrier.ArriveAndWait(ctx)
		}()
	}
	group.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatalf("barrier failed: %v", err)
		}
	}
}

func TestFakeAdapterSimulatesControlCompletionAndCrash(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	adapter := testkit.NewFakeAdapter(descriptor())
	handle, err := adapter.StartTurn(ctx, openruntime.TurnRequest{}, openruntime.EventSinkFunc(func(context.Context, openruntime.RuntimeEvent) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	started, err := adapter.NextStartedTurn(ctx)
	if err != nil || started.Handle != handle {
		t.Fatalf("started turn mismatch: err=%v", err)
	}

	message := domain.Message{ID: "message-1", TaskID: "task-1", SenderAgentID: "sender", Kind: domain.MessageKindSupplement, Content: "more"}
	if err := handle.Steer(ctx, message); err != nil {
		t.Fatal(err)
	}
	decision := domain.ApprovalDecision{ID: "decision-1", ApprovalRequestID: "approval-1", DecidedBy: "human-1", Decision: domain.ApprovalDecisionApprove, State: domain.ApprovalDecisionPersisted, IdempotencyKey: "idem-1"}
	if err := handle.DecideApproval(ctx, decision); err != nil {
		t.Fatal(err)
	}
	if err := handle.RequestCancel(ctx); err != nil {
		t.Fatal(err)
	}

	fakeHandle := handle.(*testkit.FakeTurnHandle)
	select {
	case got := <-fakeHandle.Steers():
		if got.ID != message.ID {
			t.Fatalf("steer ID = %s", got.ID)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case <-fakeHandle.Approvals():
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case <-fakeHandle.Cancels():
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	fakeHandle.Complete(openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "ok", SideEffectsKnown: true})
	result, err := handle.Wait(ctx)
	if err != nil || result.Status != openruntime.TurnResultSucceeded {
		t.Fatalf("turn completion = %+v, %v", result, err)
	}

	crashHandle := testkit.NewFakeTurnHandle(descriptor())
	crashHandle.Crash(openruntime.ErrBackendCrashed)
	if _, err := crashHandle.Wait(ctx); !errors.Is(err, openruntime.ErrBackendCrashed) {
		t.Fatalf("crash error = %v", err)
	}
}

func TestTargetSQLiteHelper(t *testing.T) {
	t.Parallel()
	db := testkit.OpenTargetSQLite(t)
	var version int
	if err := db.QueryRow("SELECT version FROM schema_meta WHERE singleton=1").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("schema version = %d", version)
	}
}
