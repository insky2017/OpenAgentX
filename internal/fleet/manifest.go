package fleet

import (
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
	"openagentx/internal/domain"
)

const (
	ManifestVersion = 1
	SessionName     = "agentx"
	OverviewWindow  = "overview"
)

type Manifest struct {
	Version int     `yaml:"version"`
	Session string  `yaml:"session,omitempty"`
	Agents  []Agent `yaml:"agents"`
}

type Agent struct {
	AgentID      string `yaml:"agent_id"`
	IdentityFile string `yaml:"identity_file"`
	WorkerConfig string `yaml:"worker_config"`
	Enabled      bool   `yaml:"enabled"`
}

func LoadFile(path string) (Manifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("open Fleet manifest: %w", err)
	}
	defer file.Close()
	return Decode(file)
}

func Decode(reader io.Reader) (Manifest, error) {
	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode Fleet manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Manifest{}, fmt.Errorf("Fleet manifest must contain exactly one YAML document")
	}
	if manifest.Session == "" {
		manifest.Session = SessionName
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (m Manifest) Validate() error {
	if m.Version != ManifestVersion {
		return fmt.Errorf("Fleet manifest version must be %d", ManifestVersion)
	}
	if m.Session != SessionName {
		return fmt.Errorf("Fleet tmux session must be %q", SessionName)
	}
	if len(m.Agents) == 0 {
		return fmt.Errorf("Fleet manifest must explicitly list at least one Agent")
	}
	seen := make(map[string]struct{}, len(m.Agents))
	for index, agent := range m.Agents {
		if err := domain.ValidateIdentifier("agent_id", agent.AgentID); err != nil {
			return fmt.Errorf("Fleet Agent %d: %w", index, err)
		}
		if agent.AgentID == OverviewWindow {
			return fmt.Errorf("Fleet Agent %q conflicts with reserved overview window", agent.AgentID)
		}
		if _, exists := seen[agent.AgentID]; exists {
			return fmt.Errorf("Fleet Agent %q is duplicated", agent.AgentID)
		}
		seen[agent.AgentID] = struct{}{}
		if strings.TrimSpace(agent.IdentityFile) == "" || strings.TrimSpace(agent.WorkerConfig) == "" {
			return fmt.Errorf("Fleet Agent %q requires identity_file and worker_config", agent.AgentID)
		}
	}
	return nil
}

func (m Manifest) WindowNames() []string {
	windows := make([]string, 0, len(m.Agents)+1)
	windows = append(windows, OverviewWindow)
	for _, agent := range m.Agents {
		windows = append(windows, agent.AgentID)
	}
	return windows
}
