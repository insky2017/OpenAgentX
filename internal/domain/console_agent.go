package domain

import "fmt"

// ConsoleAgentOption is the control-plane-safe Agent selector projection.
// It deliberately excludes principal, credential, Worker transport and runtime payload data.
type ConsoleAgentOption struct {
	AgentID         string           `json:"agent_id"`
	OrganizationID  string           `json:"organization_id"`
	DisplayName     string           `json:"display_name"`
	WorkerStatus    WorkerStatus     `json:"worker_status"`
	Generation      int64            `json:"generation"`
	ActiveRunStatus RunAttemptStatus `json:"active_run_status,omitempty"`
}

func (o ConsoleAgentOption) Validate() error {
	if err := ValidateIdentifier("agent_id", o.AgentID); err != nil {
		return err
	}
	if o.DisplayName == "" {
		return ErrInvalidInput("Agent display_name cannot be empty")
	}
	if err := ValidateOpaqueID("organization_id", o.OrganizationID); err != nil {
		return err
	}
	if !o.WorkerStatus.Valid() {
		return ErrInvalidInput("invalid Console Agent Worker status")
	}
	if o.Generation < 0 || (o.Generation == 0 && o.WorkerStatus != WorkerStatusOffline) {
		return ErrInvalidInput("invalid Console Agent Worker generation")
	}
	if o.ActiveRunStatus != "" && !o.ActiveRunStatus.Active() {
		return fmt.Errorf("Console Agent active Run status is not active")
	}
	return nil
}
