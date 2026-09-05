package network

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"
	"openagentx/internal/api"
	"openagentx/internal/domain"
)

const (
	materializerKeyFile = ".integrity-key"
	materializerKeySize = 32
	maxMaterializedSize = 1 << 20
)

type Materializer struct {
	root         string
	rootDir      *os.File
	integrityKey []byte
}

func NewMaterializer(root string) (*Materializer, error) {
	trimmed := strings.TrimSpace(root)
	if trimmed == "" {
		return nil, domain.ErrInvalidInput("network materialization directory is required")
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return nil, domain.ErrInvalidInput("network materialization directory is required")
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("create network materialization directory: %w", err)
	}
	dirFD, err := unix.Open(abs, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, domain.ErrInvalidInput("network materialization directory must be a non-symlink 0700 directory")
	}
	rootDir := os.NewFile(uintptr(dirFD), abs)
	info, err := rootDir.Stat()
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
		_ = rootDir.Close()
		return nil, domain.ErrInvalidInput("network materialization directory must be a non-symlink 0700 directory")
	}
	key, err := loadOrCreateIntegrityKey(rootDir)
	if err != nil {
		_ = rootDir.Close()
		return nil, err
	}
	return &Materializer{root: abs, rootDir: rootDir, integrityKey: key}, nil
}

func (m *Materializer) Materialize(content domain.NetworkProfileContent, secret *api.NetworkSecretPayload, identity domain.RuntimeIdentity) (domain.NetworkPolicy, error) {
	if err := content.Validate(); err != nil {
		return domain.NetworkPolicy{}, err
	}
	if err := identity.Validate(); err != nil {
		return domain.NetworkPolicy{}, err
	}
	if content.SecretVersion != "" && secret == nil {
		return domain.NetworkPolicy{}, domain.ErrNotFound
	}
	if content.Mode == "only_http_proxy" && secret != nil && (secret.Username != "" || secret.Password != "") {
		return domain.NetworkPolicy{}, domain.ErrUnsupportedCapability
	}
	config, err := renderConfig(content, secret)
	if err != nil {
		return domain.NetworkPolicy{}, err
	}
	black, err := renderBlackIP(content.DirectIPs)
	if err != nil {
		return domain.NetworkPolicy{}, err
	}
	idBytes, _ := json.Marshal(struct {
		Manifest, Secret string
		Identity         domain.RuntimeIdentity
	}{content.ManifestDigest, content.SecretVersion, identity})
	idSum := sha256.Sum256(idBytes)
	id := hex.EncodeToString(idSum[:])
	blackPath := ""
	if len(black) > 0 {
		blackLeaf := id + ".blackip"
		blackPath = filepath.Join(m.root, blackLeaf)
		if err := writeImmutableAt(m.rootDir, blackLeaf, black); err != nil {
			return domain.NetworkPolicy{}, err
		}
	}
	// Publish the credential-bearing config last. A failed final publish can
	// leave only a non-sensitive, deterministically reusable blackip file.
	configLeaf := id + ".conf"
	configPath := filepath.Join(m.root, configLeaf)
	if err := writeImmutableAt(m.rootDir, configLeaf, config); err != nil {
		return domain.NetworkPolicy{}, err
	}
	digest := m.authenticate("materialization-v1", config, black)
	return domain.NetworkPolicy{Mode: domain.NetworkNamedProfile, ProfileID: content.ProfileID, ProfileVersion: content.ContentVersion, ProxyMode: content.Mode, ConfigFile: configPath, BlackIPFile: blackPath, DirectDestinations: append([]string(nil), content.DirectIPs...), ManifestDigest: content.ManifestDigest, SecretVersion: content.SecretVersion, RuntimeIdentity: identity, MaterializationDigest: digest}, nil
}

func (m *Materializer) PrepareRun(policy domain.NetworkPolicy) (domain.NetworkPolicy, error) {
	if policy.Mode != domain.NetworkNamedProfile {
		return policy, policy.Validate()
	}
	if policy.ManifestDigest == "" || policy.MaterializationDigest == "" || policy.RuntimeIdentity.IsZero() {
		return domain.NetworkPolicy{}, domain.ErrUnsupportedCapability
	}
	idBytes, _ := json.Marshal(struct {
		Manifest, Secret string
		Identity         domain.RuntimeIdentity
	}{policy.ManifestDigest, policy.SecretVersion, policy.RuntimeIdentity})
	idSum := sha256.Sum256(idBytes)
	id := hex.EncodeToString(idSum[:])
	configLeaf := id + ".conf"
	configPath := filepath.Join(m.root, configLeaf)
	config, err := readMaterializedAt(m.rootDir, configLeaf, maxMaterializedSize)
	if err != nil {
		return domain.NetworkPolicy{}, err
	}
	blackPath := ""
	var black []byte
	if len(policy.DirectDestinations) > 0 {
		blackLeaf := id + ".blackip"
		blackPath = filepath.Join(m.root, blackLeaf)
		black, err = readMaterializedAt(m.rootDir, blackLeaf, maxMaterializedSize)
		if err != nil {
			return domain.NetworkPolicy{}, err
		}
	}
	expected, err := hex.DecodeString(policy.MaterializationDigest)
	if err != nil || hmac.Equal(expected, m.authenticateBytes("materialization-v1", config, black)) == false {
		return domain.NetworkPolicy{}, domain.ErrInvalidManifest
	}
	local := policy
	local.ConfigFile = configPath
	local.BlackIPFile = blackPath
	return local, nil
}

func loadOrCreateIntegrityKey(rootDir *os.File) ([]byte, error) {
	for {
		key, err := readIntegrityKeyAt(rootDir)
		if err == nil {
			return key, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		key = make([]byte, materializerKeySize)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("generate network materialization integrity key: %w", err)
		}
		created, err := publishMaterializedNoReplaceAt(rootDir, materializerKeyFile, key)
		if err != nil {
			return nil, fmt.Errorf("publish network materialization integrity key: %w", err)
		}
		if created {
			return key, nil
		}
	}
}

func readIntegrityKeyAt(rootDir *os.File) ([]byte, error) {
	key, err := readMaterializedAt(rootDir, materializerKeyFile, materializerKeySize)
	if err != nil {
		return nil, err
	}
	if len(key) != materializerKeySize {
		return nil, domain.ErrInvalidManifest
	}
	return key, nil
}

func readIntegrityKey(path string) ([]byte, error) {
	key, err := readMaterializedBounded(path, materializerKeySize)
	if err != nil {
		return nil, err
	}
	if len(key) != materializerKeySize {
		return nil, domain.ErrInvalidManifest
	}
	return key, nil
}

func (m *Materializer) authenticate(label string, payloads ...[]byte) string {
	return hex.EncodeToString(m.authenticateBytes(label, payloads...))
}

func (m *Materializer) authenticateBytes(label string, payloads ...[]byte) []byte {
	mac := hmac.New(sha256.New, m.integrityKey)
	_, _ = mac.Write([]byte(label))
	for _, payload := range payloads {
		_, _ = mac.Write([]byte{0})
		_, _ = mac.Write(payload)
	}
	return mac.Sum(nil)
}

func renderConfig(content domain.NetworkProfileContent, secret *api.NetworkSecretPayload) ([]byte, error) {
	if secret != nil && (strings.ContainsAny(secret.Username, "\r\n") || strings.ContainsAny(secret.Password, "\r\n")) {
		return nil, domain.ErrInvalidInput("network secret contains control characters")
	}
	var b strings.Builder
	b.WriteString("select_proxy_mode = ")
	b.WriteString(content.Mode)
	b.WriteByte('\n')
	key := "http_proxy"
	if content.Mode == "only_socks5" {
		key = "socks5"
	}
	b.WriteString(key + " = " + netipHost(content.Host) + fmt.Sprintf(":%d\n", content.Port))
	if secret != nil {
		if content.Mode != "only_socks5" {
			return nil, domain.ErrUnsupportedCapability
		}
		if secret.Username != "" {
			b.WriteString("socks5_username = " + secret.Username + "\n")
		}
		if secret.Password != "" {
			b.WriteString("socks5_password = " + secret.Password + "\n")
		}
	}
	return []byte(b.String()), nil
}
func netipHost(host string) string {
	if a, err := netip.ParseAddr(host); err == nil && a.Is6() {
		return "[" + a.String() + "]"
	}
	return host
}
func renderBlackIP(values []string) ([]byte, error) {
	if len(values) == 0 {
		return nil, nil
	}
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		addr, err := netip.ParseAddr(value)
		if err != nil || addr.Is4In6() {
			return nil, domain.ErrInvalidInput("blackip rules require canonical non-mapped IP addresses")
		}
		if addr.Is6() {
			normalized = append(normalized, addr.StringExpanded())
		} else {
			normalized = append(normalized, addr.String())
		}
	}
	sort.Strings(normalized)
	return []byte(strings.Join(normalized, "\n") + "\n"), nil
}
func writeImmutableAt(rootDir *os.File, name string, payload []byte) error {
	if len(payload) == 0 || len(payload) > maxMaterializedSize {
		return domain.ErrInvalidInput("materialized network payload has invalid size")
	}
	if existing, err := readMaterializedAt(rootDir, name, maxMaterializedSize); err == nil {
		if len(existing) == len(payload) && subtle.ConstantTimeCompare(existing, payload) == 1 {
			return nil
		}
		return domain.ErrInvalidManifest
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tmpLeaf := ".tmp-" + uuid.NewString()
	fd, err := unix.Openat(int(rootDir.Fd()), tmpLeaf, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), tmpLeaf)
	defer unix.Unlinkat(int(rootDir.Fd()), tmpLeaf, 0)
	if _, err = file.Write(payload); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = unix.Renameat2(int(rootDir.Fd()), tmpLeaf, int(rootDir.Fd()), name, unix.RENAME_NOREPLACE); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return writeImmutableAt(rootDir, name, payload)
		}
		return err
	}
	return rootDir.Sync()
}
func readMaterialized(path string) ([]byte, error) {
	return readMaterializedBounded(path, maxMaterializedSize)
}

func readMaterializedBounded(path string, limit int64) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readMaterializedFile(file, limit)
}

func readMaterializedAt(rootDir *os.File, name string, limit int64) ([]byte, error) {
	fd, err := unix.Openat(int(rootDir.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	return readMaterializedFile(file, limit)
}

func readMaterializedFile(file *os.File, limit int64) ([]byte, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return nil, domain.ErrInvalidInput("materialized network file must be a non-symlink 0600 regular file")
	}
	if info.Size() < 0 || info.Size() > limit {
		return nil, domain.ErrInvalidInput("materialized network file is too large")
	}
	payload, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > limit {
		return nil, domain.ErrInvalidInput("materialized network file is too large")
	}
	return payload, nil
}

func publishMaterializedNoReplaceAt(rootDir *os.File, name string, payload []byte) (bool, error) {
	tmpLeaf := ".tmp-" + uuid.NewString()
	fd, err := unix.Openat(int(rootDir.Fd()), tmpLeaf, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return false, err
	}
	file := os.NewFile(uintptr(fd), tmpLeaf)
	defer unix.Unlinkat(int(rootDir.Fd()), tmpLeaf, 0)
	if _, err = file.Write(payload); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return false, err
	}
	if err = unix.Renameat2(int(rootDir.Fd()), tmpLeaf, int(rootDir.Fd()), name, unix.RENAME_NOREPLACE); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return false, nil
		}
		return false, err
	}
	return true, rootDir.Sync()
}

// ImportConfig parses only the native keys emitted by Materialize. A caller
// supplies the already registered Backend path; no browser-controlled path is
// accepted by this API.
func (m *Materializer) ImportConfig(path string) (api.ImportedNetworkProfile, error) {
	payload, err := readMaterialized(path)
	if err != nil {
		return api.ImportedNetworkProfile{}, err
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(payload), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return api.ImportedNetworkProfile{}, domain.ErrInvalidManifest
		}
		key, value := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		switch key {
		case "select_proxy_mode", "socks5", "http_proxy", "socks5_username", "socks5_password":
			if _, duplicate := values[key]; duplicate {
				return api.ImportedNetworkProfile{}, domain.ErrInvalidManifest
			}
			values[key] = value
		default:
			return api.ImportedNetworkProfile{}, domain.ErrInvalidManifest
		}
	}
	mode := values["select_proxy_mode"]
	endpointKey := "http_proxy"
	if mode == "only_socks5" {
		endpointKey = "socks5"
	} else if mode != "only_http_proxy" {
		return api.ImportedNetworkProfile{}, domain.ErrUnsupportedCapability
	}
	endpoint := strings.TrimPrefix(values[endpointKey], "http://")
	host, portText, err := net.SplitHostPort(endpoint)
	if err != nil || host == "" {
		return api.ImportedNetworkProfile{}, domain.ErrInvalidManifest
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return api.ImportedNetworkProfile{}, domain.ErrInvalidManifest
	}
	result := api.ImportedNetworkProfile{Mode: mode, Host: host, Port: port}
	if values["socks5_username"] != "" || values["socks5_password"] != "" {
		result.Secret = &api.NetworkSecretPayload{Username: values["socks5_username"], Password: values["socks5_password"]}
	}
	secretFingerprint := ""
	if result.Secret != nil {
		secretFingerprint = m.authenticate("import-secret-v1", []byte(result.Secret.Username), []byte(result.Secret.Password))
	}
	identityPayload, _ := json.Marshal(struct {
		Mode              string   `json:"mode"`
		Host              string   `json:"host"`
		Port              int      `json:"port"`
		DirectIPs         []string `json:"direct_ips"`
		SecretFingerprint string   `json:"secret_fingerprint,omitempty"`
	}{result.Mode, strings.ToLower(result.Host), result.Port, result.DirectIPs, secretFingerprint})
	result.SourceIdentity = m.authenticate("import-source-v1", identityPayload)
	return result, nil
}
