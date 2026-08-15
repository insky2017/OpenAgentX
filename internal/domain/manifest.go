package domain

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type AgentManifest struct {
	Version      int      `yaml:"version" json:"version"`
	ID           string   `yaml:"id" json:"id"`
	Role         string   `yaml:"role" json:"role"`
	Runtime      string   `yaml:"runtime" json:"runtime"`
	Connector    string   `yaml:"connector" json:"connector"`
	Address      string   `yaml:"address" json:"address"`
	Workspace    string   `yaml:"workspace" json:"workspace"`
	Instructions string   `yaml:"instructions" json:"instructions"`
	Capabilities []string `yaml:"capabilities" json:"capabilities"`
}

func LoadManifest(configPath string, repoRoot string) (*AgentManifest, *AgentProfile, *Agent, error) {
	cleanConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: failed to resolve config path: %v", ErrInvalidManifest, err)
	}

	fi, err := os.Stat(cleanConfigPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: config file not found '%s': %v", ErrInvalidManifest, cleanConfigPath, err)
	}
	if fi.IsDir() || !fi.Mode().IsRegular() {
		return nil, nil, nil, fmt.Errorf("%w: config file '%s' is not a regular file", ErrInvalidManifest, cleanConfigPath)
	}
	if fi.Size() == 0 {
		return nil, nil, nil, fmt.Errorf("%w: config file '%s' is empty", ErrInvalidManifest, cleanConfigPath)
	}
	if fi.Size() > (1 << 20) {
		return nil, nil, nil, fmt.Errorf("%w: config file '%s' exceeds 1 MiB limit", ErrInvalidManifest, cleanConfigPath)
	}

	data, err := os.ReadFile(cleanConfigPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: failed to read config file '%s': %v", ErrInvalidManifest, cleanConfigPath, err)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	var manifest AgentManifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, nil, nil, fmt.Errorf("%w: YAML decode error in '%s': %v", ErrInvalidManifest, cleanConfigPath, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, nil, nil, fmt.Errorf("%w: config file '%s' contains multiple YAML documents or trailing data", ErrInvalidManifest, cleanConfigPath)
	}

	// Validate fields
	if manifest.Version != 1 {
		return nil, nil, nil, fmt.Errorf("%w: version must be 1, got %d", ErrInvalidManifest, manifest.Version)
	}
	if !identifierRegex.MatchString(manifest.ID) {
		return nil, nil, nil, fmt.Errorf("%w: invalid agent id '%s'", ErrInvalidManifest, manifest.ID)
	}
	if !identifierRegex.MatchString(manifest.Role) {
		return nil, nil, nil, fmt.Errorf("%w: invalid role '%s'", ErrInvalidManifest, manifest.Role)
	}
	if !identifierRegex.MatchString(manifest.Runtime) {
		return nil, nil, nil, fmt.Errorf("%w: invalid runtime '%s'", ErrInvalidManifest, manifest.Runtime)
	}

	manifest.Connector = strings.TrimSpace(manifest.Connector)
	if manifest.Connector != ConnectorTmux && manifest.Connector != ConnectorNone {
		return nil, nil, nil, fmt.Errorf("%w: connector must be 'tmux' or 'none', got '%s'", ErrInvalidManifest, manifest.Connector)
	}

	manifest.Address = strings.TrimSpace(manifest.Address)
	if strings.ContainsAny(manifest.Address, "\r\n\t") {
		return nil, nil, nil, fmt.Errorf("%w: address cannot contain control characters", ErrInvalidManifest)
	}

	// Resolve workspace
	wsPath := manifest.Workspace
	if wsPath == "" {
		wsPath = "."
	}
	var resolvedWorkspace string
	if filepath.IsAbs(wsPath) {
		resolvedWorkspace = filepath.Clean(wsPath)
	} else {
		if repoRoot == "" {
			repoRoot = filepath.Dir(filepath.Dir(cleanConfigPath))
		}
		resolvedWorkspace = filepath.Clean(filepath.Join(repoRoot, wsPath))
	}
	wsFi, err := os.Stat(resolvedWorkspace)
	if err != nil || !wsFi.IsDir() {
		return nil, nil, nil, fmt.Errorf("%w: workspace directory '%s' does not exist or is not a directory", ErrInvalidManifest, resolvedWorkspace)
	}

	// Resolve instructions path (relative to manifest directory)
	instrPath := manifest.Instructions
	if strings.TrimSpace(instrPath) == "" {
		return nil, nil, nil, fmt.Errorf("%w: instructions path cannot be empty", ErrInvalidManifest)
	}
	var resolvedInstructions string
	if filepath.IsAbs(instrPath) {
		resolvedInstructions = filepath.Clean(instrPath)
	} else {
		resolvedInstructions = filepath.Clean(filepath.Join(filepath.Dir(cleanConfigPath), instrPath))
	}

	instrFi, err := os.Stat(resolvedInstructions)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: instructions file '%s' not found: %v", ErrInvalidManifest, resolvedInstructions, err)
	}
	if instrFi.IsDir() || !instrFi.Mode().IsRegular() {
		return nil, nil, nil, fmt.Errorf("%w: instructions file '%s' is not a regular file", ErrInvalidManifest, resolvedInstructions)
	}
	if instrFi.Size() == 0 {
		return nil, nil, nil, fmt.Errorf("%w: instructions file '%s' is empty", ErrInvalidManifest, resolvedInstructions)
	}
	if instrFi.Size() > (1 << 20) {
		return nil, nil, nil, fmt.Errorf("%w: instructions file '%s' exceeds 1 MiB limit", ErrInvalidManifest, resolvedInstructions)
	}

	// Verify readability
	f, err := os.Open(resolvedInstructions)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: instructions file '%s' is not readable: %v", ErrInvalidManifest, resolvedInstructions, err)
	}
	_ = f.Close()

	// Validate capabilities
	capMap := make(map[string]bool)
	var cleanCaps []string
	for _, cap := range manifest.Capabilities {
		trimmed := strings.TrimSpace(cap)
		if !identifierRegex.MatchString(trimmed) {
			return nil, nil, nil, fmt.Errorf("%w: invalid capability identifier '%s'", ErrInvalidManifest, trimmed)
		}
		if !capMap[trimmed] {
			capMap[trimmed] = true
			cleanCaps = append(cleanCaps, trimmed)
		}
	}
	sort.Strings(cleanCaps)
	manifest.Capabilities = cleanCaps

	agent := &Agent{
		ID:        manifest.ID,
		Role:      manifest.Role,
		Connector: manifest.Connector,
		Address:   manifest.Address,
		Status:    AgentStatusRegistered,
	}

	profile := &AgentProfile{
		AgentID:          manifest.ID,
		ManifestVersion:  manifest.Version,
		Runtime:          manifest.Runtime,
		Workspace:        resolvedWorkspace,
		ConfigPath:       cleanConfigPath,
		InstructionsPath: resolvedInstructions,
		Capabilities:     cleanCaps,
	}

	return &manifest, profile, agent, nil
}
