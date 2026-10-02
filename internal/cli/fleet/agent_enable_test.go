package fleet

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	fleetmodel "openagentx/internal/fleet"
)

func TestPreparedEnableAtomicRollbackAndPreservesOtherAgents(t *testing.T) {
	f := newFleetFixture(t)
	writeManifest(t, f, "quote", "risk")
	manifest, err := fleetmodel.LoadFile(f.manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Agents[0].Enabled = false
	bytes, err := fleetmodel.Encode(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(f.manifest, bytes, 0600); err != nil {
		t.Fatal(err)
	}
	o := agentOptions{id: "quote", paths: fleetPaths{workerDir: f.workerDir, manifest: f.manifest}}
	if err = writeJoinReceipt(o); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("injected activation failure")
	if err = enablePreparedAgent(o, Dependencies{BeforeAtomicRename: func(string) error { return failure }}); !errors.Is(err, failure) {
		t.Fatalf("error=%v", err)
	}
	after, err := os.ReadFile(f.manifest)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(bytes) {
		t.Fatal("failed activation mutated manifest")
	}
	temporary, err := filepath.Glob(filepath.Join(filepath.Dir(f.manifest), ".fleet-enable-*"))
	if err != nil || len(temporary) != 0 {
		t.Fatalf("temporary files=%v err=%v", temporary, err)
	}
	if err = enablePreparedAgent(o, Dependencies{}); err != nil {
		t.Fatal(err)
	}
	enabled, err := fleetmodel.LoadFile(f.manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !enabled.Agents[0].Enabled {
		t.Fatal("successful activation did not persist enabled")
	}
	if !reflect.DeepEqual(enabled.Agents[1], manifest.Agents[1]) {
		t.Fatal("activation changed unrelated Agent")
	}
	if err = enablePreparedAgent(o, Dependencies{BeforeAtomicRename: func(string) error { t.Fatal("already enabled should be idempotent"); return nil }}); err != nil {
		t.Fatal(err)
	}
}
