package conformance

import (
	"context"
	"testing"
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

// ValidateDescriptor is shared by AGY, ACP and future adapters. It keeps
// capability declarations honest before an adapter is admitted to a Worker.
func ValidateDescriptor(descriptor openruntime.AdapterDescriptor) error {
	return descriptor.Validate()
}

// Run executes the non-process portion of the adapter contract. Process-backed
// smoke tests may provide a real TurnRequest separately; this helper only
// asserts descriptor/ExecutionSpec consistency and is deterministic in CI.
func Run(t *testing.T, adapter openruntime.AgentRuntimeAdapter, spec domain.ExecutionSpec) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	descriptor, err := adapter.Descriptor(ctx)
	if err != nil {
		t.Fatalf("descriptor: %v", err)
	}
	if err := ValidateDescriptor(descriptor); err != nil {
		t.Fatalf("descriptor validation: %v", err)
	}
	if err := adapter.Validate(ctx, spec); err != nil {
		t.Fatalf("execution validation: %v", err)
	}
}
