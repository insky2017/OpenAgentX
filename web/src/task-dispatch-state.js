const agentID = (agent) => agent?.agent_id || agent?.id || ''

export const dispatchTarget = (agents, workers, selectedID, now) => {
  const latest = new Map()
  for (const worker of workers) {
    if (!Number.isSafeInteger(worker.generation) || worker.generation < 1) continue
    const current = latest.get(worker.agent_id)
    if (!current || worker.generation > current.generation) latest.set(worker.agent_id, worker)
  }
  const ready = (worker) => worker?.status === 'online' && Date.parse(worker.lease_until) > now
  const available = agents.filter((agent) => ready(latest.get(agentID(agent))))
  const agent = selectedID
    ? agents.find((item) => agentID(item) === selectedID)
    : available.length === 1 ? available[0] : null
  if (!agent) return {
    id: '', agent: null, worker: null, ready: false,
    reason: selectedID ? '所选 Agent 不存在，请重新选择'
      : available.length > 1 ? '请选择执行指令的 Agent'
        : '暂无在线 Worker，请先启动或恢复 Worker',
  }
  const id = agentID(agent)
  const worker = latest.get(id)
  const canStart = ready(worker)
  return {
    id, agent, worker, ready: canStart,
    reason: canStart ? `Worker 在线 · generation ${worker.generation}`
      : worker?.status === 'draining' ? 'Worker 正在退出，不接受新任务'
        : worker?.status === 'bootstrapping' ? 'Worker 正在启动，请等待就绪'
          : worker?.status === 'online' ? 'Worker 心跳已过期，请等待恢复'
            : 'Worker 离线或未就绪，暂不能发送',
  }
}

// The official create response is a receipt, not a Task snapshot.
export const createdTaskID = (response) => {
  if (!response || typeof response.task_id !== 'string' || !response.task_id ||
      !Number.isSafeInteger(response.task_version) || response.task_version < 1 ||
      !Number.isSafeInteger(response.sequence) || response.sequence < 1 ||
      !['queued', 'dispatching', 'running', 'waiting_input', 'waiting_approval', 'cancel_requested',
        'succeeded', 'failed', 'canceled', 'uncertain'].includes(response.task_status)) return ''
  return response.task_id
}

export const taskProgress = (task, run, worker) => {
  if (!task) return ''
  if (run?.turn_result?.runtime_status === 'succeeded' || run?.status === 'succeeded') {
    return task.status === 'uncertain'
      ? 'Runtime 已执行并返回；任务业务核验尚未完成'
      : 'Runtime 已执行完成，回复见下方'
  }
  if (['failed', 'uncertain', 'canceled'].includes(run?.status)) return '本次执行已结束，请查看结果与诊断'
  if (task.status === 'waiting_input') return '正在等待你的补充回复'
  if (task.status === 'waiting_approval') return '正在等待审批'
  if (task.status === 'queued' || task.status === 'dispatching') {
    return worker ? '指令已入队，等待 Worker 领取' : '指令已入队，但目标 Worker 离线，尚未开始执行'
  }
  if (run?.status === 'starting') return 'Worker 已领取，正在启动 Runtime；过程会自动更新'
  if (run || task.status === 'running') return 'Worker 已领取，执行过程会自动更新'
  return '请查看当前任务状态与执行结果'
}
