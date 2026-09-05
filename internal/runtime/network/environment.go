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

var commonEnvironmentKeys = map[string]struct{}{
	"HOME": {}, "USER": {}, "LOGNAME": {}, "PATH": {}, "SHELL": {}, "TERM": {}, "COLORTERM": {},
	"TMPDIR": {}, "TMP": {}, "TEMP": {}, "LANG": {}, "TZ": {}, "SSH_AUTH_SOCK": {},
	"SSL_CERT_FILE": {}, "SSL_CERT_DIR": {}, "NIX_SSL_CERT_FILE": {}, "REQUESTS_CA_BUNDLE": {}, "CURL_CA_BUNDLE": {},
	"XDG_CONFIG_HOME": {}, "XDG_CACHE_HOME": {}, "XDG_DATA_HOME": {}, "XDG_STATE_HOME": {}, "XDG_RUNTIME_DIR": {},
	// These names are used only by hermetic Adapter fixtures.
	"AGY_TEST_ARGV_LOG": {}, "AGY_TEST_STDIN_LOG": {}, "AGY_TEST_REAL_ENV_LOG": {},
	"OAX_ARGS": {}, "OAX_PROMPT": {}, "OAX_CHILD_PID": {}, "OAX_READY": {},
}

var agyEnvironmentKeys = map[string]struct{}{
	"AGY_GRAFT_REAL_BIN": {}, "AGY_GRAFT_MGRAFTCP_BIN": {}, "AGY_GRAFT_GOMAXPROCS": {},
	"AGY_GRAFT_IPV4_ONLY": {}, "AGY_GRAFT_IPV4_ONLY_FILE": {},
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
	if len(policy.DirectDestinations) > 0 && mode != domain.NetworkDirect && mode != domain.NetworkNamedProfile {
		return nil, domain.ErrInvalidInput("direct_destinations require direct or named_profile network mode")
	}
	if mode == domain.NetworkNamedProfile && len(policy.DirectDestinations) > 0 && policy.BlackIPFile == "" {
		return nil, domain.ErrInvalidInput("named_profile direct destinations require a materialized blackip file")
	}
	filtered := make([]string, 0, len(base))
	seen := make(map[string]struct{}, len(base))
	for index := len(base) - 1; index >= 0; index-- {
		entry := base[index]
		name := entry
		if idx := strings.IndexByte(entry, '='); idx >= 0 {
			name = entry[:idx]
		}
		if _, blocked := blockedKeys[name]; blocked {
			continue
		}
		if _, duplicate := seen[name]; duplicate {
			continue
		}
		seen[name] = struct{}{}
		_, common := commonEnvironmentKeys[name]
		_, proxy := proxyKeys[name]
		_, agy := agyEnvironmentKeys[name]
		if !common && !proxy && !agy && !strings.HasPrefix(name, "LC_") {
			continue
		}
		if agy && adapterID != "agy-batch" {
			continue
		}
		if mode == domain.NetworkInherit && adapterID == "agy-batch" && proxy {
			value := ""
			if separator := strings.IndexByte(entry, '='); separator >= 0 {
				value = entry[separator+1:]
			}
			if strings.Contains(value, "@") {
				return nil, domain.ErrUnsupportedCapability
			}
		}
		if mode == domain.NetworkDirect || mode == domain.NetworkNamedProfile {
			if proxy {
				continue
			}
		}
		filtered = append(filtered, entry)
	}
	out := make([]string, 0, len(filtered)+4)
	for index := len(filtered) - 1; index >= 0; index-- {
		out = append(out, filtered[index])
	}
	if mode == domain.NetworkDirect && len(policy.DirectDestinations) > 0 {
		value := strings.Join(policy.DirectDestinations, ",")
		out = append(out, "NO_PROXY="+value, "no_proxy="+value)
	}
	if mode == domain.NetworkNamedProfile {
		path, err := validateControlledFile(policy.ConfigFile, "network config_file")
		if err != nil {
			return nil, err
		}
		out = append(out, "AGY_GRAFT_CONFIG="+path)
		if policy.BlackIPFile != "" {
			blackPath, err := validateControlledFile(policy.BlackIPFile, "network blackip_file")
			if err != nil {
				return nil, err
			}
			out = append(out, "AGY_GRAFT_BLACKIP_FILE="+blackPath)
		}
		if policy.ProxyMode != "" {
			out = append(out, "AGY_GRAFT_SELECT_PROXY_MODE="+policy.ProxyMode)
		}
	}
	return out, nil
}

func validateControlledFile(value, label string) (string, error) {
	path := filepath.Clean(strings.TrimSpace(value))
	if !filepath.IsAbs(path) {
		return "", domain.ErrInvalidInput(label + " must be absolute")
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", domain.ErrInvalidInput(label + " must be a non-symlink regular file")
	}
	if info.Mode().Perm() != 0o600 {
		return "", domain.ErrInvalidInput(label + " must have mode 0600")
	}
	return path, nil
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
