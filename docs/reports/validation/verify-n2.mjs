#!/usr/bin/env node
// N2 validation harness. It is deliberately inert until a frozen build is
// available and OAX_N2_ALLOW_EXECUTION=1 is set for the isolated fixture.
import { lstat, mkdir, readFile, writeFile } from 'node:fs/promises'
import { dirname, isAbsolute, join, resolve } from 'node:path'
import { request as httpRequest } from 'node:http'
import { request as httpsRequest } from 'node:https'
import WebSocket from 'ws'

const command = process.argv[2]
const fixtureDir = process.env.OAX_N2_FIXTURE_DIR
const execute = process.env.OAX_N2_ALLOW_EXECUTION === '1'
const baseURL = process.env.OAX_N2_BASE_URL
const username = process.env.OAX_N2_USER
const loginPasswordFile = process.env.OAX_N2_PASSWORD_FILE
const networkSecretFile = process.env.OAX_N2_NETWORK_SECRET_FILE
const socks5Username = 'n2-fixture-socks-user'
const target = {
  agentID: process.env.OAX_N2_AGENT_ID,
  workerInstanceID: process.env.OAX_N2_WORKER_INSTANCE_ID,
  generation: Number(process.env.OAX_N2_WORKER_GENERATION),
  backendID: process.env.OAX_N2_BACKEND_ID,
}

const probeLayers = [
  'configuration', 'secret', 'endpoint', 'direct_rules', 'runtime_health',
  'network_effect', 'model_call',
]

function assert(condition, message) {
  if (!condition) throw new Error(message)
}

function commandMeta(prefix, expectedVersion) {
  const meta = { idempotency_key: `${prefix}-${crypto.randomUUID()}` }
  if (expectedVersion !== undefined) meta.expected_version = expectedVersion
  return meta
}

function safeFixturePath(path) {
  assert(path && isAbsolute(path), 'OAX_N2_FIXTURE_DIR must be an absolute path')
  const resolved = resolve(path)
  assert(/^openagentx-n2-[A-Za-z0-9._-]+$/.test(resolved.split('/').at(-1)), 'fixture directory basename must start with openagentx-n2-')
  return resolved
}

async function requirePrivateFile(path, label) {
  assert(path && isAbsolute(path), `${label} must be an absolute file path`)
  const info = await lstat(path)
  assert(!info.isSymbolicLink(), `${label} must not be a symbolic link`)
  assert(info.isFile(), `${label} must be a regular file`)
  assert((info.mode & 0o777) === 0o600, `${label} must have mode 0600`)
  const parent = await lstat(dirname(path))
  assert(!parent.isSymbolicLink() && parent.isDirectory() && (parent.mode & 0o777) === 0o700, `${label} parent directory must have mode 0700 and must not be a symbolic link`)
  const value = (await readFile(path, 'utf8')).trim()
  assert(value, `${label} must not be empty`)
  return value
}

async function prepare() {
  const root = safeFixturePath(fixtureDir)
  await mkdir(root, { mode: 0o700 })
  await mkdir(join(root, 'workspace'), { mode: 0o700 })
  await mkdir(join(root, 'configs'), { mode: 0o700 })
  await writeFile(join(root, 'ROLE.md'), 'Isolated N2 validation fixture.\n', { mode: 0o600 })
  await writeFile(join(root, 'n2-agent.yaml'), [
    'version: 1',
    'agent_id: n2-fixture-agent',
    'principal_id: n2-fixture-principal',
    'organization_id: n2-fixture-org',
    'display_name: N2 Fixture Agent',
    'profile:',
    '  instructions_path: ROLE.md',
    '  workspace_root: workspace',
    '  capabilities:',
    '    - coding',
    '',
  ].join('\n'), { mode: 0o600 })
  // This sentinel is a test-only secret. Its value is never printed or stored
  // outside this 0700 fixture directory.
  await writeFile(join(root, 'network-secret'), `n2-${crypto.randomUUID()}-${crypto.randomUUID()}`, { mode: 0o600 })
  console.log(JSON.stringify({ prepared: true, fixture_dir_private: true, secret_file_private: true }))
}

function requireExecutionEnvironment() {
  assert(execute, 'set OAX_N2_ALLOW_EXECUTION=1 only after N2 freeze authorization')
  assert(baseURL, 'OAX_N2_BASE_URL is required')
  let endpoint
  try { endpoint = new URL(baseURL) } catch { throw new Error('OAX_N2_BASE_URL must be an http(s) URL') }
  const loopback = endpoint.hostname === '::1' || endpoint.hostname === '[::1]' || /^127(?:\.\d{1,3}){3}$/.test(endpoint.hostname)
  assert((endpoint.protocol === 'http:' || endpoint.protocol === 'https:') && loopback, 'OAX_N2_BASE_URL must use an explicit loopback http(s) origin')
  assert(!endpoint.username && !endpoint.password && endpoint.pathname === '/' && !endpoint.search && !endpoint.hash, 'OAX_N2_BASE_URL must not contain credentials, a path, query, or fragment')
  assert(username, 'OAX_N2_USER is required')
  for (const [name, value] of Object.entries(target)) assert(value && value !== 'NaN', `OAX_N2_${name.replace(/[A-Z]/g, (c) => `_${c}`).toUpperCase()} is required`)
}

async function request(method, path, { headers = {}, body, session, allow = [200], expectJSON = true } = {}) {
  const url = new URL(path, baseURL)
  const encoded = body === undefined ? undefined : JSON.stringify(body)
  const transport = url.protocol === 'https:' ? httpsRequest : httpRequest
  const response = await new Promise((resolveResponse, reject) => {
    const req = transport(url, {
      method,
      headers: {
        accept: 'application/json',
        ...(encoded ? { 'content-type': 'application/json', 'content-length': Buffer.byteLength(encoded) } : {}),
        ...(session ? { cookie: session.cookie, 'x-csrf-token': session.csrf } : {}),
        ...headers,
      },
      timeout: 20_000,
    }, (res) => {
      let text = ''
      let size = 0
      res.setEncoding('utf8')
      res.on('data', (chunk) => {
        size += Buffer.byteLength(chunk)
        if (size > 256 * 1024) {
          req.destroy(new Error('response exceeded validation limit'))
          return
        }
        text += chunk
      })
      res.on('end', () => resolveResponse({ status: res.statusCode || 0, headers: res.headers, text }))
    })
    req.once('error', reject)
    req.once('timeout', () => req.destroy(new Error(`${method} ${path} timed out`)))
    if (encoded) req.write(encoded)
    req.end()
  })
  if (!allow.includes(response.status)) throw new Error(`${method} ${path} returned HTTP ${response.status}`)
  let json = null
  if (expectJSON) {
    assert(response.text, `${method} ${path} returned an empty JSON response`)
    try { json = JSON.parse(response.text) } catch { throw new Error(`${method} ${path} returned non-JSON`) }
  }
  return { ...response, json }
}

async function login() {
  const password = await requirePrivateFile(loginPasswordFile, 'OAX_N2_PASSWORD_FILE')
  const result = await request('POST', '/api/auth/v1/login', { body: { username, password }, allow: [200] })
  const setCookies = typeof result.headers['set-cookie'] === 'string' ? [result.headers['set-cookie']] : (result.headers['set-cookie'] || [])
  const cookie = setCookies.find(Boolean)?.split(';', 1)[0]
  assert(cookie && result.json?.csrf_token, 'authenticated login did not return session state')
  return { cookie, csrf: result.json.csrf_token }
}

async function control(path, session, body) {
  assert(body.meta?.idempotency_key, `Control request ${path} needs an idempotency key`)
  return request('POST', path, { session, body, headers: { 'idempotency-key': body.meta.idempotency_key }, allow: [200, 201] })
}

async function overview(session) {
  const result = await request('GET', `/api/observe/v1/network-profiles?agent_id=${encodeURIComponent(target.agentID)}`, { session })
  assert(Array.isArray(result.json?.profiles) && Array.isArray(result.json?.versions) && Array.isArray(result.json?.tests) && Array.isArray(result.json?.mode_tests) && Array.isArray(result.json?.bindings) && Array.isArray(result.json?.active_runs), 'network overview shape is invalid')
  return result.json
}

async function executionOptions(session) {
  const result = await request('GET', '/api/observe/v1/execution-options', { session })
  assert(Array.isArray(result.json?.backends), 'execution options shape is invalid')
  return result.json.backends
}

function profileIn(data, profileID) {
  const profile = data.profiles.find((item) => item.profile_id === profileID)
  assert(profile, `profile ${profileID} is absent from overview`)
  return profile
}

function bindingIn(data, profileID) {
  const binding = data.bindings.find((item) => item.agent_id === target.agentID && item.backend_id === target.backendID && item.profile_id === profileID)
  assert(binding, 'expected network binding is absent from overview')
  return binding
}

function targetBinding(data) {
  return data.bindings.find((item) => item.agent_id === target.agentID && item.backend_id === target.backendID) || null
}

function bindingState(binding) {
  if (!binding) return null
  return JSON.stringify({
    mode: binding.mode, profile_id: binding.profile_id || '', profile_version: binding.profile_version || 0,
    policy_version: binding.policy_version || 0, test_id: binding.test_id || '', manifest_digest: binding.manifest_digest || '',
    version: binding.version, desired_status: binding.desired_status, applied_worker_id: binding.applied_worker_id || '',
    applied_generation: binding.applied_generation || 0, applied_mode: binding.applied_mode || '',
    applied_profile_id: binding.applied_profile_id || '', applied_profile_version: binding.applied_profile_version || 0,
    applied_policy_version: binding.applied_policy_version || 0, applied_binding_revision: binding.applied_binding_revision || 0,
  })
}

function targetHealth(options) {
  const option = options.find((item) => item.worker_id === target.workerInstanceID && item.generation === target.generation && item.agent_id === target.agentID && item.backend?.backend_id === target.backendID)
  assert(option, 'target Worker Backend is absent from execution options')
  assert(typeof option.backend.health === 'string' && option.backend.health, 'target Worker Backend has no explicit health state')
  return option.backend.health
}

function containsValue(value, sentinel) {
  if (typeof value === 'string') return value.includes(sentinel)
  if (Array.isArray(value)) return value.some((item) => containsValue(item, sentinel))
  if (value && typeof value === 'object') return Object.values(value).some((item) => containsValue(item, sentinel))
  return false
}

function successfulProbeResults(test, mode, description) {
  assert(Array.isArray(test?.probe_results) && test.probe_results.length === probeLayers.length, `${description} did not return every fixed probe layer`)
  const byLayer = new Map()
  for (const result of test.probe_results) {
    assert(probeLayers.includes(result?.layer) && !byLayer.has(result.layer), `${description} has an unknown or duplicate probe layer`)
    assert(['passed', 'failed', 'not_applicable', 'not_verified'].includes(result.state), `${description} has an unsupported probe state`)
    byLayer.set(result.layer, result)
  }
  assert(test.state === 'succeeded', `${description} must be succeeded before its probe results can pass N2`)
  for (const layer of ['configuration', 'secret', 'endpoint', 'direct_rules', 'runtime_health']) {
    const result = byLayer.get(layer)
    assert(result, `${description} omitted probe layer ${layer}`)
    const inheritedConfiguration = mode === 'inherit' && (layer === 'secret' || layer === 'endpoint')
    if (inheritedConfiguration) {
      assert(result.state === 'not_verified' && result.diagnostic_code === 'INHERITED_CONFIGURATION_UNVERIFIED', `${description} ${layer} must honestly report inherited configuration as unverified`)
      continue
    }
    assert(result.state === 'passed' || result.state === 'not_applicable', `${description} ${layer} is not applicable success evidence (${result.state})`)
    assert(!result.diagnostic_code, `${description} ${layer} successful result must not include a diagnostic`)
  }
  for (const layer of ['network_effect', 'model_call']) {
    const result = byLayer.get(layer)
    assert(result.state === 'not_verified' && result.diagnostic_code === 'NOT_VERIFIED', `${description} ${layer} must remain explicitly unverified for N2 configuration testing`)
  }
  return byLayer
}

function assertModeDesired(binding, test, expectedRevision) {
  assert(binding, 'published mode binding disappeared from overview')
  assert((binding.desired_status === 'pending' || binding.desired_status === 'applied') && binding.mode === test.mode && binding.policy_version === test.policy_version && binding.test_id === test.test_id && binding.manifest_digest === test.manifest_digest && binding.version === expectedRevision, 'mode binding does not match the publish receipt and tested policy')
}

function assertModeApplied(binding, test) {
  assert(binding.desired_status === 'applied', 'mode binding is not applied')
  assert(binding.applied_worker_id === target.workerInstanceID && binding.applied_generation === target.generation && binding.applied_mode === test.mode && binding.applied_policy_version === test.policy_version && binding.applied_binding_revision === binding.version, 'mode binding applied receipt identity does not match the target Worker')
  assert(!binding.applied_profile_id && !binding.applied_profile_version, 'non-profile mode ACK retained named profile fields')
}

async function waitFor(session, description, predicate, timeoutMS = 7 * 60_000) {
  const deadline = Date.now() + timeoutMS
  for (;;) {
    const state = await overview(session)
    if (predicate(state)) return state
    if (Date.now() >= deadline) throw new Error(`${description} did not reach its expected state before timeout`)
    await new Promise((resolveWait) => setTimeout(resolveWait, 60_000))
  }
}

async function namedWorkflow() {
  requireExecutionEnvironment()
  const session = await login()
  const sentinel = await requirePrivateFile(networkSecretFile, 'OAX_N2_NETWORK_SECRET_FILE')
  const profileID = `n2-verify-${crypto.randomUUID().replaceAll('-', '')}`
  const host = process.env.OAX_N2_PROXY_HOST || '127.0.0.1'
  const port = Number(process.env.OAX_N2_PROXY_PORT || '18080')
  assert(Number.isInteger(port) && port > 0 && port < 65536, 'OAX_N2_PROXY_PORT must be a valid port')

  const createBody = { meta: commandMeta('n2-create'), profile_id: profileID, mode: 'only_socks5', host, port, direct_ips: [] }
  const created = await control('/api/control/v1/network-profiles', session, createBody)
  const replay = await control('/api/control/v1/network-profiles', session, createBody)
  assert(JSON.stringify(created.json) === JSON.stringify(replay.json), 'create idempotency replay changed its receipt')
  let state = await overview(session)
  let profile = profileIn(state, profileID)
  assert(profile.state === 'draft' && profile.state_revision === 1, 'create did not yield draft revision 1')

  await control(`/api/control/v1/network-profiles/${encodeURIComponent(profileID)}/draft`, session, {
    meta: commandMeta('n2-edit', profile.state_revision), mode: 'only_socks5', host, port, direct_ips: [],
  })
  state = await overview(session)
  profile = profileIn(state, profileID)
  assert(profile.current_content_version === 2 && profile.state === 'draft', 'edit did not create an immutable draft version')

  await control(`/api/control/v1/network-profiles/${encodeURIComponent(profileID)}/secret`, session, {
    meta: commandMeta('n2-secret', profile.state_revision), username: socks5Username, password: sentinel,
  })
  state = await overview(session)
  profile = profileIn(state, profileID)
  assert(profile.secret_present === true && !containsValue(state, sentinel), 'overview leaked sentinel or secret replacement was not reflected')

  const started = await control(`/api/control/v1/network-profiles/${encodeURIComponent(profileID)}/tests`, session, {
    meta: commandMeta('n2-test', profile.state_revision), worker_instance_id: target.workerInstanceID,
    generation: target.generation, backend_id: target.backendID,
  })
  const testID = started.json?.receipt?.test_id
  assert(typeof testID === 'string' && testID, 'profile test receipt omitted test_id')
  state = await waitFor(session, 'target Worker test', (current) => {
    const test = current.tests.find((item) => item.test_id === testID)
    assert(test, 'profile test disappeared from overview')
    if (test.state === 'failed' || test.state === 'stale') throw new Error(`profile test ended ${test.state}`)
    const currentProfile = profileIn(current, profileID)
    if (currentProfile.state === 'stale') throw new Error('profile became stale while waiting for its test')
    return test.state === 'succeeded' && currentProfile.state === 'ready' && currentProfile.ready_test_id === testID
  })
  profile = profileIn(state, profileID)
  const readyTest = state.tests.find((item) => item.test_id === testID)
  assert(profile.ready_test_id === testID && readyTest?.state === 'succeeded' && readyTest.profile_id === profileID && readyTest.content_version === profile.current_content_version && readyTest.worker_instance_id === target.workerInstanceID && readyTest.generation === target.generation && readyTest.backend_id === target.backendID, 'ready state lacks matching profile, version, and target Worker test evidence')
  successfulProbeResults(readyTest, 'named_profile', 'named profile test')

  await control(`/api/control/v1/network-profiles/${encodeURIComponent(profileID)}/publish`, session, { meta: commandMeta('n2-publish', profile.state_revision) })
  state = await overview(session)
  profile = profileIn(state, profileID)
  assert(profile.state === 'published' && profile.published_content_version === profile.current_content_version, 'publish did not preserve the ready version')

  await control('/api/control/v1/network-bindings', session, {
    meta: commandMeta('n2-bind', 0), agent_id: target.agentID, backend_id: target.backendID,
    profile_id: profileID, profile_version: profile.current_content_version,
    worker_instance_id: target.workerInstanceID, generation: target.generation,
  })
  state = await waitFor(session, 'explicit binding application', (current) => {
    const currentBinding = bindingIn(current, profileID)
    if (currentBinding.desired_status === 'failed' || currentBinding.desired_status === 'stale') throw new Error(`named binding application ended ${currentBinding.desired_status}`)
    return currentBinding.desired_status === 'applied'
  })
  const binding = bindingIn(state, profileID)
  assert(binding.mode === 'named_profile' && binding.profile_id === profileID && binding.profile_version === profile.current_content_version && binding.desired_status === 'applied' && binding.applied_worker_id === target.workerInstanceID && binding.applied_generation === target.generation && binding.applied_mode === 'named_profile' && binding.applied_profile_id === profileID && binding.applied_profile_version === profile.current_content_version && binding.applied_binding_revision === binding.version && binding.version > 0 && JSON.stringify(binding.runtime_identity) === JSON.stringify(readyTest.runtime_identity), 'applied named binding does not match the desired profile, target Worker, runtime test evidence, or CAS revision')
  assert(!containsValue(state, sentinel), 'sentinel was exposed through overview after application')
  console.log(JSON.stringify({ named_profile_flow: 'complete', profile_created: true, target_test_ready: true, binding_applied: true, sentinel_leaked: false }))
}

async function modeWorkflow() {
  requireExecutionEnvironment()
  const mode = process.env.OAX_N2_MODE
  assert(mode === 'inherit' || mode === 'direct', 'OAX_N2_MODE must be inherit or direct')
  const session = await login()
  let state = await overview(session)
  const beforeBinding = bindingState(targetBinding(state))
  const beforeHealth = targetHealth(await executionOptions(session))
  const expectedBindingRevision = targetBinding(state)?.version || 0

  const started = await control('/api/control/v1/network-bindings/mode/tests', session, {
    meta: commandMeta('n2-mode-test', expectedBindingRevision), agent_id: target.agentID, backend_id: target.backendID,
    mode, worker_instance_id: target.workerInstanceID, generation: target.generation,
  })
  const testID = started.json?.receipt?.test_id
  assert(typeof testID === 'string' && testID, 'mode test receipt omitted test_id')
  state = await overview(session)
  assert(bindingState(targetBinding(state)) === beforeBinding, 'mode test changed the authoritative binding before publication')
  assert(targetHealth(await executionOptions(session)) === beforeHealth, 'mode test changed target Backend health before publication')

  state = await waitFor(session, 'mode test', (current) => {
    const test = current.mode_tests.find((item) => item.test_id === testID)
    assert(test, 'mode test disappeared from overview')
    if (test.state === 'failed' || test.state === 'stale') throw new Error(`mode test ended ${test.state}`)
    return test.state === 'succeeded'
  })
  const test = state.mode_tests.find((item) => item.test_id === testID)
  assert(test?.agent_id === target.agentID && test.backend_id === target.backendID && test.mode === mode && test.worker_instance_id === target.workerInstanceID && test.generation === target.generation && test.binding_revision === expectedBindingRevision, 'mode test target identity or binding revision does not match its request')
  assert(Number.isInteger(test.policy_version) && test.policy_version > 0 && /^[0-9a-f]{64}$/.test(test.manifest_digest), 'mode test lacks immutable policy evidence')
  successfulProbeResults(test, mode, 'mode test')
  assert(bindingState(targetBinding(state)) === beforeBinding, 'completed mode test changed the authoritative binding')
  assert(targetHealth(await executionOptions(session)) === beforeHealth, 'completed mode test changed target Backend health')

  const published = await control('/api/control/v1/network-bindings/mode/publish', session, {
    meta: commandMeta('n2-mode-publish', test.binding_revision), test_id: testID,
    worker_instance_id: target.workerInstanceID, generation: target.generation,
  })
  const publishReceipt = published.json?.receipt
  assert(publishReceipt?.state === 'pending' && publishReceipt.binding_revision === expectedBindingRevision + 1, 'mode publish receipt does not prove a pending binding commit at the next revision')
  state = await overview(session)
  let binding = targetBinding(state)
  if (binding?.desired_status === 'failed') throw new Error('mode binding application failed')
  assertModeDesired(binding, test, publishReceipt.binding_revision)

  if (binding.desired_status === 'pending') {
    state = await waitFor(session, 'mode binding application', (current) => {
      const currentBinding = targetBinding(current)
      assert(currentBinding, 'published mode binding disappeared from overview')
      if (currentBinding.desired_status === 'failed' || currentBinding.desired_status === 'stale') throw new Error(`mode binding application ended ${currentBinding.desired_status}`)
      assertModeDesired(currentBinding, test, publishReceipt.binding_revision)
      return currentBinding.desired_status === 'applied'
    })
  }
  binding = targetBinding(state)
  assertModeApplied(binding, test)
  console.log(JSON.stringify({ mode_workflow: 'complete', mode, test_succeeded: true, binding_pending_before_ack: true, binding_applied: true }))
}

async function unauthenticatedReadCheck() {
  requireExecutionEnvironment()
  const result = await request('GET', `/api/observe/v1/network-profiles?agent_id=${encodeURIComponent(target.agentID)}`, { allow: [401, 403], expectJSON: false })
  console.log(JSON.stringify({ unauthenticated_overview_rejected: true, status: result.status }))
}

async function appliedStateCheck() {
  requireExecutionEnvironment()
  const session = await login()
  const state = await overview(session)

  const named = state.bindings.find((item) => item.agent_id === target.agentID && item.backend_id === 'named')
  assert(named?.mode === 'named_profile' && named.desired_status === 'applied', 'named binding is not applied')
  assert(named.applied_worker_id === target.workerInstanceID && named.applied_generation === target.generation && named.applied_mode === 'named_profile' && named.applied_profile_id === named.profile_id && named.applied_profile_version === named.profile_version && named.applied_binding_revision === named.version, 'named binding applied identity is incomplete')
  const namedProfile = profileIn(state, named.profile_id)
  const namedTest = state.tests.find((item) => item.test_id === namedProfile.ready_test_id)
  assert(namedTest, 'named binding test is absent')
  successfulProbeResults(namedTest, 'named_profile', 'named binding test')

  const fixture = targetBinding(state)
  assert(fixture?.desired_status === 'applied' && (fixture.mode === 'inherit' || fixture.mode === 'direct'), 'fixture mode binding is not applied')
  const modeTest = state.mode_tests.find((item) => item.test_id === fixture.test_id)
  assert(modeTest, 'fixture mode binding test is absent')
  assertModeApplied(fixture, modeTest)
  successfulProbeResults(modeTest, fixture.mode, 'fixture mode binding test')

  console.log(JSON.stringify({
    named_binding_applied: true,
    named_test_succeeded: true,
    fixture_mode: fixture.mode,
    fixture_binding_applied: true,
    fixture_test_succeeded: true,
  }))
}

async function browserLogin() {
  requireExecutionEnvironment()
  const cdpPort = Number(process.env.OAX_N2_CDP_PORT || '18240')
  assert(Number.isInteger(cdpPort) && cdpPort > 0 && cdpPort < 65536, 'OAX_N2_CDP_PORT must be a valid port')
  const password = await requirePrivateFile(loginPasswordFile, 'OAX_N2_PASSWORD_FILE')
  const pages = await new Promise((resolvePages, reject) => {
    const req = httpRequest(`http://127.0.0.1:${cdpPort}/json`, { timeout: 10_000 }, (res) => {
      let text = ''
      res.setEncoding('utf8')
      res.on('data', (chunk) => { text += chunk })
      res.on('end', () => {
        try { resolvePages(JSON.parse(text)) } catch { reject(new Error('Chrome DevTools target list was not JSON')) }
      })
    })
    req.once('error', reject)
    req.once('timeout', () => req.destroy(new Error('Chrome DevTools target list timed out')))
    req.end()
  })
  const page = pages.find((item) => item.type === 'page' && item.webSocketDebuggerUrl)
  assert(page, 'Chrome DevTools has no page target')
  const socket = new WebSocket(page.webSocketDebuggerUrl)
  await new Promise((resolveOpen, reject) => {
    socket.addEventListener('open', resolveOpen, { once: true })
    socket.addEventListener('error', reject, { once: true })
  })
  let nextID = 1
  const pending = new Map()
  socket.addEventListener('message', (event) => {
    let message
    try { message = JSON.parse(String(event.data)) } catch { return }
    const deferred = pending.get(message.id)
    if (!deferred) return
    pending.delete(message.id)
    if (message.error) deferred.reject(new Error(`Chrome DevTools ${message.error.message || 'command failed'}`))
    else deferred.resolve(message.result)
  })
  const send = (method, params = {}) => new Promise((resolveResult, reject) => {
    const id = nextID++
    pending.set(id, { resolve: resolveResult, reject })
    socket.send(JSON.stringify({ id, method, params }))
  })
  const evaluate = async (expression) => (await send('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true })).result?.value
  try {
    await send('Page.navigate', { url: baseURL })
    for (let attempt = 0; attempt < 50; attempt += 1) {
      if (await evaluate("Boolean(document.querySelector('#username') && document.querySelector('#password'))")) break
      await new Promise((resolveWait) => setTimeout(resolveWait, 100))
    }
    assert(await evaluate("Boolean(document.querySelector('#username') && document.querySelector('#password'))"), 'login form did not render')
    const expression = `(() => {
      const setInput = (selector, value) => {
        const input = document.querySelector(selector)
        const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set
        setter.call(input, value)
        input.dispatchEvent(new Event('input', { bubbles: true }))
        input.dispatchEvent(new Event('change', { bubbles: true }))
      }
      setInput('#username', ${JSON.stringify(username)})
      setInput('#password', ${JSON.stringify(password)})
      document.querySelector('form').requestSubmit()
      return true
    })()`
    await evaluate(expression)
    for (let attempt = 0; attempt < 100; attempt += 1) {
      if (await evaluate("Boolean(document.querySelector('nav.bottom'))")) break
      await new Promise((resolveWait) => setTimeout(resolveWait, 100))
    }
    assert(await evaluate("Boolean(document.querySelector('nav.bottom'))"), 'browser login did not establish an authenticated panel session')
    console.log(JSON.stringify({ browser_login_succeeded: true }))
  } finally {
    socket.close()
  }
}

async function browserSecretClearCheck() {
  requireExecutionEnvironment()
  const cdpPort = Number(process.env.OAX_N2_CDP_PORT || '18240')
  const secret = await requirePrivateFile(networkSecretFile, 'OAX_N2_NETWORK_SECRET_FILE')
  const pages = await new Promise((resolvePages, reject) => {
    const req = httpRequest(`http://127.0.0.1:${cdpPort}/json`, { timeout: 10_000 }, (res) => {
      let text = ''
      res.setEncoding('utf8')
      res.on('data', (chunk) => { text += chunk })
      res.on('end', () => { try { resolvePages(JSON.parse(text)) } catch { reject(new Error('Chrome DevTools target list was not JSON')) } })
    })
    req.once('error', reject)
    req.once('timeout', () => req.destroy(new Error('Chrome DevTools target list timed out')))
    req.end()
  })
  const page = pages.find((item) => item.type === 'page' && item.webSocketDebuggerUrl)
  assert(page, 'Chrome DevTools has no page target')
  const socket = new WebSocket(page.webSocketDebuggerUrl)
  await new Promise((resolveOpen, reject) => {
    socket.addEventListener('open', resolveOpen, { once: true })
    socket.addEventListener('error', reject, { once: true })
  })
  let nextID = 1
  const pending = new Map()
  socket.addEventListener('message', (event) => {
    let message
    try { message = JSON.parse(String(event.data)) } catch { return }
    const deferred = pending.get(message.id)
    if (!deferred) return
    pending.delete(message.id)
    if (message.error) deferred.reject(new Error(`Chrome DevTools ${message.error.message || 'command failed'}`))
    else deferred.resolve(message.result)
  })
  const evaluate = (expression) => new Promise((resolveResult, reject) => {
    const id = nextID++
    pending.set(id, { resolve: resolveResult, reject })
    socket.send(JSON.stringify({ id, method: 'Runtime.evaluate', params: { expression, returnByValue: true, awaitPromise: true } }))
  }).then((result) => result.result?.value)
  try {
    const expression = `(() => {
      const setInput = (selector, value) => {
        const input = document.querySelector(selector)
        const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set
        setter.call(input, value)
        input.dispatchEvent(new Event('input', { bubbles: true }))
      }
      const forms = [...document.querySelectorAll('.network-secret-form')]
      const form = forms.at(-1)
      if (!form) return false
      const inputs = form.querySelectorAll('input')
      setInput('.network-secret-form input[type=text], .network-secret-form input:not([type])', 'n2-fixture-socks-user')
      setInput('.network-secret-form input[type=password]', ${JSON.stringify(secret)})
      form.requestSubmit()
      return inputs.length === 2
    })()`
    assert(await evaluate(expression), 'browser secret form is unavailable')
    for (let attempt = 0; attempt < 100; attempt += 1) {
      if (await evaluate("(() => { const form = document.querySelector('.network-secret-form'); return Boolean(form && [...form.querySelectorAll('input')].every((input) => input.value === '') && document.body.innerText.includes('凭据已替换')) })()")) break
      await new Promise((resolveWait) => setTimeout(resolveWait, 100))
    }
    assert(await evaluate("(() => { const form = document.querySelector('.network-secret-form'); return Boolean(form && [...form.querySelectorAll('input')].every((input) => input.value === '')) })()"), 'secret inputs were retained after submission')
    console.log(JSON.stringify({ browser_secret_submission_succeeded: true, secret_inputs_cleared: true }))
  } finally {
    socket.close()
  }
}

if (command === 'prepare') await prepare()
else if (command === 'named-workflow') await namedWorkflow()
else if (command === 'mode-workflow') await modeWorkflow()
else if (command === 'unauthenticated-read-check') await unauthenticatedReadCheck()
else if (command === 'applied-state-check') await appliedStateCheck()
else if (command === 'browser-login') await browserLogin()
else if (command === 'browser-secret-clear-check') await browserSecretClearCheck()
else throw new Error('usage: verify-n2.mjs prepare | named-workflow | mode-workflow | unauthenticated-read-check | applied-state-check | browser-login | browser-secret-clear-check')
