package domain_test

import (
	"testing"

	"agentbus/internal/domain"
)

func TestAgentValidationEdgeCases(t *testing.T) {
	validAgents := []*domain.Agent{
		{ID: "coordinator", Role: "coordinator", Connector: domain.ConnectorTmux, Address: "%50"},
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
		{ID: "coordinator", Role: ""},
		{ID: "coordinator", Role: "role\nwith\nnewline"},
		{ID: "coordinator", Role: "role with space"},
		{ID: "coordinator", Role: "worker", Connector: "invalid_connector"},
		{ID: "coordinator", Role: "worker", Connector: domain.ConnectorTmux, Address: ""},
		{ID: "coordinator", Role: "worker", Connector: domain.ConnectorTmux, Address: "%50\nnewline"},
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
		SenderAgentID:  "coordinator",
		TargetAgentID:  "quote",
		IdempotencyKey: "idem-valid-01",
		Content:        "Task content",
	}
	if err := validTask.Validate(); err != nil {
		t.Fatalf("expected valid task, got error: %v", err)
	}

	invalidTasks := []*domain.Task{
		{ID: "", SenderAgentID: "coordinator", TargetAgentID: "quote", IdempotencyKey: "k1", Content: "c"},
		{ID: "task\nnewline", SenderAgentID: "coordinator", TargetAgentID: "quote", IdempotencyKey: "k1", Content: "c"},
		{ID: "t1", SenderAgentID: "invalid sender space", TargetAgentID: "quote", IdempotencyKey: "k1", Content: "c"},
		{ID: "t1", SenderAgentID: "coordinator", TargetAgentID: "invalid;target", IdempotencyKey: "k1", Content: "c"},
		{ID: "t1", SenderAgentID: "coordinator", TargetAgentID: "quote", IdempotencyKey: "", Content: "c"},
		{ID: "t1", SenderAgentID: "coordinator", TargetAgentID: "quote", IdempotencyKey: "key\nwith\nnewline", Content: "c"},
		{ID: "t1", SenderAgentID: "coordinator", TargetAgentID: "quote", IdempotencyKey: "k1", Content: ""},
	}
	for _, tCase := range invalidTasks {
		if err := tCase.Validate(); err == nil {
			t.Errorf("expected error for invalid task %+v, got nil", tCase)
		}
	}
}
