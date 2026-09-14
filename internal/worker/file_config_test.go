package worker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openagentx/internal/domain"
)

func TestLoadProcessConfigIsStrictAndGeneratesRunnerConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.yaml")
	writeTestConfig(t, path, `version: 1
agent_id: quote
transport: unix
unix_socket: /run/openagentx/openagentx.sock
capabilities: [coding]
heartbeat_interval: 5s
mailbox_wait: 20s
control_wait: 10s
shutdown_timeout: 15s
runtime_backends:
  - backend_id: local
    adapter_id: fake
    options:
      fixture: blocking
`)
	config, err := LoadProcessConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	runner := config.RunnerConfig("worker-process-1")
	if runner.AgentID != "quote" || runner.WorkerInstanceID != "worker-process-1" ||
		runner.HeartbeatInterval != 5*time.Second || !runner.EnableControlLoop {
		t.Fatalf("unexpected Runner config: %+v", runner)
	}

	unknown := filepath.Join(t.TempDir(), "unknown.yaml")
	writeTestConfig(t, unknown, `version: 1
agent_id: quote
transport: unix
unix_socket: /run/openagentx/openagentx.sock
legacy_terminal_field: deprecated
runtime_backends:
  - backend_id: local
    adapter_id: fake
`)
	if _, err := LoadProcessConfig(unknown); err == nil {
		t.Fatal("strict Worker config accepted a legacy/unknown field")
	}
}

func TestDecodeProcessConfigUsesCapturedStrictDocument(t *testing.T) {
	content := `version: 1
agent_id: quote
transport: unix
unix_socket: /run/openagentx/openagentx.sock
runtime_backends:
  - backend_id: local
    adapter_id: fake
`
	config, err := DecodeProcessConfig(strings.NewReader(content))
	if err != nil || config.AgentID != "quote" {
		t.Fatalf("config=%+v err=%v", config, err)
	}
	if _, err := DecodeProcessConfig(strings.NewReader(content + "---\nversion: 1\n")); err == nil {
		t.Fatal("multiple Worker config documents accepted")
	}
}

func TestLoadProcessConfigRejectsSymlink(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target.yaml")
	writeTestConfig(t, target, `version: 1
agent_id: quote
transport: unix
unix_socket: /run/openagentx/openagentx.sock
runtime_backends:
  - backend_id: local
    adapter_id: fake
`)
	link := filepath.Join(directory, "link.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProcessConfig(link); err == nil {
		t.Fatal("Worker config symlink accepted")
	}
}

func TestLoadProcessConfigSupportsStrictHTTPSBinding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "remote-agent.yaml")
	writeTestConfig(t, path, `version: 1
agent_id: quote
transport: https
endpoint: https://worker-gateway.example.test:18101
ca_file: /etc/openagentx/worker-ca.pem
client_cert_file: /etc/openagentx/worker-client.pem
client_key_file: /etc/openagentx/worker-client.key
server_name: worker-gateway.example.test
runtime_backends:
  - backend_id: local
    adapter_id: fake
`)
	config, err := LoadProcessConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.Transport != domain.WorkerTransportHTTPS || config.Endpoint == "" || config.UnixSocket != "" {
		t.Fatalf("unexpected HTTPS config=%+v", config)
	}

	cases := map[string]string{
		"unix with remote fields": `version: 1
agent_id: quote
transport: unix
unix_socket: /run/openagentx.sock
endpoint: https://worker-gateway.example.test:18101
runtime_backends:
  - backend_id: local
    adapter_id: fake
`,
		"https with unix socket": `version: 1
agent_id: quote
transport: https
unix_socket: /run/openagentx.sock
endpoint: https://worker-gateway.example.test:18101
ca_file: ca.pem
client_cert_file: client.pem
client_key_file: client.key
runtime_backends:
  - backend_id: local
    adapter_id: fake
`,
		"https missing endpoint": `version: 1
agent_id: quote
transport: https
ca_file: ca.pem
client_cert_file: client.pem
client_key_file: client.key
runtime_backends:
  - backend_id: local
    adapter_id: fake
`,
		"https endpoint credentials": `version: 1
agent_id: quote
transport: https
endpoint: https://user:password@worker-gateway.example.test:18101
ca_file: ca.pem
client_cert_file: client.pem
client_key_file: client.key
runtime_backends:
  - backend_id: local
    adapter_id: fake
`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			casePath := filepath.Join(t.TempDir(), "agent.yaml")
			writeTestConfig(t, casePath, content)
			if _, err := LoadProcessConfig(casePath); err == nil {
				t.Fatal("invalid transport binding was accepted")
			}
		})
	}
}

func writeTestConfig(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
