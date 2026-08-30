package workercli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
	residentworker "openagentx/internal/worker"
)

func TestAssembleCodeBuddyAdapterFromWorkerConfig(t *testing.T) {
	directory := t.TempDir()
	argsPath := filepath.Join(directory, "args")
	cwdPath := filepath.Join(directory, "cwd")
	promptPath := filepath.Join(directory, "prompt")
	binary := writeCodeBuddyWorkerFixture(t, directory, argsPath, cwdPath, promptPath)
	configPath := filepath.Join(directory, "agent.yaml")
	configYAML := fmt.Sprintf(`version: 1
agent_id: verifier
transport: unix
unix_socket: run/openagentx.sock
capabilities: [verification]
runtime_backends:
  - backend_id: primary
    adapter_id: codebuddy-cli
    options:
      binary: %s
      working_dir: .
      models: [hy4-preview]
      effort: medium
      permission_mode: dontAsk
      max_turns: 7
`, binary)
	if err := os.WriteFile(configPath, []byte(configYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	processConfig, err := residentworker.LoadProcessConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := assembleM1Adapter(processConfig.RuntimeBackendConfig[0], filepath.Dir(configPath))
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := adapter.Descriptor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.AdapterID != "codebuddy-cli" {
		t.Fatalf("descriptor adapter_id=%q", descriptor.AdapterID)
	}
	if len(descriptor.Models) != 1 || descriptor.Models[0] != "hy4-preview" {
		t.Fatalf("descriptor models=%v", descriptor.Models)
	}
	if !containsCodeBuddyReasoningMode(descriptor.ReasoningModes, domain.ReasoningEffort) {
		t.Fatalf("descriptor reasoning modes missing effort: %v", descriptor.ReasoningModes)
	}
	handle, err := adapter.StartTurn(context.Background(), codeBuddyAssemblyTurnRequest("verify worker assembly"), nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := handle.Wait(context.Background())
	if err != nil || result.Status != openruntime.TurnResultSucceeded || result.Result != "assembled-by-codebuddy-fixture" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	argumentText := readCodeBuddyWorkerFixtureFile(t, argsPath)
	for _, expected := range []string{
		"--model", "hy4-preview", "--effort", "medium",
		"--permission-mode", "dontAsk", "--max-turns", "7",
	} {
		if !strings.Contains(argumentText, expected) {
			t.Fatalf("arguments missing %q: %s", expected, argumentText)
		}
	}
	if strings.Contains(argumentText, "verify worker assembly") {
		t.Fatal("task prompt leaked into process arguments")
	}
	prompt := readCodeBuddyWorkerFixtureFile(t, promptPath)
	if !strings.Contains(prompt, "verify worker assembly") || !strings.Contains(prompt, "task-assembly") {
		t.Fatalf("prompt=%q", prompt)
	}
	resolvedDirectory, err := filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatal(err)
	}
	if cwd := strings.TrimSpace(readCodeBuddyWorkerFixtureFile(t, cwdPath)); cwd != resolvedDirectory {
		t.Fatalf("process cwd=%q want=%q", cwd, resolvedDirectory)
	}
}

func TestAssembleCodeBuddyAdapterDefaults(t *testing.T) {
	directory := t.TempDir()
	argsPath := filepath.Join(directory, "args")
	promptPath := filepath.Join(directory, "prompt")
	binary := writeCodeBuddyWorkerFixture(t, directory, argsPath, "", promptPath)
	adapter, err := assembleM1Adapter(residentworker.RuntimeBackendConfig{
		BackendID: "primary", AdapterID: "codebuddy-cli",
		Options: map[string]any{"binary": binary},
	}, directory)
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := adapter.Descriptor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(descriptor.Models) != 1 || descriptor.Models[0] != "hy4-preview" {
		t.Fatalf("default descriptor models=%v", descriptor.Models)
	}
	handle, err := adapter.StartTurn(context.Background(), codeBuddyAssemblyTurnRequest("run with defaults"), nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := handle.Wait(context.Background())
	if err != nil || result.Status != openruntime.TurnResultSucceeded {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	argumentText := readCodeBuddyWorkerFixtureFile(t, argsPath)
	for _, expected := range []string{
		"--model", "hy4-preview", "--effort", "high",
		"--permission-mode", "acceptEdits", "--max-turns", "40",
	} {
		if !strings.Contains(argumentText, expected) {
			t.Fatalf("arguments missing %q: %s", expected, argumentText)
		}
	}
	if strings.Contains(argumentText, "run with defaults") {
		t.Fatal("task prompt leaked into process arguments")
	}
	if prompt := readCodeBuddyWorkerFixtureFile(t, promptPath); !strings.Contains(prompt, "run with defaults") {
		t.Fatalf("prompt=%q", prompt)
	}
}

func TestAssembleCodeBuddyAdapterRejectsInvalidOptions(t *testing.T) {
	directory := t.TempDir()
	binary := writeCodeBuddyWorkerFixture(t, directory, "", "", "")
	cases := map[string]map[string]any{
		"unknown option":          {"binary": binary, "unexpected": "value"},
		"model and models":        {"binary": binary, "model": "hy4-preview", "models": []any{"hy4-preview"}},
		"model wrong type":        {"binary": binary, "model": 7},
		"model blank":             {"binary": binary, "model": "   "},
		"models wrong type":       {"binary": binary, "models": "hy4-preview"},
		"models empty":            {"binary": binary, "models": []any{}},
		"models non string entry": {"binary": binary, "models": []any{7}},
		"binary wrong type":       {"binary": 40},
		"working dir missing":     {"binary": binary, "working_dir": filepath.Join(directory, "missing")},
		"working dir wrong type":  {"binary": binary, "working_dir": 40},
		"effort unsupported":      {"binary": binary, "effort": "infinite"},
		"permission unsupported":  {"binary": binary, "permission_mode": "yolo"},
		"max turns zero":          {"binary": binary, "max_turns": 0},
		"max turns negative":      {"binary": binary, "max_turns": -3},
		"max turns wrong type":    {"binary": binary, "max_turns": "40"},
	}
	for name, options := range cases {
		if _, err := assembleM1Adapter(residentworker.RuntimeBackendConfig{
			BackendID: "primary", AdapterID: "codebuddy-cli", Options: options,
		}, directory); err == nil {
			t.Fatalf("%s: expected assembly error, got none", name)
		}
	}
}

func codeBuddyAssemblyTurnRequest(content string) openruntime.TurnRequest {
	spec := domain.ExecutionSpec{
		AdapterID: "codebuddy-cli", BackendID: "primary", Model: "hy4-preview",
		Reasoning: domain.ReasoningSpec{Mode: domain.ReasoningBackendDefault},
		Session:   domain.SessionSpec{Mode: domain.SessionModeNew, ContextID: "task-assembly"},
		Timeout:   time.Minute,
	}
	return openruntime.TurnRequest{
		Task:      domain.Task{ID: "task-assembly", Content: content},
		Execution: domain.ResolvedExecutionSpec{Version: 1, Spec: spec},
	}
}

func containsCodeBuddyReasoningMode(values []domain.ReasoningMode, target domain.ReasoningMode) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func writeCodeBuddyWorkerFixture(t *testing.T, directory, argsPath, cwdPath, promptPath string) string {
	t.Helper()
	var script strings.Builder
	script.WriteString("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo \"fixture 1\"; exit 0; fi\n")
	if argsPath != "" {
		fmt.Fprintf(&script, "printf '%%s\\n' \"$@\" > %q\n", argsPath)
	}
	if cwdPath != "" {
		fmt.Fprintf(&script, "pwd > %q\n", cwdPath)
	}
	if promptPath != "" {
		fmt.Fprintf(&script, "cat > %q\n", promptPath)
	} else {
		script.WriteString("cat > /dev/null\n")
	}
	script.WriteString("printf 'assembled-by-codebuddy-fixture'\n")
	path := filepath.Join(directory, "codebuddy-worker-fixture")
	if err := os.WriteFile(path, []byte(script.String()), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func readCodeBuddyWorkerFixtureFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
