package domain

import "time"

type WorkerTransport string

const (
	WorkerTransportUnix  WorkerTransport = "unix"
	WorkerTransportHTTPS WorkerTransport = "https"
)

func (t WorkerTransport) Valid() bool {
	return t == WorkerTransportUnix || t == WorkerTransportHTTPS
}

type WorkerStatus string

const (
	WorkerStatusBootstrapping WorkerStatus = "bootstrapping"
	WorkerStatusOnline        WorkerStatus = "online"
	WorkerStatusDegraded      WorkerStatus = "degraded"
	WorkerStatusDraining      WorkerStatus = "draining"
	WorkerStatusOffline       WorkerStatus = "offline"
)

func (s WorkerStatus) Valid() bool {
	switch s {
	case WorkerStatusBootstrapping, WorkerStatusOnline, WorkerStatusDegraded, WorkerStatusDraining, WorkerStatusOffline:
		return true
	default:
		return false
	}
}

type WorkerInstance struct {
	ID                     string          `json:"worker_instance_id"`
	AgentID                string          `json:"agent_id"`
	Generation             int64           `json:"generation"`
	Transport              WorkerTransport `json:"transport"`
	AuthenticatedPrincipal string          `json:"authenticated_principal"`
	Capabilities           []string        `json:"capabilities"`
	Status                 WorkerStatus    `json:"status"`
	LastHeartbeatAt        time.Time       `json:"last_heartbeat_at"`
	LeaseUntil             time.Time       `json:"lease_until"`
	FencingToken           int64           `json:"fencing_token"`
	StartedAt              time.Time       `json:"started_at"`
	UpdatedAt              time.Time       `json:"updated_at"`
}

func (w WorkerInstance) Validate() error {
	if err := ValidateOpaqueID("worker_instance_id", w.ID); err != nil {
		return err
	}
	if err := ValidateIdentifier("agent_id", w.AgentID); err != nil {
		return err
	}
	if err := ValidatePositiveVersion("generation", w.Generation); err != nil {
		return err
	}
	if !w.Transport.Valid() {
		return ErrInvalidInput("unsupported worker transport")
	}
	if err := ValidateOpaqueID("authenticated_principal", w.AuthenticatedPrincipal); err != nil {
		return err
	}
	if !w.Status.Valid() {
		return ErrInvalidInput("unsupported worker status")
	}
	if err := ValidatePositiveVersion("fencing_token", w.FencingToken); err != nil {
		return err
	}
	for _, capability := range w.Capabilities {
		if err := ValidateIdentifier("capability", capability); err != nil {
			return err
		}
	}
	return nil
}

type WorkerCommandKind string

const (
	WorkerCommandDrain       WorkerCommandKind = "drain"
	WorkerCommandStop        WorkerCommandKind = "stop"
	WorkerCommandForceStop   WorkerCommandKind = "force_stop"
	WorkerCommandHealthCheck WorkerCommandKind = "health_check"
)

func (k WorkerCommandKind) Valid() bool {
	return k == WorkerCommandDrain || k == WorkerCommandStop || k == WorkerCommandForceStop || k == WorkerCommandHealthCheck
}

type WorkerCommandState string

const (
	WorkerCommandPending WorkerCommandState = "pending"
	WorkerCommandClaimed WorkerCommandState = "claimed"
	WorkerCommandApplied WorkerCommandState = "applied"
	WorkerCommandFailed  WorkerCommandState = "failed"
)

func (s WorkerCommandState) Valid() bool {
	return s == WorkerCommandPending || s == WorkerCommandClaimed || s == WorkerCommandApplied || s == WorkerCommandFailed
}

type WorkerCommand struct {
	ID               string             `json:"worker_command_id"`
	WorkerInstanceID string             `json:"worker_instance_id"`
	Generation       int64              `json:"generation"`
	Kind             WorkerCommandKind  `json:"kind"`
	State            WorkerCommandState `json:"state"`
	RequestedBy      string             `json:"requested_by"`
	IdempotencyKey   string             `json:"idempotency_key"`
	LeaseUntil       *time.Time         `json:"lease_until,omitempty"`
	Attempts         int                `json:"attempts"`
	CreatedAt        time.Time          `json:"created_at"`
	ClaimedAt        *time.Time         `json:"claimed_at,omitempty"`
	AppliedAt        *time.Time         `json:"applied_at,omitempty"`
	Result           string             `json:"result,omitempty"`
}

func (c WorkerCommand) Validate() error {
	if err := ValidateOpaqueID("worker_command_id", c.ID); err != nil {
		return err
	}
	if err := ValidateOpaqueID("worker_instance_id", c.WorkerInstanceID); err != nil {
		return err
	}
	if err := ValidatePositiveVersion("generation", c.Generation); err != nil {
		return err
	}
	if !c.Kind.Valid() || !c.State.Valid() {
		return ErrInvalidInput("unsupported worker command kind or state")
	}
	if err := ValidateOpaqueID("requested_by", c.RequestedBy); err != nil {
		return err
	}
	if err := ValidateOpaqueID("idempotency_key", c.IdempotencyKey); err != nil {
		return err
	}
	if c.Attempts < 0 {
		return ErrInvalidInput("worker command attempts cannot be negative")
	}
	return nil
}
