package domain

// RuntimeSessionReference explicitly selects an existing native work session.
// An absent reference keeps the normal new-session behavior. The provider ID
// is private routing data and is not copied into public observation records.
type RuntimeSessionReference struct {
	BackendID         string `json:"backend_id"`
	ProviderSessionID string `json:"provider_session_id,omitempty"`
	SourceTaskID      string `json:"source_task_id,omitempty"`
}

func (r RuntimeSessionReference) Validate() error {
	if err := ValidateIdentifier("backend_id", r.BackendID); err != nil {
		return err
	}
	if (r.ProviderSessionID == "") == (r.SourceTaskID == "") {
		return ErrInvalidInput("runtime_session requires exactly one provider_session_id or source_task_id")
	}
	if r.ProviderSessionID != "" {
		return ValidateOpaqueID("provider_session_id", r.ProviderSessionID)
	}
	return ValidateOpaqueID("source_task_id", r.SourceTaskID)
}
