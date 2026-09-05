package domain

import (
	"fmt"
	"net"
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
	ID         string               `json:"profile_id"`
	Version    int64                `json:"version"`
	Status     NetworkProfileStatus `json:"status"`
	Mode       string               `json:"mode"`
	Host       string               `json:"host"`
	Port       int                  `json:"port"`
	ConfigFile string               `json:"config_file,omitempty"`
	SecretRef  string               `json:"secret_ref,omitempty"`
	CreatedBy  string               `json:"created_by"`
	CreatedAt  time.Time            `json:"created_at"`
	UpdatedAt  time.Time            `json:"updated_at"`
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
	if net.ParseIP(p.Host) == nil {
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
	if err := ValidateOpaqueID("network profile created_by", p.CreatedBy); err != nil {
		return fmt.Errorf("%w: created_by", err)
	}
	if p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() {
		return ErrInvalidInput("network profile timestamps are required")
	}
	return nil
}

type NetworkBinding struct {
	AgentID               string        `json:"agent_id"`
	BackendID             string        `json:"backend_id"`
	ProfileID             string        `json:"profile_id"`
	ProfileVersion        int64         `json:"profile_version"`
	Version               int64         `json:"version"`
	DesiredStatus         string        `json:"desired_status"`
	AppliedWorkerID       string        `json:"applied_worker_id,omitempty"`
	AppliedGeneration     int64         `json:"applied_generation,omitempty"`
	AppliedProfileVersion int64         `json:"applied_profile_version,omitempty"`
	UpdatedAt             time.Time     `json:"updated_at"`
	Profile               *ProxyProfile `json:"profile,omitempty"`
	Diagnostic            string        `json:"diagnostic,omitempty"`
}

func (b NetworkBinding) Validate() error {
	if err := ValidateIdentifier("network binding agent_id", b.AgentID); err != nil {
		return err
	}
	if err := ValidateIdentifier("network binding backend_id", b.BackendID); err != nil {
		return err
	}
	if err := ValidateIdentifier("network binding profile_id", b.ProfileID); err != nil {
		return err
	}
	if err := ValidatePositiveVersion("network binding profile_version", b.ProfileVersion); err != nil {
		return err
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
