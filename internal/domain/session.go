package domain

const (
	SessionStatusBootstrapping  = "bootstrapping"
	SessionStatusReady          = "ready"
	SessionStatusDeliveryFailed = "delivery_failed"
)

type AgentSession struct {
	AgentID        string  `json:"agent_id"`
	Generation     int64   `json:"generation"`
	Status         string  `json:"status"`
	ResolvedPaneID string  `json:"resolved_pane_id"`
	DeliveryError  *string `json:"delivery_error,omitempty"`
	StartedAt      string  `json:"started_at"`
	ReadyAt        *string `json:"ready_at,omitempty"`
	UpdatedAt      string  `json:"updated_at"`
}

func (s *AgentSession) IsReady() bool {
	return s != nil && s.Status == SessionStatusReady
}
