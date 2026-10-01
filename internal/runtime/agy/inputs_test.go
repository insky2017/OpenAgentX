package agy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

func TestAGYFrozenInputControlsProcessCwdAndPrompt(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fixture")
	if err := os.WriteFile(script, []byte("#!/bin/sh\npwd > cwd.log\ncat > prompt.json\necho '{\"event\":\"result\",\"result\":{\"status\":\"SUCCESS\",\"response\":\"ok\"}}'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	content := "角色冻结文本"
	sum := sha256.Sum256([]byte(content))
	request := testTurnRequest(validSpec())
	request.Execution.AgentInput = &domain.AgentExecutionInput{ProfileVersion: 3, WorkspaceRoot: dir, InstructionsPath: filepath.Join(dir, "role-removed-after-snapshot.md"), InstructionsContent: content, InstructionsSHA256: hex.EncodeToString(sum[:])}
	adapter := NewAdapterForTest(Config{Binary: script, WorkingDir: t.TempDir()})
	handle, err := adapter.StartTurn(context.Background(), request, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := handle.Wait(context.Background()); err != nil || result.Status != openruntime.TurnResultSucceeded {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	cwd, err := os.ReadFile(filepath.Join(dir, "cwd.log"))
	if err != nil || strings.TrimSpace(string(cwd)) != dir {
		t.Fatalf("cwd=%s err=%v", cwd, err)
	}
	prompt, err := os.ReadFile(filepath.Join(dir, "prompt.json"))
	if err != nil {
		t.Fatal(err)
	}
	var input struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(prompt, &input); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{content, "profile version 3", request.Execution.AgentInput.InstructionsSHA256, dir, "Task:\ndo work"} {
		if !strings.Contains(input.Message.Content, want) {
			t.Fatalf("missing %q in %q", want, input.Message.Content)
		}
	}
	request.Execution.AgentInput.InstructionsContent = "tampered"
	if _, err := adapter.StartTurn(context.Background(), request, nil); err == nil {
		t.Fatal("tampered snapshot accepted")
	}
}

func TestAGYFrozenDeadlineCannotBeExtendedByStartDelay(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "slow")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec sleep 10\n"), 0700); err != nil {
		t.Fatal(err)
	}
	adapter := NewAdapterForTest(Config{Binary: script})
	request := testTurnRequest(validSpec())
	request.Execution.DeadlineAt = time.Now().Add(50 * time.Millisecond)
	started := time.Now()
	handle, err := adapter.StartTurn(context.Background(), request, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := handle.Wait(context.Background())
	if err == nil || result.Status != openruntime.TurnResultUncertain || result.FinalReply || time.Since(started) > time.Second {
		t.Fatalf("result=%+v err=%v elapsed=%s", result, err, time.Since(started))
	}
	if _, err := adapter.StartTurn(context.Background(), request, nil); err == nil || !strings.Contains(err.Error(), "deadline has expired") {
		t.Fatalf("expired err=%v", err)
	}
}

func TestAGYRejectsUnconsumedExecutionFields(t *testing.T) {
	adapter := NewAdapterForTest(Config{})
	for _, change := range []func(*domain.ExecutionSpec){
		func(s *domain.ExecutionSpec) { s.ApprovalPolicy = "never" },
		func(s *domain.ExecutionSpec) { s.BackendOptions = json.RawMessage(`{"unknown":true}`) },
		func(s *domain.ExecutionSpec) { s.BackendOptions = json.RawMessage(`null`) },
		func(s *domain.ExecutionSpec) { s.Reasoning.Value = "high" },
	} {
		spec := validSpec()
		change(&spec)
		if err := adapter.Validate(context.Background(), spec); err == nil {
			t.Fatalf("unsupported spec=%+v", spec)
		}
	}
}
