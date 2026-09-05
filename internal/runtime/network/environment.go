package network

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"openagentx/internal/domain"
)

var proxyKeys = map[string]struct{}{
	"http_proxy": {}, "HTTP_PROXY": {}, "https_proxy": {}, "HTTPS_PROXY": {},
	"all_proxy": {}, "ALL_PROXY": {}, "no_proxy": {}, "NO_PROXY": {},
}

var blockedKeys = map[string]struct{}{
	"LD_PRELOAD": {}, "LD_LIBRARY_PATH": {}, "BASH_ENV": {}, "ENV": {},
	"NODE_OPTIONS": {}, "RUBYOPT": {}, "PERL5OPT": {}, "PYTHONPATH": {},
	"DYLD_INSERT_LIBRARIES": {}, "DYLD_LIBRARY_PATH": {},
}

// Environment applies a closed NetworkPolicy to a child process environment.
// The returned slice is detached from the input and never contains credentials
// from a policy object. Existing non-proxy process variables are preserved so
// runtime binaries retain their normal locale and PATH behaviour.
func Environment(base []string, policy domain.NetworkPolicy, adapterID, binary string) ([]string, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	mode := policy.Mode
	if mode == "" {
		mode = domain.NetworkInherit
	}
	if mode == domain.NetworkNamedProfile && adapterID != "agy-batch" {
		return nil, domain.ErrUnsupportedCapability
	}
	if mode == domain.NetworkDirect && adapterID == "agy-batch" && filepath.Base(binary) == "agy-graft" {
		// agy-graft deliberately selects a local proxy when no endpoint is
		// configured; claiming direct mode through that wrapper is unsafe.
		return nil, domain.ErrUnsupportedCapability
	}
	if len(policy.DirectDestinations) > 0 && mode != domain.NetworkDirect {
		return nil, domain.ErrInvalidInput("direct_destinations require direct network mode")
	}
	out := make([]string, 0, len(base)+3)
	for _, entry := range base {
		name := entry
		if idx := strings.IndexByte(entry, '='); idx >= 0 {
			name = entry[:idx]
		}
		if _, blocked := blockedKeys[name]; blocked {
			continue
		}
		if mode == domain.NetworkDirect || mode == domain.NetworkNamedProfile {
			if _, isProxy := proxyKeys[name]; isProxy {
				continue
			}
		}
		if strings.HasPrefix(name, "AGY_GRAFT_") && mode == domain.NetworkNamedProfile {
			continue
		}
		out = append(out, entry)
	}
	if len(policy.DirectDestinations) > 0 {
		value := strings.Join(policy.DirectDestinations, ",")
		out = append(out, "NO_PROXY="+value, "no_proxy="+value)
	}
	if mode == domain.NetworkNamedProfile {
		path := filepath.Clean(strings.TrimSpace(policy.ConfigFile))
		if !filepath.IsAbs(path) {
			return nil, domain.ErrInvalidInput("network config_file must be absolute")
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return nil, domain.ErrInvalidInput("network config_file must be a regular file")
		}
		if info.Mode().Perm() != 0o600 {
			return nil, domain.ErrInvalidInput("network config_file must have mode 0600")
		}
		out = append(out, "AGY_GRAFT_CONFIG="+path)
		if policy.ProxyMode != "" {
			out = append(out, "AGY_GRAFT_SELECT_PROXY_MODE="+policy.ProxyMode)
		}
	}
	return out, nil
}

// ValidateConfigFile is exposed for frontends and diagnostics that need to
// check a worker-owned profile without reading its contents into a journal.
func ValidateConfigFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("network config_file is required")
	}
	_, err := Environment(nil, domain.NetworkPolicy{Mode: domain.NetworkNamedProfile, ProfileID: "profile", ProfileVersion: 1, ConfigFile: path}, "agy-batch", "agy-graft")
	return err
}
