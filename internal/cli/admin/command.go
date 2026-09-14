package admincli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
	webAuth "openagentx/internal/auth/web"
	"openagentx/internal/domain"
	openagentsqlite "openagentx/internal/persistence/sqlite"
)

type PasswordReader func(prompt string) (string, error)

type Dependencies struct {
	Out          io.Writer
	Err          io.Writer
	ReadPassword PasswordReader
	Now          func() time.Time
	NewID        func(prefix string) string
}

func DefaultDependencies() Dependencies {
	return Dependencies{
		Out: os.Stdout,
		Err: os.Stderr,
		ReadPassword: func(prompt string) (string, error) {
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return "", fmt.Errorf("interactive terminal is required for password input")
			}
			fmt.Fprint(os.Stderr, prompt)
			value, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			return string(value), err
		},
		Now:   time.Now,
		NewID: func(prefix string) string { return prefix + "-" + uuid.NewString() },
	}
}

func (d Dependencies) withDefaults() Dependencies {
	defaults := DefaultDependencies()
	if d.Out == nil {
		d.Out = defaults.Out
	}
	if d.Err == nil {
		d.Err = defaults.Err
	}
	if d.ReadPassword == nil {
		d.ReadPassword = defaults.ReadPassword
	}
	if d.Now == nil {
		d.Now = defaults.Now
	}
	if d.NewID == nil {
		d.NewID = defaults.NewID
	}
	return d
}

func ExecuteInit(args []string, dependencies Dependencies) int {
	deps := dependencies.withDefaults()
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(deps.Err)
	databasePath := flags.String("db", "", "Target SQLite database path")
	username := flags.String("owner-username", "owner", "Initial owner username")
	displayName := flags.String("owner-display-name", "Owner", "Initial owner display name")
	organizationID := flags.String("organization-id", "default", "Initial Organization ID")
	organizationName := flags.String("organization-name", "Default Organization", "Initial Organization name")
	if err := flags.Parse(args); err != nil || strings.TrimSpace(*databasePath) == "" {
		fmt.Fprintln(deps.Err, "Usage: openagentx init --db <path> [--owner-username owner] [--organization-id default]")
		return 2
	}
	ctx := context.Background()
	repository, err := openagentsqlite.Open(ctx, *databasePath, openagentsqlite.Options{Now: deps.Now})
	if err != nil {
		fmt.Fprintf(deps.Err, "initialize database: %v\n", err)
		return 1
	}
	defer repository.Close()
	initialized, err := repository.IsInitialized(ctx)
	if err != nil {
		fmt.Fprintf(deps.Err, "inspect initialization: %v\n", err)
		return 1
	}
	if initialized {
		fmt.Fprintln(deps.Out, "OpenAgentX is already initialized; no changes were made")
		return 0
	}
	password, err := deps.ReadPassword("Owner password: ")
	if err != nil {
		fmt.Fprintf(deps.Err, "read owner password: %v\n", err)
		return 1
	}
	confirmation, err := deps.ReadPassword("Confirm owner password: ")
	if err != nil {
		fmt.Fprintf(deps.Err, "confirm owner password: %v\n", err)
		return 1
	}
	if password != confirmation {
		fmt.Fprintln(deps.Err, "owner password confirmation does not match")
		return 1
	}
	if len(password) < 12 {
		fmt.Fprintln(deps.Err, "owner password must contain at least 12 characters")
		return 1
	}
	digest, err := webAuth.HashPassword(password)
	if err != nil {
		fmt.Fprintf(deps.Err, "hash owner password: %v\n", err)
		return 1
	}
	ownerID := deps.NewID("human-owner")
	webUserID := deps.NewID("web-user")
	now := deps.Now().UTC()
	owner := &domain.Principal{ID: ownerID, Kind: domain.PrincipalHuman, DisplayName: strings.TrimSpace(*displayName), Status: domain.IdentityActive}
	systemPrincipal := &domain.Principal{ID: "daemon", Kind: domain.PrincipalSystem, DisplayName: "OpenAgentX daemon", Status: domain.IdentityActive}
	organization := &domain.Organization{ID: strings.TrimSpace(*organizationID), Name: strings.TrimSpace(*organizationName), Status: domain.IdentityActive}
	webUser := &domain.WebUserRecord{ID: webUserID, PrincipalID: ownerID, Username: strings.TrimSpace(*username), PasswordDigest: digest, Roles: []domain.WebRole{domain.WebRoleOwner}, Status: domain.IdentityActive, PasswordSetAt: now}
	events := []*domain.JournalEvent{
		newEvent(deps, ownerID, "", "principal", ownerID, "principal.created", map[string]any{"kind": domain.PrincipalHuman}),
		newEvent(deps, ownerID, "", "principal", systemPrincipal.ID, "principal.created", map[string]any{"kind": domain.PrincipalSystem}),
		newEvent(deps, ownerID, organization.ID, "organization", organization.ID, "organization.created", map[string]any{"name": organization.Name}),
		newEvent(deps, ownerID, organization.ID, "web_user", webUser.ID, "web_user.created", map[string]any{"username": webUser.Username, "roles": webUser.Roles}),
	}
	created, err := repository.Initialize(ctx, owner, systemPrincipal, organization, webUser, events)
	if err != nil {
		fmt.Fprintf(deps.Err, "initialize OpenAgentX: %v\n", err)
		return 1
	}
	if !created {
		fmt.Fprintln(deps.Out, "OpenAgentX is already initialized; no changes were made")
		return 0
	}
	fmt.Fprintf(deps.Out, "Initialized OpenAgentX owner %q and Organization %q\n", webUser.Username, organization.ID)
	return 0
}

func ExecuteAgent(args []string, dependencies Dependencies) int {
	deps := dependencies.withDefaults()
	if len(args) == 0 || args[0] != "apply" {
		fmt.Fprintln(deps.Err, "Usage: openagentx agent apply --db <path> --file <identity.yaml> [--owner-username owner]")
		return 2
	}
	flags := flag.NewFlagSet("agent apply", flag.ContinueOnError)
	flags.SetOutput(deps.Err)
	databasePath := flags.String("db", "", "Target SQLite database path")
	definitionPath := flags.String("file", "", "Agent identity definition")
	ownerUsername := flags.String("owner-username", "owner", "Owner username")
	if err := flags.Parse(args[1:]); err != nil || strings.TrimSpace(*databasePath) == "" || strings.TrimSpace(*definitionPath) == "" {
		fmt.Fprintln(deps.Err, "Usage: openagentx agent apply --db <path> --file <identity.yaml> [--owner-username owner]")
		return 2
	}
	definition, err := LoadAgentDefinition(*definitionPath)
	if err != nil {
		fmt.Fprintf(deps.Err, "load Agent identity: %v\n", err)
		return 1
	}
	ctx := context.Background()
	repository, err := openagentsqlite.Open(ctx, *databasePath, openagentsqlite.Options{Now: deps.Now})
	if err != nil {
		fmt.Fprintf(deps.Err, "open database: %v\n", err)
		return 1
	}
	defer repository.Close()
	owner, err := repository.GetWebUserByUsername(ctx, strings.TrimSpace(*ownerUsername))
	if err != nil || owner.Status != domain.IdentityActive || !owner.HasRole(domain.WebRoleOwner) {
		fmt.Fprintln(deps.Err, "Agent apply requires an active owner")
		return 1
	}
	password, err := deps.ReadPassword("Owner password: ")
	if err != nil {
		fmt.Fprintf(deps.Err, "read owner password: %v\n", err)
		return 1
	}
	if !webAuth.VerifyPassword(owner.PasswordDigest, password) {
		fmt.Fprintln(deps.Err, "owner authentication failed")
		return 1
	}
	principal := &domain.Principal{ID: definition.PrincipalID, Kind: domain.PrincipalAgent, DisplayName: definition.DisplayName, Status: domain.IdentityActive}
	agent := &domain.AgentIdentity{ID: definition.AgentID, PrincipalID: definition.PrincipalID, OrganizationID: definition.OrganizationID, DisplayName: definition.DisplayName, Status: domain.AgentIdentityActive, Version: 1}
	profile := &domain.AgentProfileRecord{AgentID: definition.AgentID, Version: 1, InstructionsPath: definition.Profile.InstructionsPath, WorkspaceRoot: definition.Profile.WorkspaceRoot, DefaultExecutionProfileID: definition.Profile.DefaultExecutionProfileID, Capabilities: definition.Profile.Capabilities}
	events := []*domain.JournalEvent{
		newEvent(deps, owner.PrincipalID, definition.OrganizationID, "principal", principal.ID, "principal.created", map[string]any{"kind": domain.PrincipalAgent}),
		newEvent(deps, owner.PrincipalID, definition.OrganizationID, "agent", agent.ID, "agent.created", map[string]any{"display_name": agent.DisplayName, "profile_version": profile.Version}),
	}
	created, err := repository.ApplyAgent(ctx, principal, agent, profile, events)
	if err != nil {
		fmt.Fprintf(deps.Err, "apply Agent identity: %v\n", err)
		return 1
	}
	if created {
		fmt.Fprintf(deps.Out, "Created Agent %q in Organization %q\n", agent.ID, agent.OrganizationID)
	} else {
		fmt.Fprintf(deps.Out, "Agent %q is unchanged; no events were appended\n", agent.ID)
	}
	return 0
}

func newEvent(deps Dependencies, actorID, organizationID, aggregateType, aggregateID, eventType string, payload any) *domain.JournalEvent {
	encoded, _ := json.Marshal(payload)
	return &domain.JournalEvent{ID: deps.NewID("event"), OrganizationID: organizationID, AggregateType: aggregateType, AggregateID: aggregateID, EventType: eventType, ActorPrincipalID: actorID, Payload: encoded, CreatedAt: deps.Now().UTC()}
}

type AgentDefinition struct {
	Version        int    `yaml:"version"`
	AgentID        string `yaml:"agent_id"`
	PrincipalID    string `yaml:"principal_id"`
	OrganizationID string `yaml:"organization_id"`
	DisplayName    string `yaml:"display_name"`
	Profile        struct {
		InstructionsPath          string   `yaml:"instructions_path"`
		WorkspaceRoot             string   `yaml:"workspace_root"`
		DefaultExecutionProfileID *string  `yaml:"default_execution_profile_id,omitempty"`
		Capabilities              []string `yaml:"capabilities"`
	} `yaml:"profile"`
}

func LoadAgentDefinition(path string) (*AgentDefinition, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	var definition AgentDefinition
	if err := decoder.Decode(&definition); err != nil {
		return nil, err
	}
	if definition.Version != 1 {
		return nil, domain.ErrInvalidInput("Agent identity version must be 1")
	}
	base, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	definition.Profile.InstructionsPath, err = resolveDefinitionPath(base, definition.Profile.InstructionsPath)
	if err != nil {
		return nil, fmt.Errorf("resolve instructions_path: %w", err)
	}
	definition.Profile.WorkspaceRoot, err = resolveDefinitionPath(base, definition.Profile.WorkspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace_root: %w", err)
	}
	principal := domain.Principal{ID: definition.PrincipalID, Kind: domain.PrincipalAgent, DisplayName: definition.DisplayName, Status: domain.IdentityActive}
	agent := domain.AgentIdentity{ID: definition.AgentID, PrincipalID: definition.PrincipalID, OrganizationID: definition.OrganizationID, DisplayName: definition.DisplayName, Status: domain.AgentIdentityActive, Version: 1}
	profile := domain.AgentProfileRecord{AgentID: definition.AgentID, Version: 1, InstructionsPath: definition.Profile.InstructionsPath, WorkspaceRoot: definition.Profile.WorkspaceRoot, DefaultExecutionProfileID: definition.Profile.DefaultExecutionProfileID, Capabilities: definition.Profile.Capabilities}
	if err := principal.Validate(); err != nil {
		return nil, err
	}
	if err := agent.Validate(); err != nil {
		return nil, err
	}
	if err := profile.Validate(); err != nil {
		return nil, err
	}
	instructions, err := os.Stat(profile.InstructionsPath)
	if err != nil || !instructions.Mode().IsRegular() || instructions.Size() == 0 {
		return nil, domain.ErrInvalidInput("instructions_path must be a non-empty regular file")
	}
	workspace, err := os.Stat(profile.WorkspaceRoot)
	if err != nil || !workspace.IsDir() {
		return nil, domain.ErrInvalidInput("workspace_root must be an existing directory")
	}
	return &definition, nil
}

func resolveDefinitionPath(base, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", domain.ErrInvalidInput("path cannot be empty")
	}
	if !filepath.IsAbs(trimmed) {
		trimmed = filepath.Join(base, trimmed)
	}
	return filepath.Abs(filepath.Clean(trimmed))
}
