package fleet

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
	fleetmodel "openagentx/internal/fleet"
	runtimenetwork "openagentx/internal/runtime/network"
)

// Runtime inputs are saved in the canonical private systemd EnvironmentFile.
// Codex defaults come from a persistent local proxy configuration, never a guessed port.
func persistAgentEnvironment(o agentOptions, configPath string, deps Dependencies) error {
	path := strings.TrimSuffix(configPath, ".yaml") + ".env"
	if _, err := os.Lstat(path); err == nil {
		_, err = fleetmodel.ReadSecureFile(path, fleetmodel.SecureFileOptions{MaximumBytes: 1 << 20, RequirePrivate: true})
		return err
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	names := []string{"AGY_GRAFT_NATIVE_PROXY", "AGY_GRAFT_REAL_BIN", "AGY_GRAFT_MGRAFTCP_BIN", "AGY_GRAFT_GOMAXPROCS", "AGY_GRAFT_IPV4_ONLY", "AGY_GRAFT_IPV4_ONLY_FILE", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "all_proxy", "no_proxy"}
	var values []string
	if o.runtime == "codex" {
		names = []string{"CODEX_HOME", "OPENAI_API_KEY", "OPENAI_BASE_URL", "NO_PROXY", "no_proxy"}
		home, err := deps.UserHomeDir()
		if err != nil {
			return err
		}
		proxy, err := persistentAgentProxy(home)
		if err != nil {
			return err
		}
		if proxy != "" {
			for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
				values = append(values, name+"="+proxy)
			}
		}
		if os.Getenv("CODEX_HOME") == "" {
			values = append(values, "CODEX_HOME="+filepath.Join(home, ".codex"))
		}
	}
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			values = append(values, name+"="+value)
		}
	}
	if o.runtime == "codex" {
		values = runtimenetwork.WithLoopbackNoProxy(values)
	}
	var encoded strings.Builder
	for _, entry := range values {
		name, value, _ := strings.Cut(entry, "=")
		if strings.ContainsAny(value, "\r\n\x00") {
			return fmt.Errorf("environment %s must be a single line", name)
		}
		value = strings.ReplaceAll(strings.ReplaceAll(value, "\\", "\\\\"), "\"", "\\\"")
		fmt.Fprintf(&encoded, "%s=\"%s\"\n", name, value)
	}
	_, err := fleetmodel.WriteExactFileAtomic(path, []byte(encoded.String()), fleetmodel.AtomicFileOptions{})
	return err
}

func persistentAgentProxy(home string) (string, error) {
	path := filepath.Join(home, ".config", "mihomo", "config.yaml")
	data, err := fleetmodel.ReadSecureFile(path, fleetmodel.SecureFileOptions{MaximumBytes: 4 << 20})
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read persistent proxy configuration: %w", err)
	}
	var document yaml.Node
	if err = yaml.Unmarshal(data, &document); err != nil {
		return "", fmt.Errorf("invalid persistent proxy YAML; use --environment-file")
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return "", fmt.Errorf("persistent proxy configuration must be a mapping")
	}
	mapping := document.Content[0]
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == "mixed-port" {
			port, e := strconv.Atoi(mapping.Content[i+1].Value)
			if e != nil || port < 1 || port > 65535 {
				return "", fmt.Errorf("persistent proxy mixed-port must be 1..65535")
			}
			return "http://127.0.0.1:" + strconv.Itoa(port), nil
		}
	}
	return "", nil
}
