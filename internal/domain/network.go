package domain

import "strings"

// NetworkMode describes the network environment selected for one Runtime
// execution. Values are intentionally small and closed; callers must not pass
// arbitrary shell environment or command fragments.
type NetworkMode string

const (
	NetworkInherit      NetworkMode = "inherit"
	NetworkDirect       NetworkMode = "direct"
	NetworkNamedProfile NetworkMode = "named_profile"
)

func (m NetworkMode) Valid() bool {
	return m == NetworkInherit || m == NetworkDirect || m == NetworkNamedProfile
}

// NetworkPolicy is safe to persist in a ResolvedExecutionSpec. It contains
// references and non-sensitive paths only; proxy credentials must live in a
// worker-owned 0600 file and are never represented here.
type NetworkPolicy struct {
	Mode               NetworkMode `json:"mode,omitempty" yaml:"mode,omitempty"`
	ProfileID          string      `json:"profile_id,omitempty" yaml:"profile_id,omitempty"`
	ProfileVersion     int64       `json:"profile_version,omitempty" yaml:"profile_version,omitempty"`
	ProxyMode          string      `json:"proxy_mode,omitempty" yaml:"proxy_mode,omitempty"`
	ConfigFile         string      `json:"config_file,omitempty" yaml:"config_file,omitempty"`
	DirectDestinations []string    `json:"direct_destinations,omitempty" yaml:"direct_destinations,omitempty"`
	BindingRevision    int64       `json:"binding_revision,omitempty" yaml:"binding_revision,omitempty"`
}

func (p NetworkPolicy) IsZero() bool {
	return p.Mode == "" && p.ProfileID == "" && p.ProfileVersion == 0 &&
		p.ProxyMode == "" && p.ConfigFile == "" && len(p.DirectDestinations) == 0 && p.BindingRevision == 0
}

func (p NetworkPolicy) Validate() error {
	if p.IsZero() {
		return nil
	}
	if !p.Mode.Valid() {
		return ErrInvalidInput("unsupported network mode")
	}
	if p.Mode == NetworkNamedProfile {
		if err := ValidateIdentifier("network profile_id", p.ProfileID); err != nil {
			return err
		}
		if err := ValidatePositiveVersion("network profile_version", p.ProfileVersion); err != nil {
			return err
		}
		if strings.TrimSpace(p.ConfigFile) == "" {
			return ErrInvalidInput("network named_profile requires config_file")
		}
	} else if p.ProfileID != "" || p.ProfileVersion != 0 || p.ConfigFile != "" || p.ProxyMode != "" {
		return ErrInvalidInput("network profile fields require named_profile mode")
	}
	if p.ProxyMode != "" && p.ProxyMode != "only_http_proxy" && p.ProxyMode != "only_socks5" {
		return ErrInvalidInput("unsupported network proxy_mode")
	}
	if len(p.DirectDestinations) > 0 && p.Mode != NetworkDirect {
		return ErrInvalidInput("direct_destinations require direct network mode")
	}
	for _, destination := range p.DirectDestinations {
		if strings.TrimSpace(destination) == "" || strings.ContainsAny(destination, "\r\n") {
			return ErrInvalidInput("network direct_destinations must contain non-empty values")
		}
	}
	if p.BindingRevision < 0 {
		return ErrInvalidInput("network binding_revision cannot be negative")
	}
	if p.BindingRevision > 0 && p.Mode != NetworkNamedProfile {
		return ErrInvalidInput("network binding_revision requires named_profile mode")
	}
	return nil
}
