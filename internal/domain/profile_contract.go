package domain

import (
	"strings"
	"time"
)

type AgentProfileRecord struct {
	AgentID                   string    `json:"agent_id"`
	Version                   int64     `json:"profile_version"`
	InstructionsPath          string    `json:"instructions_path"`
	WorkspaceRoot             string    `json:"workspace_root"`
	DefaultExecutionProfileID *string   `json:"default_execution_profile_id,omitempty"`
	Capabilities              []string  `json:"capabilities"`
	CreatedAt                 time.Time `json:"created_at"`
	UpdatedAt                 time.Time `json:"updated_at"`
}

func (p AgentProfileRecord) Validate() error {
	if err := ValidateIdentifier("agent_id", p.AgentID); err != nil {
		return err
	}
	if err := ValidatePositiveVersion("profile version", p.Version); err != nil {
		return err
	}
	if strings.TrimSpace(p.InstructionsPath) == "" || strings.TrimSpace(p.WorkspaceRoot) == "" {
		return ErrInvalidInput("profile instructions_path and workspace_root are required")
	}
	if p.DefaultExecutionProfileID != nil {
		if err := ValidateOpaqueID("default_execution_profile_id", *p.DefaultExecutionProfileID); err != nil {
			return err
		}
	}
	seen := make(map[string]struct{}, len(p.Capabilities))
	for _, capability := range p.Capabilities {
		if err := ValidateIdentifier("capability", capability); err != nil {
			return err
		}
		if _, exists := seen[capability]; exists {
			return ErrInvalidInput("profile capabilities must be unique")
		}
		seen[capability] = struct{}{}
	}
	return nil
}
