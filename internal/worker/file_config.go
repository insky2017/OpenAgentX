package worker

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"openagentx/internal/domain"
)

type RuntimeBackendConfig struct {
	BackendID string         `yaml:"backend_id"`
	AdapterID string         `yaml:"adapter_id"`
	Options   map[string]any `yaml:"options,omitempty"`
}

type ProcessConfig struct {
	Version              int                    `yaml:"version"`
	AgentID              string                 `yaml:"agent_id"`
	Transport            domain.WorkerTransport `yaml:"transport"`
	UnixSocket           string                 `yaml:"unix_socket"`
	Capabilities         []string               `yaml:"capabilities"`
	HeartbeatInterval    time.Duration          `yaml:"heartbeat_interval"`
	MailboxWait          time.Duration          `yaml:"mailbox_wait"`
	ControlWait          time.Duration          `yaml:"control_wait"`
	ShutdownTimeout      time.Duration          `yaml:"shutdown_timeout"`
	EnableWorkerControl  *bool                  `yaml:"enable_worker_control,omitempty"`
	RuntimeBackendConfig []RuntimeBackendConfig `yaml:"runtime_backends"`
}

func LoadProcessConfig(path string) (*ProcessConfig, error) {
	if strings.TrimSpace(path) == "" {
		return nil, domain.ErrInvalidInput("Worker config path is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open Worker config: %w", err)
	}
	defer file.Close()
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	var config ProcessConfig
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("decode Worker config: %w", err)
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &config, nil
}

func (c *ProcessConfig) Validate() error {
	if c == nil || c.Version != 1 {
		return domain.ErrInvalidInput("Worker config version must be 1")
	}
	if err := domain.ValidateIdentifier("agent_id", c.AgentID); err != nil {
		return err
	}
	if c.Transport != domain.WorkerTransportUnix {
		return domain.ErrInvalidInput("M1 Worker process requires unix transport")
	}
	if strings.TrimSpace(c.UnixSocket) == "" {
		return domain.ErrInvalidInput("unix_socket is required")
	}
	if len(c.RuntimeBackendConfig) == 0 {
		return domain.ErrInvalidInput("Worker config requires at least one Runtime Backend")
	}
	seen := make(map[string]struct{}, len(c.RuntimeBackendConfig))
	for _, backend := range c.RuntimeBackendConfig {
		if err := domain.ValidateIdentifier("backend_id", backend.BackendID); err != nil {
			return err
		}
		if err := domain.ValidateIdentifier("adapter_id", backend.AdapterID); err != nil {
			return err
		}
		if _, exists := seen[backend.BackendID]; exists {
			return domain.ErrInvalidInput("Worker config Backend IDs must be unique")
		}
		seen[backend.BackendID] = struct{}{}
	}
	return nil
}

func (c *ProcessConfig) RunnerConfig(workerInstanceID string) Config {
	enableControl := true
	if c.EnableWorkerControl != nil {
		enableControl = *c.EnableWorkerControl
	}
	return Config{
		AgentID: c.AgentID, WorkerInstanceID: workerInstanceID, Transport: c.Transport,
		Capabilities:      append([]string(nil), c.Capabilities...),
		HeartbeatInterval: c.HeartbeatInterval, MailboxWait: c.MailboxWait,
		ControlWait: c.ControlWait, ShutdownTimeout: c.ShutdownTimeout,
		EnableControlLoop: enableControl,
	}.withDefaults()
}
