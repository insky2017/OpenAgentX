package domain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type AgentProfile struct {
	AgentID          string   `json:"agent_id"`
	ManifestVersion  int      `json:"manifest_version"`
	Runtime          string   `json:"runtime"`
	Workspace        string   `json:"workspace"`
	ConfigPath       string   `json:"config_path"`
	InstructionsPath string   `json:"instructions_path"`
	Capabilities     []string `json:"capabilities"`
	UpdatedAt        string   `json:"updated_at"`
}

func (p *AgentProfile) Validate() error {
	if p == nil {
		return fmt.Errorf("%w: profile is nil", ErrInvalidManifest)
	}
	if !identifierRegex.MatchString(p.AgentID) {
		return fmt.Errorf("%w: invalid agent_id '%s'", ErrInvalidManifest, p.AgentID)
	}
	if p.ManifestVersion != 1 {
		return fmt.Errorf("%w: manifest_version must be 1, got %d", ErrInvalidManifest, p.ManifestVersion)
	}
	if !identifierRegex.MatchString(p.Runtime) {
		return fmt.Errorf("%w: invalid runtime '%s'", ErrInvalidManifest, p.Runtime)
	}
	if strings.TrimSpace(p.Workspace) == "" {
		return fmt.Errorf("%w: workspace cannot be empty", ErrInvalidManifest)
	}
	if !filepath.IsAbs(p.Workspace) {
		return fmt.Errorf("%w: workspace must be an absolute path: '%s'", ErrInvalidManifest, p.Workspace)
	}
	if wsFi, err := os.Stat(p.Workspace); err != nil || !wsFi.IsDir() {
		return fmt.Errorf("%w: workspace directory '%s' does not exist or is not a directory", ErrInvalidManifest, p.Workspace)
	}

	if strings.TrimSpace(p.ConfigPath) == "" {
		return fmt.Errorf("%w: config_path cannot be empty", ErrInvalidManifest)
	}
	if !filepath.IsAbs(p.ConfigPath) {
		return fmt.Errorf("%w: config_path must be an absolute path: '%s'", ErrInvalidManifest, p.ConfigPath)
	}

	if strings.TrimSpace(p.InstructionsPath) == "" {
		return fmt.Errorf("%w: instructions_path cannot be empty", ErrInvalidManifest)
	}
	if !filepath.IsAbs(p.InstructionsPath) {
		return fmt.Errorf("%w: instructions_path must be an absolute path: '%s'", ErrInvalidManifest, p.InstructionsPath)
	}
	instrFi, err := os.Stat(p.InstructionsPath)
	if err != nil {
		return fmt.Errorf("%w: instructions file '%s' not found: %v", ErrInvalidManifest, p.InstructionsPath, err)
	}
	if instrFi.IsDir() || !instrFi.Mode().IsRegular() {
		return fmt.Errorf("%w: instructions file '%s' is not a regular file", ErrInvalidManifest, p.InstructionsPath)
	}
	if instrFi.Size() == 0 {
		return fmt.Errorf("%w: instructions file '%s' is empty", ErrInvalidManifest, p.InstructionsPath)
	}
	if instrFi.Size() > (1 << 20) {
		return fmt.Errorf("%w: instructions file '%s' exceeds 1 MiB limit", ErrInvalidManifest, p.InstructionsPath)
	}

	// Verify readability
	f, err := os.Open(p.InstructionsPath)
	if err != nil {
		return fmt.Errorf("%w: instructions file '%s' is not readable: %v", ErrInvalidManifest, p.InstructionsPath, err)
	}
	_ = f.Close()

	for _, cap := range p.Capabilities {
		if !identifierRegex.MatchString(cap) {
			return fmt.Errorf("%w: invalid capability '%s'", ErrInvalidManifest, cap)
		}
	}
	return nil
}

func (p *AgentProfile) CapabilitiesJSON() string {
	if len(p.Capabilities) == 0 {
		return "[]"
	}
	caps := make([]string, len(p.Capabilities))
	copy(caps, p.Capabilities)
	sort.Strings(caps)
	bytes, _ := json.Marshal(caps)
	return string(bytes)
}
