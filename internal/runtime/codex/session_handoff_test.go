package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openagentx/internal/domain"
)

// Protocol regression only. The real Codex/Worker handoff is covered by the
// isolated E2E script and must not be inferred from this server fixture.
func TestSessionHandoffDoesNotReimportJoinedThreadOrReceipt(t *testing.T) {
	for _, fresh := range []bool{true, false} {
		t.Run(map[bool]string{true: "fresh", false: "resumed_after_restart"}[fresh], func(t *testing.T) {
			f := newFake(t)
			path := filepath.Join(t.TempDir(), "old-join.md")
			if err := os.WriteFile(path, []byte("DO_NOT_IMPORT_OLD_RECEIPT"), 0600); err != nil {
				t.Fatal(err)
			}
			a := f.adapter(t, func(c *Config) { c.ThreadID, c.HandoffFile = "old-thread", path })
			req := fixtureRequest()
			req.Task.Content = "CURRENT_HANDOFF"
			req.Execution.Spec.Session.ForceNew = fresh
			if !fresh {
				req.Execution.Spec.Session.Mode = domain.SessionModeResume
				req.SessionBinding = &domain.SessionBinding{ProviderSessionID: "native-thread"}
			}
			handle, err := a.StartTurn(context.Background(), req, successfulSink())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := handle.Wait(context.Background()); err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			seen := false
			for i, method := range f.methods {
				if method == "thread/start" || method == "thread/resume" {
					seen = true
					if fresh && method != "thread/start" || !fresh && (method != "thread/resume" || f.params[i]["threadId"] != "native-thread") {
						t.Fatalf("wrong session route: %s %v", method, f.params[i])
					}
				}
				if method == "turn/start" {
					raw, _ := json.Marshal(f.params[i])
					if strings.Contains(string(raw), "DO_NOT_IMPORT_OLD_RECEIPT") || !strings.Contains(string(raw), "CURRENT_HANDOFF") {
						t.Fatalf("wrong handoff: %s", raw)
					}
				}
			}
			if !seen {
				t.Fatal("missing thread request")
			}
		})
	}
}
