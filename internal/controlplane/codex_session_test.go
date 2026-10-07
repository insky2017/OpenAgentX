package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"openagentx/internal/api"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodexBeginFreezesProfileRoleCwdTimeoutAndNextRun(t *testing.T) {
	e := newWorkerTestEnvironment(t, nil)
	dir := t.TempDir()
	role := filepath.Join(dir, "role.md")
	if err := os.WriteFile(role, []byte("角色第一版"), 0600); err != nil {
		t.Fatal(err)
	}
	state := &agyProfileState{WorkerState: e.repository, profile: &domain.AgentProfileRecord{AgentID: e.agentID, Version: 7, InstructionsPath: role, WorkspaceRoot: dir}}
	e.service.state = state
	e.backend.Descriptor.AdapterID = "codex-app-server"
	e.backend.Descriptor.RuntimeIdentity.AdapterID = "codex-app-server"
	e.backend.Descriptor.SessionModes = []domain.SessionMode{domain.SessionModeNew, domain.SessionModeResume}
	e.backend.Descriptor.DefaultTimeout = 47 * time.Second
	session := e.register(t, "worker-codex-inputs")
	e.heartbeat(t, session)
	beginRun := func(suffix string) *api.BeginAttemptResponse {
		e.createTask(t, suffix)
		item, err := e.service.ClaimMailbox(context.Background(), e.workerID, session.SessionToken, claimRequest(session, 1))
		if err != nil || item == nil {
			t.Fatalf("claim=%v err=%v", item, err)
		}
		begin, err := e.service.BeginAttempt(context.Background(), e.workerID, session.SessionToken, item.ID, api.BeginAttemptRequest{WorkerInstanceID: session.Worker.ID, AgentID: e.agentID, Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken, ExpectedItemState: domain.MailboxStateClaimed})
		if err != nil {
			t.Fatal(err)
		}
		return begin
	}
	first := beginRun("codex-input-first")
	batch := api.EventBatch{WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken, ExpectedRunVersion: first.Turn.RunAttempt.Version,
		Events: []openruntime.RuntimeEvent{{Type: "session.bound", Payload: json.RawMessage(`{"provider_session_id":"private-codex-session","source":"new"}`), OccurredAt: e.clock.Now()}}}
	if err := e.service.AppendEvents(context.Background(), e.workerID, session.SessionToken, first.Turn.RunAttempt.ID, batch); err != nil {
		t.Fatal(err)
	}
	binding, err := e.repository.GetSessionBinding(context.Background(), first.Turn.Task.ID, e.agentID, e.backend.BackendID)
	if err != nil || binding.ProviderSessionID != "private-codex-session" {
		t.Fatal("formal AppendEvents failed to persist early binding")
	}
	plan, err := (M1TurnPlanner{Bindings: e.repository}).Plan(context.Background(), first.Turn.Task, nil, []openruntime.BackendRegistration{e.backend})
	if err != nil || plan.Execution.Spec.Session.Mode != domain.SessionModeResume || plan.SessionBinding == nil || plan.SessionBinding.ProviderSessionID != binding.ProviderSessionID {
		t.Fatal("early binding was unavailable for same-Task resume planning")
	}
	journal, err := e.repository.ListTaskJournal(context.Background(), first.Turn.Task.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range journal {
		if strings.Contains(string(event.Payload), "private-codex-session") {
			t.Fatal("formal AppendEvents leaked provider identity to Journal")
		}
	}
	input := first.Turn.Execution.AgentInput
	sum := sha256.Sum256([]byte("角色第一版"))
	if input == nil || input.ProfileVersion != 7 || input.WorkspaceRoot != dir || input.InstructionsPath != role || input.InstructionsSHA256 != hex.EncodeToString(sum[:]) || input.InstructionsContent != "角色第一版" {
		t.Fatalf("input=%+v", input)
	}
	if first.Turn.Execution.Spec.Timeout != 47*time.Second || !first.Turn.Execution.DeadlineAt.Equal(first.Turn.RunAttempt.StartedAt.Add(47*time.Second)) {
		t.Fatalf("execution=%+v", first.Turn.Execution)
	}
	if err := os.WriteFile(role, []byte("角色第二版"), 0600); err != nil {
		t.Fatal(err)
	}
	state.profile.Version = 8
	saved, err := e.repository.GetRunAttempt(context.Background(), first.Turn.RunAttempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	var frozen domain.ResolvedExecutionSpec
	if err := json.Unmarshal([]byte(saved.ResolvedExecutionJSON), &frozen); err != nil {
		t.Fatal(err)
	}
	if frozen.AgentInput.InstructionsContent != "角色第一版" || frozen.AgentInput.ProfileVersion != 7 || input.InstructionsContent != "角色第一版" {
		t.Fatalf("snapshot drift=%+v", frozen)
	}
	finish := api.FinishRunRequest{WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken, ExpectedTaskVersion: 2, ExpectedRunVersion: 1, Result: openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "done", ProviderSessionID: "wrong-session", SideEffectsKnown: true}}
	if err := e.service.Finish(context.Background(), e.workerID, session.SessionToken, first.Turn.RunAttempt.ID, finish); err == nil {
		t.Fatal("Finish changed the early provider session")
	}
	finish.Result.ProviderSessionID = binding.ProviderSessionID
	err = e.service.Finish(context.Background(), e.workerID, session.SessionToken, first.Turn.RunAttempt.ID, finish)
	if err != nil {
		t.Fatal(err)
	}
	binding, err = e.repository.GetSessionBinding(context.Background(), first.Turn.Task.ID, e.agentID, e.backend.BackendID)
	if err != nil || binding.Version != 2 || binding.ProviderSessionID != "private-codex-session" {
		t.Fatal("Finish failed to preserve and advance early SessionBinding")
	}
	second := beginRun("codex-input-second")
	if second.Turn.SessionBinding != nil || second.Turn.Execution.Spec.Session.Mode != domain.SessionModeNew {
		t.Fatal("new Task silently resumed the previous Task session")
	}
	if second.Turn.Execution.AgentInput.InstructionsContent != "角色第二版" || second.Turn.Execution.AgentInput.ProfileVersion != 8 || second.Turn.Execution.AgentInput.InstructionsSHA256 == input.InstructionsSHA256 {
		t.Fatalf("new input=%+v", second.Turn.Execution.AgentInput)
	}
}

func TestRuntimeSessionPayloadRejectsUnknownInvalidAndTrailingData(t *testing.T) {
	for _, payload := range []string{
		`{"provider_session_id":"id","task_id":"forged"}`,
		`{"provider_session_id":" id "}`,
		`{"provider_session_id":""}`,
		`{"provider_session_id":"id","source":"unexpected"}`,
		`{"provider_session_id":"id"} {}`,
		`null`, `[]`,
	} {
		if _, _, err := publicRuntimeEvent(openruntime.RuntimeEvent{Type: "session.bound", Payload: json.RawMessage(payload), OccurredAt: time.Now()}, time.Now()); err == nil {
			t.Fatalf("invalid session.bound payload was accepted: %s", payload)
		}
	}
}

func TestBeginFreezesCodexUnlimitedDefaultWithoutChangingAGY(t *testing.T) {
	for _, adapterID := range []string{"codex-app-server", "agy-batch"} {
		t.Run(adapterID, func(t *testing.T) {
			e := newWorkerTestEnvironment(t, nil)
			dir := t.TempDir()
			role := filepath.Join(dir, "role.md")
			if err := os.WriteFile(role, []byte("Isolated timeout validation role."), 0600); err != nil {
				t.Fatal(err)
			}
			e.service.state = &agyProfileState{WorkerState: e.repository, profile: &domain.AgentProfileRecord{AgentID: e.agentID, Version: 1, InstructionsPath: role, WorkspaceRoot: dir}}
			e.backend.Descriptor.AdapterID = adapterID
			e.backend.Descriptor.RuntimeIdentity.AdapterID = adapterID
			e.backend.Descriptor.DefaultTimeout = 0
			session := e.register(t, "worker-default-timeout")
			e.heartbeat(t, session)
			e.createTask(t, "default-timeout")
			item, err := e.service.ClaimMailbox(context.Background(), e.workerID, session.SessionToken, claimRequest(session, 1))
			if err != nil || item == nil {
				t.Fatalf("claim=%v err=%v", item, err)
			}
			begin, err := e.service.BeginAttempt(context.Background(), e.workerID, session.SessionToken, item.ID, api.BeginAttemptRequest{WorkerInstanceID: session.Worker.ID, AgentID: e.agentID, Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken, ExpectedItemState: domain.MailboxStateClaimed})
			if err != nil {
				t.Fatal(err)
			}
			saved, err := e.repository.GetRunAttempt(context.Background(), begin.Turn.RunAttempt.ID)
			if err != nil {
				t.Fatal(err)
			}
			var frozen domain.ResolvedExecutionSpec
			if err = json.Unmarshal([]byte(saved.ResolvedExecutionJSON), &frozen); err != nil {
				t.Fatal(err)
			}
			for _, execution := range []domain.ResolvedExecutionSpec{begin.Turn.Execution, frozen} {
				if err := execution.Spec.ValidateShape(); err != nil {
					t.Fatal(err)
				}
				if adapterID == "codex-app-server" {
					if execution.Spec.Timeout != 0 || !execution.DeadlineAt.IsZero() {
						t.Fatalf("Codex default gained a cutoff: %+v", execution)
					}
				} else {
					if execution.Spec.Timeout != 30*time.Minute || !execution.DeadlineAt.Equal(saved.StartedAt.Add(30*time.Minute)) {
						t.Fatalf("AGY default timeout changed: %+v", execution)
					}
					zero := execution.Spec
					zero.Timeout = 0
					if err := zero.ValidateShape(); err == nil {
						t.Fatal("AGY accepted an unlimited execution spec")
					}
				}
			}
			t.Log("Real SQLite BeginAttempt and persisted Run agree on adapter-specific default timeout; no Runtime/model execution claimed.")
		})
	}
}
