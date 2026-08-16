package cli

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runWaitWithPipes(stdin string, args []string, pollInterval time.Duration) (int, string, string) {
	origStdin := os.Stdin
	origStdout := os.Stdout
	origStderr := os.Stderr
	defer func() {
		os.Stdin = origStdin
		os.Stdout = origStdout
		os.Stderr = origStderr
	}()

	inR, inW, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		panic(err)
	}

	os.Stdin = inR
	os.Stdout = outW
	os.Stderr = errW

	go func() {
		if stdin != "" {
			_, _ = inW.Write([]byte(stdin))
		}
		_ = inW.Close()
	}()

	code := runTaskWaitWithPollInterval(args, pollInterval)

	_ = outW.Close()
	_ = errW.Close()

	outBytes, _ := io.ReadAll(outR)
	errBytes, _ := io.ReadAll(errR)

	return code, strings.TrimSpace(string(outBytes)), strings.TrimSpace(string(errBytes))
}

func TestTaskWaitServerHangFailsFastInternal(t *testing.T) {
	dir, err := os.MkdirTemp("", "agentbus-wait-hang-internal-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	hangSocket := filepath.Join(dir, "hang.sock")
	l, err := net.Listen("unix", hangSocket)
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer l.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task": map[string]any{
				"id":              id,
				"sender_agent_id": "orchestrator",
				"target_agent_id": "worker",
				"status":          "running",
			},
			"messages": []any{},
		})
	})
	mux.HandleFunc("GET /api/v1/tasks/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		// Hang until connection closed
		<-r.Context().Done()
	})

	srv := &http.Server{Handler: mux}
	go func() {
		_ = srv.Serve(l)
	}()
	defer srv.Close()

	waitStart := time.Now()
	code, stdout, errOut := runWaitWithPipes("", []string{
		"task-hang-1",
		"--agent", "orchestrator",
		"--timeout", "10s",
		"--socket", hangSocket,
	}, 50*time.Millisecond)
	waitElapsed := time.Since(waitStart)

	if code != 1 {
		t.Fatalf("expected code 1 on server hang during events poll, got %d (stdout: %s, err: %s)", code, stdout, errOut)
	}
	if stdout != "" {
		t.Fatalf("expected empty stdout on error, got: %s", stdout)
	}
	if !strings.Contains(errOut, "deadline exceeded") && !strings.Contains(errOut, "timeout") && !strings.Contains(errOut, "failed") {
		t.Fatalf("expected deadline/timeout diagnostic in stderr, got: %s", errOut)
	}
	if waitElapsed > 2*time.Second {
		t.Fatalf("wait took too long (%v), expected fast return (<2s << 10s)", waitElapsed)
	}
}
