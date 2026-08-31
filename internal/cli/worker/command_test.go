package workercli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestOpenAgentXWorkerRunExitCodes(t *testing.T) {
	if code := ExecuteOpenAgentX([]string{"worker", "run", "--config", "agent.yaml"},
		func(context.Context, string) error { return nil }); code != 0 {
		t.Fatalf("controlled Worker stop exit code=%d want=0", code)
	}
	if code := ExecuteOpenAgentX([]string{"worker", "run", "--config", "agent.yaml"},
		func(context.Context, string) error { return errors.New("lease lost") }); code == 0 {
		t.Fatal("abnormal Worker stop must return nonzero")
	}
	if code := ExecuteOpenAgentX([]string{"worker", "run"}, func(context.Context, string) error { return nil }); code == 0 {
		t.Fatal("missing Worker config must return nonzero")
	}
}

func TestAGYConfigFromOptions(t *testing.T) {
	configDir := t.TempDir()
	workingDir := filepath.Join(configDir, "workspace")
	if err := os.Mkdir(workingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	config, err := agyConfigFromOptions(map[string]any{
		"binary":      "agy-graft",
		"models":      []any{"model-one", "model-two"},
		"working_dir": "workspace",
	}, configDir)
	if err != nil || config.Binary != "agy-graft" || config.WorkingDir != workingDir || !reflect.DeepEqual(config.Models, []string{"model-one", "model-two"}) {
		t.Fatalf("config=%+v err=%v", config, err)
	}
	for _, options := range []map[string]any{
		{"binary": " "},
		{"working_dir": " "},
		{"working_dir": "missing"},
		{"models": []any{}},
		{"models": []any{"model-one", "model-one"}},
		{"models": []any{"model-one", 42}},
		{"unexpected": true},
	} {
		if _, err := agyConfigFromOptions(options, configDir); err == nil {
			t.Fatalf("options=%#v unexpectedly accepted", options)
		}
	}
}
