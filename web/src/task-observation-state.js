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

// Overview is not a transactional snapshot. Its latest sequence is only a
// display hint and must never advance the reconnect cursor.
export const sseResumeAfter = (confirmedSequence, _overviewLatestSequence) => {
  const sequence = Number(confirmedSequence)
  return Number.isSafeInteger(sequence) && sequence > 0 ? sequence : 0
}

export const isCurrentObservation = (expected, current) => (
  expected.taskID === current.taskID &&
  expected.selection === current.selection &&
  (expected.request === undefined || expected.request === current.request) &&
  (expected.view === undefined || expected.view === current.view)
)
