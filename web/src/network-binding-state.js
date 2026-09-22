const hasText = (value) => typeof value === 'string' && value.length > 0

const isPositiveInteger = (value) => Number.isInteger(value) && value > 0

export const isCurrentExactBindingApplication = (target, binding) => {
  if (!target || !binding || !target.backend) return false
  if (!hasText(target.agent_id) || !hasText(target.backend.backend_id) || !hasText(target.worker_id)
    || !isPositiveInteger(target.generation)) return false
  if (binding.agent_id !== target.agent_id || binding.backend_id !== target.backend.backend_id
    || !isPositiveInteger(binding.version) || binding.desired_status !== 'applied'
    || binding.applied_worker_id !== target.worker_id || binding.applied_generation !== target.generation
    || binding.applied_binding_revision !== binding.version || binding.applied_mode !== binding.mode) return false

  if (binding.mode === 'named_profile') {
    return hasText(binding.profile_id)
      && isPositiveInteger(binding.profile_version)
      && binding.applied_profile_id === binding.profile_id
      && binding.applied_profile_version === binding.profile_version
  }

  return (binding.mode === 'inherit' || binding.mode === 'direct')
    && isPositiveInteger(binding.policy_version)
    && binding.applied_policy_version === binding.policy_version
}

export const isCurrentModeTestPublishable = (target, binding, mode, test) => {
  const bindingRevision = binding?.version || 0
  return Boolean(target?.backend
    && test?.state === 'succeeded'
    && test.agent_id === target.agent_id
    && test.backend_id === target.backend.backend_id
    && test.mode === mode
    && test.worker_instance_id === target.worker_id
    && test.generation === target.generation
    && test.binding_revision === bindingRevision)
}
