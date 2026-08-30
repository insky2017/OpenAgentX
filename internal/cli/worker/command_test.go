package workercli

import (
	"context"
	"errors"
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
