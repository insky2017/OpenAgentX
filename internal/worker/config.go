package worker

import (
	"fmt"
	"time"

	"openagentx/internal/domain"
)

type Config struct {
	AgentID           string
	WorkerInstanceID  string
	Transport         domain.WorkerTransport
	Capabilities      []string
	HeartbeatInterval time.Duration
	MailboxWait       time.Duration
	ControlWait       time.Duration
	ShutdownTimeout   time.Duration
	EnableControlLoop bool
}

func (c Config) withDefaults() Config {
	if c.Transport == "" {
		c.Transport = domain.WorkerTransportUnix
	}
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = 10 * time.Second
	}
	if c.MailboxWait <= 0 {
		c.MailboxWait = 30 * time.Second
	}
	if c.ControlWait <= 0 {
		c.ControlWait = 30 * time.Second
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = 30 * time.Second
	}
	return c
}

func (c Config) Validate() error {
	if err := domain.ValidateIdentifier("agent_id", c.AgentID); err != nil {
		return err
	}
	if err := domain.ValidateOpaqueID("worker_instance_id", c.WorkerInstanceID); err != nil {
		return err
	}
	if !c.Transport.Valid() {
		return domain.ErrInvalidInput("unsupported Worker transport")
	}
	for _, capability := range c.Capabilities {
		if err := domain.ValidateIdentifier("capability", capability); err != nil {
			return err
		}
	}
	intervals := []struct {
		name  string
		value time.Duration
	}{
		{name: "heartbeat_interval", value: c.HeartbeatInterval},
		{name: "mailbox_wait", value: c.MailboxWait},
		{name: "control_wait", value: c.ControlWait},
		{name: "shutdown_timeout", value: c.ShutdownTimeout},
	}
	for _, interval := range intervals {
		if interval.value <= 0 {
			return fmt.Errorf("%s must be positive", interval.name)
		}
	}
	if c.MailboxWait > 30*time.Second || c.ControlWait > 30*time.Second ||
		c.MailboxWait%time.Second != 0 || c.ControlWait%time.Second != 0 {
		return domain.ErrInvalidInput("Worker poll waits must be whole seconds no greater than 30s")
	}
	return nil
}
