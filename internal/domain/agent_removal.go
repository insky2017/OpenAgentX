package domain

// AgentRemovalPlan contains metadata only; selected row contents never leave persistence.
type AgentRemovalPlan struct {
	PreservedReferences map[string]int64 `json:"preserved_references"`
	AgentIDs            []string         `json:"agent_ids"`
	InstallationID      string           `json:"installation_id"`
	SchemaVersion       int              `json:"schema_version"`
	Counts              map[string]int64 `json:"counts"`
	Replayed            bool             `json:"replayed"`
	WorkersToStop       []string         `json:"workers_to_stop"`
	Blockers            []string         `json:"blockers"`
	Digest              string           `json:"digest"`
}

type AgentRemovalResult struct {
	PreservedReferences map[string]int64 `json:"preserved_references"`
	AgentIDs            []string         `json:"agent_ids"`
	Digest              string           `json:"digest"`
	Counts              map[string]int64 `json:"counts"`
	Replayed            bool             `json:"replayed"`
}
