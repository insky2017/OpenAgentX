import React, { useEffect, useMemo, useRef, useState } from 'react'
import { createRoot } from 'react-dom/client'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import {
  isCurrentObservation,
  mergeHistoryTaskDetail,
  mergeLiveTaskDetail,
  sseResumeAfter,
} from './task-observation-state.js'
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

const isStandalone = () => window.matchMedia?.('(display-mode: standalone)').matches || navigator.standalone === true
const isIOS = () => /iphone|ipad|ipod/i.test(navigator.userAgent) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1)

const safeUrl = (url) => {
  if (!url) return ''
  try {
    const parsed = new URL(url, window.location.origin)
    if (parsed.protocol === 'http:' || parsed.protocol === 'https:') return parsed.href
    if (parsed.protocol === 'mailto:') return parsed.href
  } catch {
    return ''
  }
  return ''
}

function MarkdownContent({ value, compact = false }) {
  const [raw, setRaw] = useState(false)
  const source = typeof value === 'string' && value ? value : '暂无内容'
  if (raw) {
    return (
      <div className={`markdown-source ${compact ? 'compact' : ''}`}>
        <button className="text-button markdown-toggle" type="button" onClick={() => setRaw(false)}>返回渲染</button>
        <pre>{source}</pre>
      </div>
    )
  }
  return (
    <div className={`markdown-body ${compact ? 'compact' : ''}`}>
      <div className="markdown-tools">
        <button className="text-button" type="button" onClick={() => setRaw(true)}>查看原文</button>
        <button className="text-button" type="button" onClick={() => navigator.clipboard?.writeText(source)}>复制原文</button>
      </div>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        urlTransform={safeUrl}
        components={{
          a: ({ node, ...props }) => <a {...props} target="_blank" rel="noreferrer noopener" />,
          img: () => <span className="blocked-media">图片已隐藏</span>,
          pre: ({ children }) => <pre className="markdown-pre">{children}</pre>,
          code: ({ inline, children, ...props }) => inline
            ? <code {...props}>{children}</code>
            : <code {...props}>{children}</code>,
        }}
      >
        {source}
      </ReactMarkdown>
    </div>
  )
}

const eventLabel = (type) => ({
  'task.created': '任务创建',
  'task.running': '任务开始运行',
  'task.waiting_input': '等待输入',
  'task.waiting_approval': '等待审批',
  'task.succeeded': '任务成功',
  'task.failed': '任务失败',
  'task.canceled': '任务已取消',
  'run_attempt.started': 'Runtime 启动',
  'run_attempt.finished': 'Runtime 结束',
  'run_attempt.uncertain': '结果待核对',
  'message.created': '收到消息',
  'approval.created': '审批请求',
  'approval.decided': '审批决定',
  'worker.heartbeat': 'Worker 心跳',
}[type] || type)

function RunTimeline({ detail, onLoadMore, loadingMore }) {
  const events = [...(detail.events || [])].sort((left, right) => left.sequence - right.sequence)
  const runs = detail.run_attempts || []
  return (
    <div className="run-view">
      <div className="run-summary">
        {runs.map((run) => <article className="run-card" key={run.run_id}><div><span className={`status-dot ${run.status?.startsWith('succeed') ? 'ready' : run.status?.startsWith('fail') ? 'error-dot' : 'busy'}`} /><strong>{run.adapter_id} / {run.backend_id}</strong></div><p>{run.model} · {run.status}</p><small>Worker {run.worker_instance_id} · spec v{run.execution_spec_version}</small><time>{new Date(run.started_at).toLocaleString()}</time></article>)}
        {!runs.length && <div className="empty-state">尚无可观察的 RunAttempt</div>}
      </div>
      {detail.has_older_events && <button className="load-more history-more" type="button" onClick={onLoadMore} disabled={loadingMore}>{loadingMore ? '加载中...' : '加载更早事件'}</button>}
      <ol className="timeline" aria-label="运行时间线">
        {events.map((event) => <li key={`${event.sequence}-${event.event_id}`}><span className="timeline-marker" /><div><strong>{eventLabel(event.event_type)}</strong><p>{event.event_type} · {event.aggregate_type}</p><time>{new Date(event.created_at).toLocaleString()} · #{event.sequence}</time></div></li>)}
        {!events.length && <li className="empty-state">暂无运行事件</li>}
      </ol>
    </div>
  )
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
  const [tab, setTab] = useState(() => {
    const params = new URLSearchParams(window.location.search)
    return params.get('view') === 'tasks' || params.has('task') ? 'tasks' : 'command'
  })
  const [browserOnline, setBrowserOnline] = useState(navigator.onLine)
  const [streamState, setStreamState] = useState('connecting')
  const [draft, setDraft] = useState('')
  const [selectedAgent, setSelectedAgent] = useState('')
  const [replyTask, setReplyTask] = useState(null)
  const [error, setError] = useState('')
  const [writing, setWriting] = useState(false)
  const [selectedTaskID, setSelectedTaskID] = useState(() => new URLSearchParams(window.location.search).get('task') || '')
  const [taskDetail, setTaskDetail] = useState(null)
  const [taskDetailState, setTaskDetailState] = useState('idle')
  const [taskQuery, setTaskQuery] = useState('')
  const [taskStatusFilter, setTaskStatusFilter] = useState('')
  const [taskAgentFilter, setTaskAgentFilter] = useState('')
  const [taskTimeFilter, setTaskTimeFilter] = useState('')
  const [taskTimeAnchor, setTaskTimeAnchor] = useState(Date.now())
  const [taskPage, setTaskPage] = useState({ tasks: [], next_cursor: '', has_more: false })
  const [taskListState, setTaskListState] = useState('idle')
  const [taskListError, setTaskListError] = useState('')
  const [loadingMoreTasks, setLoadingMoreTasks] = useState(false)
  const [taskRefreshTick, setTaskRefreshTick] = useState(0)
  const [taskView, setTaskView] = useState('content')
  const [newOutput, setNewOutput] = useState(false)
  const [loadingMoreEvents, setLoadingMoreEvents] = useState(false)
  const lastSequenceRef = useRef(0)
  const selectedTaskRef = useRef('')
  const taskDetailRef = useRef(null)
  const detailSelectionRef = useRef(0)
  const detailInitialRequestRef = useRef(0)
  const detailHistoryRequestRef = useRef(0)
  const detailLiveRequestRef = useRef(0)
  const detailCatchUpRunningRef = useRef(false)
  const detailCatchUpPendingRef = useRef(false)
  const taskListRequestRef = useRef(0)
  const taskListRefreshRef = useRef(() => {})
  const detailCatchUpRef = useRef(() => {})
  const replayRetryRef = useRef(() => {})
  const detailScrollRef = useRef(null)
  const readingLatestRef = useRef(true)
  const taskViewRef = useRef('content')
  const returnFocusRef = useRef(null)
  const [now, setNow] = useState(Date.now())
  const deferredInstallPrompt = useRef(null)
  const [installState, setInstallState] = useState('hidden')
  const [networkState, setNetworkState] = useState({ profiles: [], bindings: [] })
  const [runtimeOptions, setRuntimeOptions] = useState([])
  const [profileForm, setProfileForm] = useState({ profile_id: '', mode: 'only_http_proxy', host: '', port: '8080', config_file: '', secret_ref: '' })
  const [networkLoading, setNetworkLoading] = useState(false)

  useEffect(() => {
    api('/api/auth/v1/session').then(setSession).catch(() => setSession(false))
    const handleOnline = () => setBrowserOnline(true)
    const handleOffline = () => setBrowserOnline(false)
    const handleBeforeInstallPrompt = (event) => {
      event.preventDefault()
      deferredInstallPrompt.current = event
      setInstallState('available')
    }
    const handleAppInstalled = () => {
      deferredInstallPrompt.current = null
      setInstallState('installed')
    }
    addEventListener('online', handleOnline)
    addEventListener('offline', handleOffline)
    addEventListener('beforeinstallprompt', handleBeforeInstallPrompt)
    addEventListener('appinstalled', handleAppInstalled)
    if (isStandalone()) setInstallState('installed')
    else if (isIOS()) setInstallState('manual')
    if ('serviceWorker' in navigator) navigator.serviceWorker.register('/sw.js').catch(() => {})
    return () => {
      removeEventListener('online', handleOnline)
      removeEventListener('offline', handleOffline)
      removeEventListener('beforeinstallprompt', handleBeforeInstallPrompt)
      removeEventListener('appinstalled', handleAppInstalled)
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
        if (!disposed) {
          setData(overview)
        }
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
        const after = sseResumeAfter(lastSequenceRef.current, overview.latest_sequence)
        source = new EventSource(`/api/observe/v1/events/stream?after_sequence=${after}`)
        source.onmessage = (event) => {
          const sequence = Number(event.lastEventId) || 0
          if (sequence && sequence <= lastSequenceRef.current) return
          if (sequence) lastSequenceRef.current = sequence
          clearTimeout(refreshTimer)
          refreshTimer = setTimeout(async () => {
            await refresh().catch(() => {})
            taskListRefreshRef.current()
            detailCatchUpRef.current()
          }, 150)
        }
        source.onopen = () => {
          setStreamState('online')
          detailCatchUpRef.current()
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

  useEffect(() => {
    if (!session || !browserOnline || tab !== 'runtime') return
    let disposed = false
    setNetworkLoading(true)
    Promise.all([
      api('/api/observe/v1/network-profiles'),
      api('/api/observe/v1/execution-options'),
    ]).then(([profiles, options]) => {
      if (!disposed) {
        setNetworkState(profiles || { profiles: [], bindings: [] })
        setRuntimeOptions(options?.backends || [])
      }
    }).catch((requestError) => { if (!disposed) setError(requestError.message) })
      .finally(() => { if (!disposed) setNetworkLoading(false) })
    return () => { disposed = true }
  }, [session, browserOnline, tab])

  const agents = data.agents || []
  const overviewTasks = data.tasks || []
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
  const attentionTasks = overviewTasks.filter((task) => ['waiting_input', 'waiting_approval'].includes(task.status))
  const onlineAgents = agents.filter((agent) => activeWorkers.get(agentID(agent))?.status === 'online').length
  const tasks = taskPage.tasks || []
  const selectedTask = taskDetail?.task || tasks.find((task) => taskID(task) === selectedTaskID)
  const selectTask = (task, trigger) => {
    const id = typeof task === 'string' ? task : taskID(task)
    if (id === selectedTaskRef.current) return
    detailSelectionRef.current += 1
    selectedTaskRef.current = id
    taskDetailRef.current = null
    if (trigger) returnFocusRef.current = trigger
    setSelectedTaskID(id)
    setTaskDetail(null)
    setReplyTask(null)
    setTaskView('content')
    setNewOutput(false)
  }

  const closeTaskDetail = () => {
    detailSelectionRef.current += 1
    const selection = detailSelectionRef.current
    selectedTaskRef.current = ''
    detailCatchUpPendingRef.current = false
    setSelectedTaskID('')
    setTaskDetail(null)
    setTaskDetailState('idle')
    requestAnimationFrame(() => {
      if (!isCurrentObservation(
        { taskID: '', selection },
        { taskID: selectedTaskRef.current, selection: detailSelectionRef.current },
      )) return
      const target = returnFocusRef.current?.isConnected ? returnFocusRef.current : document.querySelector('.task-list')
      target?.focus?.()
    })
  }

  const refreshOverview = async () => {
    const overview = await api('/api/observe/v1/overview')
    setData(overview)
  }

  const taskListURL = (cursor = '') => {
    const params = new URLSearchParams({ limit: '50' })
    if (taskAgentFilter) params.set('agent_id', taskAgentFilter)
    if (taskStatusFilter) params.set('status', taskStatusFilter)
    if (taskQuery.trim()) params.set('query', taskQuery.trim())
    if (taskTimeFilter) {
      const milliseconds = { '1h': 3_600_000, '24h': 86_400_000, '7d': 604_800_000 }[taskTimeFilter]
      params.set('updated_after', new Date(taskTimeAnchor - milliseconds).toISOString())
    }
    if (cursor) params.set('cursor', cursor)
    return `/api/observe/v1/tasks?${params}`
  }

  const loadTaskPage = async (cursor = '', append = false) => {
    const requestID = ++taskListRequestRef.current
    if (append) setLoadingMoreTasks(true)
    else {
      setTaskListState('loading')
      setTaskListError('')
    }
    try {
      const page = await api(taskListURL(cursor))
      if (requestID !== taskListRequestRef.current) return
      setTaskPage((current) => ({
        ...page,
        tasks: append ? Array.from(new Map([...(current.tasks || []), ...(page.tasks || [])].map((task) => [task.id, task])).values()) : (page.tasks || []),
      }))
      setTaskListState('ready')
    } catch (requestError) {
      if (requestID !== taskListRequestRef.current) return
      if (requestError.status === 401) setSession(false)
      else {
        setTaskListState(requestError.status === 403 ? 'forbidden' : 'error')
        setTaskListError(requestError.message)
      }
    } finally {
      if (requestID === taskListRequestRef.current) setLoadingMoreTasks(false)
    }
  }

  const loadTaskDetail = async (id) => {
    if (!id) {
      setTaskDetail(null)
      setTaskDetailState('idle')
      return
    }
    const selection = detailSelectionRef.current
    const requestID = ++detailInitialRequestRef.current
    setTaskDetailState('loading')
    try {
      const detail = await api(`/api/observe/v1/tasks/${encodeURIComponent(id)}?limit=100`)
      if (requestID !== detailInitialRequestRef.current || selection !== detailSelectionRef.current || selectedTaskRef.current !== id) return
      taskDetailRef.current = detail
      setTaskDetail(detail)
      setTaskDetailState('ready')
      setNewOutput(false)
      if (lastSequenceRef.current > Number(detail.snapshot_sequence || 0)) {
        requestAnimationFrame(() => {
          if (isCurrentObservation(
            { taskID: id, selection, request: requestID },
            { taskID: selectedTaskRef.current, selection: detailSelectionRef.current, request: detailInitialRequestRef.current },
          )) detailCatchUpRef.current()
        })
      }
      return detail
    } catch (requestError) {
	  if (requestID !== detailInitialRequestRef.current || selection !== detailSelectionRef.current || selectedTaskRef.current !== id) return
      if (requestError.status === 401) setSession(false)
      else setTaskDetailState(requestError.status === 404 ? 'missing' : requestError.status === 403 ? 'forbidden' : 'error')
    }
  }

  const catchUpTaskDetail = async () => {
    const id = selectedTaskRef.current
    const current = taskDetailRef.current
    if (!id || !current) return
    if (detailCatchUpRunningRef.current) {
      detailCatchUpPendingRef.current = true
      return
    }
    const selection = detailSelectionRef.current
    const requestID = ++detailLiveRequestRef.current
    detailCatchUpRunningRef.current = true
    let cursor = Number(current.live_after_sequence) || 0
    try {
      for (;;) {
        const incoming = await api(`/api/observe/v1/tasks/${encodeURIComponent(id)}?after_sequence=${cursor}&limit=499`)
        if (requestID !== detailLiveRequestRef.current || selection !== detailSelectionRef.current || selectedTaskRef.current !== id) return
        const nextCursor = Number(incoming.live_after_sequence) || 0
        if (nextCursor < cursor || (incoming.has_more_live_events && nextCursor === cursor)) throw new Error('事件回放游标未前进')
        const latest = taskDetailRef.current
        if (!latest || latest.task?.id !== id) return
        const previousEventCount = latest.events?.length || 0
        const merged = mergeLiveTaskDetail(latest, incoming)
        cursor = nextCursor
        taskDetailRef.current = merged
        setTaskDetail(merged)
        if ((merged.events?.length || 0) > previousEventCount) {
          if (taskViewRef.current === 'run' && readingLatestRef.current) {
            requestAnimationFrame(() => {
              if (!isCurrentObservation(
                { taskID: id, selection, request: requestID, view: 'run' },
                { taskID: selectedTaskRef.current, selection: detailSelectionRef.current, request: detailLiveRequestRef.current, view: taskViewRef.current },
              )) return
              detailScrollRef.current?.scrollTo({ top: detailScrollRef.current.scrollHeight })
            })
          } else setNewOutput(true)
        }
        if (!incoming.has_more_live_events) break
      }
      setTaskDetailState('ready')
    } catch (requestError) {
      if (requestID !== detailLiveRequestRef.current || selection !== detailSelectionRef.current || selectedTaskRef.current !== id) return
      if (requestError.status === 401) setSession(false)
      else if (requestError.status === 403) setTaskDetailState('forbidden')
      else if (requestError.status === 404) setTaskDetailState('missing')
      else if (requestError.status === 409) await loadTaskDetail(id)
      else {
        replayRetryRef.current = () => detailCatchUpRef.current()
        setTaskDetailState('replay_error')
      }
    } finally {
      detailCatchUpRunningRef.current = false
      if (detailCatchUpPendingRef.current) {
        detailCatchUpPendingRef.current = false
        requestAnimationFrame(() => detailCatchUpRef.current())
      }
    }
  }

  taskListRefreshRef.current = () => setTaskRefreshTick((value) => value + 1)
  detailCatchUpRef.current = catchUpTaskDetail
  taskViewRef.current = taskView

  useEffect(() => {
    selectedTaskRef.current = selectedTaskID
    taskDetailRef.current = taskDetail
  }, [selectedTaskID, taskDetail])

  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    if (selectedTaskID) params.set('task', selectedTaskID)
    else params.delete('task')
    if (tab === 'tasks') params.set('view', 'tasks')
    else params.delete('view')
    const query = params.toString()
    window.history.replaceState(null, '', `${window.location.pathname}${query ? `?${query}` : ''}`)
  }, [selectedTaskID, tab])

  useEffect(() => {
    if (selectedTaskID) loadTaskDetail(selectedTaskID)
    else setTaskDetail(null)
  }, [selectedTaskID])

  useEffect(() => {
    if (!session || !browserOnline || tab !== 'tasks') return undefined
    const timer = setTimeout(() => loadTaskPage(), taskQuery ? 250 : 0)
    return () => clearTimeout(timer)
  }, [session, browserOnline, tab, taskAgentFilter, taskStatusFilter, taskTimeFilter, taskTimeAnchor, taskQuery, taskRefreshTick])

  useEffect(() => {
    if (taskView !== 'run') return
    const taskID = selectedTaskRef.current
    const selection = detailSelectionRef.current
    requestAnimationFrame(() => {
      if (!isCurrentObservation(
        { taskID, selection, view: 'run' },
        { taskID: selectedTaskRef.current, selection: detailSelectionRef.current, view: taskViewRef.current },
      )) return
      const scroll = detailScrollRef.current
      if (scroll) scroll.scrollTop = scroll.scrollHeight
      readingLatestRef.current = true
      setNewOutput(false)
    })
  }, [taskView, selectedTaskID])

  useEffect(() => {
    if (!selectedTaskID || !taskDetail?.task || !['ready', 'filtered'].includes(taskDetailState)) return
    const task = taskDetail.task
    const threshold = taskTimeFilter ? taskTimeAnchor - ({ '1h': 3_600_000, '24h': 86_400_000, '7d': 604_800_000 }[taskTimeFilter]) : 0
    const text = taskQuery.trim().toLowerCase()
    const matches = (!taskAgentFilter || task.target_agent_id === taskAgentFilter) &&
      (!taskStatusFilter || task.status === taskStatusFilter) &&
      (!threshold || Date.parse(task.updated_at) >= threshold) &&
      (!text || `${task.id} ${task.target_agent_id} ${task.content}`.toLowerCase().includes(text))
    setTaskDetailState(matches ? 'ready' : 'filtered')
  }, [selectedTaskID, taskDetail?.task?.version, taskAgentFilter, taskStatusFilter, taskTimeFilter, taskTimeAnchor, taskQuery])

  const loadMoreEvents = async () => {
    const id = selectedTaskRef.current
    const before = Number(taskDetailRef.current?.history_before_sequence) || 0
    if (!id || !before || loadingMoreEvents) return
    const selection = detailSelectionRef.current
    const requestID = ++detailHistoryRequestRef.current
    const scroll = detailScrollRef.current
    const previousHeight = scroll?.scrollHeight || 0
    const previousTop = scroll?.scrollTop || 0
    setLoadingMoreEvents(true)
    try {
      const incoming = await api(`/api/observe/v1/tasks/${encodeURIComponent(id)}?before_sequence=${before}&limit=100`)
      if (requestID !== detailHistoryRequestRef.current || selection !== detailSelectionRef.current || selectedTaskRef.current !== id) return
      const current = taskDetailRef.current
      if (!current || current.task?.id !== id) return
      const merged = mergeHistoryTaskDetail(current, incoming)
      taskDetailRef.current = merged
      setTaskDetail(merged)
      setTaskDetailState('ready')
      requestAnimationFrame(() => {
        if (!isCurrentObservation(
          { taskID: id, selection, request: requestID, view: 'run' },
          { taskID: selectedTaskRef.current, selection: detailSelectionRef.current, request: detailHistoryRequestRef.current, view: taskViewRef.current },
        )) return
        const currentScroll = detailScrollRef.current
        if (currentScroll) currentScroll.scrollTop = previousTop + currentScroll.scrollHeight - previousHeight
      })
    } catch (requestError) {
      if (requestID !== detailHistoryRequestRef.current || selection !== detailSelectionRef.current || selectedTaskRef.current !== id) return
      if (requestError.status === 401) setSession(false)
      else if (requestError.status === 403) setTaskDetailState('forbidden')
      else if (requestError.status === 404) setTaskDetailState('missing')
      else {
        replayRetryRef.current = () => loadMoreEvents()
        setTaskDetailState('replay_error')
      }
    } finally {
      if (requestID === detailHistoryRequestRef.current) setLoadingMoreEvents(false)
    }
  }

  const jumpToLatest = () => {
    const id = selectedTaskRef.current
    const selection = detailSelectionRef.current
    setNewOutput(false)
    setTaskView('run')
    requestAnimationFrame(() => {
      if (!isCurrentObservation(
        { taskID: id, selection, view: 'run' },
        { taskID: selectedTaskRef.current, selection: detailSelectionRef.current, view: taskViewRef.current },
      )) return
      readingLatestRef.current = true
      detailScrollRef.current?.scrollTo({ top: detailScrollRef.current.scrollHeight, behavior: 'smooth' })
    })
  }

  const installPWA = async () => {
    const prompt = deferredInstallPrompt.current
    if (!prompt) return
    try {
      await prompt.prompt()
      await prompt.userChoice
    } catch {
      // The browser owns the install dialog; a dismissed or unavailable
      // prompt must not be presented as an installed app.
    } finally {
      deferredInstallPrompt.current = null
      setInstallState('hidden')
    }
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
      taskListRefreshRef.current()
      if (selectedTaskRef.current) await catchUpTaskDetail()
      return true
    } catch (requestError) {
      setError(requestError.message)
      return false
    } finally {
      setWriting(false)
    }
  }

  const networkWrite = async (path, body) => {
    if (!canWrite) return
    setWriting(true)
    setError('')
    const key = newCommandKey('network')
    try {
      await api(path, {
        method: 'POST',
        headers: { 'X-CSRF-Token': session.csrf_token, 'Idempotency-Key': key },
        body: JSON.stringify({ ...body, meta: { idempotency_key: key, ...(body.meta || {}) } }),
      })
      const [profiles, options] = await Promise.all([api('/api/observe/v1/network-profiles'), api('/api/observe/v1/execution-options')])
      setNetworkState(profiles || { profiles: [], bindings: [] })
      setRuntimeOptions(options?.backends || [])
    } catch (requestError) {
      setError(requestError.message)
    } finally {
      setWriting(false)
    }
  }

  const createProfile = async (event) => {
    event.preventDefault()
    const form = profileForm
    if (!form.profile_id.trim() || !form.host.trim() || !form.port) return
    await networkWrite('/api/control/v1/network-profiles', {
      profile_id: form.profile_id.trim(), mode: form.mode, host: form.host.trim(), port: Number(form.port), config_file: form.config_file.trim() || undefined, secret_ref: form.secret_ref.trim() || undefined,
    })
    setProfileForm({ profile_id: '', mode: 'only_http_proxy', host: '', port: '8080', config_file: '', secret_ref: '' })
  }

  const publishProfile = (profile) => networkWrite(`/api/control/v1/network-profiles/${encodeURIComponent(profile.profile_id)}/publish`, { meta: { expected_version: profile.version } })
  const bindProfile = (option, profile) => networkWrite('/api/control/v1/network-bindings', {
    agent_id: option.agent_id, backend_id: option.backend.backend_id, profile_id: profile.profile_id, profile_version: profile.version,
    meta: { expected_version: 0 },
  })

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
          {installState === 'available' && <button className="install-button" type="button" onClick={installPWA}>⇩ 安装</button>}
          {installState === 'manual' && <span className="install-hint" role="status">可从浏览器菜单添加到主屏幕</span>}
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
          <section className="workbench" aria-label="任务工作台">
            <div className={`task-pane ${selectedTaskID ? 'has-selection' : ''}`}>
              <div className="page-head workbench-head">
                <button className="back" onClick={() => setTab('command')} aria-label="返回">←</button>
                <div>
                  <p className="eyebrow">观察窗口</p>
                  <h2>任务</h2>
                </div>
                <span className="connection-note">{taskListState === 'loading' ? '加载中' : `${tasks.length}${taskPage.has_more ? '+' : ''} 项`}</span>
              </div>
              <div className="task-filters">
                <label className="search-field">
                  <span className="sr-only">搜索任务</span>
                  <input value={taskQuery} onChange={(event) => setTaskQuery(event.target.value)} placeholder="搜索任务、Agent 或内容" />
                </label>
                <select aria-label="按 Agent 筛选" value={taskAgentFilter} onChange={(event) => setTaskAgentFilter(event.target.value)}>
                  <option value="">全部 Agent</option>
                  {agents.map((agent) => <option value={agentID(agent)} key={agentID(agent)}>{agent.display_name || agentID(agent)}</option>)}
                </select>
                <select aria-label="按状态筛选" value={taskStatusFilter} onChange={(event) => setTaskStatusFilter(event.target.value)}>
                  <option value="">全部状态</option>
                  {['queued', 'dispatching', 'running', 'waiting_input', 'waiting_approval', 'cancel_requested', 'succeeded', 'failed', 'canceled', 'uncertain'].map((status) => <option value={status} key={status}>{status}</option>)}
                </select>
                <select aria-label="按更新时间筛选" value={taskTimeFilter} onChange={(event) => { setTaskTimeFilter(event.target.value); setTaskTimeAnchor(Date.now()) }}>
                  <option value="">全部时间</option>
                  <option value="1h">最近 1 小时</option>
                  <option value="24h">最近 24 小时</option>
                  <option value="7d">最近 7 天</option>
                </select>
              </div>
              <div className="task-list" role="list" aria-label="任务列表" tabIndex={-1} aria-busy={taskListState === 'loading'}>
                {tasks.map((task) => {
                  const id = taskID(task)
                  return (
                    <button className={`task-row ${id === selectedTaskID ? 'selected' : ''}`} key={id} role="listitem" aria-selected={id === selectedTaskID} onClick={(event) => selectTask(task, event.currentTarget)}>
                      <span className="task-row-main">
                        <strong>{task.summary || id}</strong>
                        <small>{id} · {task.target_agent_id}</small>
                      </span>
                      <span className={`pill status-${task.status}`}>{task.status}</span>
                      <time dateTime={task.updated_at}>{formatAge(task.updated_at)}</time>
                    </button>
                  )
                })}
                {taskListState === 'loading' && !tasks.length && <div className="empty-state" role="status">正在加载任务...</div>}
                {taskListState === 'forbidden' && <div className="empty-state error-state" role="alert">当前账号无权浏览任务</div>}
                {taskListState === 'error' && <div className="empty-state error-state" role="alert"><p>任务列表加载失败</p>{taskListError && <small>{taskListError}</small>}<button className="outline" type="button" onClick={() => loadTaskPage()}>重试</button></div>}
                {taskListState === 'ready' && !tasks.length && <div className="empty-state">{taskAgentFilter || taskStatusFilter || taskTimeFilter || taskQuery.trim() ? '没有符合筛选条件的任务' : '暂无任务'}</div>}
                {taskListState === 'loading' && tasks.length > 0 && <div className="inline-loading" role="status">正在更新任务列表...</div>}
              </div>
              {taskPage.has_more && <button className="load-more" type="button" disabled={loadingMoreTasks} onClick={() => loadTaskPage(taskPage.next_cursor, true)}>{loadingMoreTasks ? '加载中...' : '加载更多任务'}</button>}
              <div className="composer">
                <div className="composer-label">
                  <label htmlFor="command">{replyTask ? `回复任务 ${taskID(replyTask)}` : `向 ${targetAgent || 'Agent'} 发送业务指令`}</label>
                  {replyTask && <button className="text-button" type="button" onClick={() => setReplyTask(null)}>改为新任务</button>}
                </div>
                <div>
                  <textarea id="command" value={draft} onChange={(event) => setDraft(event.target.value)} placeholder="输入业务指令..." disabled={!canWrite || !targetAgent} rows={2} />
                  <button className="send" type="button" disabled={!canWrite || !draft.trim() || !targetAgent} onClick={sendInstruction} aria-label="发送">↑</button>
                </div>
              </div>
            </div>
            <div className={`detail-pane ${selectedTaskID ? 'open' : ''}`} aria-live="polite">
              {!selectedTaskID && <div className="detail-empty"><span className="detail-icon">◎</span><h3>选择一个任务</h3><p>从左侧列表打开详情，查看运行事实与结果。</p></div>}
              {selectedTaskID && taskDetailState === 'loading' && <div className="detail-empty" role="status"><p>正在加载任务详情...</p></div>}
              {selectedTaskID && taskDetailState === 'missing' && <div className="detail-empty"><h3>任务不存在</h3><p>任务已删除或该链接已失效。</p><button className="outline" type="button" onClick={closeTaskDetail}>返回列表</button></div>}
              {selectedTaskID && taskDetailState === 'forbidden' && <div className="detail-empty"><h3>权限不足</h3><p>当前账号无权查看这个任务。</p><button className="outline" type="button" onClick={closeTaskDetail}>返回列表</button></div>}
              {selectedTaskID && taskDetailState === 'error' && <div className="detail-empty"><h3>详情加载失败</h3><p>无法读取任务快照，请稍后重试。</p><button className="outline" type="button" onClick={() => loadTaskDetail(selectedTaskID)}>重试</button></div>}
              {selectedTaskID && taskDetailState === 'filtered' && <div className="detail-empty"><h3>任务不再匹配</h3><p>任务状态或更新时间已超出当前筛选范围。</p><button className="outline" type="button" onClick={closeTaskDetail}>返回筛选结果</button></div>}
              {selectedTaskID && taskDetail && ['ready', 'replay_error'].includes(taskDetailState) && (
                <>
                  <div className="detail-head">
                    <button className="mobile-back" type="button" onClick={closeTaskDetail} aria-label="返回任务列表">←</button>
                    <div className="detail-title">
                      <span className="eyebrow">{taskDetail.task.target_agent_id}</span>
                      <h2>{taskDetail.task.content?.split('\n')[0] || taskDetail.task.id}</h2>
                      <p>{taskDetail.task.id} · 更新于 {formatAge(taskDetail.task.updated_at)}</p>
                    </div>
                    <span className={`pill status-${taskDetail.task.status}`}>{taskDetail.task.status}</span>
                  </div>
                  <div className="detail-actions">
                    {taskDetail.task.status === 'waiting_input' && <button className="outline" type="button" disabled={!canWrite} onClick={() => { setReplyTask(taskDetail.task); setDraft(''); document.getElementById('command')?.focus() }}>回复</button>}
                    {!terminalTask(taskDetail.task.status) && taskDetail.task.status !== 'cancel_requested' && <button className="outline danger" type="button" disabled={!canWrite} onClick={() => cancelTask(taskDetail.task)}>取消任务</button>}
                    {newOutput && <button className="new-output" type="button" onClick={jumpToLatest}>跳到最新</button>}
                  </div>
                  {taskDetailState === 'replay_error' && <div className="replay-error" role="alert"><span>事件回放失败，当前内容可能不是最新。</span><button className="outline" type="button" onClick={() => replayRetryRef.current()}>重试</button></div>}
                  <div className="detail-tabs" role="tablist" aria-label="任务详情视图">
                    {['content', 'conversation', 'run', 'result'].map((view) => <button type="button" role="tab" aria-selected={taskView === view} className={taskView === view ? 'active' : ''} onClick={() => setTaskView(view)} key={view}>{({ content: '内容', conversation: '对话', run: '运行', result: '结果' })[view]}</button>)}
                  </div>
                  <div className="detail-scroll" ref={detailScrollRef} onScroll={(event) => { const node = event.currentTarget; readingLatestRef.current = node.scrollHeight - node.scrollTop - node.clientHeight < 48; if (readingLatestRef.current) setNewOutput(false) }}>
                    {taskView === 'content' && <MarkdownContent value={taskDetail.task.content} />}
                    {taskView === 'conversation' && <div className="conversation-list">{(taskDetail.messages || []).map((message) => <article className="message-item" key={message.id}><div><strong>{message.sender_principal_id}</strong><time>{message.created_at}</time></div><MarkdownContent value={message.content} compact /></article>)}{!taskDetail.messages?.length && <div className="empty-state">暂无对话消息</div>}</div>}
                    {taskView === 'run' && <RunTimeline detail={taskDetail} onLoadMore={loadMoreEvents} loadingMore={loadingMoreEvents} />}
                    {taskView === 'result' && <div className="result-view">{taskDetail.task.result && <MarkdownContent value={taskDetail.task.result} />}{taskDetail.task.error && <div className="diagnostic"><strong>执行诊断</strong><pre>{taskDetail.task.error}</pre></div>}{!taskDetail.task.result && !taskDetail.task.error && <div className="empty-state">任务尚未产生最终结果</div>}</div>}
                  </div>
                </>
              )}
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

        {tab === 'runtime' && (
          <section className="page runtime-page">
            <div className="page-head">
              <button className="back" onClick={() => setTab('command')} aria-label="返回">←</button>
              <div><p className="eyebrow">Runtime</p><h2>网络配置</h2></div>
              <span className="connection-note">{networkLoading ? '同步中' : `${networkState.profiles?.length || 0} 个方案`}</span>
            </div>
            <form className="profile-form" onSubmit={createProfile}>
              <h3>创建代理方案草稿</h3>
              <div className="profile-grid">
                <input aria-label="方案 ID" placeholder="方案 ID" value={profileForm.profile_id} onChange={(e) => setProfileForm({ ...profileForm, profile_id: e.target.value })} />
                <select aria-label="代理模式" value={profileForm.mode} onChange={(e) => setProfileForm({ ...profileForm, mode: e.target.value })}><option value="only_http_proxy">HTTP 代理</option><option value="only_socks5">SOCKS5 代理</option></select>
                <input aria-label="代理主机" placeholder="代理主机" value={profileForm.host} onChange={(e) => setProfileForm({ ...profileForm, host: e.target.value })} />
                <input aria-label="代理端口" type="number" min="1" max="65535" placeholder="端口" value={profileForm.port} onChange={(e) => setProfileForm({ ...profileForm, port: e.target.value })} />
                <input aria-label="Worker 配置文件" placeholder="Worker 配置文件绝对路径" value={profileForm.config_file} onChange={(e) => setProfileForm({ ...profileForm, config_file: e.target.value })} />
                <input aria-label="Secret 引用" placeholder="Secret 引用（可选）" value={profileForm.secret_ref} onChange={(e) => setProfileForm({ ...profileForm, secret_ref: e.target.value })} />
                <button className="primary" type="submit" disabled={!canWrite}>保存草稿</button>
              </div>
            </form>
            <div className="profile-list">
              {(networkState.profiles || []).map((profile) => <article className="profile-card" key={`${profile.profile_id}-${profile.version}`}>
                <div><strong>{profile.profile_id}</strong><span className={`pill ${profile.status === 'published' ? 'status-succeeded' : 'status-queued'}`}>{profile.status} · v{profile.version}</span></div>
                <p>{profile.mode} · {profile.host}:{profile.port}{profile.secret_ref ? ` · Secret ${profile.secret_ref}` : ''}</p>
                <div className="profile-actions">{profile.status === 'draft' && <button className="outline" disabled={!canWrite} onClick={() => publishProfile(profile)}>发布</button>}{profile.status === 'published' && runtimeOptions.filter((option) => option.backend?.descriptor?.network_modes?.includes('named_profile')).map((option) => <button className="outline" key={`${profile.profile_id}-${option.agent_id}-${option.backend.backend_id}`} disabled={!canWrite} onClick={() => bindProfile(option, profile)}>绑定 {option.agent_id}/{option.backend.backend_id}</button>)}</div>
              </article>)}
              {!networkState.profiles?.length && <div className="empty-state">暂无网络方案</div>}
            </div>
            {(networkState.bindings || []).length > 0 && <div className="binding-list"><h3>绑定状态</h3>{networkState.bindings.map((binding) => <p key={`${binding.agent_id}-${binding.backend_id}`}>{binding.agent_id}/{binding.backend_id} · {binding.profile_id} v{binding.profile_version} · {binding.desired_status}{binding.applied_worker_id ? ` · Worker ${binding.applied_worker_id}` : ''}</p>)}</div>}
          </section>
        )}
      </main>

      <nav className="bottom" aria-label="主导航">
        {[
          ['command', '⌂', '指挥'],
          ['tasks', '▣', '任务'],
          ['org', '⌘', '组织'],
          ['runtime', '⚙', 'Runtime'],
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
