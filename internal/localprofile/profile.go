package localprofile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"openagentx/internal/domain"
)

const (
	EnvHome            = "OPENAGENTX_HOME"
	EnvSocketPath      = "OPENAGENTX_SOCKET_PATH"
	EnvDatabasePath    = "OPENAGENTX_DATABASE_PATH"
	EnvFleetManifest   = "OPENAGENTX_FLEET_MANIFEST"
	EnvWorkerConfigDir = "OPENAGENTX_WORKER_CONFIG_DIR"
	EnvCredentialsPath = "OPENAGENTX_CREDENTIALS_PATH"
)

var (
	ErrHomeUnavailable = errors.New("OpenAgentX user home is unavailable")
	ErrEmptyPath       = errors.New("OpenAgentX path override is empty")
	ErrRelativePath    = errors.New("OpenAgentX path override must be absolute")
	ErrInvalidPath     = errors.New("OpenAgentX path cannot be canonicalized")
	ErrInvalidAgentID  = errors.New("invalid Agent ID for Worker config path")
	ErrPathConflict    = errors.New("OpenAgentX paths conflict")
)

type Resource string

const (
	SocketPath      Resource = "UDS"
	DatabasePath    Resource = "database"
	FleetManifest   Resource = "Fleet manifest"
	WorkerConfigDir Resource = "Worker config directory"
	CredentialsPath Resource = "CLI credentials"
)

type Source string

const (
	SourceExplicit    Source = "explicit"
	SourceResourceEnv Source = "resource_env"
	SourceHomeEnv     Source = "home_env"
	SourceUserHome    Source = "user_home"
)

type Override struct {
	Value string
	Set   bool
}

// PathFlag retains whether a path flag appeared, including --flag=.
type PathFlag struct {
	value string
	set   bool
}

func (f *PathFlag) String() string {
	if f == nil {
		return ""
	}
	return f.value
}

func (f *PathFlag) Set(value string) error {
	f.value = value
	f.set = true
	return nil
}

func (f *PathFlag) Override() Override {
	if f == nil {
		return Override{}
	}
	return Override{Value: f.value, Set: f.set}
}

type ResolvedPath struct {
	Path   string
	Source Source
}

type Resolver struct {
	LookupEnv   func(string) (string, bool)
	UserHomeDir func() (string, error)
}

func DefaultResolver() Resolver {
	return Resolver{LookupEnv: os.LookupEnv, UserHomeDir: os.UserHomeDir}
}

type resourceSpec struct {
	environment string
	relative    string
}

func (r Resource) spec() (resourceSpec, error) {
	switch r {
	case SocketPath:
		return resourceSpec{environment: EnvSocketPath, relative: filepath.Join("run", "openagentx.sock")}, nil
	case DatabasePath:
		return resourceSpec{environment: EnvDatabasePath, relative: filepath.Join("data", "openagentx.db")}, nil
	case FleetManifest:
		return resourceSpec{environment: EnvFleetManifest, relative: "fleet.yaml"}, nil
	case WorkerConfigDir:
		return resourceSpec{environment: EnvWorkerConfigDir, relative: "workers"}, nil
	case CredentialsPath:
		return resourceSpec{environment: EnvCredentialsPath, relative: "credentials.json"}, nil
	default:
		return resourceSpec{}, fmt.Errorf("%w: unknown resource %q", ErrInvalidPath, r)
	}
}

func (r Resolver) Resolve(resource Resource, explicit Override) (ResolvedPath, error) {
	spec, err := resource.spec()
	if err != nil {
		return ResolvedPath{}, err
	}
	lookup := r.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	if explicit.Set {
		path, err := canonicalOverride("explicit "+string(resource), explicit.Value)
		return ResolvedPath{Path: path, Source: SourceExplicit}, err
	}
	if value, ok := lookup(spec.environment); ok {
		path, err := canonicalOverride(spec.environment, value)
		return ResolvedPath{Path: path, Source: SourceResourceEnv}, err
	}
	if value, ok := lookup(EnvHome); ok {
		home, err := canonicalOverride(EnvHome, value)
		if err != nil {
			return ResolvedPath{}, err
		}
		return ResolvedPath{Path: filepath.Join(home, spec.relative), Source: SourceHomeEnv}, nil
	}
	homeResolver := r.UserHomeDir
	if homeResolver == nil {
		homeResolver = os.UserHomeDir
	}
	userHome, err := homeResolver()
	if err != nil {
		return ResolvedPath{}, fmt.Errorf("%w: %v", ErrHomeUnavailable, err)
	}
	if strings.TrimSpace(userHome) == "" {
		return ResolvedPath{}, fmt.Errorf("%w: home path is empty", ErrHomeUnavailable)
	}
	if !filepath.IsAbs(userHome) || strings.ContainsRune(userHome, '\x00') {
		return ResolvedPath{}, fmt.Errorf("%w: home path is not an absolute canonical path", ErrHomeUnavailable)
	}
	home := filepath.Join(filepath.Clean(userHome), ".openagentx")
	return ResolvedPath{Path: filepath.Join(home, spec.relative), Source: SourceUserHome}, nil
}

func (r Resolver) WorkerConfig(explicitDirectory Override, agentID string) (ResolvedPath, error) {
	if err := domain.ValidateIdentifier("agent_id", agentID); err != nil {
		return ResolvedPath{}, fmt.Errorf("%w: %v", ErrInvalidAgentID, err)
	}
	directory, err := r.Resolve(WorkerConfigDir, explicitDirectory)
	if err != nil {
		return ResolvedPath{}, err
	}
	path := filepath.Join(directory.Path, strings.TrimSpace(agentID)+".yaml")
	if filepath.Dir(path) != directory.Path {
		return ResolvedPath{}, ErrInvalidAgentID
	}
	return ResolvedPath{Path: path, Source: directory.Source}, nil
}

func (r Resolver) SourceHint(resource Resource) string {
	spec, err := resource.spec()
	if err != nil {
		return "invalid resource"
	}
	lookup := r.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	if _, ok := lookup(spec.environment); ok {
		return "$" + spec.environment
	}
	if _, ok := lookup(EnvHome); ok {
		return "$" + EnvHome + string(filepath.Separator) + spec.relative
	}
	return "~" + string(filepath.Separator) + filepath.Join(".openagentx", spec.relative)
}

func PathUsage(resource Resource, description string) string {
	return fmt.Sprintf("%s (default source: %s)", description, DefaultResolver().SourceHint(resource))
}

func EnsureDistinct(paths map[string]string) error {
	labels := make([]string, 0, len(paths))
	for label := range paths {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	seen := make(map[string]string, len(paths))
	for _, label := range labels {
		path := filepath.Clean(paths[label])
		if previous, ok := seen[path]; ok {
			return fmt.Errorf("%w: %s and %s both resolve to %q", ErrPathConflict, previous, label, path)
		}
		seen[path] = label
	}
	return nil
}

func canonicalOverride(source, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%w: %s is set but empty", ErrEmptyPath, source)
	}
	if strings.ContainsRune(value, '\x00') {
		return "", fmt.Errorf("%w: %s contains a NUL byte", ErrInvalidPath, source)
	}
	if !filepath.IsAbs(value) {
		return "", fmt.Errorf("%w: %s is relative", ErrRelativePath, source)
	}
	return filepath.Clean(value), nil
}
