package domain

import "time"

type WorkerRegistration struct {
	WorkerInstanceID   string
	AgentID            string
	Transport          WorkerTransport
	PrincipalID        string
	Capabilities       []string
	SessionTokenDigest string
	TokenExpiresAt     time.Time
	LeaseUntil         time.Time
}

func (r WorkerRegistration) Validate() error {
	if err := ValidateOpaqueID("worker_instance_id", r.WorkerInstanceID); err != nil {
		return err
	}
	if err := ValidateIdentifier("agent_id", r.AgentID); err != nil {
		return err
	}
	if !r.Transport.Valid() {
		return ErrInvalidInput("unsupported worker transport")
	}
	if err := ValidateOpaqueID("principal_id", r.PrincipalID); err != nil {
		return err
	}
	if err := ValidateOpaqueID("session_token_digest", r.SessionTokenDigest); err != nil {
		return err
	}
	if r.TokenExpiresAt.IsZero() || r.LeaseUntil.IsZero() {
		return ErrInvalidInput("worker token expiry and lease are required")
	}
	for _, capability := range r.Capabilities {
		if err := ValidateIdentifier("capability", capability); err != nil {
			return err
		}
	}
	return nil
}

type WorkerCredential struct {
	Worker             WorkerInstance
	SessionTokenDigest string
	TokenExpiresAt     time.Time
}

type WorkerWriteGuard struct {
	WorkerInstanceID   string
	AgentID            string
	PrincipalID        string
	SessionTokenDigest string
	Generation         int64
	FencingToken       int64
	CheckedAt          time.Time
}

func (g WorkerWriteGuard) Validate() error {
	if err := ValidateOpaqueID("worker_instance_id", g.WorkerInstanceID); err != nil {
		return err
	}
	if err := ValidateIdentifier("agent_id", g.AgentID); err != nil {
		return err
	}
	if err := ValidateOpaqueID("principal_id", g.PrincipalID); err != nil {
		return err
	}
	if err := ValidateOpaqueID("session_token_digest", g.SessionTokenDigest); err != nil {
		return err
	}
	if err := ValidatePositiveVersion("generation", g.Generation); err != nil {
		return err
	}
	if err := ValidatePositiveVersion("fencing_token", g.FencingToken); err != nil {
		return err
	}
	if g.CheckedAt.IsZero() {
		return ErrInvalidInput("worker guard checked_at is required")
	}
	return nil
}
