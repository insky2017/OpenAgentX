#!/usr/bin/env node
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { request as httpRequest } from 'node:http'
import { join } from 'node:path'

const command = process.argv[2]
const fixtureDir = process.env.OAX_VERIFY_FIXTURE_DIR
const baseURL = process.env.OAX_VERIFY_BASE_URL
const username = process.env.OAX_VERIFY_USER
const password = process.env.OAX_VERIFY_PASSWORD
const passwordFile = process.env.OAX_VERIFY_PASSWORD_FILE
const statePath = process.env.OAX_VERIFY_STATE_FILE
const seedPath = fixtureDir && join(fixtureDir, 'seed.json')
const routeWorkerPath = fixtureDir && join(fixtureDir, 'route-worker.json')

if (!command || !fixtureDir) throw new Error('command and OAX_VERIFY_FIXTURE_DIR are required')

const agents = [
  { id: 'u1-agent-a', principal: 'u1-agent-a-principal', name: 'U1 Agent A' },
  { id: 'u1-agent-b', principal: 'u1-agent-b-principal', name: 'U1 Agent B' },
]

const requestID = (prefix, index = '') => `${prefix}-${index || crypto.randomUUID()}`

async function writeFixtureDefinitions() {
  await mkdir(join(fixtureDir, 'workspace'), { recursive: true })
  await writeFile(join(fixtureDir, 'owner-password'), crypto.randomUUID() + crypto.randomUUID(), { mode: 0o600 })
  await writeFile(join(fixtureDir, 'ROLE.md'), 'Isolated browser validation fixture.\n', { mode: 0o600 })
  for (const agent of agents) {
    await writeFile(join(fixtureDir, `${agent.id}.yaml`), [
      'version: 1',
      `agent_id: ${agent.id}`,
      `principal_id: ${agent.principal}`,
      'organization_id: u1-fixture-org',
      `display_name: ${agent.name}`,
      'profile:',
      '  instructions_path: ROLE.md',
      '  workspace_root: workspace',
      '  capabilities:',
      '    - coding',
      '',
    ].join('\n'), { mode: 0o600 })
  }
  console.log(JSON.stringify({ prepared: true, agents: agents.map((agent) => agent.id) }))
}

function requireClientEnvironment() {
  if (!baseURL || !username || (!password && !passwordFile) || !statePath) {
    throw new Error('OAX_VERIFY_BASE_URL, OAX_VERIFY_USER, OAX_VERIFY_PASSWORD or OAX_VERIFY_PASSWORD_FILE, and OAX_VERIFY_STATE_FILE are required')
  }
}

async function login() {
  requireClientEnvironment()
  const credential = password || (await readFile(passwordFile, 'utf8')).trim()
  const response = await fetch(`${baseURL}/api/auth/v1/login`, {
    method: 'POST', headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ username, password: credential }),
  })
  if (!response.ok) throw new Error(`login failed: ${response.status}`)
  const session = await response.json()
  const values = response.headers.getSetCookie?.() || [response.headers.get('set-cookie')]
  const cookie = values.find(Boolean)?.split(';', 1)[0]
  if (!cookie || !session.csrf_token) throw new Error('login did not return session state')
  const [name, value] = cookie.split('=', 2)
  const origin = new URL(baseURL)
  await writeFile(statePath, JSON.stringify({
    cookies: [{ name, value, domain: origin.hostname, path: '/', expires: -1, httpOnly: true, secure: true, sameSite: 'Strict' }], origins: [],
  }), { mode: 0o600 })
  return { cookie, csrf: session.csrf_token }
}

async function api(path, session, options = {}) {
  const headers = { cookie: session.cookie, ...(options.headers || {}) }
  const response = await fetch(`${baseURL}${path}`, { ...options, headers })
  const text = await response.text()
  let body = null
  if (text) {
    try { body = JSON.parse(text) } catch { body = text }
  }
  if (!response.ok) {
    const diagnostic = typeof body === 'string' ? body.trim().slice(0, 240) : JSON.stringify(body).slice(0, 240)
    throw new Error(`${options.method || 'GET'} ${path} failed: ${response.status}${diagnostic ? ` (${diagnostic})` : ''}`)
  }
  return body
}

async function control(path, session, body) {
  const idempotencyKey = body.meta.idempotency_key
  return api(path, session, {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-csrf-token': session.csrf, 'idempotency-key': idempotencyKey },
    body: JSON.stringify(body),
  })
}

async function taskDetail(taskID, session, suffix = '') {
  return api(`/api/observe/v1/tasks/${encodeURIComponent(taskID)}${suffix}`, session)
}

async function appendMessage(taskID, session, content, suffix) {
  const detail = await taskDetail(taskID, session)
  return control(`/api/control/v1/tasks/${encodeURIComponent(taskID)}/messages`, session, {
    meta: { idempotency_key: requestID('u1-message', suffix), expected_version: detail.task.version }, content,
  })
}

// This client intentionally exercises the daemon's Unix Worker API. It only
// creates a cancellable active Run; it does not emulate or claim any external
// Runtime side effect.
async function worker(path, payload, token = '') {
  const encoded = JSON.stringify(payload)
  return new Promise((resolve, reject) => {
    const request = httpRequest({
      socketPath: join(fixtureDir, 'openagentx.sock'), path, method: 'POST',
      headers: {
        'content-type': 'application/json', 'content-length': Buffer.byteLength(encoded),
        ...(token ? { authorization: `Bearer ${token}` } : {}),
      },
    }, (response) => {
      let text = ''
      response.setEncoding('utf8')
      response.on('data', (chunk) => { text += chunk })
      response.on('end', () => {
        if (response.statusCode < 200 || response.statusCode >= 300) {
          reject(new Error(`Worker API ${path} failed: ${response.statusCode}`))
          return
        }
        try { resolve(text ? JSON.parse(text) : null) } catch { reject(new Error(`Worker API ${path} returned invalid JSON`)) }
      })
    })
    request.on('error', reject)
    request.end(encoded)
  })
}

function routeWorkerRegistration() {
  return {
    contract_version: 'v1', agent_id: agents[0].id, worker_instance_id: `u1-route-worker-${crypto.randomUUID()}`,
    transport: 'unix', capabilities: ['coding'], backends: [{
      backend_id: 'fixture-route', health: 'healthy', descriptor: {
        adapter_id: 'fixture', backend_type: 'fixture', version: '1', launch_protocol: 'inproc',
        models: ['fixture-model'], reasoning_modes: ['backend_default'], session_modes: ['new'],
        steer: 'queued', approval: 'unsupported', cancel: 'process_signal', max_concurrency: 1,
      },
    }],
  }
}

async function startRouteWorker() {
  const registration = await worker('/api/v1/workers/register', routeWorkerRegistration())
  const { worker: instance, session_token: token } = registration
  assert(instance && token, 'route Worker registration did not return a session')
  const guard = { worker_instance_id: instance.worker_instance_id, generation: instance.generation, fencing_token: instance.fencing_token }
  const heartbeat = async () => worker(`/api/v1/workers/${encodeURIComponent(instance.worker_instance_id)}/heartbeat`, {
    ...guard, status: 'online', backend_health: { 'fixture-route': 'healthy' },
  }, token)
  await heartbeat()
  await writeFile(routeWorkerPath, JSON.stringify({ guard, token }), { mode: 0o600 })
  console.log(JSON.stringify({ route_worker: 'online', worker_instance_id: instance.worker_instance_id, heartbeat_seconds: 10 }))
  const interval = setInterval(() => heartbeat().catch((err) => {
    console.error(`route worker heartbeat failed: ${err.message}`)
    process.exitCode = 1
  }), 10_000)
  const release = async () => {
    clearInterval(interval)
    try { await worker(`/api/v1/workers/${encodeURIComponent(instance.worker_instance_id)}/release`, guard, token) } catch (err) { console.error(`route worker release failed: ${err.message}`) }
  }
  process.once('SIGINT', () => release().finally(() => process.exit()))
  process.once('SIGTERM', () => release().finally(() => process.exit()))
  await new Promise(() => {})
}

async function releaseRouteWorker() {
  const saved = JSON.parse(await readFile(routeWorkerPath, 'utf8'))
  await worker(`/api/v1/workers/${encodeURIComponent(saved.guard.worker_instance_id)}/release`, saved.guard, saved.token)
  console.log(JSON.stringify({ route_worker: 'released', worker_instance_id: saved.guard.worker_instance_id }))
}

async function makeCancelableTask(taskIDs, session) {
  const registration = await worker('/api/v1/workers/register', {
    contract_version: 'v1', agent_id: agents[0].id, worker_instance_id: `u1-fixture-worker-a-${crypto.randomUUID()}`,
    transport: 'unix', capabilities: ['coding'], backends: [{
      backend_id: 'fixture-local', health: 'healthy', descriptor: {
        adapter_id: 'fixture', backend_type: 'fixture', version: '1', launch_protocol: 'inproc',
        models: ['fixture-model'], reasoning_modes: ['backend_default'], session_modes: ['new'],
        steer: 'queued', approval: 'unsupported', cancel: 'process_signal', max_concurrency: 1,
      },
    }],
  })
  const { worker: instance, session_token: token } = registration
  assert(instance && token, 'Worker registration did not return a session')
  const guard = { worker_instance_id: instance.worker_instance_id, generation: instance.generation, fencing_token: instance.fencing_token }
  await worker(`/api/v1/workers/${encodeURIComponent(instance.worker_instance_id)}/heartbeat`, {
    ...guard, status: 'online', backend_health: { 'fixture-local': 'healthy' },
  }, token)
  const claim = await worker(`/api/v1/workers/${encodeURIComponent(instance.worker_instance_id)}/mailbox/claim?wait=0s`, {
    ...guard, agent_id: agents[0].id, work_capacity: 1, wait_seconds: 0,
  }, token)
  assert(taskIDs.has(claim.item?.task_id) && claim.item?.kind === 'task', 'Worker did not claim a fixture task addressed to Agent A')
  const begin = await worker(`/api/v1/mailbox/${encodeURIComponent(claim.item.mailbox_item_id)}/begin-attempt`, {
    ...guard, agent_id: agents[0].id, expected_item_state: 'claimed',
  }, token)
  assert(begin.turn?.task?.status === 'running' && begin.turn?.run_attempt?.run_id, 'Worker begin did not create an active Run')
  const taskID = claim.item.task_id
  const running = await taskDetail(taskID, session)
  const canceled = await control(`/api/control/v1/tasks/${encodeURIComponent(taskID)}/cancel`, session, {
    meta: { idempotency_key: requestID('u1-cancel', taskID), expected_version: running.task.version },
  })
  assert(canceled.task?.status === 'cancel_requested', 'Control cancel did not persist cancel_requested')
  return { taskID, status: canceled.task.status }
}

function assert(condition, description) {
  if (!condition) throw new Error(description)
}

async function listAllTaskPages(session, query = '') {
  let cursor = ''
  const taskIDs = new Set()
  let pages = 0
  do {
    const joiner = query ? '&' : '?'
    const page = await api(`/api/observe/v1/tasks?limit=50${query}${cursor ? `${joiner}cursor=${encodeURIComponent(cursor)}` : ''}`, session)
    pages += 1
    for (const task of page.tasks) {
      assert(!taskIDs.has(task.id), `duplicate task across pages: ${task.id}`)
      taskIDs.add(task.id)
    }
    cursor = page.next_cursor || ''
    if (!page.has_more) break
    assert(cursor, 'page claims has_more without next_cursor')
  } while (true)
  return { pages, taskIDs }
}

async function readAllTaskEvents(taskID, session) {
  const full = await taskDetail(taskID, session, '?after_sequence=0&limit=499')
  assert(!full.has_more_live_events, 'fixture event range exceeded 499 events')
  const expected = new Set(full.events.map((event) => event.sequence))
  const first = await taskDetail(taskID, session, '?limit=50')
  assert(first.has_older_events, 'fixture did not produce more than one event history page')
  const seen = new Set(first.events.map((event) => event.sequence))
  const snapshotSequence = first.snapshot_sequence
  await appendMessage(taskID, session, 'U1 live event while history is loading', 'history-race')
  let before = first.history_before_sequence
  while (before) {
    const page = await taskDetail(taskID, session, `?before_sequence=${before}&limit=50`)
    for (const event of page.events) {
      assert(!seen.has(event.sequence), `duplicate history event: ${event.sequence}`)
      seen.add(event.sequence)
    }
    before = page.has_older_events ? page.history_before_sequence : 0
  }
  const live = await taskDetail(taskID, session, `?after_sequence=${snapshotSequence}&limit=100`)
  for (const event of live.events) {
    assert(!seen.has(event.sequence), `duplicate live event: ${event.sequence}`)
    seen.add(event.sequence)
  }
  const afterRace = await taskDetail(taskID, session, '?after_sequence=0&limit=499')
  const expectedAfterRace = new Set(afterRace.events.map((event) => event.sequence))
  assert(seen.size === expectedAfterRace.size, `event coverage mismatch: got=${seen.size} expected=${expectedAfterRace.size}`)
  for (const sequence of expectedAfterRace) assert(seen.has(sequence), `missing event sequence: ${sequence}`)
  return { initialSnapshotSequence: snapshotSequence, eventCount: seen.size, firstPageEvents: first.events.length }
}

async function seed() {
  const session = await login()
  const cancelTask = (await control('/api/control/v1/tasks', session, {
    meta: { idempotency_key: requestID('u1-cancel-fixture', 'one') }, sender_principal_id: 'ignored',
    target_agent_id: agents[0].id, organization_id: 'u1-fixture-org', dispatch_mode: 'direct',
    content: 'U1 cancel-state fixture',
  })).task_id
  assert(cancelTask, 'cancel fixture did not return a task ID')
  const canceled = await makeCancelableTask(new Set([cancelTask]), session)
  const created = []
  for (let index = 0; index < 60; index += 1) {
    const agent = agents[index % agents.length]
    const result = await control('/api/control/v1/tasks', session, {
      meta: { idempotency_key: requestID('u1-task', index) }, sender_principal_id: 'ignored',
      target_agent_id: agent.id, organization_id: 'u1-fixture-org', dispatch_mode: 'direct',
      content: `U1 pagination fixture ${String(index).padStart(2, '0')} ${agent.id}`,
    })
    created.push(result.task_id)
  }
  const focus = (await control('/api/control/v1/tasks', session, {
    meta: { idempotency_key: requestID('u1-focus', 'one') }, sender_principal_id: 'ignored',
    target_agent_id: agents[0].id, organization_id: 'u1-fixture-org', dispatch_mode: 'direct',
    content: 'U1 history focus task',
  })).task_id
  for (let index = 0; index < 220; index += 1) {
    await appendMessage(focus, session, `U1 historical event ${String(index).padStart(3, '0')}`, index)
  }
  const all = await listAllTaskPages(session, '&query=U1%20pagination%20fixture')
  assert(all.pages > 1 && all.taskIDs.size === 60, `task pagination coverage failed: pages=${all.pages} tasks=${all.taskIDs.size}`)
  const agentFiltered = await listAllTaskPages(session, '&agent_id=u1-agent-a&query=U1%20pagination%20fixture')
  assert(agentFiltered.taskIDs.size === 30, `agent filter count=${agentFiltered.taskIDs.size}`)
  const statusFiltered = await listAllTaskPages(session, '&status=cancel_requested')
  assert(statusFiltered.taskIDs.size === 1 && statusFiltered.taskIDs.has(canceled.taskID), `status filter count=${statusFiltered.taskIDs.size}`)
  const history = await readAllTaskEvents(focus, session)
  await writeFile(seedPath, JSON.stringify({ focus, history, taskPages: all.pages, createdTaskCount: created.length + 2, canceledTaskID: canceled.taskID }), { mode: 0o600 })
  console.log(JSON.stringify({ seeded: true, task_pages: all.pages, tasks: created.length + 2, canceled_task_id: canceled.taskID, focus_task_id: focus, event_count: history.eventCount, snapshot_sequence: history.initialSnapshotSequence }))
}

async function append() {
  const session = await login()
  const seed = JSON.parse(await readFile(seedPath, 'utf8'))
  const suffix = process.env.OAX_VERIFY_APPEND_SUFFIX || 'sse-reconnect'
  await appendMessage(seed.focus, session, `U1 SSE reconnect live event ${suffix}`, suffix)
  const detail = await taskDetail(seed.focus, session, '?after_sequence=0&limit=499')
  console.log(JSON.stringify({ appended: true, focus_task_id: seed.focus, event_count: detail.events.length, latest_sequence: detail.snapshot_sequence }))
}

if (command === 'prepare') await writeFixtureDefinitions()
else if (command === 'login') await login()
else if (command === 'seed') await seed()
else if (command === 'append') await append()
else if (command === 'route-worker') await startRouteWorker()
else if (command === 'release-route-worker') await releaseRouteWorker()
else throw new Error(`unsupported command: ${command}`)
