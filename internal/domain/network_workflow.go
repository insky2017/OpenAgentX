package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/netip"
	"sort"
	"strings"
	"time"
)

type NetworkProfileState string

const (
	NetworkStateDraft     NetworkProfileState = "draft"
	NetworkStateTesting   NetworkProfileState = "testing"
	NetworkStateReady     NetworkProfileState = "ready"
	NetworkStatePublished NetworkProfileState = "published"
	NetworkStateStale     NetworkProfileState = "stale"
)

func (s NetworkProfileState) Valid() bool {
	return s == NetworkStateDraft || s == NetworkStateTesting || s == NetworkStateReady || s == NetworkStatePublished || s == NetworkStateStale
}

// RuntimeIdentity contains publish-time evidence without exposing host paths.
type RuntimeIdentity struct {
	AdapterID        string `json:"adapter_id,omitempty" yaml:"adapter_id,omitempty"`
	AdapterVersion   string `json:"adapter_version,omitempty" yaml:"adapter_version,omitempty"`
	ExecutableSHA256 string `json:"executable_sha256,omitempty" yaml:"executable_sha256,omitempty"`
	WrapperSHA256    string `json:"wrapper_sha256,omitempty" yaml:"wrapper_sha256,omitempty"`
	HelperVersion    string `json:"helper_version,omitempty" yaml:"helper_version,omitempty"`
	HelperSHA256     string `json:"helper_sha256,omitempty" yaml:"helper_sha256,omitempty"`
}

func (i RuntimeIdentity) IsZero() bool {
	return i.AdapterID == "" && i.AdapterVersion == "" && i.ExecutableSHA256 == "" && i.WrapperSHA256 == "" && i.HelperVersion == "" && i.HelperSHA256 == ""
}

func (i RuntimeIdentity) Validate() error {
	if err := ValidateIdentifier("runtime identity adapter_id", i.AdapterID); err != nil {
		return err
	}
	if err := ValidateOpaqueID("runtime identity adapter_version", i.AdapterVersion); err != nil {
		return err
	}
	for _, value := range []string{i.ExecutableSHA256, i.WrapperSHA256, i.HelperSHA256} {
		if value != "" && !validSHA256Digest(value) {
			return ErrInvalidInput("runtime identity digest must be sha256")
		}
	}
	if i.HelperSHA256 != "" && strings.TrimSpace(i.HelperVersion) == "" {
		return ErrInvalidInput("runtime helper version is required")
	}
	return nil
}

func validSHA256Digest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

type NetworkProfileContent struct {
	ProfileID         string    `json:"profile_id"`
	ContentVersion    int64     `json:"content_version"`
	Mode              string    `json:"mode"`
	Host              string    `json:"host"`
	Port              int       `json:"port"`
	DirectIPs         []string  `json:"direct_ips,omitempty"`
	SecretVersion     string    `json:"secret_version,omitempty"`
	ManifestDigest    string    `json:"manifest_digest"`
	CreatedBy         string    `json:"created_by"`
	CreatedAt         time.Time `json:"created_at"`
	LegacySecretStale bool      `json:"legacy_secret_stale,omitempty"`
}

func (c NetworkProfileContent) Validate() error {
	if err := ValidateIdentifier("profile_id", c.ProfileID); err != nil {
		return err
	}
	if err := ValidatePositiveVersion("content_version", c.ContentVersion); err != nil {
		return err
	}
	if c.Mode != "only_http_proxy" && c.Mode != "only_socks5" {
		return ErrInvalidInput("unsupported network profile mode")
	}
	if strings.TrimSpace(c.Host) == "" || strings.ContainsAny(c.Host, "\r\n/@ ") {
		return ErrInvalidInput("network profile host is invalid")
	}
	if c.Port < 1 || c.Port > 65535 {
		return ErrInvalidInput("network profile port must be between 1 and 65535")
	}
	seen := map[string]struct{}{}
	for _, ip := range c.DirectIPs {
		addr, err := netip.ParseAddr(ip)
		if err != nil || addr.Zone() != "" || addr.Is4In6() {
			return ErrInvalidInput("direct rules must contain IP addresses")
		}
		if _, ok := seen[ip]; ok {
			return ErrInvalidInput("direct rules must be unique")
		}
		seen[ip] = struct{}{}
	}
	if c.SecretVersion != "" {
		if err := ValidateOpaqueID("secret_version", c.SecretVersion); err != nil {
			return err
		}
	}
	if !validSHA256Digest(c.ManifestDigest) {
		return ErrInvalidInput("manifest_digest must be sha256")
	}
	if err := ValidateOpaqueID("created_by", c.CreatedBy); err != nil {
		return err
	}
	if c.CreatedAt.IsZero() {
		return ErrInvalidInput("content created_at is required")
	}
	if got := c.ComputeManifestDigest(); got != c.ManifestDigest {
		return ErrInvalidInput("network profile manifest digest mismatch")
	}
	return nil
}

func (c NetworkProfileContent) ComputeManifestDigest() string {
	direct := append([]string(nil), c.DirectIPs...)
	sort.Strings(direct)
	payload, _ := json.Marshal(struct {
		ProfileID string   `json:"profile_id"`
		Version   int64    `json:"content_version"`
		Mode      string   `json:"mode"`
		Host      string   `json:"host"`
		Port      int      `json:"port"`
		Direct    []string `json:"direct_ips"`
		Secret    string   `json:"secret_version"`
	}{c.ProfileID, c.ContentVersion, c.Mode, strings.ToLower(c.Host), c.Port, direct, c.SecretVersion})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

type NetworkProfileHead struct {
	ProfileID               string                 `json:"profile_id"`
	CurrentContentVersion   int64                  `json:"current_content_version"`
	State                   NetworkProfileState    `json:"state"`
	StateRevision           int64                  `json:"state_revision"`
	ReadyTestID             string                 `json:"ready_test_id,omitempty"`
	PublishedContentVersion int64                  `json:"published_content_version,omitempty"`
	UpdatedAt               time.Time              `json:"updated_at"`
	Content                 *NetworkProfileContent `json:"content,omitempty"`
}

type NetworkTest struct {
	ID               string               `json:"test_id"`
	ProfileID        string               `json:"profile_id"`
	ContentVersion   int64                `json:"content_version"`
	SecretVersion    string               `json:"secret_version,omitempty"`
	WorkerInstanceID string               `json:"worker_instance_id"`
	Generation       int64                `json:"generation"`
	BackendID        string               `json:"backend_id"`
	RuntimeIdentity  RuntimeIdentity      `json:"runtime_identity"`
	State            string               `json:"state"`
	DiagnosticCode   string               `json:"diagnostic_code,omitempty"`
	DurationMS       int64                `json:"duration_ms,omitempty"`
	ProbeResults     []NetworkProbeResult `json:"probe_results,omitempty"`
	CreatedBy        string               `json:"created_by"`
	CreatedAt        time.Time            `json:"created_at"`
	FinishedAt       *time.Time           `json:"finished_at,omitempty"`
}

type NetworkModePolicy struct {
	AgentID        string      `json:"agent_id"`
	BackendID      string      `json:"backend_id"`
	PolicyVersion  int64       `json:"policy_version"`
	Mode           NetworkMode `json:"mode"`
	ManifestDigest string      `json:"manifest_digest"`
	CreatedBy      string      `json:"created_by"`
	CreatedAt      time.Time   `json:"created_at"`
}

func (p NetworkModePolicy) ComputeManifestDigest() string {
	payload, _ := json.Marshal(struct {
		AgentID       string      `json:"agent_id"`
		BackendID     string      `json:"backend_id"`
		PolicyVersion int64       `json:"policy_version"`
		Mode          NetworkMode `json:"mode"`
	}{p.AgentID, p.BackendID, p.PolicyVersion, p.Mode})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func (p NetworkModePolicy) Validate() error {
	if err := ValidateIdentifier("mode policy agent_id", p.AgentID); err != nil {
		return err
	}
	if err := ValidateIdentifier("mode policy backend_id", p.BackendID); err != nil {
		return err
	}
	if err := ValidatePositiveVersion("mode policy version", p.PolicyVersion); err != nil {
		return err
	}
	if p.Mode != NetworkInherit && p.Mode != NetworkDirect {
		return ErrInvalidInput("mode policy must be inherit or direct")
	}
	if !validSHA256Digest(p.ManifestDigest) || p.ComputeManifestDigest() != p.ManifestDigest {
		return ErrInvalidInput("mode policy manifest digest mismatch")
	}
	if err := ValidateOpaqueID("mode policy created_by", p.CreatedBy); err != nil {
		return err
	}
	if p.CreatedAt.IsZero() {
		return ErrInvalidInput("mode policy created_at is required")
	}
	return nil
}

type NetworkModeTest struct {
	ID               string               `json:"test_id"`
	AgentID          string               `json:"agent_id"`
	BackendID        string               `json:"backend_id"`
	PolicyVersion    int64                `json:"policy_version"`
	Mode             NetworkMode          `json:"mode"`
	ManifestDigest   string               `json:"manifest_digest"`
	WorkerInstanceID string               `json:"worker_instance_id"`
	Generation       int64                `json:"generation"`
	RuntimeIdentity  RuntimeIdentity      `json:"runtime_identity"`
	BindingRevision  int64                `json:"binding_revision"`
	State            string               `json:"state"`
	DiagnosticCode   string               `json:"diagnostic_code,omitempty"`
	DurationMS       int64                `json:"duration_ms,omitempty"`
	ProbeResults     []NetworkProbeResult `json:"probe_results,omitempty"`
	CreatedBy        string               `json:"created_by"`
	CreatedAt        time.Time            `json:"created_at"`
	FinishedAt       *time.Time           `json:"finished_at,omitempty"`
}

type NetworkWorkKind string

const (
	NetworkWorkTest   NetworkWorkKind = "test"
	NetworkWorkApply  NetworkWorkKind = "apply"
	NetworkWorkImport NetworkWorkKind = "import"
)

type NetworkWork struct {
	ID               string                 `json:"work_id"`
	Kind             NetworkWorkKind        `json:"kind"`
	ProfileID        string                 `json:"profile_id,omitempty"`
	ContentVersion   int64                  `json:"content_version,omitempty"`
	SecretVersion    string                 `json:"secret_version,omitempty"`
	AgentID          string                 `json:"agent_id"`
	BackendID        string                 `json:"backend_id"`
	WorkerInstanceID string                 `json:"worker_instance_id"`
	Generation       int64                  `json:"generation"`
	BindingRevision  int64                  `json:"binding_revision,omitempty"`
	Mode             NetworkMode            `json:"mode,omitempty"`
	PolicyVersion    int64                  `json:"policy_version,omitempty"`
	ManifestDigest   string                 `json:"manifest_digest,omitempty"`
	RuntimeIdentity  RuntimeIdentity        `json:"runtime_identity"`
	State            string                 `json:"state"`
	DiagnosticCode   string                 `json:"diagnostic_code,omitempty"`
	CreatedAt        time.Time              `json:"created_at"`
	FinishedAt       *time.Time             `json:"finished_at,omitempty"`
	Content          *NetworkProfileContent `json:"content,omitempty"`
}

const (
	NetworkDiagnosticInvalidConfig  = "INVALID_CONFIG"
	NetworkDiagnosticSecretMissing  = "SECRET_MISSING"
	NetworkDiagnosticEndpoint       = "ENDPOINT_UNREACHABLE"
	NetworkDiagnosticProxyAuth      = "PROXY_AUTHENTICATION_REQUIRED"
	NetworkDiagnosticMaterialize    = "MATERIALIZATION_FAILED"
	NetworkDiagnosticRuntime        = "RUNTIME_HEALTH_FAILED"
	NetworkDiagnosticIdentity       = "RUNTIME_IDENTITY_CHANGED"
	NetworkDiagnosticUnsupported    = "UNSUPPORTED_CAPABILITY"
	NetworkDiagnosticNotVerified    = "NOT_VERIFIED"
	NetworkDiagnosticInheritUnknown = "INHERITED_CONFIGURATION_UNVERIFIED"
)

func ValidNetworkDiagnostic(value string) bool {
	switch value {
	case "", NetworkDiagnosticInvalidConfig, NetworkDiagnosticSecretMissing, NetworkDiagnosticEndpoint, NetworkDiagnosticProxyAuth, NetworkDiagnosticMaterialize, NetworkDiagnosticRuntime, NetworkDiagnosticIdentity, NetworkDiagnosticUnsupported, NetworkDiagnosticNotVerified, NetworkDiagnosticInheritUnknown:
		return true
	default:
		return false
	}
}

type NetworkProbeLayer string

const (
	NetworkProbeConfiguration NetworkProbeLayer = "configuration"
	NetworkProbeSecret        NetworkProbeLayer = "secret"
	NetworkProbeEndpoint      NetworkProbeLayer = "endpoint"
	NetworkProbeDirectRules   NetworkProbeLayer = "direct_rules"
	NetworkProbeRuntimeHealth NetworkProbeLayer = "runtime_health"
	NetworkProbeNetworkEffect NetworkProbeLayer = "network_effect"
	NetworkProbeModelCall     NetworkProbeLayer = "model_call"
)

type NetworkProbeState string

const (
	NetworkProbePassed        NetworkProbeState = "passed"
	NetworkProbeFailed        NetworkProbeState = "failed"
	NetworkProbeNotApplicable NetworkProbeState = "not_applicable"
	NetworkProbeNotVerified   NetworkProbeState = "not_verified"
)

type NetworkProbeResult struct {
	Layer          NetworkProbeLayer `json:"layer"`
	State          NetworkProbeState `json:"state"`
	DiagnosticCode string            `json:"diagnostic_code,omitempty"`
	DurationMS     int64             `json:"duration_ms,omitempty"`
}

var networkProbeLayers = []NetworkProbeLayer{
	NetworkProbeConfiguration, NetworkProbeSecret, NetworkProbeEndpoint,
	NetworkProbeDirectRules, NetworkProbeRuntimeHealth, NetworkProbeNetworkEffect,
	NetworkProbeModelCall,
}

func ValidateNetworkProbeResults(workState string, results []NetworkProbeResult) error {
	if len(results) != len(networkProbeLayers) {
		return ErrInvalidInput("network probe results must contain every fixed layer exactly once")
	}
	seen := make(map[NetworkProbeLayer]struct{}, len(results))
	failed := false
	for _, result := range results {
		validLayer := false
		for _, layer := range networkProbeLayers {
			validLayer = validLayer || result.Layer == layer
		}
		if !validLayer {
			return ErrInvalidInput("network probe result contains an unsupported layer")
		}
		if _, exists := seen[result.Layer]; exists {
			return ErrInvalidInput("network probe result layers must be unique")
		}
		seen[result.Layer] = struct{}{}
		if result.DurationMS < 0 {
			return ErrInvalidInput("network probe duration cannot be negative")
		}
		switch result.State {
		case NetworkProbePassed, NetworkProbeNotApplicable:
			if result.DiagnosticCode != "" {
				return ErrInvalidInput("successful or inapplicable network probe cannot include a diagnostic")
			}
		case NetworkProbeFailed:
			if result.DiagnosticCode == "" || !ValidNetworkDiagnostic(result.DiagnosticCode) || result.DiagnosticCode == NetworkDiagnosticNotVerified || result.DiagnosticCode == NetworkDiagnosticInheritUnknown {
				return ErrInvalidInput("failed network probe requires a failure diagnostic")
			}
			failed = true
		case NetworkProbeNotVerified:
			if result.DiagnosticCode != NetworkDiagnosticNotVerified && result.DiagnosticCode != NetworkDiagnosticInheritUnknown {
				return ErrInvalidInput("unverified network probe requires a fixed diagnostic")
			}
		default:
			return ErrInvalidInput("network probe result contains an unsupported state")
		}
	}
	if workState == "succeeded" && failed {
		return ErrInvalidInput("successful network test cannot contain a failed probe layer")
	}
	if workState == "failed" && !failed {
		return ErrInvalidInput("failed network test requires a failed probe layer")
	}
	return nil
}

type NetworkCommandReceipt struct {
	Actor          string          `json:"actor"`
	Operation      string          `json:"operation"`
	IdempotencyKey string          `json:"idempotency_key"`
	RequestDigest  string          `json:"-"`
	ResultJSON     json.RawMessage `json:"result"`
	CreatedAt      time.Time       `json:"created_at"`
}

type NetworkImport struct {
	WorkerInstanceID string     `json:"worker_instance_id"`
	Generation       int64      `json:"generation"`
	BackendID        string     `json:"backend_id"`
	SourceIdentity   string     `json:"source_identity"`
	WorkID           string     `json:"work_id"`
	ProfileID        string     `json:"profile_id,omitempty"`
	ContentVersion   int64      `json:"content_version,omitempty"`
	State            string     `json:"state"`
	CreatedAt        time.Time  `json:"created_at"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
}

type NetworkPublication struct {
	ProfileID       string          `json:"profile_id"`
	ContentVersion  int64           `json:"content_version"`
	TestID          string          `json:"test_id"`
	RuntimeIdentity RuntimeIdentity `json:"runtime_identity"`
	PublishedBy     string          `json:"published_by"`
	PublishedAt     time.Time       `json:"published_at"`
}

type NetworkActiveRunSnapshot struct {
	RunID                  string           `json:"run_id"`
	TaskID                 string           `json:"task_id"`
	AgentID                string           `json:"agent_id"`
	Status                 RunAttemptStatus `json:"status"`
	BackendID              string           `json:"backend_id"`
	WorkerInstanceID       string           `json:"worker_instance_id"`
	WorkerGeneration       int64            `json:"worker_generation"`
	NetworkMode            NetworkMode      `json:"network_mode,omitempty"`
	NetworkProfileID       string           `json:"network_profile_id,omitempty"`
	NetworkProfileVersion  int64            `json:"network_profile_version,omitempty"`
	NetworkPolicyVersion   int64            `json:"network_policy_version,omitempty"`
	NetworkBindingRevision int64            `json:"network_binding_revision,omitempty"`
}

type NetworkCommandMutation struct {
	Receipt                 NetworkCommandReceipt
	ExpectedStateRevision   int64
	ExpectedBindingRevision int64
	Content                 *NetworkProfileContent
	Head                    *NetworkProfileHead
	Test                    *NetworkTest
	ModePolicy              *NetworkModePolicy
	ModeTest                *NetworkModeTest
	Work                    *NetworkWork
	Binding                 *NetworkBinding
	Import                  *NetworkImport
	Publication             *NetworkPublication
	CheckBindingRevision    bool
	BindingAgentID          string
	BindingBackendID        string
	Event                   *JournalEvent
}
