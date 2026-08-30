import React, { useEffect, useMemo, useState } from 'react'
import { createRoot } from 'react-dom/client'
import './styles.css'

class APIError extends Error {
  constructor(status, message) {
    super(message)
    this.status = status
  }
}

const api = async (path, options = {}) => {
  const response = await fetch(path, {
    credentials: 'include',
    headers: { 'Content-Type': 'application/json', ...(options.headers || {}) },
    ...options,
  })
  if (!response.ok) {
    throw new APIError(response.status, (await response.text()) || response.statusText)
  }
  return response.status === 204 ? null : response.json()
}

const agentID = (agent) => agent?.agent_id || agent?.id || ''
const taskID = (task) => task?.task_id || task?.id || ''
const newCommandKey = (prefix) => `${prefix}-${crypto.randomUUID?.() || `${Date.now()}-${Math.random()}`}`
const terminalTask = (status) => ['canceled', 'succeeded', 'failed', 'uncertain'].includes(status)
const formatAge = (value) => {
  const milliseconds = Date.now() - Date.parse(value)
  if (!Number.isFinite(milliseconds)) return '未知'
  if (milliseconds < 60_000) return `${Math.max(0, Math.round(milliseconds / 1000))} 秒前`
  return `${Math.max(1, Math.round(milliseconds / 60_000))} 分钟前`
}

function Login({ onLogin }) {
  const [username, setUsername] = useState('owner')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const submit = async (event) => {
    event.preventDefault()
    setError('')
    setSubmitting(true)
    try {
      const session = await api('/api/auth/v1/login', {
        method: 'POST',
        body: JSON.stringify({ username, password }),
      })
      onLogin(session)
    } catch {
      setError('用户名或密码错误')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <main className="login">
      <form onSubmit={submit}>
        <p className="eyebrow">OPENAGENTX</p>
        <h1>指挥台</h1>
        <label htmlFor="username">用户名</label>
        <input id="username" name="username" value={username} onChange={(event) => setUsername(event.target.value)} autoComplete="username" />
        <label htmlFor="password">密码</label>
        <input id="password" name="password" type="password" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete="current-password" />
        {error && <p className="error">{error}</p>}
        <button className="primary" type="submit" disabled={submitting}>
          {submitting ? '登录中...' : '登录'}
        </button>
      </form>
    </main>
  )
}

function App() {
  const [session, setSession] = useState(null)
  const [data, setData] = useState({ agents: [], workers: [], tasks: [], approvals: [], latest_sequence: 0 })
  const [tab, setTab] = useState('command')
  const [browserOnline, setBrowserOnline] = useState(navigator.onLine)
  const [streamState, setStreamState] = useState('connecting')
  const [draft, setDraft] = useState('')
  const [selectedAgent, setSelectedAgent] = useState('')
  const [replyTask, setReplyTask] = useState(null)
  const [error, setError] = useState('')
  const [writing, setWriting] = useState(false)
  const [now, setNow] = useState(Date.now())

  useEffect(() => {
    api('/api/auth/v1/session').then(setSession).catch(() => setSession(false))
    const handleOnline = () => setBrowserOnline(true)
    const handleOffline = () => setBrowserOnline(false)
    addEventListener('online', handleOnline)
    addEventListener('offline', handleOffline)
    if ('serviceWorker' in navigator) navigator.serviceWorker.register('/sw.js').catch(() => {})
    return () => {
      removeEventListener('online', handleOnline)
      removeEventListener('offline', handleOffline)
    }
  }, [])

  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 5_000)
    return () => clearInterval(timer)
  }, [])

  useEffect(() => {
    if (!session || !browserOnline) {
      setStreamState(browserOnline ? 'connecting' : 'offline')
      return undefined
    }

    let disposed = false
    let source
    let refreshTimer
    const refresh = async () => {
      try {
        const overview = await api('/api/observe/v1/overview')
        if (!disposed) setData(overview)
        return overview
      } catch (requestError) {
        if (!disposed && requestError.status === 401) setSession(false)
        else if (!disposed) setError(requestError.message)
        throw requestError
      }
    }

    setStreamState('connecting')
    refresh()
      .then((overview) => {
        if (disposed) return
        const after = Math.max(0, Number(overview.latest_sequence) || 0)
        source = new EventSource(`/api/observe/v1/events/stream?after_sequence=${after}`)
        source.onopen = () => setStreamState('online')
        source.onmessage = () => {
          clearTimeout(refreshTimer)
          refreshTimer = setTimeout(() => refresh().catch(() => {}), 150)
        }
        source.onerror = () => setStreamState('connecting')
      })
      .catch(() => {})

    return () => {
      disposed = true
      clearTimeout(refreshTimer)
      source?.close()
    }
  }, [session, browserOnline])

  const agents = data.agents || []
  const tasks = data.tasks || []
  const workers = data.workers || []
  const approvals = data.approvals || []
  const roles = session?.principal?.roles || []
  const writable = roles.includes('owner') || roles.includes('operator')
  const canWrite = browserOnline && writable && !writing

  const activeWorkers = useMemo(() => {
    const byAgent = new Map()
    workers
      .filter((worker) => ['bootstrapping', 'online', 'degraded', 'draining'].includes(worker.status) && Date.parse(worker.lease_until) > now)
      .sort((left, right) => right.generation - left.generation)
      .forEach((worker) => {
        if (!byAgent.has(worker.agent_id)) byAgent.set(worker.agent_id, worker)
      })
    return byAgent
  }, [workers, now])

  const targetAgent = selectedAgent || agentID(agents[0])
  const organizationID = agents.find((agent) => agentID(agent) === targetAgent)?.organization_id || agents[0]?.organization_id || ''
  const attentionTasks = tasks.filter((task) => ['waiting_input', 'waiting_approval'].includes(task.status))
  const onlineAgents = agents.filter((agent) => activeWorkers.get(agentID(agent))?.status === 'online').length

  const refreshOverview = async () => {
    const overview = await api('/api/observe/v1/overview')
    setData(overview)
  }

  const write = async (path, body, idempotencyKey) => {
    if (!canWrite) return false
    setWriting(true)
    setError('')
    try {
      await api(path, {
        method: 'POST',
        headers: {
          'X-CSRF-Token': session.csrf_token,
          'Idempotency-Key': idempotencyKey,
        },
        body: JSON.stringify(body),
      })
      await refreshOverview()
      return true
    } catch (requestError) {
      setError(requestError.message)
      return false
    } finally {
      setWriting(false)
    }
  }

  const sendInstruction = async () => {
    const content = draft.trim()
    if (!content || !targetAgent) return
    if (replyTask) {
      const key = newCommandKey('message')
      const sent = await write(
        `/api/control/v1/tasks/${taskID(replyTask)}/messages`,
        { meta: { idempotency_key: key, expected_version: replyTask.version }, content },
        key,
      )
      if (sent) {
        setDraft('')
        setReplyTask(null)
      }
      return
    }
    const key = newCommandKey('task')
    const sent = await write(
      '/api/control/v1/tasks',
      {
        meta: { idempotency_key: key },
        target_agent_id: targetAgent,
        organization_id: organizationID,
        dispatch_mode: 'direct',
        content,
      },
      key,
    )
    if (sent) setDraft('')
  }

  const cancelTask = async (task) => {
    const key = newCommandKey('cancel')
    await write(
      `/api/control/v1/tasks/${taskID(task)}/cancel`,
      { meta: { idempotency_key: key, expected_version: task.version } },
      key,
    )
  }

  const decideApproval = async (approval, decision) => {
    const key = newCommandKey('approval')
    await write(
      `/api/control/v1/approvals/${approval.approval_request_id}/decisions`,
      {
        meta: { idempotency_key: key, expected_version: Math.max(1, approval.expected_run_version || 0) },
        decision,
      },
      key,
    )
  }

  const logout = async () => {
    if (!browserOnline) return
    try {
      await api('/api/auth/v1/logout', { method: 'POST', headers: { 'X-CSRF-Token': session.csrf_token } })
      setSession(false)
    } catch (requestError) {
      setError(requestError.message)
    }
  }

  if (session === null) return <div className="loading">连接中...</div>
  if (session === false) return <Login onLogin={setSession} />

  const connectionLabel = !browserOnline ? '离线' : streamState === 'online' ? '在线' : '连接中'

  return (
    <div className="shell">
      <header className="top">
        <div>
          <span className="eyebrow">OPENAGENTX</span>
          <h1>指挥台</h1>
        </div>
        <div className="connection">
          <i className={connectionLabel === '在线' ? '' : 'offline-dot'} />
          {connectionLabel}
          <button className="avatar" aria-label="退出" title="退出" disabled={!browserOnline} onClick={logout}>退</button>
        </div>
      </header>

      {!browserOnline && <div className="offline-banner">当前离线，写操作已暂停</div>}
      {!writable && <div className="readonly-banner">当前账号为只读权限</div>}
      {error && <div className="error-banner">{error}<button onClick={() => setError('')} aria-label="关闭">x</button></div>}

      <main>
        {tab === 'command' && (
          <>
            <section className="hero">
              <div>
                <p className="eyebrow">组织态势</p>
                <h2>{onlineAgents} 个 Agent 在线</h2>
                <p className="muted">{approvals.length} 个审批待处理 · {activeWorkers.size} 个有效 Worker</p>
              </div>
              <button className="primary" onClick={() => setTab('tasks')}>查看任务 <span>→</span></button>
            </section>

            <section className="section">
              <div className="section-head">
                <h3>Agent 状态</h3>
                <button className="text-button" onClick={() => setTab('org')}>组织视图</button>
              </div>
              <div className="agent-grid">
                {agents.map((agent) => {
                  const id = agentID(agent)
                  const worker = activeWorkers.get(id)
                  const activeTask = tasks.find((task) => task.target_agent_id === id && !terminalTask(task.status))
                  const availability = activeTask?.status === 'waiting_input'
                    ? 'waiting_input'
                    : activeTask?.status === 'waiting_approval'
                      ? 'waiting_approval'
                      : activeTask
                        ? 'busy'
                        : 'idle'
                  return (
                    <article className="agent" key={id}>
                      <div className="agent-head">
                        <span className={`status-dot ${worker?.status === 'online' ? 'ready' : 'warn'}`} />
                        <strong>{agent.display_name || id}</strong>
                        <span className="state">{worker?.status || 'offline'} · {availability}</span>
                      </div>
                      <p>{id} · {worker ? `心跳 ${formatAge(worker.last_heartbeat_at)}` : '暂无有效 Worker'}</p>
                      <button className="link" onClick={() => { setSelectedAgent(id); setReplyTask(null); setTab('tasks') }}>
                        进入工作台 <span>↗</span>
                      </button>
                    </article>
                  )
                })}
              </div>
            </section>

            <section className="section">
              <div className="section-head">
                <h3>需要你处理</h3>
                <span className="count">{attentionTasks.length + approvals.length}</span>
              </div>
              {approvals.slice(0, 3).map((approval) => (
                <div className="attention" key={approval.approval_request_id}>
                  <div>
                    <b>任务 {approval.task_id}</b>
                    <p>{approval.mode} · {approval.scope_digest}</p>
                  </div>
                  <div className="attention-actions">
                    <button className="outline" disabled={!canWrite} onClick={() => decideApproval(approval, 'reject')}>拒绝</button>
                    <button className="approve" disabled={!canWrite} onClick={() => decideApproval(approval, 'approve')}>批准</button>
                  </div>
                </div>
              ))}
              {attentionTasks.slice(0, Math.max(0, 3 - approvals.length)).map((task) => (
                <div className="attention" key={taskID(task)}>
                  <div>
                    <b>{task.target_agent_id}</b>
                    <p>{task.content}</p>
                  </div>
                  <button
                    className="outline"
                    disabled={!canWrite || task.status !== 'waiting_input'}
                    onClick={() => { setSelectedAgent(task.target_agent_id); setReplyTask(task); setTab('tasks') }}
                  >
                    {task.status === 'waiting_approval' ? '待审批' : '回复'}
                  </button>
                </div>
              ))}
            </section>
          </>
        )}

        {tab === 'tasks' && (
          <section className="page">
            <div className="page-head">
              <button className="back" onClick={() => setTab('command')} aria-label="返回">←</button>
              <div>
                <p className="eyebrow">工作队列</p>
                <h2>任务</h2>
              </div>
            </div>
            <label className="agent-picker">
              目标 Agent
              <select value={targetAgent} onChange={(event) => { setSelectedAgent(event.target.value); setReplyTask(null) }}>
                {agents.map((agent) => <option value={agentID(agent)} key={agentID(agent)}>{agent.display_name || agentID(agent)}</option>)}
              </select>
            </label>
            <div className="task-list">
              {tasks.filter((task) => !targetAgent || task.target_agent_id === targetAgent).map((task) => (
                <article className="task" key={taskID(task)}>
                  <div className="task-line">
                    <span className="task-id">{taskID(task)}</span>
                    <span className="pill">{task.status}</span>
                  </div>
                  <h3>{task.content}</h3>
                  <p>{task.target_agent_id} · {task.updated_at}</p>
                  <div className="task-actions">
                    {task.status === 'waiting_input' && (
                      <button className="outline" disabled={!canWrite} onClick={() => { setReplyTask(task); setDraft('') }}>回复</button>
                    )}
                    {!terminalTask(task.status) && task.status !== 'cancel_requested' && (
                      <button className="outline danger" disabled={!canWrite} onClick={() => cancelTask(task)}>取消</button>
                    )}
                  </div>
                </article>
              ))}
            </div>
            <div className="composer">
              <div className="composer-label">
                <label htmlFor="command">{replyTask ? `回复任务 ${taskID(replyTask)}` : `向 ${targetAgent || 'Agent'} 发送业务指令`}</label>
                {replyTask && <button className="text-button" onClick={() => setReplyTask(null)}>改为新任务</button>}
              </div>
              <div>
                <input
                  id="command"
                  value={draft}
                  onChange={(event) => setDraft(event.target.value)}
                  placeholder="输入业务指令..."
                  disabled={!canWrite || !targetAgent}
                />
                <button className="send" disabled={!canWrite || !draft.trim() || !targetAgent} onClick={sendInstruction} aria-label="发送">↑</button>
              </div>
            </div>
          </section>
        )}

        {tab === 'org' && (
          <section className="page">
            <div className="page-head">
              <button className="back" onClick={() => setTab('command')} aria-label="返回">←</button>
              <div>
                <p className="eyebrow">组织</p>
                <h2>责任链</h2>
              </div>
            </div>
            <div className="org-tree">
              <div className="org-node root"><b>指挥者</b><span>{session.principal.username}</span></div>
              {agents.map((agent) => (
                <div className="org-node" key={agentID(agent)}>
                  <b>{agent.display_name || agentID(agent)}</b>
                  <span>{agent.status} · {agentID(agent)}</span>
                </div>
              ))}
            </div>
          </section>
        )}
      </main>

      <nav className="bottom" aria-label="主导航">
        {[
          ['command', '⌂', '指挥'],
          ['tasks', '▣', '任务'],
          ['org', '⌘', '组织'],
        ].map(([key, icon, label]) => (
          <button className={tab === key ? 'active' : ''} onClick={() => setTab(key)} key={key}>
            <span>{icon}</span>{label}
          </button>
        ))}
      </nav>
    </div>
  )
}

createRoot(document.getElementById('root')).render(<App />)
