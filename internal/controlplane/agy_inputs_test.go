package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openagentx/internal/api"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

// Test fixture supplies the profile through the same GetAgent interface used by
// the real repository; Task/Run/Journal transitions still use SQLite transactions.
type agyProfileState struct {
	WorkerState
	profile *domain.AgentProfileRecord
}

func (s *agyProfileState) GetAgent(context.Context, string) (*domain.AgentIdentity, *domain.AgentProfileRecord, error) {
	return nil, s.profile, nil
}

func TestAGYBeginFreezesProfileRoleCwdTimeoutAndNextRun(t *testing.T) {
	e := newWorkerTestEnvironment(t, nil)
	dir := t.TempDir()
	role := filepath.Join(dir, "role.md")
	if err := os.WriteFile(role, []byte("角色第一版"), 0600); err != nil {
		t.Fatal(err)
	}
	state := &agyProfileState{WorkerState: e.repository, profile: &domain.AgentProfileRecord{AgentID: e.agentID, Version: 7, InstructionsPath: role, WorkspaceRoot: dir}}
	e.service.state = state
	e.backend.Descriptor.AdapterID = "agy-batch"
	e.backend.Descriptor.RuntimeIdentity.AdapterID = "agy-batch"
	e.backend.Descriptor.DefaultTimeout = 47 * time.Second
	session := e.register(t, "worker-agy-inputs")
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
	first := beginRun("agy-input-first")
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
	err = e.service.Finish(context.Background(), e.workerID, session.SessionToken, first.Turn.RunAttempt.ID, api.FinishRunRequest{WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken, ExpectedTaskVersion: 2, ExpectedRunVersion: 1, Result: openruntime.TurnResult{Status: openruntime.TurnResultSucceeded, Result: "done", SideEffectsKnown: true}})
	if err != nil {
		t.Fatal(err)
	}
	second := beginRun("agy-input-second")
	if second.Turn.Execution.AgentInput.InstructionsContent != "角色第二版" || second.Turn.Execution.AgentInput.ProfileVersion != 8 || second.Turn.Execution.AgentInput.InstructionsSHA256 == input.InstructionsSHA256 {
		t.Fatalf("new input=%+v", second.Turn.Execution.AgentInput)
	}
}

func TestAGYRoleInputRejectsMissingEmptyAndInvalidWorkspace(t *testing.T) {
	dir := t.TempDir()
	p := &domain.AgentProfileRecord{Version: 1, WorkspaceRoot: dir, InstructionsPath: filepath.Join(dir, "missing.md")}
	if _, err := freezeAGYAgentInput(p); err == nil || !strings.Contains(err.Error(), "instructions file unavailable") {
		t.Fatalf("missing err=%v", err)
	} else {
		var publicError *domain.DomainError
		if !errors.As(err, &publicError) || publicError.Code != "INVALID_INPUT" {
			t.Fatalf("missing file reason must survive Worker API error mapping: %v", err)
		}
	}
	if err := os.WriteFile(p.InstructionsPath, []byte("  \n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := freezeAGYAgentInput(p); err == nil || !strings.Contains(err.Error(), "non-empty UTF-8") {
		t.Fatalf("empty err=%v", err)
	}
	p.WorkspaceRoot = filepath.Join(dir, "missing")
	if _, err := freezeAGYAgentInput(p); err == nil || !strings.Contains(err.Error(), "workspace_root") {
		t.Fatalf("workspace err=%v", err)
	}
}

func TestAGYMissingRoleDoesNotCreateOrAcceptRun(t *testing.T) {
	e := newWorkerTestEnvironment(t, nil)
	e.backend.Descriptor.AdapterID = "agy-batch"
	e.backend.Descriptor.RuntimeIdentity.AdapterID = "agy-batch"
	// Real repository profile is /roles/quote.md; use an isolated explicit missing
	// path to avoid depending on host contents while exercising the transaction.
	e.service.state = &agyProfileState{WorkerState: e.repository, profile: &domain.AgentProfileRecord{Version: 1, WorkspaceRoot: t.TempDir(), InstructionsPath: filepath.Join(t.TempDir(), "missing.md")}}
	session := e.register(t, "worker-agy-missing")
	e.heartbeat(t, session)
	created := e.createTask(t, "agy-missing")
	item, err := e.service.ClaimMailbox(context.Background(), e.workerID, session.SessionToken, claimRequest(session, 1))
	if err != nil || item == nil {
		t.Fatalf("claim=%+v err=%v", item, err)
	}
	_, err = e.service.BeginAttempt(context.Background(), e.workerID, session.SessionToken, item.ID, api.BeginAttemptRequest{WorkerInstanceID: session.Worker.ID, AgentID: e.agentID, Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken, ExpectedItemState: domain.MailboxStateClaimed})
	if err == nil || !strings.Contains(err.Error(), "instructions file unavailable") {
		t.Fatalf("begin err=%v", err)
	}
	task, err := e.repository.GetTask(context.Background(), created.Task.ID)
	if err != nil || task.Status != domain.TaskStatusQueued {
		t.Fatalf("task=%+v err=%v", task, err)
	}
	savedItem, err := e.repository.GetMailboxItem(context.Background(), item.ID)
	if err != nil || savedItem.State != domain.MailboxStateClaimed {
		t.Fatalf("item=%+v err=%v", savedItem, err)
	}
}
