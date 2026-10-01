package controlplane

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"openagentx/internal/api"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

func TestRepeatedRecoveryPreservesHeartbeatAndNeverReplaysExpiredRun(t *testing.T) {
	ctx := context.Background()
	env := newWorkerTestEnvironment(t, nil)
	if err := env.repository.CreatePrincipal(ctx, &domain.Principal{ID: "recovery-daemon", Kind: domain.PrincipalSystem, DisplayName: "Recovery", Status: domain.IdentityActive}, &domain.JournalEvent{
		ID: "event-recovery-daemon", EventType: "principal.created", ActorPrincipalID: env.ownerID, Payload: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	old := env.register(t, "worker-before-crash")
	env.heartbeat(t, old)
	created := env.createTask(t, "before-crash")
	item, err := env.service.ClaimMailbox(ctx, env.workerID, old.SessionToken, claimRequest(old, 1))
	if err != nil || item == nil {
		t.Fatalf("claim before crash: item=%+v err=%v", item, err)
	}
	begin, err := env.service.BeginAttempt(ctx, env.workerID, old.SessionToken, item.ID, api.BeginAttemptRequest{
		WorkerInstanceID: old.Worker.ID, AgentID: env.agentID, Generation: old.Worker.Generation,
		FencingToken: old.Worker.FencingToken, ExpectedItemState: domain.MailboxStateClaimed,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Run past both original Worker and Run leases while normal heartbeats
	// extend the active lease. Repeated recovery must leave the run untouched.
	for i := 0; i < 4; i++ {
		env.clock.Advance(45 * time.Second)
		env.heartbeat(t, old)
		if err := env.service.Reconcile(ctx); err != nil {
			t.Fatal(err)
		}
		run, err := env.repository.GetRunAttempt(ctx, begin.Turn.RunAttempt.ID)
		if err != nil || run.Status != begin.Turn.RunAttempt.Status || run.Version != begin.Turn.RunAttempt.Version || !run.LeaseUntil.After(env.clock.Now()) {
			t.Fatalf("healthy run was recovered: %+v err=%v", run, err)
		}
		task, err := env.repository.GetTask(ctx, created.Task.ID)
		if err != nil || task.Status != domain.TaskStatusRunning || task.Version != begin.Turn.Task.Version {
			t.Fatalf("healthy task was recovered: %+v err=%v", task, err)
		}
	}
	// Simulate a lost Worker by stopping its heartbeats; no DB mutation or
	// daemon restart is needed to make its expired execution uncertain.
	env.clock.Advance(time.Minute + time.Second)
	for i := 0; i < 3; i++ {
		if err := env.service.Reconcile(ctx); err != nil {
			t.Fatal(err)
		}
	}
	run, err := env.repository.GetRunAttempt(ctx, begin.Turn.RunAttempt.ID)
	if err != nil || run.Status != domain.RunAttemptUncertain || run.Version != begin.Turn.RunAttempt.Version+1 {
		t.Fatalf("expired run not settled once: %+v err=%v", run, err)
	}
	var result openruntime.TurnResult
	if err := json.Unmarshal([]byte(run.ResultJSON), &result); err != nil || result.Status != openruntime.TurnResultUncertain || result.SideEffectsKnown {
		t.Fatalf("recovery overstated side effects: %+v err=%v", result, err)
	}
	task, err := env.repository.GetTask(ctx, created.Task.ID)
	if err != nil || task.Status != domain.TaskStatusUncertain || task.Version != begin.Turn.Task.Version+1 {
		t.Fatalf("expired task not settled once: %+v err=%v", task, err)
	}
	mailbox, err := env.repository.GetMailboxItem(ctx, item.ID)
	if err != nil || mailbox.State != domain.MailboxStateAccepted {
		t.Fatalf("previously executed work was requeued: %+v err=%v", mailbox, err)
	}
	events, err := env.repository.ListTaskJournal(ctx, created.Task.ID, 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	uncertainEvents := 0
	for _, event := range events {
		if event.EventType == "task.uncertain" {
			uncertainEvents++
		}
	}
	if uncertainEvents != 1 {
		t.Fatalf("repeated recovery wrote %d task.uncertain events", uncertainEvents)
	}

	current := env.register(t, "worker-after-crash")
	env.heartbeat(t, current)
	if current.Worker.Generation <= old.Worker.Generation {
		t.Fatal("recovery did not allocate a new generation")
	}
	if err := env.service.AppendEvents(ctx, env.workerID, old.SessionToken, run.ID, api.EventBatch{
		WorkerInstanceID: old.Worker.ID, Generation: old.Worker.Generation, FencingToken: old.Worker.FencingToken,
		ExpectedRunVersion: begin.Turn.RunAttempt.Version,
		Events:             []openruntime.RuntimeEvent{{Type: "turn.output", Payload: json.RawMessage(`{"text":"late"}`), OccurredAt: env.clock.Now()}},
	}); err == nil {
		t.Fatal("old generation wrote a late event after recovery")
	}
	claimed, err := env.service.ClaimMailbox(ctx, env.workerID, current.SessionToken, claimRequest(current, 1))
	if err != nil || claimed != nil {
		t.Fatalf("uncertain work was automatically replayed: %+v err=%v", claimed, err)
	}
	next := env.createTask(t, "independent-after-crash")
	claimed, err = env.service.ClaimMailbox(ctx, env.workerID, current.SessionToken, claimRequest(current, 1))
	if err != nil || claimed == nil || claimed.TaskID != next.Task.ID {
		t.Fatalf("expired execution blocked independent new work: %+v err=%v", claimed, err)
	}
	if _, err := env.service.BeginAttempt(ctx, env.workerID, current.SessionToken, claimed.ID, api.BeginAttemptRequest{
		WorkerInstanceID: current.Worker.ID, AgentID: env.agentID, Generation: current.Worker.Generation,
		FencingToken: current.Worker.FencingToken, ExpectedItemState: domain.MailboxStateClaimed,
	}); err != nil {
		t.Fatalf("new generation cannot begin independent work: %v", err)
	}
}
