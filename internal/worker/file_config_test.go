package worker

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

func writeTestConfig(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
