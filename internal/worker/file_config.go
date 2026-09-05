package worker

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"openagentx/internal/domain"
)

type RuntimeBackendConfig struct {
	BackendID string               `yaml:"backend_id"`
	AdapterID string               `yaml:"adapter_id"`
	Options   map[string]any       `yaml:"options,omitempty"`
	Network   domain.NetworkPolicy `yaml:"network,omitempty"`
}

type ProcessConfig struct {
	Version                   int                    `yaml:"version"`
	AgentID                   string                 `yaml:"agent_id"`
	Transport                 domain.WorkerTransport `yaml:"transport"`
	UnixSocket                string                 `yaml:"unix_socket"`
	Endpoint                  string                 `yaml:"endpoint"`
	CAFile                    string                 `yaml:"ca_file"`
	ClientCertFile            string                 `yaml:"client_cert_file"`
	ClientKeyFile             string                 `yaml:"client_key_file"`
	ServerName                string                 `yaml:"server_name"`
	Capabilities              []string               `yaml:"capabilities"`
	HeartbeatInterval         time.Duration          `yaml:"heartbeat_interval"`
	MailboxWait               time.Duration          `yaml:"mailbox_wait"`
	ControlWait               time.Duration          `yaml:"control_wait"`
	ShutdownTimeout           time.Duration          `yaml:"shutdown_timeout"`
	EnableWorkerControl       *bool                  `yaml:"enable_worker_control,omitempty"`
	NetworkMaterializationDir string                 `yaml:"network_materialization_dir,omitempty"`
	RuntimeBackendConfig      []RuntimeBackendConfig `yaml:"runtime_backends"`
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
	switch c.Transport {
	case domain.WorkerTransportUnix:
		if strings.TrimSpace(c.UnixSocket) == "" {
			return domain.ErrInvalidInput("unix_socket is required for unix transport")
		}
		if hasRemoteBinding(c) {
			return domain.ErrInvalidInput("https Worker fields are not allowed with unix transport")
		}
	case domain.WorkerTransportHTTPS:
		if strings.TrimSpace(c.UnixSocket) != "" {
			return domain.ErrInvalidInput("unix_socket is not allowed with https transport")
		}
		if err := validateHTTPSWorkerEndpoint(c); err != nil {
			return err
		}
	default:
		return domain.ErrInvalidInput("unsupported Worker transport")
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
		if err := backend.Network.Validate(); err != nil {
			return fmt.Errorf("Worker Backend %s network: %w", backend.BackendID, err)
		}
	}
	if c.NetworkMaterializationDir != "" && strings.ContainsAny(c.NetworkMaterializationDir, "\r\n") {
		return domain.ErrInvalidInput("network_materialization_dir is invalid")
	}
	return nil
}

func hasRemoteBinding(c *ProcessConfig) bool {
	return strings.TrimSpace(c.Endpoint) != "" || strings.TrimSpace(c.CAFile) != "" ||
		strings.TrimSpace(c.ClientCertFile) != "" || strings.TrimSpace(c.ClientKeyFile) != "" ||
		strings.TrimSpace(c.ServerName) != ""
}

func validateHTTPSWorkerEndpoint(c *ProcessConfig) error {
	if strings.TrimSpace(c.Endpoint) == "" {
		return domain.ErrInvalidInput("endpoint is required for https transport")
	}
	parsed, err := url.Parse(strings.TrimSpace(c.Endpoint))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return domain.ErrInvalidInput("endpoint must be an https URL without credentials, query, or fragment")
	}
	for name, value := range map[string]string{
		"ca_file": c.CAFile, "client_cert_file": c.ClientCertFile, "client_key_file": c.ClientKeyFile,
	} {
		if strings.TrimSpace(value) == "" {
			return domain.ErrInvalidInput(name + " is required for https transport")
		}
	}
	if strings.ContainsAny(c.ServerName, "\r\n") {
		return domain.ErrInvalidInput("server_name must not contain control characters")
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
