package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

// Explicit opt-in only: uses an isolated CODEX_HOME supplied by the operator,
// starts its own app-server, and never attaches to the user's shared daemon.
func TestRealCodexAdapter(t *testing.T) {
	root := os.Getenv("OAX_CODEX_REAL_ROOT")
	if root == "" {
		t.Skip("set OAX_CODEX_REAL_ROOT to a private evidence directory with isolated home/")
	}
	workspace := filepath.Join(root, "adapter-workspace")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	log, err := os.OpenFile(filepath.Join(root, "adapter-events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	var mu sync.Mutex
	sink := openruntime.EventSinkFunc(func(_ context.Context, e openruntime.RuntimeEvent) error {
		mu.Lock()
		defer mu.Unlock()
		return json.NewEncoder(log).Encode(e)
	})
	a, err := NewAdapter(Config{Binary: "codex", WorkingDir: workspace, Models: []string{"gpt-6-astra"}, StateDir: filepath.Join(root, "adapter-state"), Environment: []string{"CODEX_HOME=" + filepath.Join(root, "home")}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	if err = a.Health(ctx); err != nil {
		t.Fatal(err)
	}
	r := fixtureRequest()
	r.Task.ID = "task-real-codex-file"
	r.RunAttempt.ID = "run-real-codex-file"
	r.Task.Content = "Use a tool to create adapter-proof.txt containing exactly OAX_REAL_ADAPTER_OK followed by a newline. Reply exactly OAX_REAL_FILE_COMPLETE."
	r.Execution.Spec.Model = "gpt-6-astra"
	r.Execution.Spec.Reasoning = domain.ReasoningSpec{Mode: domain.ReasoningEffort, Value: "high"}
	r.Execution.Spec.ApprovalPolicy = "never"
	r.Execution.Spec.Sandbox = "danger-full-access"
	r.Execution.Spec.Timeout = 3 * time.Minute
	r.Execution.AgentInput = &domain.AgentExecutionInput{ProfileVersion: 1, InstructionsSHA256: "protocol-test-role", WorkspaceRoot: workspace, InstructionsContent: "You are an isolated protocol verification agent. Only access the supplied workspace. Do not inspect credentials, other projects, or delegate."}
	h, err := a.StartTurn(ctx, r, sink)
	if err != nil {
		t.Fatal(err)
	}
	result, err := h.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.MarshalIndent(result, "", "  ")
	_ = os.WriteFile(filepath.Join(root, "adapter-first-result.json"), raw, 0600)
	if result.Status != openruntime.TurnResultSucceeded || !result.FinalReply {
		t.Fatalf("first result=%+v", result)
	}
	data, err := os.ReadFile(filepath.Join(workspace, "adapter-proof.txt"))
	if err != nil || string(data) != "OAX_REAL_ADAPTER_OK\n" {
		t.Fatalf("independent artifact=%q err=%v", data, err)
	}
	r.Task.ID = "task-real-codex-resume"
	r.RunAttempt.ID = "run-real-codex-resume"
	r.Task.Content = "Without tools, repeat the exact content you wrote to adapter-proof.txt in the previous turn."
	r.Execution.Spec.Session.Mode = domain.SessionModeResume
	r.SessionBinding = &domain.SessionBinding{ProviderSessionID: result.ProviderSessionID}
	h, err = a.StartTurn(ctx, r, sink)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := h.Wait(ctx)
	raw, _ = json.MarshalIndent(resumed, "", "  ")
	_ = os.WriteFile(filepath.Join(root, "adapter-resume-result.json"), raw, 0600)
	if err != nil || resumed.Status != openruntime.TurnResultSucceeded || !resumed.FinalReply || resumed.ProviderSessionID != result.ProviderSessionID {
		t.Fatalf("resumed=%+v err=%v", resumed, err)
	}
	if strings.TrimSpace(resumed.Result) != "OAX_REAL_ADAPTER_OK" && strings.TrimSpace(resumed.Result) != "`OAX_REAL_ADAPTER_OK`" {
		t.Fatalf("missing native conversation context: %q", resumed.Result)
	}
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRealCodexCancellation(t *testing.T) {
	root := os.Getenv("OAX_CODEX_REAL_ROOT")
	if root == "" {
		t.Skip("set OAX_CODEX_REAL_ROOT to isolated evidence root")
	}
	workspace := filepath.Join(root, "cancel-adapter-workspace")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	a, err := NewAdapter(Config{Binary: "codex", Models: []string{"gpt-6-astra"}, WorkingDir: workspace, StateDir: filepath.Join(root, "cancel-adapter-state"), Environment: []string{"CODEX_HOME=" + filepath.Join(root, "home")}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	if err = a.Health(ctx); err != nil {
		t.Fatal(err)
	}
	events, unsub := a.client.Subscribe()
	defer unsub()
	log, err := os.OpenFile(filepath.Join(root, "cancel-adapter-events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	var mu sync.Mutex
	sink := openruntime.EventSinkFunc(func(_ context.Context, e openruntime.RuntimeEvent) error {
		mu.Lock()
		defer mu.Unlock()
		return json.NewEncoder(log).Encode(e)
	})
	r := fixtureRequest()
	r.Task.ID = "task-real-cancel"
	r.RunAttempt.ID = "run-real-cancel"
	r.Task.Content = "Use the shell tool to run exactly: sleep 20; printf SHOULD_NOT_EXIST > late-cancel-sentinel.txt . Wait for the command to finish before replying."
	r.Execution.Spec.Model = "gpt-6-astra"
	r.Execution.Spec.Reasoning = domain.ReasoningSpec{Mode: domain.ReasoningEffort, Value: "high"}
	r.Execution.Spec.ApprovalPolicy = "never"
	r.Execution.Spec.Sandbox = "danger-full-access"
	r.Execution.Spec.Timeout = 2 * time.Minute
	r.Execution.AgentInput = &domain.AgentExecutionInput{ProfileVersion: 1, InstructionsSHA256: "cancel-role", WorkspaceRoot: workspace, InstructionsContent: "Only access this isolated workspace. Do not inspect other projects or credentials; do not delegate."}
	h, err := a.StartTurn(ctx, r, sink)
	if err != nil {
		t.Fatal(err)
	}
	var started RPCMessage
	waiting := true
	for waiting {
		select {
		case m := <-events:
			var p struct {
				Item providerItem `json:"item"`
			}
			_ = json.Unmarshal(m.Params, &p)
			if m.Method == "item/started" && p.Item.Type == "commandExecution" {
				started = m
				waiting = false
			}
		case <-ctx.Done():
			t.Fatal("tool never started")
		}
	}
	startedAt := time.Now()
	raw, _ := json.MarshalIndent(started, "", "  ")
	_ = os.WriteFile(filepath.Join(root, "cancel-adapter-tool-started.json"), raw, 0600)
	beforePID := a.State().PID
	if err = h.RequestCancel(ctx); err != nil {
		t.Fatal(err)
	}
	result, err := h.Wait(ctx)
	raw, _ = json.MarshalIndent(result, "", "  ")
	_ = os.WriteFile(filepath.Join(root, "cancel-adapter-result.json"), raw, 0600)
	if err != nil || result.Status != openruntime.TurnResultCanceled {
		t.Fatalf("cancel result=%+v err=%v", result, err)
	}
	if remaining := time.Until(startedAt.Add(23 * time.Second)); remaining > 0 {
		timer := time.NewTimer(remaining)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			t.Fatal(ctx.Err())
		}
	}
	_, statErr := os.Stat(filepath.Join(workspace, "late-cancel-sentinel.txt"))
	if !os.IsNotExist(statErr) {
		t.Fatal("canceled tool wrote the delayed sentinel")
	}
	proof := map[string]any{"checked_at": time.Now().UTC(), "tool_started_at": startedAt, "waited_past_tool_deadline": true, "sentinel_absent": true, "original_host_pid": beforePID, "host_still_alive": identityAlive(a.processIdentity)}
	raw, _ = json.MarshalIndent(proof, "", "  ")
	_ = os.WriteFile(filepath.Join(root, "cancel-adapter-delayed-proof.json"), raw, 0600)
	r.Task.ID = "task-after-real-cancel"
	r.RunAttempt.ID = "run-after-real-cancel"
	r.Task.Content = "Do not call tools. Reply exactly OAX_AFTER_CANCEL_READY."
	r.Execution.Spec.Session.Mode = domain.SessionModeResume
	r.SessionBinding = &domain.SessionBinding{ProviderSessionID: result.ProviderSessionID}
	h, err = a.StartTurn(ctx, r, sink)
	if err != nil {
		t.Fatal(err)
	}
	next, err := h.Wait(ctx)
	raw, _ = json.MarshalIndent(next, "", "  ")
	_ = os.WriteFile(filepath.Join(root, "cancel-adapter-next-result.json"), raw, 0600)
	if err != nil || next.Status != openruntime.TurnResultSucceeded || !next.FinalReply {
		t.Fatalf("next turn=%+v err=%v", next, err)
	}
}
