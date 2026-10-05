package fleet

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	admincli "openagentx/internal/cli/admin"
	fleetmodel "openagentx/internal/fleet"
	workerconfig "openagentx/internal/worker"
)

type agentRemovalFile struct {
	Path        string `json:"path"`
	Kind        string `json:"kind"`
	Entries     int    `json:"entries"`
	fingerprint string
}
type agentRemovalService struct {
	Unit   string `json:"unit"`
	Loaded bool   `json:"loaded"`
}
type agentRemovalLocalPlan struct {
	Files            []agentRemovalFile    `json:"files"`
	Services         []agentRemovalService `json:"services"`
	Preserved        []string              `json:"preserved"`
	PreservedReasons map[string]string     `json:"preserved_reasons"`
	Blockers         []string              `json:"blockers"`
	manifest         []byte
	replacement      []byte
}

func planAgentRemovalLocal(ctx context.Context, o agentRemovalOptions, deps Dependencies) (agentRemovalLocalPlan, error) {
	p := agentRemovalLocalPlan{Files: []agentRemovalFile{}, Services: []agentRemovalService{}, Preserved: []string{}, Blockers: []string{}}
	selected := map[string]bool{}
	var preservedWorkspaces []string
	for _, id := range o.ids {
		selected[id] = true
		switch id {
		case "openagentx", "rhythm", "pay-service", "quote-service", "identity-service", "oneaxe-voice", "orchestrator":
			p.Blockers = append(p.Blockers, "protected Agent: "+id)
		}
	}
	original, err := fleetmodel.ReadSecureFile(o.paths.manifest, fleetmodel.SecureFileOptions{MaximumBytes: 1 << 20, RequirePrivate: true})
	manifest := fleetmodel.Manifest{Version: fleetmodel.ManifestVersion, Session: fleetmodel.SessionName}
	if err == nil {
		manifest, err = fleetmodel.Decode(bytes.NewReader(original))
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return p, err
	}
	p.manifest = original
	kept := manifest
	kept.Agents = nil
	// A path referenced by a retained Agent is never eligible for removal.
	shared := map[string]bool{}
	for _, a := range manifest.Agents {
		if selected[a.AgentID] {
			continue
		}
		kept.Agents = append(kept.Agents, a)
		shared[resolveRemovalPath(o.paths.manifest, a.WorkerConfig)] = true
		if a.IdentityFile != "" {
			shared[resolveRemovalPath(o.paths.manifest, a.IdentityFile)] = true
			definition, err := admincli.LoadAgentDefinition(resolveRemovalPath(o.paths.manifest, a.IdentityFile))
			if err != nil {
				return p, fmt.Errorf("cannot establish retained Agent %s identity ownership: %w", a.AgentID, err)
			}
			if definition.Profile.InstructionsPath != "" {
				shared[filepath.Clean(definition.Profile.InstructionsPath)] = true
			}
			if definition.Profile.WorkspaceRoot != "" {
				shared[filepath.Clean(definition.Profile.WorkspaceRoot)] = true
			}
		}
		data, e := readWorkerSource(resolveRemovalPath(o.paths.manifest, a.WorkerConfig))
		if e != nil {
			return p, fmt.Errorf("cannot establish retained Agent %s file ownership: %w", a.AgentID, e)
		}
		cfg, e := workerconfig.DecodeProcessConfig(bytes.NewReader(data))
		if e != nil {
			return p, e
		}
		for _, b := range cfg.RuntimeBackendConfig {
			for _, k := range []string{"state_dir", "working_dir", "handoff_file"} {
				if v, ok := b.Options[k].(string); ok && v != "" {
					shared[filepath.Clean(v)] = true
				}
			}
		}
	}
	if len(kept.Agents) > 0 {
		p.replacement, err = fleetmodel.Encode(kept)
		if err != nil {
			return p, err
		}
	}
	add := func(path, kind string) {
		file, e := inspectAgentRemovalFile(path, kind)
		if errors.Is(e, os.ErrNotExist) {
			return
		}
		if e != nil {
			p.Blockers = append(p.Blockers, e.Error())
			return
		}
		for s := range shared {
			if pathsOverlap(path, s) {
				p.Blockers = append(p.Blockers, "path overlaps retained Agent: "+path)
				return
			}
		}
		for _, prior := range p.Files {
			if prior.Path == path {
				return
			}
		}
		p.Files = append(p.Files, file)
	}
	for _, id := range o.ids {
		configPath := filepath.Join(o.paths.workerDir, id+".yaml")
		identityPath := filepath.Join(o.paths.workerDir, "identities", id+".yaml")
		for _, a := range manifest.Agents {
			if a.AgentID != id {
				continue
			}
			if resolveRemovalPath(o.paths.manifest, a.WorkerConfig) != configPath {
				p.Blockers = append(p.Blockers, "noncanonical Worker configuration for "+id)
			}
			if a.IdentityFile != "" && resolveRemovalPath(o.paths.manifest, a.IdentityFile) != identityPath {
				p.Preserved = append(p.Preserved, resolveRemovalPath(o.paths.manifest, a.IdentityFile))
			}
		}
		data, e := readWorkerSource(configPath)
		if e == nil {
			cfg, e := validateWorkerConfig(data, id, o.paths.socket)
			if e != nil {
				p.Blockers = append(p.Blockers, e.Error())
			} else {
				add(configPath, "worker_config")
				add(filepath.Join(o.paths.workerDir, id+".env"), "environment")
				for _, b := range cfg.RuntimeBackendConfig {
					if v, ok := b.Options["working_dir"].(string); ok && v != "" {
						p.Preserved = append(p.Preserved, v)
						preservedWorkspaces = append(preservedWorkspaces, v)
					}
					if v, ok := b.Options["state_dir"].(string); ok && v != "" {
						expected := filepath.Join(o.paths.workerDir, "codex", id)
						if filepath.Clean(v) == expected {
							add(expected, "state_directory")
						} else {
							p.Preserved = append(p.Preserved, v)
						}
					}
				}
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			p.Blockers = append(p.Blockers, e.Error())
		}
		if _, e = os.Lstat(identityPath); e == nil {
			def, e := admincli.LoadAgentDefinition(identityPath)
			if e != nil || def.AgentID != id {
				p.Blockers = append(p.Blockers, "identity ownership mismatch: "+identityPath)
			} else {
				add(identityPath, "identity")
				p.Preserved = append(p.Preserved, def.Profile.WorkspaceRoot)
				preservedWorkspaces = append(preservedWorkspaces, def.Profile.WorkspaceRoot)
				canonicalRole := false
				for _, suffix := range []string{".md", ".role.md"} {
					path := filepath.Join(o.paths.workerDir, "identities", id+suffix)
					if filepath.Clean(def.Profile.InstructionsPath) == path {
						canonicalRole = true
						add(path, "role")
					}
				}
				if !canonicalRole && def.Profile.InstructionsPath != "" {
					p.Preserved = append(p.Preserved, def.Profile.InstructionsPath)
				}
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			p.Blockers = append(p.Blockers, e.Error())
		}
		receiptPath := filepath.Join(o.paths.workerDir, "joins", id+".json")
		if receipt, e := fleetmodel.ReadSecureFile(receiptPath, fleetmodel.SecureFileOptions{MaximumBytes: 1 << 20, RequirePrivate: true}); e == nil {
			var r joinReceipt
			if json.Unmarshal(receipt, &r) != nil || r.AgentID != id || r.Version != 1 {
				p.Blockers = append(p.Blockers, "join receipt ownership mismatch: "+receiptPath)
			} else {
				if r.Workspace != "" {
					p.Preserved = append(p.Preserved, r.Workspace)
					preservedWorkspaces = append(preservedWorkspaces, r.Workspace)
				}
				add(receiptPath, "join_receipt")
				add(receiptPath+".registered", "join_registration")
				if r.HandoffFile == filepath.Join(o.paths.workerDir, "joins", id+".handoff.md") {
					add(r.HandoffFile, "handoff")
				}
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			p.Blockers = append(p.Blockers, e.Error())
		}
		unit := workerUnit(id)
		state, e := deps.RunSystemctl(ctx, "--user", "show", unit, "--property=LoadState", "--value")
		if e != nil {
			p.Blockers = append(p.Blockers, "cannot inspect selected user service: "+unit)
			continue
		}
		service := agentRemovalService{Unit: unit, Loaded: strings.TrimSpace(state) == "loaded"}
		if strings.TrimSpace(state) != "loaded" && strings.TrimSpace(state) != "not-found" {
			p.Blockers = append(p.Blockers, "unexpected selected unit load state: "+unit)
		}
		if service.Loaded {
			raw, e := deps.RunSystemctl(ctx, "--user", "show", unit, "--property=ExecStart", "--value")
			argv, parseErr := parseSystemdExecArgv(raw)
			binary, binaryErr := canonicalUserBinary(deps)
			if e != nil || parseErr != nil || binaryErr != nil || !equalStrings(argv, []string{binary, "worker", "run", "--config", configPath}) {
				active, activeErr := deps.RunSystemctl(ctx, "--user", "show", unit, "--property=ActiveState", "--value")
				enabled, enabledErr := deps.RunSystemctl(ctx, "--user", "show", unit, "--property=UnitFileState", "--value")
				fragment, fragmentErr := deps.RunSystemctl(ctx, "--user", "show", unit, "--property=FragmentPath", "--value")
				if activeErr == nil && enabledErr == nil && fragmentErr == nil && strings.TrimSpace(active) == "inactive" && (strings.TrimSpace(enabled) == "disabled" || strings.TrimSpace(enabled) == "static") && strings.HasSuffix(strings.TrimSpace(fragment), "/openagentx-worker@.service") {
					service.Loaded = false
					p.Preserved = append(p.Preserved, strings.TrimSpace(fragment)+" (unowned inactive template; no service action)")
				} else {
					p.Blockers = append(p.Blockers, "selected service ExecStart ownership mismatch: "+unit)
				}
			}
		}
		if service.Loaded {
			fragment, fragmentErr := deps.RunSystemctl(ctx, "--user", "show", unit, "--property=FragmentPath", "--value")
			if fragmentErr == nil && strings.TrimSpace(fragment) != "" {
				p.Preserved = append(p.Preserved, strings.TrimSpace(fragment))
			}
		}
		p.Services = append(p.Services, service)
	}
	// Collect all selected workspaces before filtering, including workspaces
	// declared by another selected Agent or only by its identity/join receipt.
	eligible := p.Files[:0]
	for _, file := range p.Files {
		overlapsWorkspace := false
		for _, workspace := range preservedWorkspaces {
			if workspace == "" {
				continue
			}
			workspace = filepath.Clean(workspace)
			if resolved, err := filepath.EvalSymlinks(workspace); err == nil {
				workspace = resolved
			}
			if pathsOverlap(file.Path, workspace) {
				p.Blockers = append(p.Blockers, "removal path overlaps preserved workspace: "+file.Path+" -> "+workspace)
				overlapsWorkspace = true
				break
			}
		}
		if !overlapsWorkspace {
			eligible = append(eligible, file)
		}
	}
	p.Files = eligible
	// Keep ownership proofs until their dependent artifacts have been removed,
	// so a partial filesystem failure remains safely retryable.
	rank := map[string]int{"join_receipt": 80, "identity": 90, "worker_config": 100}
	sort.Slice(p.Files, func(i, j int) bool {
		if rank[p.Files[i].Kind] != rank[p.Files[j].Kind] {
			return rank[p.Files[i].Kind] < rank[p.Files[j].Kind]
		}
		return p.Files[i].Path < p.Files[j].Path
	})
	sort.Strings(p.Preserved)
	p.Preserved = uniqueRemovalStrings(p.Preserved)
	p.PreservedReasons = make(map[string]string, len(p.Preserved))
	for _, path := range p.Preserved {
		reason := "workspace or external artifact; working directories and paths without canonical exclusive ownership are preserved"
		if strings.Contains(path, ".service") {
			reason = "systemd unit definition retained; only a verified selected instance may be disabled"
		}
		p.PreservedReasons[path] = reason
	}
	sort.Strings(p.Blockers)
	return p, nil
}

func resolveRemovalPath(manifest, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(filepath.Dir(manifest), path)
}
func pathsOverlap(a, b string) bool {
	separator := string(os.PathSeparator)
	return a == b || strings.HasPrefix(a, strings.TrimSuffix(b, separator)+separator) || strings.HasPrefix(b, strings.TrimSuffix(a, separator)+separator)
}
func uniqueRemovalStrings(values []string) []string {
	out := []string{}
	for _, v := range values {
		if v != "" && (len(out) == 0 || out[len(out)-1] != v) {
			out = append(out, v)
		}
	}
	return out
}
func inspectAgentRemovalFile(path, kind string) (agentRemovalFile, error) {
	out := agentRemovalFile{Path: path, Kind: kind}
	if !filepath.IsAbs(path) {
		return out, fmt.Errorf("removal path must be absolute: %s", path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return out, err
	}
	if resolved != filepath.Clean(path) {
		return out, fmt.Errorf("symlink removal path refused: %s", path)
	}
	hash := sha256.New()
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 || (!d.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("nonregular removal entry: %s", p)
		}
		fmt.Fprintf(hash, "%s\x00%d\x00%d\x00%d\x00", p, info.Mode(), info.Size(), info.ModTime().UnixNano())
		if !d.IsDir() {
			file, e := os.Open(p)
			if e != nil {
				return e
			}
			_, copyErr := io.Copy(hash, file)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
		out.Entries++
		return nil
	})
	if err != nil {
		return out, err
	}
	out.fingerprint = fmt.Sprintf("%x", hash.Sum(nil))
	return out, nil
}
func sameAgentRemovalLocal(a, b agentRemovalLocalPlan) bool { return reflect.DeepEqual(a, b) }
func deleteAgentRemovalFile(file agentRemovalFile) error {
	current, err := inspectAgentRemovalFile(file.Path, file.Kind)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if current.fingerprint != file.fingerprint {
		return fmt.Errorf("removal path changed; preserved: %s", file.Path)
	}
	if file.Kind == "state_directory" {
		return os.RemoveAll(file.Path)
	}
	return os.Remove(file.Path)
}

// Caller holds the same .lock flock as AddAgent and prepared activation.
func writeAgentRemovalManifest(path string, plan agentRemovalLocalPlan, deps Dependencies) error {
	if plan.manifest == nil {
		return nil
	}
	current, err := fleetmodel.ReadSecureFile(path, fleetmodel.SecureFileOptions{MaximumBytes: 1 << 20, RequirePrivate: true})
	if err != nil {
		return err
	}
	if !bytes.Equal(current, plan.manifest) {
		return fmt.Errorf("Fleet manifest changed; refusing replacement")
	}
	if plan.replacement == nil {
		if err = os.Remove(path); err != nil {
			return err
		}
	} else {
		temp, err := os.CreateTemp(filepath.Dir(path), ".fleet-remove-*")
		if err != nil {
			return err
		}
		defer os.Remove(temp.Name())
		defer temp.Close()
		if _, err = temp.Write(plan.replacement); err != nil {
			return err
		}
		if err = temp.Sync(); err != nil {
			return err
		}
		if err = temp.Close(); err != nil {
			return err
		}
		if deps.BeforeAtomicRename != nil {
			if err = deps.BeforeAtomicRename(path); err != nil {
				return err
			}
		}
		current, err = fleetmodel.ReadSecureFile(path, fleetmodel.SecureFileOptions{MaximumBytes: 1 << 20, RequirePrivate: true})
		if err != nil {
			return err
		}
		if !bytes.Equal(current, plan.manifest) {
			return fmt.Errorf("Fleet changed before removal commit")
		}
		if err = os.Rename(temp.Name(), path); err != nil {
			return err
		}
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
