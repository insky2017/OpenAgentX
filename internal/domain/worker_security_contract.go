package domain

import (
	"crypto/subtle"
	"time"
)

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

func (c WorkerCredential) Authorize(guard WorkerWriteGuard) error {
	if err := guard.Validate(); err != nil {
		return err
	}
	if c.Worker.ID != guard.WorkerInstanceID ||
		c.Worker.AuthenticatedPrincipal != guard.PrincipalID ||
		subtle.ConstantTimeCompare([]byte(c.SessionTokenDigest), []byte(guard.SessionTokenDigest)) != 1 {
		return ErrUnauthorized
	}
	if c.Worker.AgentID != guard.AgentID {
		return ErrForbidden("Worker is not bound to requested Agent")
	}
	if c.Worker.Generation != guard.Generation {
		return ErrSessionGenerationConflict
	}
	if c.Worker.FencingToken != guard.FencingToken {
		return ErrFencingRejected
	}
	if !guard.CheckedAt.Before(c.TokenExpiresAt) {
		return ErrUnauthorized
	}
	if !guard.CheckedAt.Before(c.Worker.LeaseUntil) || c.Worker.Status == WorkerStatusOffline {
		return ErrLeaseExpired
	}
	return nil
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
