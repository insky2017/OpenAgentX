import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createRoot } from 'react-dom/client'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import {
  isCurrentObservation,
  mergeHistoryTaskDetail,
  mergeLiveTaskDetail,
  sseResumeAfter,
} from './task-observation-state.js'
import { createdTaskID, dispatchTarget, taskProgress, latestTaskRun } from './task-dispatch-state.js'
import NetworkSettings from './NetworkSettings.jsx'
import './styles.css'

class APIError extends Error {
  constructor(status, message) {
    super(message)
    this.status = status
  }
}

const api = async (path, options = {}) => {
  const response = await fetch(path, {
    ...options,
    credentials: 'include',
    headers: { 'Content-Type': 'application/json', ...(options.headers || {}) },
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

class MarkdownBoundary extends React.Component {
  constructor(props) {
    super(props)
    this.state = { failed: false }
  }

  static getDerivedStateFromError() {
    return { failed: true }
  }

  render() {
    if (this.state.failed) {
      return <pre className="markdown-fallback" role="status">{this.props.source}</pre>
    }
    return this.props.children
  }
}

function MarkdownContent({ value, compact = false }) {
  const [raw, setRaw] = useState(false)
  const [copyState, setCopyState] = useState('')
  const source = typeof value === 'string' && value ? value : '暂无内容'

  const copySource = async () => {
    try {
      if (!navigator.clipboard?.writeText) throw new Error('clipboard unavailable')
      await navigator.clipboard.writeText(source)
      setCopyState('已复制')
    } catch {
      setCopyState('复制失败')
    }
  }

  return (
    <div className={`${raw ? 'markdown-source' : 'markdown-body'} ${compact ? 'compact' : ''}`}>
      <div className="markdown-tools">
        <button className="text-button" type="button" onClick={() => setRaw((current) => !current)}>{raw ? '返回渲染' : '查看原文'}</button>
        <button className="text-button" type="button" onClick={copySource}>复制原文</button>
        <span className={`copy-status ${copyState === '复制失败' ? 'failed' : ''}`} role="status">{copyState}</span>
      </div>
      {raw ? <pre>{source}</pre> : (
        <MarkdownBoundary key={source} source={source}>
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
        </MarkdownBoundary>
      )}
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
        {runs.map((run) => <article className="run-card" key={run.run_id}><div><span className={`status-dot ${run.status?.startsWith('succeed') ? 'ready' : run.status?.startsWith('fail') || run.status === 'uncertain' ? 'error-dot' : 'busy'}`} /><strong>{run.adapter_id} / {run.backend_id}</strong></div><p>{run.model} · 阶段 {run.status}</p><small>Worker {run.worker_instance_id} · generation {run.worker_generation ?? '未知'} · spec v{run.execution_spec_version}</small><small>网络 {run.network_mode || '未知'}{run.network_profile_version ? ` · profile v${run.network_profile_version}` : ''}{run.network_policy_version ? ` · policy v${run.network_policy_version}` : ''}{run.network_binding_revision ? ` · binding r${run.network_binding_revision}` : ''}</small>{run.turn_result?.error && <p className="run-error">{run.turn_result.error}</p>}<time>{new Date(run.started_at).toLocaleString()}</time></article>)}
        {!runs.length && <div className="empty-state">尚无可观察的 RunAttempt</div>}
      </div>
      {detail.has_older_events && <button className="load-more history-more" type="button" onClick={onLoadMore} disabled={loadingMore}>{loadingMore ? '加载中...' : '加载更早事件'}</button>}
      <ol className="timeline" aria-label="运行时间线">
        {events.map((event) => <li key={`${event.sequence}-${event.event_id}`}><span className="timeline-marker" /><div><strong>{eventLabel(event.event_type)}</strong><p>{event.event_type} · {event.aggregate_type}</p>{event.output?.text && <pre className="timeline-output">{event.output.text}</pre>}{event.output?.diagnostic && <pre className="timeline-output diagnostic-output">{event.output.diagnostic}</pre>}<time>{new Date(event.created_at).toLocaleString()} · #{event.sequence}</time></div></li>)}
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
    } catch (requestError) {
      setError(requestError.status === 401 || requestError.status === 403 ? '用户名或密码错误' : '暂时无法连接服务，请稍后重试；已填写内容保留。')
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
  const [dispatchIntent, setDispatchIntent] = useState('mutation')
  const [selectedAgent, setSelectedAgent] = useState(() => new URLSearchParams(window.location.search).get('agent') || '')
  const [replyTask, setReplyTask] = useState(null)
  const [continueTask, setContinueTask] = useState(null)
  const [reviewNote, setReviewNote] = useState('')
  const pendingCommandRef = useRef(new Map())
  const writeInFlightRef = useRef(false)
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
  const [showRunLog, setShowRunLog] = useState(() => new URLSearchParams(window.location.search).has('task'))
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
  const [networkState, setNetworkState] = useState({ profiles: [], versions: [], tests: [], mode_tests: [], bindings: [], active_runs: [] })
  const [runtimeOptions, setRuntimeOptions] = useState([])
  const [networkLoading, setNetworkLoading] = useState(false)

  const refreshNetwork = useCallback(async (showLoading = true) => {
    if (!session || !browserOnline) return
    if (showLoading) setNetworkLoading(true)
    try {
      const [network, options] = await Promise.all([
        api('/api/observe/v1/network-profiles'),
        api('/api/observe/v1/execution-options'),
      ])
      setNetworkState(network || { profiles: [], versions: [], tests: [], mode_tests: [], bindings: [], active_runs: [] })
      setRuntimeOptions(options?.backends || [])
    } catch (requestError) {
      if (requestError.status === 401) setSession(false)
      else setError(requestError.message)
      throw requestError
    } finally {
      if (showLoading) setNetworkLoading(false)
    }
  }, [session, browserOnline])

  useEffect(() => {
    let sessionTimer
    let cancelled = false
    const loadSession = () => api('/api/auth/v1/session').then((value) => { if (!cancelled) setSession(value) }).catch((err) => {
      if (cancelled) return
      if (err.status === 401 || err.status === 403) setSession(false)
      else sessionTimer = setTimeout(loadSession, 2000)
    })
    loadSession()
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
      cancelled = true
      clearTimeout(sessionTimer)
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
    let reconnectTimer
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
    const connect = () => refresh()
      .then((overview) => {
        if (disposed) return
        const after = sseResumeAfter(lastSequenceRef.current, overview.latest_sequence)
        source = new EventSource(`/api/observe/v1/events/stream?mode=normal&after_sequence=${after}`)
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
          refresh().catch(() => {})
          setStreamState('online')
          detailCatchUpRef.current()
        }
        source.onerror = () => { setStreamState('connecting'); refresh().catch(() => {}) }
      })
      .catch((err) => { if (!disposed && err.status !== 401) reconnectTimer = setTimeout(connect, 2000) })
    connect()

    return () => {
      disposed = true
      clearTimeout(reconnectTimer)
      clearTimeout(refreshTimer)
      source?.close()
    }
  }, [session, browserOnline])

  useEffect(() => {
    if (!session || !browserOnline || tab !== 'runtime') return
    let disposed = false
    refreshNetwork().catch(() => {})
    return () => { disposed = true }
  }, [session, browserOnline, tab, refreshNetwork])

  useEffect(() => {
    if (!session || !browserOnline || tab !== 'runtime') return
    const hasPendingNetworkWork = (networkState.profiles || []).some((profile) => profile.state === 'testing')
      || [...(networkState.tests || []), ...(networkState.mode_tests || [])].some((test) => ['pending', 'claimed'].includes(test.state))
      || (networkState.bindings || []).some((binding) => binding.desired_status === 'pending')
    if (!hasPendingNetworkWork) return
    let disposed = false
    let timer
    const poll = async () => {
      await refreshNetwork(false).catch(() => {})
      if (!disposed) timer = setTimeout(poll, 2_000)
    }
    timer = setTimeout(poll, 2_000)
    return () => {
      disposed = true
      clearTimeout(timer)
    }
  }, [session, browserOnline, tab, networkState, refreshNetwork])

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

  const target = dispatchTarget(agents, workers, replyTask?.target_agent_id || selectedAgent, now)
  const targetAgent = target.id
  const organizationID = target.agent?.organization_id || ''
  const canSubmit = canWrite && streamState === 'online' && target.ready
  useEffect(() => {
    if (!selectedAgent && !replyTask && target.ready) setSelectedAgent(target.id)
  }, [selectedAgent, replyTask, target.id, target.ready])
  const attentionTasks = overviewTasks.filter((task) => ['waiting_input', 'waiting_approval'].includes(task.status))
  const onlineAgents = agents.filter((agent) => activeWorkers.get(agentID(agent))?.status === 'online').length
  const tasks = taskPage.tasks || []
  const selectedTask = taskDetail?.task || tasks.find((task) => taskID(task) === selectedTaskID)
  const latestRun = latestTaskRun(taskDetail?.run_attempts)
  const resultBody = taskDetail?.task?.result || latestRun?.turn_result?.body || ''
  const followUpMessages = useMemo(() => {
    return (taskDetail?.messages || []).filter((message) => {
      if (message.kind === 'instruction') return false
      if (message.sequence === 1 && message.content === taskDetail?.task?.content) return false
      return true
    })
  }, [taskDetail])
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
    setContinueTask(null)
    setReviewNote('')
    setTaskView('content')
    setShowRunLog(true)
    setNewOutput(false)
  }

  const closeTaskDetail = () => {
    detailSelectionRef.current += 1
    const selection = detailSelectionRef.current
    selectedTaskRef.current = ''
    detailCatchUpPendingRef.current = false
    setSelectedTaskID('')
    setTaskDetail(null)
    setShowRunLog(false)
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
    if (selectedAgent) params.set('agent', selectedAgent)
    else params.delete('agent')
    const query = params.toString()
    window.history.replaceState(null, '', `${window.location.pathname}${query ? `?${query}` : ''}`)
  }, [selectedTaskID, tab, selectedAgent])

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
    if (!canWrite || writeInFlightRef.current) return false
    writeInFlightRef.current = true
    const signature = JSON.stringify([path, { ...body, meta: { ...body.meta, idempotency_key: undefined } }])
    const stableKey = pendingCommandRef.current.get(signature) || idempotencyKey
    pendingCommandRef.current.set(signature, stableKey)
    body = { ...body, meta: { ...body.meta, idempotency_key: stableKey } }
    setWriting(true)
    setError('')
    try {
      const receipt = await api(path, {
        method: 'POST',
        headers: {
          'X-CSRF-Token': session.csrf_token,
          'Idempotency-Key': stableKey,
        },
        body: JSON.stringify(body),
      })
      pendingCommandRef.current.delete(signature)
      taskListRefreshRef.current()
      // The command is already accepted. An observation failure must not turn
      // this into a failed send or invite a duplicate submission.
      refreshOverview().catch(() => setError('操作已被服务端接受，但状态刷新失败；请查看任务列表，不要重复发送。'))
      if (selectedTaskRef.current) catchUpTaskDetail()
      return { receipt }
    } catch (requestError) {
      if (requestError.status >= 400 && requestError.status < 500) pendingCommandRef.current.delete(signature)
      if (requestError.status === 401) setSession(false)
      setError(requestError.message)
      return false
    } finally {
      writeInFlightRef.current = false
      setWriting(false)
    }
  }

  const networkCommand = async (path, body) => {
    if (!canWrite) throw new APIError(403, 'network writes are unavailable')
    setWriting(true)
    setError('')
    try {
      return await api(path, {
        method: 'POST',
        headers: { 'X-CSRF-Token': session.csrf_token, 'Idempotency-Key': body.meta.idempotency_key },
        body: JSON.stringify(body),
      })
    } finally {
      setWriting(false)
    }
  }

  const sendInstruction = async () => {
    const content = draft.trim()
    if (!content || !targetAgent || !canSubmit) return
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
        intent: dispatchIntent,
        ...(continueTask ? { parent_task_id: taskID(continueTask), continue_context: true } : {}),
        content,
      },
      key,
    )
    if (sent) {
      setDraft('')
      setContinueTask(null)
      const id = createdTaskID(sent.receipt)
      if (!id) {
        setError('指令已被服务端接受，但返回的任务信息异常；请查看任务列表确认，不要重复发送。')
        return
      }
      setTaskAgentFilter(targetAgent)
      setTaskStatusFilter('')
      setTaskTimeFilter('')
      setTaskQuery('')
      selectTask(id)
    }
  }

  const reviewResult = async (decision) => {
    if (!latestRun || !taskDetail) return
    const key = newCommandKey('review')
    await write(`/api/control/v1/tasks/${taskDetail.task.id}/review`, {
      meta: { idempotency_key: key, expected_version: taskDetail.task.version },
      run_id: latestRun.run_id, run_version: latestRun.version, decision, note: reviewNote,
    }, key)
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
                  const activeTask = overviewTasks.find((task) => task.target_agent_id === id && !terminalTask(task.status))
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
                        <span className="state">{agent.readiness?.reason || `${worker?.status || 'offline'} · ${availability}`}</span>
                      </div>
                      <p>{id} · {worker ? `心跳 ${formatAge(worker.last_heartbeat_at)}` : '暂无有效 Worker'}</p>
                      <button className="link" onClick={() => { setSelectedAgent(id); setReplyTask(null); setContinueTask(null); setTab('tasks') }}>
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
                    <small>{approval.mode}</small>
                    <MarkdownContent value={approval.description || approval.scope_digest} compact />
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
                    onClick={() => { setSelectedAgent(task.target_agent_id); setContinueTask(null); setReplyTask(task); setTab('tasks') }}
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
            </div>
            <div className={`workbench-main ${selectedTaskID ? 'has-selection' : ''}`}>
              <div className="composer">
                <div className="composer-target">
                  <label htmlFor="dispatch-agent">执行 Agent</label>
                  <select id="dispatch-agent" value={replyTask?.target_agent_id || selectedAgent} disabled={writing || !!replyTask || !!continueTask} onChange={(event) => setSelectedAgent(event.target.value)}>
                    <option value="">请选择执行 Agent</option>
                    {selectedAgent && !agents.some((agent) => agentID(agent) === selectedAgent) && <option value={selectedAgent}>所选 Agent 不存在</option>}
                    {agents.map((agent) => {
                      const id = agentID(agent)
                      const state = dispatchTarget(agents, workers, id, now)
                      return <option key={id} value={id}>{agent.display_name || id} ({id}) · {state.reason}</option>
                    })}
                  </select>
                </div>
                <p className={`composer-readiness ${target.ready ? '' : 'unavailable'}`} role="status">
                  {streamState !== 'online' ? '连接未恢复，暂不能发送' : target.reason}
                </p>
                <div className="composer-target">
                  <label htmlFor="dispatch-intent">任务类型</label>
                  <select id="dispatch-intent" value={replyTask?.intent || (replyTask ? 'mutation' : dispatchIntent)} disabled={!canWrite || writing || !!replyTask} onChange={(event) => setDispatchIntent(event.target.value)}>
                    <option value="mutation">执行任务（默认）</option>
                    <option value="query">问答（以完整回复交付）</option>
                  </select>
                </div>
                <div className="composer-label">
                  <label htmlFor="command">{replyTask ? `补充当前任务（下轮处理）` : continueTask ? `继续 ${taskID(continueTask)}（新任务，引用上次结果）` : `向 ${targetAgent || 'Agent'} 发送业务指令`}</label>
                  {(replyTask || continueTask) && <button className="text-button" type="button" onClick={() => { setReplyTask(null); setContinueTask(null) }}>独立新任务</button>}
                </div>
                <div>
                  <textarea id="command" value={draft} onChange={(event) => setDraft(event.target.value)} placeholder="输入业务指令..." disabled={!canWrite} rows={2} />
                  <button className="send" type="button" disabled={!canSubmit || !draft.trim()} onClick={sendInstruction} aria-label="发送">↑</button>
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
                      <p>任务类型：{taskDetail.task.intent === 'query' ? '查询任务' : taskDetail.task.intent && taskDetail.task.intent !== 'mutation' ? '未知类型' : '变更任务'}</p>
                      <p className="execution-progress" role="status">{taskDetail.review?.decision === 'accepted' ? '用户已验收；原始执行记录保留' : taskProgress(taskDetail.task, latestRun, activeWorkers.get(taskDetail.task.target_agent_id))}</p>
                      {latestRun?.deadline_at && !terminalTask(taskDetail.task.status) && <p>本次运行截止：{new Date(latestRun.deadline_at).toLocaleTimeString()}</p>}
                    </div>
                    <span className={`pill status-${taskDetail.task.status}`}>{taskDetail.task.status}</span>
                  </div>
                  {terminalTask(taskDetail.task.status) && latestRun?.status === 'succeeded' && <details className="review-note"><summary>验收备注（可选）</summary><textarea aria-label="验收备注" maxLength={4096} rows={2} value={reviewNote} disabled={!canWrite} onChange={(event) => setReviewNote(event.target.value)} placeholder="结果哪里需要修改，或已核对的文件路径" /></details>}
                  <div className="detail-actions">
                    {terminalTask(taskDetail.task.status) && <button className="outline" type="button" disabled={!canWrite} onClick={() => { setSelectedAgent(taskDetail.task.target_agent_id); setReplyTask(null); setContinueTask(taskDetail.task); setDispatchIntent(taskDetail.task.intent); setDraft(''); document.getElementById('command')?.focus() }}>继续此工作</button>}
                    {terminalTask(taskDetail.task.status) && latestRun?.status === 'succeeded' && <><button className="outline" type="button" disabled={!canWrite} onClick={() => reviewResult('accepted')}>接受结果</button><button className="outline" type="button" disabled={!canWrite} onClick={() => reviewResult('rejected')}>结果有问题</button></>}

                    {['waiting_input', 'running'].includes(taskDetail.task.status) && <button className="outline" type="button" disabled={!canWrite} onClick={() => { setSelectedAgent(taskDetail.task.target_agent_id); setContinueTask(null); setReplyTask(taskDetail.task); setDraft(''); document.getElementById('command')?.focus() }}>补充（下轮处理）</button>}
                    {!terminalTask(taskDetail.task.status) && taskDetail.task.status !== 'cancel_requested' && <button className="outline danger" type="button" disabled={!canWrite} onClick={() => cancelTask(taskDetail.task)}>取消任务</button>}
                    <button className={`outline ${showRunLog ? 'active' : ''}`} type="button" onClick={() => setShowRunLog((prev) => !prev)}>
                      {showRunLog ? '收起运行日志' : '查看运行日志'}
                    </button>
                    {newOutput && <button className="new-output" type="button" onClick={jumpToLatest}>跳到最新</button>}
                  </div>
                  {taskDetailState === 'replay_error' && <div className="replay-error" role="alert"><span>事件回放失败，当前内容可能不是最新。</span><button className="outline" type="button" onClick={() => replayRetryRef.current()}>重试</button></div>}
                  <div className="detail-scroll" ref={detailScrollRef} onScroll={(event) => { const node = event.currentTarget; readingLatestRef.current = node.scrollHeight - node.scrollTop - node.clientHeight < 48; if (readingLatestRef.current) setNewOutput(false) }}>
                    <div className="task-content-view">
                      <div className="task-section task-result-section">
                        <div className="task-section-head">
                          <h3 className="section-title">执行结果</h3>
                          {taskDetail.review && <span className="pill">{taskDetail.review.decision === 'accepted' ? '用户已验收' : '用户指出结果有问题'}</span>}
                          {taskDetail.review?.note && <p>{taskDetail.review.note}</p>}
                          {latestRun && (
                            <span className="run-meta-tag">
                              {latestRun.run_id} · Runtime {latestRun.turn_result?.runtime_status || latestRun.status}
                            </span>
                          )}
                        </div>
                        {resultBody ? (
                          <MarkdownContent value={resultBody} />
                        ) : (
                          !taskDetail.task.error && (
                            <div className="empty-state">
                              {terminalTask(taskDetail.task.status) ? '任务尚未产生最终结果' : taskProgress(taskDetail.task, latestRun, activeWorkers.get(taskDetail.task.target_agent_id))}
                            </div>
                          )
                        )}
                        {taskDetail.task.status === 'succeeded' && taskDetail.task.intent === 'query' && taskDetail.task.completion_basis === 'query_result_delivered' && (
                          <p className="result-evidence">查询回复已完整交付。不代表答案真实性、全程只读或副作用已核验。</p>
                        )}
                        {taskDetail.task.completion_basis === 'mutation_effects_known' && (
                          <p className="result-evidence">完成依据：执行副作用已知；不代表独立业务核验。</p>
                        )}
                        {latestRun?.turn_result && (
                          <p className="result-evidence">
                            {taskDetail.review ? `验收来源：用户确认（${taskDetail.review.decision === 'accepted' ? '接受' : '未接受'}）；不代表系统独立验证业务效果` : '执行结果可由你验收；业务效果尚未独立核验'}
                          </p>
                        )}
                        {taskDetail.task.error && (
                          <div className={`diagnostic ${taskDetail.task.error === 'business_effect_unverified' && latestRun?.status === 'succeeded' ? 'verification-note' : ''}`}>
                            <strong>{taskDetail.task.error === 'business_effect_unverified' && latestRun?.status === 'succeeded' ? 'Runtime 已返回，业务效果尚未独立核验' : '执行诊断'}</strong>
                            <pre>{taskDetail.task.error}</pre>
                          </div>
                        )}
                      </div>
                      <div className="task-section">
                        <div className="task-section-head">
                          <h3 className="section-title">任务指令</h3>
                        </div>
                        <MarkdownContent value={taskDetail.task.content} />
                      </div>
                      {followUpMessages.length > 0 && (
                        <div className="task-section task-conversation-section">
                          <div className="task-section-head">
                            <h3 className="section-title">后续对话 ({followUpMessages.length})</h3>
                          </div>
                          <div className="conversation-list">
                            {followUpMessages.map((message) => (
                              <article className="message-item" key={message.id}>
                                <div>
                                  <strong>{message.sender_principal_id}</strong>
                                  <time>{message.created_at}</time>
                                </div>
                                <MarkdownContent value={message.content} compact />
                              </article>
                            ))}
                          </div>
                        </div>
                      )}
                      {showRunLog && (
                        <div className="task-section task-runlog-section">
                          <div className="task-section-head">
                            <h3 className="section-title">执行过程</h3>
                            <button className="mini-close" type="button" onClick={() => setShowRunLog(false)} aria-label="收起日志">×</button>
                          </div>
                          <RunTimeline detail={taskDetail} onLoadMore={loadMoreEvents} loadingMore={loadingMoreEvents} />
                        </div>
                      )}
                    </div>
                  </div>
                </>
              )}
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

        {tab === 'runtime' && (
          <section className="page runtime-page">
            <div className="page-head">
              <button className="back" onClick={() => setTab('command')} aria-label="返回">←</button>
              <div><p className="eyebrow">Runtime</p><h2>网络配置</h2></div>
              <span className="connection-note">{networkLoading ? '同步中' : `${networkState.profiles?.length || 0} 个方案`}</span>
            </div>
            <NetworkSettings
              networkState={networkState}
              runtimeOptions={runtimeOptions}
              canWrite={writable}
              canManageSecrets={roles.includes('owner')}
              offline={!browserOnline}
              loading={networkLoading}
              busy={writing}
              onCommand={networkCommand}
              onReload={() => refreshNetwork(false)}
              onOpenTask={(id) => { selectTask(id); setTab('tasks') }}
            />
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
