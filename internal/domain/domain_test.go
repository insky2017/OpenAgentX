package domain_test

import (
	"os"
	"path/filepath"
	"testing"

	"agentbus/internal/domain"
)

func TestAgentValidationEdgeCases(t *testing.T) {
	validAgents := []*domain.Agent{
		{ID: "orchestrator", Role: "orchestrator", Connector: domain.ConnectorTmux, Address: "%50"},
		{ID: "quote-1", Role: "quote_service", Connector: domain.ConnectorTmux, Address: "%51"},
		{ID: "worker.agent_01", Role: "worker", Connector: domain.ConnectorNone},
	}
	for _, a := range validAgents {
		if err := a.Validate(); err != nil {
			t.Errorf("expected valid agent for %+v, got error: %v", a, err)
		}
	}

	invalidAgents := []*domain.Agent{
		{ID: "", Role: "worker"},
		{ID: "agent\nnewline", Role: "worker"},
		{ID: "agent;rm -rf /", Role: "worker"},
		{ID: "agent $(whoami)", Role: "worker"},
		{ID: "agent`id`", Role: "worker"},
		{ID: "-invalid-leading-hyphen", Role: "worker"},
		{ID: ".invalid-leading-dot", Role: "worker"},
		{ID: "orchestrator", Role: ""},
		{ID: "orchestrator", Role: "role\nwith\nnewline"},
		{ID: "orchestrator", Role: "role with space"},
		{ID: "orchestrator", Role: "worker", Connector: "invalid_connector"},
		{ID: "orchestrator", Role: "worker", Connector: domain.ConnectorTmux, Address: ""},
		{ID: "orchestrator", Role: "worker", Connector: domain.ConnectorTmux, Address: "%50\nnewline"},
	}
	for _, a := range invalidAgents {
		if err := a.Validate(); err == nil {
			t.Errorf("expected error for invalid agent %+v, got nil", a)
		}
	}
}

func TestTaskValidationEdgeCases(t *testing.T) {
	validTask := &domain.Task{
		ID:             "task-12345",
		SenderAgentID:  "orchestrator",
		TargetAgentID:  "quote",
		IdempotencyKey: "idem-valid-01",
		Content:        "Task content",
	}
	if err := validTask.Validate(); err != nil {
		t.Fatalf("expected valid task, got error: %v", err)
	}

	invalidTasks := []*domain.Task{
		{ID: "", SenderAgentID: "orchestrator", TargetAgentID: "quote", IdempotencyKey: "k1", Content: "c"},
		{ID: "task\nnewline", SenderAgentID: "orchestrator", TargetAgentID: "quote", IdempotencyKey: "k1", Content: "c"},
		{ID: "t1", SenderAgentID: "invalid sender space", TargetAgentID: "quote", IdempotencyKey: "k1", Content: "c"},
		{ID: "t1", SenderAgentID: "orchestrator", TargetAgentID: "invalid;target", IdempotencyKey: "k1", Content: "c"},
		{ID: "t1", SenderAgentID: "orchestrator", TargetAgentID: "quote", IdempotencyKey: "", Content: "c"},
		{ID: "t1", SenderAgentID: "orchestrator", TargetAgentID: "quote", IdempotencyKey: "key\nwith\nnewline", Content: "c"},
		{ID: "t1", SenderAgentID: "orchestrator", TargetAgentID: "quote", IdempotencyKey: "k1", Content: ""},
	}
	for _, tCase := range invalidTasks {
		if err := tCase.Validate(); err == nil {
			t.Errorf("expected error for invalid task %+v, got nil", tCase)
		}
	}
}

func TestManifestLoadingAndValidation(t *testing.T) {
	dir, err := os.MkdirTemp("", "manifest-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	rolePath := filepath.Join(dir, "ROLE.md")
	if err := os.WriteFile(rolePath, []byte("# Test Role\nRole instructions"), 0644); err != nil {
		t.Fatalf("failed to write role file: %v", err)
	}

	validYAML := `version: 1
id: test-agent
role: tester
runtime: agy
connector: tmux
address: "%51"
workspace: "."
instructions: "ROLE.md"
capabilities:
  - testing
  - automation
  - testing
`
	validConfig := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(validConfig, []byte(validYAML), 0644); err != nil {
		t.Fatalf("failed to write valid agent.yaml: %v", err)
	}

	manifest, profile, agent, err := domain.LoadManifest(validConfig, dir)
	if err != nil {
		t.Fatalf("LoadManifest failed on valid yaml: %v", err)
	}
	if manifest.ID != "test-agent" || profile.Runtime != "agy" || agent.Role != "tester" {
		t.Fatalf("unexpected loaded data: %+v", manifest)
	}
	if len(manifest.Capabilities) != 2 || manifest.Capabilities[0] != "automation" || manifest.Capabilities[1] != "testing" {
		t.Fatalf("capabilities not properly deduplicated and sorted: %+v", manifest.Capabilities)
	}

	// 1. Unknown fields rejection
	unknownYAML := `version: 1
id: test-agent
role: tester
runtime: agy
connector: tmux
address: "%51"
workspace: "."
instructions: "ROLE.md"
unknown_field: "disallowed"
`
	unknownConfig := filepath.Join(dir, "unknown.yaml")
	_ = os.WriteFile(unknownConfig, []byte(unknownYAML), 0644)
	if _, _, _, err := domain.LoadManifest(unknownConfig, dir); err == nil {
		t.Fatalf("expected error for YAML with unknown fields, got nil")
	}

	// 2. Empty ROLE file rejection
	emptyRole := filepath.Join(dir, "EMPTY_ROLE.md")
	_ = os.WriteFile(emptyRole, []byte(""), 0644)
	emptyRoleYAML := `version: 1
id: test-agent
role: tester
runtime: agy
connector: tmux
address: "%51"
workspace: "."
instructions: "EMPTY_ROLE.md"
`
	emptyRoleConfig := filepath.Join(dir, "empty_role.yaml")
	_ = os.WriteFile(emptyRoleConfig, []byte(emptyRoleYAML), 0644)
	if _, _, _, err := domain.LoadManifest(emptyRoleConfig, dir); err == nil {
		t.Fatalf("expected error for empty ROLE file, got nil")
	}

	// 3. Nonexistent instructions rejection
	missingRoleYAML := `version: 1
id: test-agent
role: tester
runtime: agy
connector: tmux
address: "%51"
workspace: "."
instructions: "NONEXISTENT.md"
`
	missingRoleConfig := filepath.Join(dir, "missing_role.yaml")
	_ = os.WriteFile(missingRoleConfig, []byte(missingRoleYAML), 0644)
	if _, _, _, err := domain.LoadManifest(missingRoleConfig, dir); err == nil {
		t.Fatalf("expected error for missing ROLE file, got nil")
	}
}

func TestAGYEventValidation(t *testing.T) {
	validEvents := []string{
		domain.AGYEventPreToolUse,
		domain.AGYEventPostToolUse,
		domain.AGYEventPreInvocation,
		domain.AGYEventPostInvocation,
		domain.AGYEventStop,
	}
	for _, ev := range validEvents {
		if !domain.IsValidAGYEvent(ev) {
			t.Errorf("expected valid event for '%s', got false", ev)
		}
	}

	invalidEvents := []string{
		"",
		"pretooluse",
		"STOP",
		"ToolUse",
		"Start",
		"UnknownEvent",
	}
	for _, ev := range invalidEvents {
		if domain.IsValidAGYEvent(ev) {
			t.Errorf("expected invalid event for '%s', got true", ev)
		}
	}
}
