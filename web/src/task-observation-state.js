export const mergeTaskEvents = (current, incoming) => {
  const events = [...(current?.events || []), ...(incoming?.events || [])]
  return Array.from(new Map(events.map((event) => [event.sequence, event])).values())
    .sort((left, right) => left.sequence - right.sequence)
}

export const mergeLiveTaskDetail = (current, incoming) => {
  if (!current || current.task?.id !== incoming.task?.id) return incoming
  return {
    ...incoming,
    events: mergeTaskEvents(current, incoming),
    history_before_sequence: current.history_before_sequence,
    has_older_events: current.has_older_events,
  }
}

export const mergeHistoryTaskDetail = (current, incoming) => {
  if (!current || current.task?.id !== incoming.task?.id) return current
  return {
    ...current,
    events: mergeTaskEvents(current, incoming),
    history_before_sequence: incoming.history_before_sequence,
    has_older_events: incoming.has_older_events,
  }
}

// The server captures live_after_sequence before reading overview projections.
// It is safe only for bootstrap. Later snapshots must never skip events after
// the last confirmed cursor; zero is a valid confirmed empty-journal cursor.
export const sseResumeAfter = (confirmedSequence, overviewLiveAfterSequence) => {
  if (Number.isSafeInteger(confirmedSequence) && confirmedSequence >= 0) return confirmedSequence
  if (confirmedSequence === null && Number.isSafeInteger(overviewLiveAfterSequence) && overviewLiveAfterSequence >= 0) return overviewLiveAfterSequence
  throw new Error('服务未返回有效的事件续接游标，请刷新后重试')
}

export const isCurrentObservation = (expected, current) => (
  expected.taskID === current.taskID &&
  expected.selection === current.selection &&
  (expected.request === undefined || expected.request === current.request) &&
  (expected.view === undefined || expected.view === current.view)
)
