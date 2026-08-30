package admincli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	openagentsqlite "openagentx/internal/persistence/sqlite"
)

func TestInitAndAgentApplyUsePublicCLIWithoutDuplicateEvents(t *testing.T) {
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "openagentx.db")
	rolePath := filepath.Join(directory, "ROLE.md")
	if err := os.WriteFile(rolePath, []byte("# Test Agent\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	definitionPath := filepath.Join(directory, "identity.yaml")
	definition := `version: 1
agent_id: test-fake-agent
principal_id: agent-test-fake-agent
organization_id: default
display_name: Test Fake Agent
profile:
  instructions_path: ROLE.md
  workspace_root: .
  capabilities: [control-plane-testing]
`
	if err := os.WriteFile(definitionPath, []byte(definition), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	id := 0
	passwords := []string{"correct horse battery staple", "correct horse battery staple"}
	deps := Dependencies{Out: &out, Err: &stderr, Now: func() time.Time { return time.Date(2026, 8, 30, 18, 0, 0, 0, time.UTC) }, NewID: func(prefix string) string {
		id++
		return fmt.Sprintf("%s-%d", prefix, id)
	}, ReadPassword: func(string) (string, error) {
		value := passwords[0]
		passwords = passwords[1:]
		return value, nil
	}}
	if code := ExecuteInit([]string{"--db", databasePath}, deps); code != 0 {
		t.Fatalf("init code=%d stderr=%s", code, stderr.String())
	}
	deps.ReadPassword = func(string) (string, error) { return "", fmt.Errorf("must not prompt on idempotent init") }
	if code := ExecuteInit([]string{"--db", databasePath}, deps); code != 0 {
		t.Fatalf("repeat init code=%d stderr=%s", code, stderr.String())
	}
	deps.ReadPassword = func(string) (string, error) { return "correct horse battery staple", nil }
	if code := ExecuteAgent([]string{"apply", "--db", databasePath, "--file", definitionPath}, deps); code != 0 {
		t.Fatalf("agent apply code=%d stderr=%s", code, stderr.String())
	}
	if code := ExecuteAgent([]string{"apply", "--db", databasePath, "--file", definitionPath}, deps); code != 0 {
		t.Fatalf("repeat agent apply code=%d stderr=%s", code, stderr.String())
	}

	repository, err := openagentsqlite.Open(context.Background(), databasePath, openagentsqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	agent, profile, err := repository.GetAgent(context.Background(), "test-fake-agent")
	if err != nil || agent.OrganizationID != "default" || len(profile.Capabilities) != 1 {
		t.Fatalf("Agent=%+v profile=%+v err=%v", agent, profile, err)
	}
	events, err := repository.ListJournal(context.Background(), 0, 100)
	if err != nil || len(events) != 6 {
		t.Fatalf("events=%d err=%v", len(events), err)
	}
	if strings.Contains(out.String(), "correct horse") || strings.Contains(stderr.String(), "correct horse") {
		t.Fatal("password leaked to CLI output")
	}
}

func TestAgentApplyRejectsWrongOwnerPassword(t *testing.T) {
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "openagentx.db")
	if err := os.WriteFile(filepath.Join(directory, "ROLE.md"), []byte("# Role\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "identity.yaml"), []byte(`version: 1
agent_id: denied-agent
principal_id: agent-denied
organization_id: default
display_name: Denied Agent
profile:
  instructions_path: ROLE.md
  workspace_root: .
  capabilities: [testing]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	passwords := []string{"correct horse battery staple", "correct horse battery staple"}
	deps := Dependencies{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, ReadPassword: func(string) (string, error) {
		value := passwords[0]
		passwords = passwords[1:]
		return value, nil
	}}
	if code := ExecuteInit([]string{"--db", databasePath}, deps); code != 0 {
		t.Fatalf("init code=%d", code)
	}
	deps.ReadPassword = func(string) (string, error) { return "wrong password", nil }
	if code := ExecuteAgent([]string{"apply", "--db", databasePath, "--file", filepath.Join(directory, "identity.yaml")}, deps); code == 0 {
		t.Fatal("Agent apply accepted wrong owner password")
	}
	repository, err := openagentsqlite.Open(context.Background(), databasePath, openagentsqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	if _, _, err := repository.GetAgent(context.Background(), "denied-agent"); err == nil {
		t.Fatal("unauthorized Agent was created")
	}
}
