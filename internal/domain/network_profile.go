package domain

import (
	"fmt"
	"net/netip"
	"path/filepath"
	"strings"
	"time"
)

type NetworkProfileStatus string

const (
	NetworkProfileDraft     NetworkProfileStatus = "draft"
	NetworkProfilePublished NetworkProfileStatus = "published"
)

type ProxyProfile struct {
	ID              string               `json:"profile_id"`
	Version         int64                `json:"version"`
	Status          NetworkProfileStatus `json:"status"`
	Mode            string               `json:"mode"`
	Host            string               `json:"host"`
	Port            int                  `json:"port"`
	ConfigFile      string               `json:"config_file,omitempty"`
	SecretRef       string               `json:"secret_ref,omitempty"`
	DirectIPs       []string             `json:"direct_ips,omitempty"`
	ManifestDigest  string               `json:"manifest_digest,omitempty"`
	RuntimeIdentity RuntimeIdentity      `json:"runtime_identity,omitempty"`
	CreatedBy       string               `json:"created_by"`
	CreatedAt       time.Time            `json:"created_at"`
	UpdatedAt       time.Time            `json:"updated_at"`
}

func (p ProxyProfile) Validate() error {
	if err := ValidateIdentifier("profile_id", p.ID); err != nil {
		return err
	}
	if err := ValidatePositiveVersion("profile version", p.Version); err != nil {
		return err
	}
	if p.Status != NetworkProfileDraft && p.Status != NetworkProfilePublished {
		return ErrInvalidInput("unsupported network profile status")
	}
	if p.Mode != "only_http_proxy" && p.Mode != "only_socks5" {
		return ErrInvalidInput("unsupported network profile mode")
	}
	if strings.TrimSpace(p.Host) == "" || strings.ContainsAny(p.Host, "\r\n/@") {
		return ErrInvalidInput("network profile host is invalid")
	}
	if _, err := netip.ParseAddr(p.Host); err != nil {
		if strings.ContainsAny(p.Host, " :") {
			return ErrInvalidInput("network profile host is invalid")
		}
	}
	if p.Port < 1 || p.Port > 65535 {
		return ErrInvalidInput("network profile port must be between 1 and 65535")
	}
	if p.ConfigFile != "" && !filepath.IsAbs(p.ConfigFile) {
		return ErrInvalidInput("network profile config_file must be absolute")
	}
	if p.SecretRef != "" {
		if err := ValidateOpaqueID("network profile secret_ref", p.SecretRef); err != nil {
			return err
		}
	}
	for _, ip := range p.DirectIPs {
		address, err := netip.ParseAddr(ip)
		if err != nil || address.Zone() != "" || address.Is4In6() {
			return ErrInvalidInput("network profile direct rules must be non-mapped IP addresses")
		}
	}
	if p.ManifestDigest != "" && !validSHA256Digest(p.ManifestDigest) {
		return ErrInvalidInput("network profile manifest_digest must be sha256")
	}
	if !p.RuntimeIdentity.IsZero() {
		if err := p.RuntimeIdentity.Validate(); err != nil {
			return err
		}
	}
	if err := ValidateOpaqueID("network profile created_by", p.CreatedBy); err != nil {
		return fmt.Errorf("%w: created_by", err)
	}
	if p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() {
		return ErrInvalidInput("network profile timestamps are required")
	}
	return nil
}

type NetworkBinding struct {
	AgentID                string          `json:"agent_id"`
	BackendID              string          `json:"backend_id"`
	Mode                   NetworkMode     `json:"mode"`
	ProfileID              string          `json:"profile_id,omitempty"`
	ProfileVersion         int64           `json:"profile_version,omitempty"`
	PolicyVersion          int64           `json:"policy_version,omitempty"`
	TestID                 string          `json:"test_id,omitempty"`
	ManifestDigest         string          `json:"manifest_digest,omitempty"`
	RuntimeIdentity        RuntimeIdentity `json:"runtime_identity,omitempty"`
	Version                int64           `json:"version"`
	DesiredStatus          string          `json:"desired_status"`
	AppliedWorkerID        string          `json:"applied_worker_id,omitempty"`
	AppliedGeneration      int64           `json:"applied_generation,omitempty"`
	AppliedMode            NetworkMode     `json:"applied_mode,omitempty"`
	AppliedProfileID       string          `json:"applied_profile_id,omitempty"`
	AppliedProfileVersion  int64           `json:"applied_profile_version,omitempty"`
	AppliedPolicyVersion   int64           `json:"applied_policy_version,omitempty"`
	AppliedBindingRevision int64           `json:"applied_binding_revision,omitempty"`
	UpdatedAt              time.Time       `json:"updated_at"`
	Profile                *ProxyProfile   `json:"profile,omitempty"`
	Diagnostic             string          `json:"diagnostic,omitempty"`
}

type NetworkBindingApplication struct {
	BackendID       string
	ProfileID       string
	ProfileVersion  int64
	BindingRevision int64
	State           string
	Diagnostic      string
	Event           *JournalEvent
}

const NetworkApplyFailedDiagnostic = "network configuration could not be applied"

func (a NetworkBindingApplication) Validate() error {
	if err := ValidateIdentifier("network binding backend_id", a.BackendID); err != nil {
		return err
	}
	if err := ValidateIdentifier("network binding profile_id", a.ProfileID); err != nil {
		return err
	}
	if err := ValidatePositiveVersion("network binding profile_version", a.ProfileVersion); err != nil {
		return err
	}
	if err := ValidatePositiveVersion("network binding revision", a.BindingRevision); err != nil {
		return err
	}
	if a.State != "applied" && a.State != "failed" {
		return ErrInvalidInput("unsupported network binding acknowledgement state")
	}
	if a.State == "applied" && a.Diagnostic != "" {
		return ErrInvalidInput("applied network binding diagnostic must be empty")
	}
	if a.State == "failed" && a.Diagnostic != NetworkApplyFailedDiagnostic {
		return ErrInvalidInput("failed network binding diagnostic is unsupported")
	}
	if a.Event == nil {
		return ErrInvalidInput("network binding acknowledgement event is required")
	}
	return nil
}

func (b NetworkBinding) Validate() error {
	if err := ValidateIdentifier("network binding agent_id", b.AgentID); err != nil {
		return err
	}
	if err := ValidateIdentifier("network binding backend_id", b.BackendID); err != nil {
		return err
	}
	if !b.Mode.Valid() {
		return ErrInvalidInput("unsupported network binding mode")
	}
	if b.Mode == NetworkNamedProfile {
		if err := ValidateIdentifier("network binding profile_id", b.ProfileID); err != nil {
			return err
		}
		if err := ValidatePositiveVersion("network binding profile_version", b.ProfileVersion); err != nil {
			return err
		}
		if b.PolicyVersion != 0 {
			return ErrInvalidInput("named profile binding cannot reference a mode policy version")
		}
	} else {
		if b.ProfileID != "" || b.ProfileVersion != 0 {
			return ErrInvalidInput("mode binding cannot reference a proxy profile")
		}
		if err := ValidatePositiveVersion("network binding policy_version", b.PolicyVersion); err != nil {
			return err
		}
		if !validSHA256Digest(b.ManifestDigest) {
			return ErrInvalidInput("mode binding manifest_digest must be sha256")
		}
	}
	if err := ValidatePositiveVersion("network binding version", b.Version); err != nil {
		return err
	}
	if b.DesiredStatus != "pending" && b.DesiredStatus != "applied" && b.DesiredStatus != "failed" {
		return ErrInvalidInput("unsupported network binding status")
	}
	if b.UpdatedAt.IsZero() {
		return ErrInvalidInput("network binding updated_at is required")
	}
	if len(b.Diagnostic) > 4096 {
		return ErrInvalidInput("network binding diagnostic is too long")
	}
	return nil
}
