import assert from 'node:assert/strict'
import test from 'node:test'

import {
  isCurrentExactBindingApplication,
  isCurrentModeTestPublishable,
} from './network-binding-state.js'

const target = {
  agent_id: 'quote',
  worker_id: 'worker-51',
  generation: 51,
  backend: { backend_id: 'primary' },
}

const directBinding = {
  agent_id: 'quote',
  backend_id: 'primary',
  mode: 'direct',
  policy_version: 9,
  version: 12,
  desired_status: 'applied',
  applied_worker_id: 'worker-51',
  applied_generation: 51,
  applied_mode: 'direct',
  applied_policy_version: 9,
  applied_binding_revision: 12,
}

const currentSuccessfulTest = {
  state: 'succeeded',
  agent_id: 'quote',
  backend_id: 'primary',
  mode: 'direct',
  worker_instance_id: 'worker-51',
  generation: 51,
  binding_revision: 12,
}

test('historical gen 47 receipt is not current for gen 51', () => {
  assert.equal(isCurrentExactBindingApplication(target, {
    ...directBinding,
    applied_generation: 47,
  }), false)
})

test('same generation receipt from another Worker is not current', () => {
  assert.equal(isCurrentExactBindingApplication(target, {
    ...directBinding,
    applied_worker_id: 'worker-other',
  }), false)
})

test('receipt revision and mode-specific policy must match the current binding', () => {
  assert.equal(isCurrentExactBindingApplication(target, {
    ...directBinding,
    applied_binding_revision: 11,
  }), false)
  assert.equal(isCurrentExactBindingApplication(target, {
    ...directBinding,
    applied_policy_version: 8,
  }), false)
  assert.equal(isCurrentExactBindingApplication(target, {
    ...directBinding,
    applied_policy_version: undefined,
  }), false)
})

test('pending and failed bindings do not claim current application', () => {
  assert.equal(isCurrentExactBindingApplication(target, {
    ...directBinding,
    desired_status: 'pending',
  }), false)
  assert.equal(isCurrentExactBindingApplication(target, {
    ...directBinding,
    desired_status: 'failed',
  }), false)
})

test('a current successful mode test remains publishable with its CAS revision', () => {
  assert.equal(isCurrentModeTestPublishable(target, directBinding, 'direct', currentSuccessfulTest), true)
  assert.equal(isCurrentModeTestPublishable(target, directBinding, 'direct', {
    ...currentSuccessfulTest,
    binding_revision: 11,
  }), false)
  assert.equal(isCurrentModeTestPublishable(target, directBinding, 'direct', {
    ...currentSuccessfulTest,
    state: 'pending',
  }), false)
  assert.equal(isCurrentModeTestPublishable(target, directBinding, 'direct', {
    ...currentSuccessfulTest,
    state: 'failed',
  }), false)
  assert.equal(isCurrentModeTestPublishable(target, directBinding, 'direct', {
    ...currentSuccessfulTest,
    state: 'stale',
  }), false)
  assert.equal(isCurrentModeTestPublishable(target, directBinding, 'direct', {
    ...currentSuccessfulTest,
    worker_instance_id: 'worker-other',
  }), false)
  assert.equal(isCurrentModeTestPublishable(target, directBinding, 'direct', null), false)
})

test('only a complete exact receipt is current applied', () => {
  assert.equal(isCurrentExactBindingApplication(target, directBinding), true)
  assert.equal(isCurrentExactBindingApplication(target, {
    ...directBinding,
    applied_mode: undefined,
  }), false)
  assert.equal(isCurrentExactBindingApplication(target, {
    ...directBinding,
    agent_id: 'other-agent',
  }), false)
})

test('named profile application requires the exact profile receipt', () => {
  const profileBinding = {
    ...directBinding,
    mode: 'named_profile',
    policy_version: undefined,
    profile_id: 'proxy-a',
    profile_version: 4,
    applied_mode: 'named_profile',
    applied_policy_version: undefined,
    applied_profile_id: 'proxy-a',
    applied_profile_version: 4,
  }
  assert.equal(isCurrentExactBindingApplication(target, profileBinding), true)
  assert.equal(isCurrentExactBindingApplication(target, {
    ...profileBinding,
    applied_profile_version: 3,
  }), false)
})
