package domain

import (
	"net/netip"
	"strings"
)

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
	Mode                  NetworkMode     `json:"mode,omitempty" yaml:"mode,omitempty"`
	PolicyVersion         int64           `json:"policy_version,omitempty" yaml:"policy_version,omitempty"`
	ProfileID             string          `json:"profile_id,omitempty" yaml:"profile_id,omitempty"`
	ProfileVersion        int64           `json:"profile_version,omitempty" yaml:"profile_version,omitempty"`
	ProxyMode             string          `json:"proxy_mode,omitempty" yaml:"proxy_mode,omitempty"`
	ConfigFile            string          `json:"config_file,omitempty" yaml:"config_file,omitempty"`
	DirectDestinations    []string        `json:"direct_destinations,omitempty" yaml:"direct_destinations,omitempty"`
	BindingRevision       int64           `json:"binding_revision,omitempty" yaml:"binding_revision,omitempty"`
	ManifestDigest        string          `json:"manifest_digest,omitempty" yaml:"manifest_digest,omitempty"`
	SecretVersion         string          `json:"secret_version,omitempty" yaml:"secret_version,omitempty"`
	RuntimeIdentity       RuntimeIdentity `json:"runtime_identity,omitempty" yaml:"runtime_identity,omitempty"`
	MaterializationDigest string          `json:"materialization_digest,omitempty" yaml:"materialization_digest,omitempty"`
	BlackIPFile           string          `json:"-" yaml:"-"`
}

func (p NetworkPolicy) IsZero() bool {
	return p.Mode == "" && p.PolicyVersion == 0 && p.ProfileID == "" && p.ProfileVersion == 0 &&
		p.ProxyMode == "" && p.ConfigFile == "" && len(p.DirectDestinations) == 0 && p.BindingRevision == 0 &&
		p.ManifestDigest == "" && p.SecretVersion == "" && p.RuntimeIdentity.IsZero() && p.MaterializationDigest == "" && p.BlackIPFile == ""
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
		if p.ConfigFile == "" && p.ManifestDigest == "" {
			return ErrInvalidInput("network named_profile requires a manifest or materialized config")
		}
	} else if p.ProfileID != "" || p.ProfileVersion != 0 || p.ConfigFile != "" || p.ProxyMode != "" ||
		p.SecretVersion != "" || p.MaterializationDigest != "" || p.BlackIPFile != "" {
		return ErrInvalidInput("network profile fields require named_profile mode")
	}
	if p.PolicyVersion < 0 {
		return ErrInvalidInput("network policy_version cannot be negative")
	}
	if p.Mode != NetworkNamedProfile {
		hasTestMetadata := p.PolicyVersion != 0 || p.ManifestDigest != "" || !p.RuntimeIdentity.IsZero()
		if hasTestMetadata && (p.PolicyVersion == 0 || p.ManifestDigest == "" || p.RuntimeIdentity.IsZero()) {
			return ErrInvalidInput("tested network mode requires policy version, manifest and runtime identity")
		}
	}
	if p.ProxyMode != "" && p.ProxyMode != "only_http_proxy" && p.ProxyMode != "only_socks5" {
		return ErrInvalidInput("unsupported network proxy_mode")
	}
	if len(p.DirectDestinations) > 0 && p.Mode != NetworkDirect && p.Mode != NetworkNamedProfile {
		return ErrInvalidInput("direct_destinations require direct or named_profile mode")
	}
	seenDestinations := make(map[string]struct{}, len(p.DirectDestinations))
	for _, destination := range p.DirectDestinations {
		if strings.TrimSpace(destination) == "" || strings.ContainsAny(destination, "\r\n") {
			return ErrInvalidInput("network direct_destinations must contain non-empty values")
		}
		if p.Mode == NetworkNamedProfile {
			address, err := netip.ParseAddr(destination)
			if err != nil || address.Zone() != "" || address.Is4In6() {
				return ErrInvalidInput("named profile direct destinations must be non-mapped IP addresses")
			}
		}
		if _, duplicate := seenDestinations[destination]; duplicate {
			return ErrInvalidInput("network direct_destinations must be unique")
		}
		seenDestinations[destination] = struct{}{}
	}
	if p.BindingRevision < 0 {
		return ErrInvalidInput("network binding_revision cannot be negative")
	}
	if p.ManifestDigest != "" && !validSHA256Digest(p.ManifestDigest) {
		return ErrInvalidInput("network manifest_digest must be sha256")
	}
	if p.MaterializationDigest != "" && !validSHA256Digest(p.MaterializationDigest) {
		return ErrInvalidInput("network materialization_digest must be sha256")
	}
	if p.SecretVersion != "" {
		if err := ValidateOpaqueID("network secret_version", p.SecretVersion); err != nil {
			return err
		}
	}
	if !p.RuntimeIdentity.IsZero() {
		if err := p.RuntimeIdentity.Validate(); err != nil {
			return err
		}
	}
	return nil
}
