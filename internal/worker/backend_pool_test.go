package worker

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"openagentx/internal/domain"
	"openagentx/internal/runtime/agy"
)

func TestBackendPoolAppliesPublishedNetworkBinding(t *testing.T) {
	profilePath := filepath.Join(t.TempDir(), "proxy.conf")
	if err := os.WriteFile(profilePath, []byte("proxy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	adapter := agy.NewAdapterForTest(agy.Config{Binary: "agy-graft", Models: []string{"model"}})
	var err error
	pool, err := NewBackendPool([]RuntimeBackend{{ID: "backend", Adapter: adapter}})
	if err != nil {
		t.Fatal(err)
	}
	binding := domain.NetworkBinding{AgentID: "agent", BackendID: "backend", ProfileID: "proxy", ProfileVersion: 2, Version: 1, DesiredStatus: "pending", Profile: &domain.ProxyProfile{ID: "proxy", Version: 2, Status: domain.NetworkProfilePublished, Mode: "only_socks5", Host: "proxy.internal", Port: 28080, ConfigFile: profilePath, CreatedBy: "owner", CreatedAt: nowForTest(), UpdatedAt: nowForTest()}, UpdatedAt: nowForTest()}
	if err := pool.ApplyNetworkBinding(binding); err != nil {
		t.Fatal(err)
	}
	pool.mu.RLock()
	applied := pool.backends["backend"].Network
	pool.mu.RUnlock()
	if applied.ProfileID != "proxy" || applied.ProfileVersion != 2 {
		t.Fatalf("applied=%+v", applied)
	}
}

func nowForTest() time.Time { return time.Now().UTC() }
