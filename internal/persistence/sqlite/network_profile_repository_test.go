package sqlite

import (
	"context"
	"testing"
	"time"

	"openagentx/internal/domain"
)

func TestNetworkProfileDraftPublishAndBindingCAS(t *testing.T) {
	r, _ := openTestRepository(t, nil)
	fixture := seedRepository(t, r)
	now := repositoryTestTime
	p := &domain.ProxyProfile{ID: "proxy-main", Version: 1, Status: domain.NetworkProfileDraft, Mode: "only_socks5", Host: "proxy.internal", Port: 28080, ConfigFile: "/etc/openagentx/proxy.conf", SecretRef: "secret-proxy-main", CreatedBy: fixture.ownerPrincipal, CreatedAt: now, UpdatedAt: now}
	if err := r.CreateProxyProfile(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	published, err := r.PublishProxyProfile(context.Background(), p.ID, 1, fixture.ownerPrincipal, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if published.Status != domain.NetworkProfilePublished || published.Version != 2 {
		t.Fatalf("published=%+v", published)
	}
	if _, err := r.PublishProxyProfile(context.Background(), p.ID, 1, fixture.ownerPrincipal, now.Add(2*time.Minute)); err == nil {
		t.Fatal("stale publish unexpectedly succeeded")
	}
	binding := &domain.NetworkBinding{AgentID: fixture.agentID, BackendID: "agy", ProfileID: p.ID, ProfileVersion: published.Version, Version: 1, DesiredStatus: "pending", UpdatedAt: now.Add(3 * time.Minute)}
	if err := r.BindNetworkProfile(context.Background(), binding, 0); err != nil {
		t.Fatal(err)
	}
	bindings, err := r.ListNetworkBindings(context.Background(), fixture.agentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 1 || bindings[0].DesiredStatus != "pending" || bindings[0].ProfileVersion != 2 {
		t.Fatalf("bindings=%+v", bindings)
	}
	if err := r.BindNetworkProfile(context.Background(), &domain.NetworkBinding{AgentID: fixture.agentID, BackendID: "agy", ProfileID: p.ID, ProfileVersion: published.Version, Version: 1, DesiredStatus: "pending", UpdatedAt: now.Add(4 * time.Minute)}, 1); err != nil {
		t.Fatal(err)
	}
	if err := r.BindNetworkProfile(context.Background(), &domain.NetworkBinding{AgentID: fixture.agentID, BackendID: "agy", ProfileID: p.ID, ProfileVersion: published.Version, Version: 1, DesiredStatus: "pending", UpdatedAt: now.Add(5 * time.Minute)}, 1); err == nil {
		t.Fatal("stale binding CAS unexpectedly succeeded")
	}
}

func TestNetworkProfileRejectsCredentialsInHost(t *testing.T) {
	p := domain.ProxyProfile{ID: "proxy", Version: 1, Status: domain.NetworkProfileDraft, Mode: "only_socks5", Host: "user:password@proxy.internal", Port: 1, CreatedBy: "human-owner", CreatedAt: repositoryTestTime, UpdatedAt: repositoryTestTime}
	if err := p.Validate(); err == nil {
		t.Fatal("credential-bearing host accepted")
	}
}
