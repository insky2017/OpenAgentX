package fleet

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"openagentx/internal/domain"
	fleetmodel "openagentx/internal/fleet"
	"openagentx/internal/localprofile"
	"openagentx/internal/persistence/sqlite"
)

type agentRemovalOptions struct {
	ids                []string
	paths              fleetPaths
	yes, dryRun, purge bool
	wait               time.Duration
}
type agentRemovalPreview struct {
	DryRun   bool                     `json:"dry_run"`
	Database *domain.AgentRemovalPlan `json:"database"`
	Local    agentRemovalLocalPlan    `json:"local"`
}

func parseAgentRemovalOptions(args []string, deps Dependencies) (agentRemovalOptions, error) {
	var o agentRemovalOptions
	f := flag.NewFlagSet("agent remove", flag.ContinueOnError)
	f.SetOutput(deps.Err)
	var manifest, database, socket, workers, credentials localprofile.PathFlag
	f.Var(&manifest, "file", "Fleet manifest")
	f.Var(&database, "db", "Existing local database")
	f.Var(&socket, "socket", "Running local daemon socket")
	f.Var(&workers, "worker-dir", "Worker config directory")
	f.Var(&credentials, "credentials", "Credential store")
	f.BoolVar(&o.yes, "yes", false, "Execute the exact selected Agent removal")
	f.BoolVar(&o.dryRun, "dry-run", false, "Preview only; never migrate or remove")
	f.BoolVar(&o.purge, "purge-history", false, "Explicitly include selected Agent history")
	f.DurationVar(&o.wait, "wait", 3*time.Minute, "Graceful stop deadline")
	// Go flag stops at a positional argument; accept documented IDs on either side.
	var flags []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			o.ids = append(o.ids, a)
			continue
		}
		flags = append(flags, a)
		name := strings.TrimLeft(strings.SplitN(a, "=", 2)[0], "-")
		if !strings.Contains(a, "=") && (name == "file" || name == "db" || name == "socket" || name == "worker-dir" || name == "credentials" || name == "wait") {
			i++
			if i >= len(args) {
				return o, fmt.Errorf("flag %s requires a value", a)
			}
			flags = append(flags, args[i])
		}
	}
	if err := f.Parse(flags); err != nil {
		return o, err
	}
	if len(o.ids) == 0 {
		return o, fmt.Errorf("at least one exact Agent ID is required")
	}
	if o.yes && o.dryRun {
		return o, fmt.Errorf("--yes and --dry-run cannot be combined")
	}
	if o.wait <= 0 {
		return o, fmt.Errorf("--wait must be positive")
	}
	seen := map[string]bool{}
	for _, id := range o.ids {
		if err := domain.ValidateIdentifier("agent_id", id); err != nil {
			return o, err
		}
		if seen[id] {
			return o, fmt.Errorf("duplicate Agent ID %s", id)
		}
		seen[id] = true
	}
	sort.Strings(o.ids)
	var err error
	o.paths, err = resolveFleetPaths(manifest, database, socket, workers, credentials)
	return o, err
}

func executeAgentRemove(args []string, deps Dependencies) int {
	o, err := parseAgentRemovalOptions(args, deps)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(deps.Err, err)
		return 2
	}
	ctx := deps.Context
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err = removeAgents(ctx, o, deps); err != nil {
		fmt.Fprintf(deps.Err, "Agent remove 未完成: %v；已报告的步骤不会自动回滚，修复阻断后可对相同 ID 重试。\n", err)
		return 1
	}
	return 0
}

func removeAgents(ctx context.Context, o agentRemovalOptions, deps Dependencies) error {
	client, session, err := authenticateFleetClient(ctx, o.paths, domain.WebRoleOwner, domain.CLIScopeFleetLifecycle, deps)
	if err != nil {
		return fmt.Errorf("owner/fleet.lifecycle authentication required: %s", safeAuthError(err))
	}
	repo, err := sqlite.OpenMaintenance(ctx, o.paths.database, true, sqlite.Options{Now: deps.Now})
	if err != nil {
		return err
	}
	defer repo.Close()
	installation, err := repo.InstallationID(ctx)
	if err != nil {
		return err
	}
	if installation != session.InstallationID {
		return fmt.Errorf("local database installation does not match authenticated daemon")
	}
	plan, err := repo.PlanAgentRemoval(ctx, session.Principal.PrincipalID, o.ids)
	if err != nil {
		return err
	}
	local, err := planAgentRemovalLocal(ctx, o, deps)
	if err != nil {
		return err
	}
	if !o.purge {
		local.Blockers = append(local.Blockers, "history deletion requires --purge-history")
	}
	if err = json.NewEncoder(deps.Out).Encode(agentRemovalPreview{DryRun: !o.yes, Database: plan, Local: local}); err != nil {
		return err
	}
	if !o.yes {
		return nil
	}
	if len(local.Blockers) > 0 {
		return fmt.Errorf("local removal blocked: %s", strings.Join(local.Blockers, "; "))
	}
	if len(plan.Blockers) > 0 {
		return fmt.Errorf("database removal blocked before lifecycle changes: %s", strings.Join(plan.Blockers, "; "))
	}
	unlock, err := lockAgentRemovalManifest(o.paths.manifest)
	if err != nil {
		return err
	}
	defer unlock()
	current, err := planAgentRemovalLocal(ctx, o, deps)
	if err != nil {
		return err
	}
	if !sameAgentRemovalLocal(local, current) {
		return fmt.Errorf("local removal inputs changed after preview")
	}
	options, err := client.ListAgentOptions(ctx)
	if err != nil {
		return err
	}
	selected := map[string]bool{}
	for _, id := range o.ids {
		selected[id] = true
	}
	stopManifest := fleetmodel.Manifest{}
	for _, option := range options {
		if selected[option.AgentID] {
			stopManifest.Agents = append(stopManifest.Agents, fleetmodel.Agent{AgentID: option.AgentID})
		}
	}
	stopCtx, stopCancel := context.WithTimeout(ctx, o.wait)
	defer stopCancel()
	progressDeps := deps
	progressDeps.Out = deps.Err
	_, targets, err := queueStops(stopCtx, client, stopManifest, false, progressDeps)
	if err != nil {
		return err
	}
	if err = waitForOffline(stopCtx, client, targets, progressDeps); err != nil {
		return err
	}
	if err = removalStep(deps, "workers_offline", o.ids); err != nil {
		return err
	}
	for _, service := range local.Services {
		if !service.Loaded {
			continue
		}
		if _, err = deps.RunSystemctl(ctx, "--user", "disable", "--now", service.Unit); err != nil {
			return fmt.Errorf("disable selected instance %s failed", service.Unit)
		}
		state, e := deps.RunSystemctl(ctx, "--user", "show", service.Unit, "--property=ActiveState", "--value")
		if e != nil || (strings.TrimSpace(state) != "inactive" && strings.TrimSpace(state) != "failed") {
			return fmt.Errorf("selected instance %s is not inactive", service.Unit)
		}
		if err = removalStep(deps, "service_disabled", service.Unit); err != nil {
			return err
		}
	}
	// Graceful shutdown legitimately updates runtime state files. Capture their
	// final contents only after the selected Workers and instances are stopped.
	current, err = planAgentRemovalLocal(ctx, o, deps)
	if err != nil {
		return err
	}
	if len(current.Blockers) != 0 {
		return fmt.Errorf("local removal blocked after stop: %s", strings.Join(current.Blockers, "; "))
	}
	if string(current.manifest) != string(local.manifest) {
		return fmt.Errorf("Fleet manifest changed during stop")
	}
	local = current
	writer, err := sqlite.OpenMaintenance(ctx, o.paths.database, false, sqlite.Options{Now: deps.Now})
	if err != nil {
		return err
	}
	defer writer.Close()
	installation, err = writer.InstallationID(ctx)
	if err != nil {
		return err
	}
	if installation != session.InstallationID {
		return fmt.Errorf("database installation changed")
	}
	// Revalidate the live owner session after any wait and before migration/purge.
	renewed, err := client.Session(ctx)
	if err != nil {
		return err
	}
	if renewed.InstallationID != installation || renewed.Principal.PrincipalID != session.Principal.PrincipalID || !roleAllowed(renewed.Principal.Roles, domain.WebRoleOwner) || !contains(renewed.Principal.Scopes, string(domain.CLIScopeFleetLifecycle)) {
		return fmt.Errorf("owner session changed")
	}
	plan, err = repo.PlanAgentRemoval(ctx, session.Principal.PrincipalID, o.ids)
	if err != nil {
		return err
	}
	if len(plan.Blockers) > 0 {
		return fmt.Errorf("database removal blocked: %s", strings.Join(plan.Blockers, "; "))
	}
	started := time.Now()
	if err = writer.UpgradeAgentRemovalSchema(ctx); err != nil {
		return fmt.Errorf("schema upgrade failed after %d ms: %w", time.Since(started).Milliseconds(), err)
	}
	if err = removalStep(deps, "schema_ready", map[string]any{"elapsed_ms": time.Since(started).Milliseconds()}); err != nil {
		return err
	}
	plan, err = repo.PlanAgentRemoval(ctx, session.Principal.PrincipalID, o.ids)
	if err != nil {
		return err
	}
	if err = json.NewEncoder(deps.Out).Encode(agentRemovalPreview{Database: plan, Local: local}); err != nil {
		return err
	}
	started = time.Now()
	result, err := writer.ApplyAgentRemoval(ctx, session.Principal.PrincipalID, o.ids, plan.Digest)
	if err != nil {
		return fmt.Errorf("database purge failed after %d ms: %w", time.Since(started).Milliseconds(), err)
	}
	if err = removalStep(deps, "database_purged", map[string]any{"removal": result, "elapsed_ms": time.Since(started).Milliseconds()}); err != nil {
		return err
	}
	if err = writeAgentRemovalManifest(o.paths.manifest, local, deps); err != nil {
		return err
	}
	if err = removalStep(deps, "fleet_entries_removed", o.ids); err != nil {
		return err
	}
	for _, file := range local.Files {
		if err = deleteAgentRemovalFile(file); err != nil {
			return err
		}
		if err = removalStep(deps, "file_removed", file.Path); err != nil {
			return err
		}
	}
	return removalStep(deps, "selected_agent_removal_complete", map[string]any{"agent_ids": o.ids, "preserved_paths": local.Preserved, "workspace_policy": "preserved; external paths require separate ownership review"})
}

func removalStep(deps Dependencies, step string, result any) error {
	return json.NewEncoder(deps.Out).Encode(map[string]any{"step": step, "result": result})
}
func lockAgentRemovalManifest(path string) (func(), error) {
	fd, err := unix.Open(path+".lock", unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("Fleet manifest lifecycle is busy: %w", err)
	}
	return func() { _ = unix.Flock(fd, unix.LOCK_UN); _ = unix.Close(fd) }, nil
}
