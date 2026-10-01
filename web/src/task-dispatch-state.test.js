import assert from 'node:assert/strict'
import test from 'node:test'
import { createdTaskID, dispatchTarget, taskProgress, latestTaskRun } from './task-dispatch-state.js'

const now = Date.parse('2026-09-22T12:00:00Z')
const agents = [
  { id: 'orchestrator', organization_id: 'default' },
  { id: 'quote-service', organization_id: 'default' },
]
const quote = { agent_id: 'quote-service', generation: 52, status: 'online', lease_until: '2026-09-22T12:00:30Z' }
const offline = { agent_id: 'orchestrator', generation: 34, status: 'offline' }

test('direct entry chooses the sole online Worker, never the offline first Agent', () => {
  const target = dispatchTarget(agents, [offline, quote], '', now)
  assert.equal(target.id, 'quote-service')
  assert.equal(target.ready, true)
  assert.equal(dispatchTarget([...agents].reverse(), [quote, offline], '', now).id, target.id)
})

test('explicit and restored URL targets are authoritative and never silently replaced', () => {
  assert.equal(dispatchTarget(agents, [quote], 'quote-service', now).ready, true)
  assert.equal(dispatchTarget(agents, [offline, quote], 'orchestrator', now).ready, false)
  assert.equal(dispatchTarget(agents, [offline, quote], 'missing', now).id, '')
})

test('no Worker, multiple ready Workers, draining and expired leases require a choice or recovery', () => {
  const cases = [
    [[], ''],
    [[quote, { ...quote, agent_id: 'orchestrator' }], ''],
    [[{ ...quote, status: 'draining' }], 'quote-service'],
    [[{ ...quote, status: 'bootstrapping' }], 'quote-service'],
    [[{ ...quote, lease_until: '2026-09-22T12:00:00Z' }], 'quote-service'],
  ]
  for (const [workers, selected] of cases) assert.equal(dispatchTarget(agents, workers, selected, now).ready, false)
})

test('an offline replacement never falls back to an older online generation', () => {
  for (const workers of [[quote, { ...quote, generation: 53, status: 'offline' }],
    [{ ...quote, generation: 53, status: 'offline' }, quote]]) {
    const target = dispatchTarget(agents, workers, 'quote-service', now)
    assert.equal(target.ready, false)
    assert.equal(target.worker.generation, 53)
  }
})

test('only the official create receipt can open the accepted Task', () => {
  const receipt = { task_id: 'task-new', task_version: 1, task_status: 'queued', sequence: 70 }
  assert.equal(createdTaskID(receipt), 'task-new')
  for (const response of [null, {}, { task: { id: 'task-new' } }, { ...receipt, task_id: '' },
    { ...receipt, task_version: 0 }, { ...receipt, sequence: 0 }, { ...receipt, task_status: 'unknown' }]) {
    assert.equal(createdTaskID(response), '')
  }
})

test('progress distinguishes not started, actual Runtime reply and unverified business outcome', () => {
  assert.match(taskProgress({ status: 'queued' }, null, null), /尚未开始执行/)
  assert.match(taskProgress({ status: 'queued' }, null, quote), /等待 Worker 领取/)
  assert.match(taskProgress({ status: 'running' }, { status: 'starting' }, quote), /正在启动 Runtime/)
  assert.match(taskProgress({ status: 'uncertain' }, { status: 'succeeded' }, quote), /业务核验尚未完成/)
})

test('query delivery comes only from authoritative Task completion evidence', () => {
  const delivered = { status: 'succeeded', intent: 'query', completion_basis: 'query_result_delivered' }
  assert.match(taskProgress(delivered, null, quote), /查询回复已完整交付/)
  assert.match(taskProgress(delivered, null, quote), /不代表答案真实性或副作用已核验/)
  for (const task of [{ ...delivered, status: 'uncertain' }, { ...delivered, completion_basis: '' },
    { ...delivered, intent: 'mutation' }]) {
    assert.doesNotMatch(taskProgress(task, { status: 'succeeded' }, quote), /查询回复已完整交付/)
  }
  assert.match(taskProgress({ status: 'uncertain', intent: 'query', error: 'query_result_unverified' }, null, quote), /证据不完整/)
})


test('backend readiness blocks dispatch even with an online Worker, busy allows queueing', () => {
  const target = { ...agents[1], readiness: { ready: false, reason: '网络尚未就绪' } }
  assert.equal(dispatchTarget([target], [quote], 'quote-service', now).ready, false)
  assert.equal(dispatchTarget([target], [quote], 'quote-service', now).reason, '网络尚未就绪')
  assert.equal(dispatchTarget([{ ...target, readiness: {ready: true, can_start_now: false, reason: '正在工作，新任务将排队'} }], [quote], 'quote-service', now).ready, true)
})

test('latest result belongs to the latest execution in either API ordering', () => {
  const oldRun = {run_id:'run-old', started_at:'2026-09-22T11:00:00Z'}
  const newRun = {run_id:'run-new', started_at:'2026-09-22T12:00:00Z'}
  assert.equal(latestTaskRun([newRun, oldRun]), newRun)
  assert.equal(latestTaskRun([oldRun, newRun]), newRun)
  assert.equal(latestTaskRun([]), undefined)
})
