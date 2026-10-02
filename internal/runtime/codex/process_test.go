package codex

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestOwnedTreeStopIncludesSetsidButNotUnrelatedProcess(t *testing.T) {
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skip("setsid unavailable")
	}
	workspace := t.TempDir()
	pidFile := filepath.Join(workspace, "child.pid")
	unrelated := exec.Command("sleep", "30")
	if err := unrelated.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { unrelated.Process.Kill(); unrelated.Wait() }()
	other, _, err := readIdentity(unrelated.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	root := exec.Command("sh", "-c", `setsid sh -c 'echo $$ > "$1"; sleep 30' sh "$1" & wait`, "sh", pidFile)
	if err = root.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { root.Process.Kill(); root.Wait() }()
	ref, _, err := readIdentity(root.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	tracker := newProcessTracker(ref)
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err = os.Stat(pidFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("setsid child unavailable")
		}
		time.Sleep(10 * time.Millisecond)
	}
	tracker.capture()
	refs, err := tracker.stop()
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) < 2 {
		t.Fatalf("missed descendant: %v", refs)
	}
	for _, process := range refs {
		if identityAlive(process) {
			t.Fatalf("owned process still alive: %+v", process)
		}
	}
	if !identityAlive(other) {
		t.Fatal("unrelated process was killed")
	}
}
