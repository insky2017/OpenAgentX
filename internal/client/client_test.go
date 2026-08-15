package client_test

import (
	"os"
	"path/filepath"
	"testing"

	"agentbus/internal/client"
)

func TestPathResolution(t *testing.T) {
	dir := client.DefaultAgentBusDir()
	if !filepath.IsAbs(dir) {
		t.Fatalf("expected DefaultAgentBusDir to return absolute path, got %s", dir)
	}

	sock := client.DefaultSocketPath()
	if !filepath.IsAbs(sock) || filepath.Base(sock) != "agentbus.sock" {
		t.Fatalf("expected DefaultSocketPath to return absolute path to agentbus.sock, got %s", sock)
	}

	db := client.DefaultDBPath()
	if !filepath.IsAbs(db) || filepath.Base(db) != "agentbus.db" {
		t.Fatalf("expected DefaultDBPath to return absolute path to agentbus.db, got %s", db)
	}

	// Test explicit path
	custom := client.ResolveSocketPath("/custom/path/bus.sock")
	if custom != "/custom/path/bus.sock" {
		t.Fatalf("expected explicit path preserved, got %s", custom)
	}

	// Test environment variable
	os.Setenv("AGENTBUS_SOCKET", "/env/socket.sock")
	defer os.Unsetenv("AGENTBUS_SOCKET")
	fromEnv := client.ResolveSocketPath("")
	if fromEnv != "/env/socket.sock" {
		t.Fatalf("expected AGENTBUS_SOCKET resolution, got %s", fromEnv)
	}
}
