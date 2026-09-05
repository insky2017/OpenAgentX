import assert from 'node:assert/strict'
import test from 'node:test'

import {
  isCurrentObservation,
  mergeHistoryTaskDetail,
  mergeLiveTaskDetail,
  sseResumeAfter,
} from './task-observation-state.js'

const events = (from, through) => Array.from(
  { length: through - from + 1 },
  (_, index) => ({ sequence: from + index, event_id: `event-${from + index}` }),
)

test('live response merges against history that completed after live was sent', () => {
  const initial = {
    task: { id: 'task-a', version: 1 },
    events: events(101, 200),
    history_before_sequence: 101,
    has_older_events: true,
    live_after_sequence: 200,
  }
  // L is sent from initial, then H writes first.
  const afterHistory = mergeHistoryTaskDetail(initial, {
    task: { id: 'task-a' },
    events: events(1, 100),
    history_before_sequence: 1,
    has_older_events: false,
  })
  // L must use the latest ref at writeback, not the object captured at send.
  const afterLive = mergeLiveTaskDetail(afterHistory, {
    task: { id: 'task-a', version: 2 },
    events: [{ sequence: 201, event_id: 'event-201' }],
    live_after_sequence: 201,
  })

  assert.deepEqual(afterLive.events.map((event) => event.sequence), events(1, 201).map((event) => event.sequence))
  assert.equal(afterLive.history_before_sequence, 1)
  assert.equal(afterLive.has_older_events, false)
  assert.equal(afterLive.live_after_sequence, 201)
})

test('SSE resume ignores an overview watermark that may cover stale projections', () => {
  const confirmedSequence = 41
  const overviewReadBeforeEventButWatermarkAfterEvent = 42
  assert.equal(sseResumeAfter(confirmedSequence, overviewReadBeforeEventButWatermarkAfterEvent), 41)
  assert.equal(sseResumeAfter(0, overviewReadBeforeEventButWatermarkAfterEvent), 0)
})

test('deferred UI work rejects a changed task, request, or view', () => {
  const expected = { taskID: 'task-a', selection: 3, request: 9, view: 'run' }
  assert.equal(isCurrentObservation(expected, expected), true)
  assert.equal(isCurrentObservation(expected, { ...expected, taskID: 'task-b' }), false)
  assert.equal(isCurrentObservation(expected, { ...expected, selection: 4 }), false)
  assert.equal(isCurrentObservation(expected, { ...expected, request: 10 }), false)
  assert.equal(isCurrentObservation(expected, { ...expected, view: 'content' }), false)
})

test('deferred close focus rejects a task reopened before the next frame', () => {
  const closed = { taskID: '', selection: 4 }
  assert.equal(isCurrentObservation(closed, closed), true)
  assert.equal(isCurrentObservation(closed, { taskID: 'task-b', selection: 5 }), false)
})
