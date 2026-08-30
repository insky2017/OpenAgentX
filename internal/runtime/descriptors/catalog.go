package descriptors

import (
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

func CodexACP() openruntime.AdapterDescriptor {
	return descriptor("codex-acp", "codex", []string{"default"})
}
func ClaudeACP() openruntime.AdapterDescriptor {
	return descriptor("claude-acp", "claude", []string{"default"})
}
func OpenCodeACP() openruntime.AdapterDescriptor {
	return descriptor("opencode-acp", "opencode", []string{"default"})
}

func descriptor(adapterID, backendType string, models []string) openruntime.AdapterDescriptor {
	return openruntime.AdapterDescriptor{AdapterID: adapterID, BackendType: backendType, Version: "1", LaunchProtocol: "acp", Models: models,
		ReasoningModes: []domain.ReasoningMode{domain.ReasoningBackendDefault, domain.ReasoningEffort}, SessionModes: []domain.SessionMode{domain.SessionModeNew, domain.SessionModeResume},
		Steer: openruntime.SteerNative, Approval: openruntime.ApprovalNative, Cancel: openruntime.CancelNative, Streams: true, MaxConcurrency: 1}
}
