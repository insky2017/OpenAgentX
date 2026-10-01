package fleet

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func TestAddAgentConcurrentPreservesEveryEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile", "fleet.yaml")
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("agent-%d", i)
			errs <- AddAgent(path, Agent{AgentID: id, WorkerConfig: "/workers/" + id + ".yaml", Enabled: true})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	m, err := LoadFile(path)
	if err != nil || len(m.Agents) != 8 {
		t.Fatalf("manifest=%+v err=%v", m, err)
	}
	existing := m.Agents[0]
	existing.Enabled = false
	if err = AddAgent(path, existing); err != nil {
		t.Fatal(err)
	}
	m, _ = LoadFile(path)
	if !m.Agents[0].Enabled {
		t.Fatal("re-add changed existing enabled setting")
	}
}
