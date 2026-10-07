package fleet

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	admincli "openagentx/internal/cli/admin"
	"openagentx/internal/domain"
	fleetmodel "openagentx/internal/fleet"
	"openagentx/internal/persistence/sqlite"
	workerconfig "openagentx/internal/worker"
)

// Bootstrap receipts describe a one-time import. Registered Agents instead
// resume against their current identity in the authenticated installation.
// This read-only repository path also serves Agents whose old receipt is damaged.
func verifyRegisteredResume(ctx context.Context, o agentOptions, deps Dependencies) (bool, error) {
	if _, err := os.Stat(o.paths.database); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	repo, err := sqlite.OpenMaintenance(ctx, o.paths.database, true, sqlite.Options{Now: deps.Now})
	if err != nil {
		return false, err
	}
	defer repo.Close()
	agent, profile, err := repo.GetAgent(ctx, o.id)
	if errors.Is(err, domain.ErrAgentNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	client, session, err := authenticateFleetClient(ctx, o.paths, domain.WebRoleOwner, domain.CLIScopeFleetLifecycle, deps)
	if err != nil {
		return false, fmt.Errorf("registered Agent resume requires owner/fleet.lifecycle authentication: %s", safeAuthError(err))
	}
	installation, err := repo.InstallationID(ctx)
	if err != nil {
		return false, err
	}
	if installation != session.InstallationID {
		return false, fmt.Errorf("local database installation does not match authenticated daemon")
	}
	if agent.Status != domain.AgentIdentityActive {
		return false, fmt.Errorf("registered Agent is not active")
	}
	options, err := client.ListAgentOptions(ctx)
	if err != nil {
		return false, err
	}
	optionMap, err := validateAgentOptions(options)
	if err != nil {
		return false, err
	}
	_, prepared, err := prepare(o.paths.manifest, o.paths.workerDir, o.paths.socket, optionMap, o.id)
	if err != nil {
		return false, err
	}
	entry := prepared[0]
	option := optionMap[o.id]
	if option.AgentID != agent.ID || option.OrganizationID != agent.OrganizationID || option.DisplayName != agent.DisplayName {
		return false, fmt.Errorf("registered Agent does not match authenticated Console identity")
	}
	if err = agent.Validate(); err != nil {
		return false, err
	}
	if err = profile.Validate(); err != nil {
		return false, err
	}
	if !filepath.IsAbs(profile.WorkspaceRoot) || !filepath.IsAbs(profile.InstructionsPath) {
		return false, fmt.Errorf("registered Agent workspace and role paths must be absolute")
	}
	workspace, err := os.Stat(profile.WorkspaceRoot)
	if err != nil || !workspace.IsDir() {
		return false, fmt.Errorf("registered Agent workspace must be an existing directory")
	}
	// Legacy Fleet entries legitimately omit identity_file. The authenticated
	// installation's identity/profile remains authoritative in that case.
	if entry.entry.IdentityFile != "" {
		identityPath := entry.entry.IdentityFile
		if !filepath.IsAbs(identityPath) {
			identityPath = filepath.Join(filepath.Dir(o.paths.manifest), identityPath)
		}
		if _, err = fleetmodel.ReadSecureFile(identityPath, fleetmodel.SecureFileOptions{MaximumBytes: 1 << 20, RequirePrivate: true}); err != nil {
			return false, err
		}
		definition, err := admincli.LoadAgentDefinition(identityPath)
		if err != nil {
			return false, err
		}
		localCapabilities := append([]string(nil), definition.Profile.Capabilities...)
		registeredCapabilities := append([]string(nil), profile.Capabilities...)
		slices.Sort(localCapabilities)
		slices.Sort(registeredCapabilities)
		if definition.AgentID != agent.ID || definition.PrincipalID != agent.PrincipalID ||
			definition.OrganizationID != agent.OrganizationID || definition.DisplayName != agent.DisplayName ||
			filepath.Clean(definition.Profile.InstructionsPath) != filepath.Clean(profile.InstructionsPath) ||
			filepath.Clean(definition.Profile.WorkspaceRoot) != filepath.Clean(profile.WorkspaceRoot) ||
			!slices.Equal(localCapabilities, registeredCapabilities) {
			return false, fmt.Errorf("local identity/role/workspace does not match registered Agent; use the formal Agent management entrypoint")
		}
	}
	// The registered role path is the Runtime's current source. Role contents
	// are frozen per Run; there is no immutable bootstrap content hash.
	role, err := fleetmodel.ReadSecureFile(profile.InstructionsPath, fleetmodel.SecureFileOptions{MaximumBytes: 1 << 20})
	if err != nil {
		return false, err
	}
	if len(role) == 0 {
		return false, fmt.Errorf("registered Agent role is empty")
	}
	config, err := workerconfig.LoadProcessConfig(entry.workerPath)
	if err != nil {
		return false, err
	}
	for _, backend := range config.RuntimeBackendConfig {
		if backend.AdapterID != "codex-app-server" && backend.AdapterID != "agy-batch" {
			continue
		}
		workingDir, _ := backend.Options["working_dir"].(string)
		if !filepath.IsAbs(workingDir) || filepath.Clean(workingDir) != filepath.Clean(profile.WorkspaceRoot) {
			return false, fmt.Errorf("Worker working_dir does not match registered Agent workspace")
		}
	}
	// Execution preferences are mutable through their own APIs, not identity.
	// In particular, do not compare default_execution_profile_id or timeout.
	return true, nil
}
