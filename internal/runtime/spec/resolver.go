package spec

import (
	"context"
	"time"

	"agentbus/internal/domain"
	openruntime "agentbus/internal/runtime"
)

type Policy struct {
	AllowedAdapters []string
	AllowedBackends []string
	AllowedModels   []string
	MaxTimeout      time.Duration
	MaxTokens       int64
}

// Resolve applies the explicit task/turn request over defaults and enforces
// policy hard limits. Sources are recorded per field for RunAttempt audit.
func Resolve(ctx context.Context, requested domain.ExecutionSpec, defaults domain.ExecutionSpec,
	registration openruntime.BackendRegistration, policy Policy) (domain.ResolvedExecutionSpec, error) {
	_ = ctx
	resolved := defaults
	sources := map[string]string{}
	if resolved.AdapterID == "" {
		resolved.AdapterID = registration.Descriptor.AdapterID
	}
	if resolved.BackendID == "" {
		resolved.BackendID = registration.BackendID
	}
	if resolved.Model == "" && len(registration.Descriptor.Models) > 0 {
		resolved.Model = registration.Descriptor.Models[0]
	}
	if requested.AdapterID != "" {
		resolved.AdapterID = requested.AdapterID
		sources["adapter"] = "task_override"
	} else {
		sources["adapter"] = "profile_default"
	}
	if requested.BackendID != "" {
		resolved.BackendID = requested.BackendID
		sources["backend"] = "task_override"
	} else {
		sources["backend"] = "profile_default"
	}
	if requested.Model != "" {
		resolved.Model = requested.Model
		sources["model"] = "task_override"
	} else {
		sources["model"] = "profile_default"
	}
	if requested.Reasoning.Mode != "" {
		resolved.Reasoning = requested.Reasoning
		sources["reasoning"] = "task_override"
	}
	if requested.Session.Mode != "" {
		resolved.Session = requested.Session
		sources["session"] = "task_override"
	}
	if requested.ApprovalPolicy != "" {
		resolved.ApprovalPolicy = requested.ApprovalPolicy
		sources["approval_policy"] = "task_override"
	}
	if requested.Sandbox != "" {
		resolved.Sandbox = requested.Sandbox
		sources["sandbox"] = "task_override"
	}
	if requested.Timeout > 0 {
		resolved.Timeout = requested.Timeout
		sources["timeout"] = "task_override"
	}
	if requested.Budget.MaxTokens > 0 {
		resolved.Budget = requested.Budget
		sources["budget"] = "task_override"
	}
	if len(requested.BackendOptions) > 0 {
		resolved.BackendOptions = requested.BackendOptions
		sources["backend_options"] = "task_override"
	}
	if resolved.Timeout <= 0 {
		resolved.Timeout = 30 * time.Minute
		sources["timeout"] = "resolver_default"
	}
	if resolved.Reasoning.Mode == "" {
		resolved.Reasoning.Mode = domain.ReasoningBackendDefault
		sources["reasoning"] = "backend_default"
	}
	if resolved.Session.Mode == "" {
		resolved.Session.Mode = domain.SessionModeNew
		sources["session"] = "backend_default"
	}
	if err := resolved.ValidateShape(); err != nil {
		return domain.ResolvedExecutionSpec{}, err
	}
	if resolved.AdapterID != registration.Descriptor.AdapterID || resolved.BackendID != registration.BackendID {
		return domain.ResolvedExecutionSpec{}, domain.ErrUnsupportedCapability
	}
	if !contains(policy.AllowedAdapters, resolved.AdapterID) && len(policy.AllowedAdapters) > 0 {
		return domain.ResolvedExecutionSpec{}, domain.ErrForbidden("Adapter is outside policy")
	}
	if !contains(policy.AllowedBackends, resolved.BackendID) && len(policy.AllowedBackends) > 0 {
		return domain.ResolvedExecutionSpec{}, domain.ErrForbidden("Backend is outside policy")
	}
	if !contains(registration.Descriptor.Models, resolved.Model) || (len(policy.AllowedModels) > 0 && !contains(policy.AllowedModels, resolved.Model)) {
		return domain.ResolvedExecutionSpec{}, domain.ErrUnsupportedCapability
	}
	if !containsReasoning(registration.Descriptor.ReasoningModes, resolved.Reasoning.Mode) || !containsSession(registration.Descriptor.SessionModes, resolved.Session.Mode) {
		return domain.ResolvedExecutionSpec{}, domain.ErrUnsupportedCapability
	}
	if policy.MaxTimeout > 0 && resolved.Timeout > policy.MaxTimeout {
		return domain.ResolvedExecutionSpec{}, domain.ErrForbidden("execution timeout exceeds policy")
	}
	if policy.MaxTokens > 0 && resolved.Budget.MaxTokens > policy.MaxTokens {
		return domain.ResolvedExecutionSpec{}, domain.ErrForbidden("execution budget exceeds policy")
	}
	return domain.ResolvedExecutionSpec{Version: 1, Spec: resolved, Sources: sources}, nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsReasoning(values []domain.ReasoningMode, target domain.ReasoningMode) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsSession(values []domain.SessionMode, target domain.SessionMode) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
