const agentID = (agent) => agent?.agent_id || agent?.id || ''

export const dispatchTarget = (agents, workers, selectedID, now) => {
  const latest = new Map()
  for (const worker of workers) {
    if (!Number.isSafeInteger(worker.generation) || worker.generation < 1) continue
    const current = latest.get(worker.agent_id)
    if (!current || worker.generation > current.generation) latest.set(worker.agent_id, worker)
  }
  const ready = (worker) => worker?.status === 'online' && Date.parse(worker.lease_until) > now
  const available = agents.filter((agent) => ready(latest.get(agentID(agent))) && (agent.readiness ? agent.readiness.ready : true))
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
  const canStart = ready(worker) && (agent.readiness ? agent.readiness.ready : true)
  return {
    id, agent, worker, ready: canStart,
    reason: agent.readiness?.reason || (canStart ? `Worker 在线 · generation ${worker.generation}`
      : worker?.status === 'draining' ? 'Worker 正在退出，不接受新任务'
        : worker?.status === 'bootstrapping' ? 'Worker 正在启动，请等待就绪'
          : worker?.status === 'online' ? 'Worker 心跳已过期，请等待恢复'
            : 'Worker 离线或未就绪，暂不能发送'),
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
  if (task.status === 'cancel_requested') return '正在停止：请求已接受，等待运行进程确认停止'
  if (task.status === 'canceled') return '已取消，运行已停止或尚未开始；已产生的文件修改不会自动撤销'
  if (task.status === 'uncertain' && run?.status === 'uncertain') return '执行结果或停止状态尚未确认，请检查诊断；不会自动重跑'
  if (task.status === 'succeeded' && task.intent === 'query' && task.completion_basis === 'query_result_delivered') {
    return '查询回复已完整交付；不代表答案真实性或副作用已核验'
  }
  if (task.error === 'query_result_unverified') return '查询最终回复证据不完整，请查看结果与诊断'
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

// The API orders runs newest-first. Choose by execution time rather than UI
// visitation history or incidental array order.
export const latestTaskRun = (runs = []) => [...runs].sort((a, b) =>
  Date.parse(b.started_at || b.created_at) - Date.parse(a.started_at || a.created_at) ||
  String(b.run_id).localeCompare(String(a.run_id))
)[0]
