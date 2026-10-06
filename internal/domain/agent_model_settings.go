package domain

// AgentModelSettings is the effective, Agent-scoped model preference. Version
// zero means the registered backend default has not been overridden.
type AgentModelSettings struct {
	AgentID      string              `json:"agent_id"`
	BackendID    string              `json:"backend_id"`
	Model        string              `json:"model"`
	Effort       string              `json:"effort"`
	Version      int64               `json:"version"`
	Models       []string            `json:"models"`
	ModelEfforts map[string][]string `json:"model_efforts"`
}

type AgentModelSettingsUpdate struct {
	BackendID       string `json:"backend_id"`
	Model           string `json:"model"`
	Effort          string `json:"effort"`
	ExpectedVersion int64  `json:"expected_version"`
}

func (u AgentModelSettingsUpdate) Validate() error {
	if err := ValidateIdentifier("backend_id", u.BackendID); err != nil {
		return err
	}
	if err := ValidateIdentifier("model", u.Model); err != nil {
		return err
	}
	if u.Effort != "" {
		if err := ValidateIdentifier("effort", u.Effort); err != nil {
			return err
		}
	}
	if u.ExpectedVersion < 0 {
		return ErrInvalidInput("expected_version cannot be negative")
	}
	return nil
}
