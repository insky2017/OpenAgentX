package unixhttp

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestServerRefusesToRemoveNonSocketPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openagentx.sock")
	if err := os.WriteFile(path, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(path, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(context.Background()); err == nil {
		t.Fatal("expected a non-socket collision to fail")
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "preserve" {
		t.Fatalf("non-socket path was modified: content=%q err=%v", content, err)
	}
}

func TestServerCleanupDoesNotRemoveReplacementPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openagentx.sock")
	server, err := NewServer(path, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- server.Start(ctx) }()
	waitForUnixSocket(t, path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop")
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "replacement" {
		t.Fatalf("replacement path was removed: content=%q err=%v", content, err)
	}
}

func TestSecondServerCannotTakeActiveSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openagentx.sock")
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	first, err := NewServer(path, handler, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- first.Start(ctx) }()
	waitForUnixSocket(t, path)
	second, err := NewServer(path, handler, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Start(context.Background()); err == nil {
		t.Fatal("second server unexpectedly replaced active socket")
	}
	cancel()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func waitForUnixSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Unix socket was not created: %s", path)
}
