import React, { useEffect, useMemo, useRef, useState } from 'react'

import './network-settings.css'

const EMPTY_FORM = {
  mode: 'only_http_proxy',
  id: '',
  host: '',
  port: '7897',
  username: '',
  password: '',
  direct_ips: '',
}

const stateLabels = {
  draft: '草稿',
  testing: '测试中',
  ready: '测试通过',
  published: '已就绪',
  stale: '需重测',
  pending: '等待 Worker',
  claimed: '执行中',
  applied: '已应用',
  failed: '失败',
  succeeded: '成功',
  healthy: '健康',
  degraded: '降级',
  unavailable: '离线',
  unknown: '未知',
}

const modeLabels = {
  inherit: '系统默认 (inherit)',
  direct: '直接联网 (direct)',
  named_profile: '代理服务器',
  only_http_proxy: 'HTTP 代理',
  only_socks5: 'SOCKS5 代理',
}

const diagnosticLabels = {
  INVALID_CONFIG: '配置无效',
  SECRET_MISSING: '缺少密码',
  ENDPOINT_UNREACHABLE: '端点不可达',
  MATERIALIZATION_FAILED: '配置写入失败',
  RUNTIME_HEALTH_FAILED: 'Runtime 健康检查失败',
  RUNTIME_IDENTITY_CHANGED: 'Runtime 身份变化',
  UNSUPPORTED_CAPABILITY: '能力不支持',
  NOT_VERIFIED: '未核验',
  INHERITED_CONFIGURATION_UNVERIFIED: '继承配置无法核验',
}

const newProfileID = () => {
  const suffix = crypto.randomUUID?.().replaceAll('-', '').slice(0, 8) || `${Date.now().toString(36)}`
  return `proxy-${suffix}`
}

const newCommandKey = () => `network-${crypto.randomUUID?.() || `${Date.now()}-${Math.random()}`}`

const directIPs = (value) => value
  .split(/[\s,]+/)
  .map((entry) => entry.trim())
  .filter(Boolean)

const targetKey = (option) => [
  option.agent_id,
  option.worker_id,
  option.generation,
  option.backend?.backend_id,
].join('|')

const targetLabel = (option, isCurrent) => {
  const backend = option.backend?.backend_id === 'primary' ? '主服务' : option.backend?.backend_id || 'default'
  const gen = `gen ${option.generation}`
  if (isCurrent) {
    const status = option.worker_status ? ` · ${displayState(option.worker_status)}` : ''
    return `${option.agent_id} (${backend}) · ${gen} [当前${status}]`
  }
  const status = option.worker_status === 'offline' ? '已离线' : (displayState(option.worker_status) || '历史')
  return `${option.agent_id} (${backend}) · ${gen} [${status}]`
}

const supportsNamedProfile = (option) => option?.backend?.descriptor?.network_modes?.includes('named_profile')

const supportedNetworkModes = (option) => option?.backend?.descriptor?.network_modes || []

const supportsAnyNetworkMode = (option) => supportedNetworkModes(option)
  .some((mode) => ['inherit', 'direct', 'named_profile'].includes(mode))

const displayState = (value) => stateLabels[value] || value || '未知'

const displayDiagnostic = (value) => value ? diagnosticLabels[value] || value : ''

const activeRunNetworkText = (run) => {
  if (!run?.network_mode || !['inherit', 'direct', 'named_profile'].includes(run.network_mode)) return '网络模式未记录'
  if (run.network_mode === 'named_profile') {
    return `代理 ${run.network_profile_id || '未知'}`
  }
  return modeLabels[run.network_mode] || run.network_mode
}

const activeRunMatchesBinding = (run, binding) => {
  if (!run || !binding || !run.network_mode || !run.network_binding_revision) return null
  if (run.network_mode !== binding.mode || run.network_binding_revision !== binding.version) return false
  if (run.network_mode === 'named_profile') {
    if (!run.network_profile_id || !run.network_profile_version) return null
    return run.network_profile_id === binding.profile_id && run.network_profile_version === binding.profile_version
  }
  if (!run.network_policy_version) return null
  return run.network_policy_version === binding.policy_version
}

const requestErrorText = (error) => {
  if (!error) return '操作失败，未知错误。'

  // 1. 网络离线、连接中断或后台服务正在重启
  const msg = typeof error.message === 'string' ? error.message.trim() : ''
  const isNetworkFailure = (
    error instanceof TypeError ||
    error.name === 'TypeError' ||
    msg.includes('Failed to fetch') ||
    msg.includes('NetworkError') ||
    msg.includes('network error') ||
    msg.includes('Load failed')
  )
  if (isNetworkFailure) {
    return '网络连接中断或后台服务正在重启，请稍候重试。'
  }

  // 提取具体的错误信息（如果是 JSON 则提取 error/message 字段）
  let detail = msg
  if (detail) {
    try {
      const parsed = JSON.parse(detail)
      detail = parsed.error || parsed.message || detail
    } catch {
      // 保持原始字符串
    }
  }

  // 2. 根据 HTTP 状态码生成可读摘要
  let prefix = ''
  switch (error?.status) {
  case 400:
    prefix = '请求参数无效'
    break
  case 401:
    prefix = '登录状态已失效，请重新登录'
    break
  case 403:
    prefix = '当前账号无权执行此操作'
    break
  case 404:
    prefix = '操作的目标对象不存在或已下线'
    break
  case 409:
    prefix = '状态或版本已发生变化，请刷新后重试'
    break
  case 422:
    prefix = '目标 Runtime 未声明所需的网络能力'
    break
  case 500:
    prefix = '服务内部处理异常'
    break
  case 501:
    prefix = '该功能在当前服务版本中暂未开放'
    break
  case 502:
  case 503:
  case 504:
    prefix = `后台服务暂时不可用或响应超时 (HTTP ${error.status})`
    break
  default:
    if (error?.status) {
      prefix = `操作失败 (HTTP ${error.status})`
    } else {
      prefix = '操作未完成'
    }
    break
  }

  if (detail && detail !== prefix && !detail.startsWith('<')) {
    return `${prefix}: ${detail}`
  }
  return `${prefix}。`
}

export default function NetworkSettings({
  networkState = {},
  runtimeOptions = [],
  canWrite = false,
  canManageSecrets = false,
  offline = false,
  loading = false,
  busy = false,
  onCommand,
  onReload,
  onOpenTask,
}) {
  const profiles = networkState.profiles || []
  const tests = networkState.tests || []
  const modeTests = networkState.mode_tests || []
  const bindings = networkState.bindings || []
  const activeRuns = networkState.active_runs || []
  const targets = useMemo(() => runtimeOptions.filter(supportsAnyNetworkMode), [runtimeOptions])

  const { activeTargets, historyTargets, isTargetCurrent } = useMemo(() => {
    const maxGenMap = new Map()
    for (const opt of targets) {
      const key = `${opt.agent_id}|${opt.backend?.backend_id || ''}`
      const cur = maxGenMap.get(key) || 0
      if (opt.generation > cur) {
        maxGenMap.set(key, opt.generation)
      }
    }

    const isCurrent = (opt) => {
      const key = `${opt.agent_id}|${opt.backend?.backend_id || ''}`
      const isMax = opt.generation === maxGenMap.get(key)
      if (opt.worker_status) {
        return isMax && opt.worker_status !== 'offline'
      }
      return isMax
    }

    const active = []
    const history = []

    for (const opt of targets) {
      if (isCurrent(opt)) {
        active.push(opt)
      } else {
        history.push(opt)
      }
    }

    active.sort((a, b) => a.agent_id.localeCompare(b.agent_id))
    history.sort((a, b) => b.generation - a.generation)

    return {
      activeTargets: active,
      historyTargets: history,
      isTargetCurrent: isCurrent,
    }
  }, [targets])

  const [selectedTargetKey, setSelectedTargetKey] = useState('')
  const [selectedProfileID, setSelectedProfileID] = useState('')
  const [modeDraft, setModeDraft] = useState('')
  const [showCreate, setShowCreate] = useState(false)
  const [createForm, setCreateForm] = useState(EMPTY_FORM)
  const [localBusy, setLocalBusy] = useState('')
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const commandAttempts = useRef(new Map())
  const modeTargetKey = useRef('')

  const selectedTarget = targets.find((option) => targetKey(option) === selectedTargetKey) || null
  const selectedProfile = profiles.find((profile) => profile.profile_id === selectedProfileID) || null
  const selectedBinding = selectedTarget
    ? bindings.find((binding) => binding.agent_id === selectedTarget.agent_id && binding.backend_id === selectedTarget.backend?.backend_id)
    : null
  const selectedActiveRun = selectedTarget
    ? activeRuns.find((run) => run.agent_id === selectedTarget.agent_id && run.backend_id === selectedTarget.backend?.backend_id) || null
    : null
  const activeRunBindingMatch = activeRunMatchesBinding(selectedActiveRun, selectedBinding)
  const selectedTargetSupportsProfile = supportsNamedProfile(selectedTarget)
  const availableTargetModes = useMemo(() => {
    if (!selectedTarget) return []
    return supportedNetworkModes(selectedTarget).filter((mode) => ['inherit', 'direct', 'named_profile'].includes(mode))
  }, [selectedTarget])
  const bindingRevision = selectedBinding?.version || 0

  // Latest test results
  const targetModeTests = modeTests
    .filter((test) => test.agent_id === selectedTarget?.agent_id && test.backend_id === selectedTarget?.backend?.backend_id)
    .sort((left, right) => Date.parse(right.created_at) - Date.parse(left.created_at))

  const latestMatchingModeTest = targetModeTests.find((test) =>
    test.mode === modeDraft
    && test.worker_instance_id === selectedTarget?.worker_id
    && test.generation === selectedTarget?.generation
    && test.binding_revision === bindingRevision
  )

  const profileTestsOnTarget = tests
    .filter((test) => test.profile_id === selectedProfileID && test.backend_id === selectedTarget?.backend?.backend_id)
    .sort((left, right) => Date.parse(right.created_at) - Date.parse(left.created_at))

  const latestProfileTest = profileTestsOnTarget[0] || null

  const isModeTestPublishable = (test) => Boolean(selectedTarget
    && test?.state === 'succeeded'
    && test.agent_id === selectedTarget.agent_id
    && test.backend_id === selectedTarget.backend?.backend_id
    && test.mode === modeDraft
    && test.worker_instance_id === selectedTarget.worker_id
    && test.generation === selectedTarget.generation
    && test.binding_revision === bindingRevision)

  const appliedToSelected = selectedBinding?.desired_status === 'applied'
    && selectedBinding.applied_mode === 'named_profile'
    && selectedBinding.applied_profile_id === selectedProfile?.profile_id
    && selectedBinding.applied_profile_version === selectedProfile?.published_content_version
    && selectedBinding.applied_worker_id === selectedTarget?.worker_id
    && selectedBinding.applied_generation === selectedTarget?.generation

  // Sync target selection (prefer active target)
  useEffect(() => {
    if (!targets.length) {
      setSelectedTargetKey('')
      return
    }
    if (!targets.some((option) => targetKey(option) === selectedTargetKey)) {
      const defaultTarget = activeTargets[0] || targets[0]
      setSelectedTargetKey(targetKey(defaultTarget))
    }
  }, [targets, activeTargets, selectedTargetKey])

  // Sync mode draft
  useEffect(() => {
    const nextTargetKey = selectedTarget ? targetKey(selectedTarget) : ''
    const targetChanged = modeTargetKey.current !== nextTargetKey
    modeTargetKey.current = nextTargetKey
    setModeDraft((current) => {
      if (!selectedTarget || !availableTargetModes.length) return ''
      if (targetChanged && selectedBinding?.mode && availableTargetModes.includes(selectedBinding.mode)) {
        return selectedBinding.mode
      }
      if (!targetChanged && current && availableTargetModes.includes(current)) return current
      if (selectedBinding?.mode && availableTargetModes.includes(selectedBinding.mode)) {
        return selectedBinding.mode
      }
      return availableTargetModes[0] || ''
    })
  }, [selectedTargetKey, availableTargetModes, selectedBinding?.mode])

  // Sync profile selection from binding
  useEffect(() => {
    if (selectedBinding?.mode === 'named_profile' && selectedBinding.profile_id) {
      if (profiles.some((p) => p.profile_id === selectedBinding.profile_id)) {
        setSelectedProfileID(selectedBinding.profile_id)
        return
      }
    }
    if (profiles.length && !profiles.some((p) => p.profile_id === selectedProfileID)) {
      setSelectedProfileID(profiles[0].profile_id)
    }
  }, [selectedBinding?.profile_id, selectedBinding?.mode, profiles, selectedProfileID])

  const writeDisabled = offline || busy || Boolean(localBusy) || !canWrite

  const reload = async () => {
    if (!onReload || offline || loading || localBusy) return
    setError('')
    setNotice('')
    try {
      await onReload()
    } catch (requestError) {
      setError(requestErrorText(requestError))
    }
  }

  const execute = async (scope, path, body, successMessage, secret = false) => {
    if (!onCommand || offline || busy || localBusy || !canWrite) return { ok: false }
    const fingerprint = secret ? '' : JSON.stringify({ path, body })
    const previous = commandAttempts.current.get(scope)
    const idempotencyKey = !secret && previous?.fingerprint === fingerprint ? previous.key : newCommandKey()
    if (!secret) commandAttempts.current.set(scope, { fingerprint, key: idempotencyKey })
    setLocalBusy(scope)
    setError('')
    setNotice('')
    try {
      await onCommand(path, {
        ...body,
        meta: {
          idempotency_key: idempotencyKey,
          expected_version: body.meta?.expected_version ?? 0,
        },
      })
      commandAttempts.current.delete(scope)
      if (successMessage) setNotice(successMessage)
      let refreshFailed = false
      if (onReload) {
        try {
          await onReload()
        } catch {
          refreshFailed = true
        }
      }
      return { ok: true, refreshFailed }
    } catch (requestError) {
      setError(requestErrorText(requestError))
      return { ok: false, status: requestError?.status }
    } finally {
      setLocalBusy('')
    }
  }

  // 1. One-click Test Connection
  const handleTestConnection = async () => {
    if (!selectedTarget) return
    setError('')
    setNotice('')

    if (!isTargetCurrent(selectedTarget)) {
      setError(`选中的 Runtime [${targetLabel(selectedTarget, false)}] 为历史已退役代次，实例已下线，无法下发测试。请切换到【当前活跃代次】后再测试。`)
      return
    }

    if (modeDraft === 'inherit' || modeDraft === 'direct') {
      await execute(`mode-test:${selectedTargetKey}`, '/api/control/v1/network-bindings/mode/tests', {
        agent_id: selectedTarget.agent_id,
        backend_id: selectedTarget.backend.backend_id,
        mode: modeDraft,
        worker_instance_id: selectedTarget.worker_id,
        generation: selectedTarget.generation,
        meta: { expected_version: bindingRevision },
      }, `已向 Worker 发送【${modeLabels[modeDraft]}】连通性测试。`)
    } else if (modeDraft === 'named_profile') {
      if (!selectedProfile) {
        setError('请先在下方选择一个代理服务器。')
        return
      }
      if (!selectedTargetSupportsProfile) {
        setError('当前 Runtime 未声明代理扩展能力。')
        return
      }
      await execute(`test:${selectedProfile.profile_id}:${selectedTargetKey}`, `/api/control/v1/network-profiles/${encodeURIComponent(selectedProfile.profile_id)}/tests`, {
        worker_instance_id: selectedTarget.worker_id,
        generation: selectedTarget.generation,
        backend_id: selectedTarget.backend.backend_id,
        meta: { expected_version: selectedProfile.state_revision },
      }, `已向 Worker 发送【${selectedProfile.profile_id}】连通性测试。`)
    }
  }

  // 2. One-click Save & Apply
  const handleSaveAndApply = async () => {
    if (!selectedTarget) return
    setError('')
    setNotice('')

    if (!isTargetCurrent(selectedTarget)) {
      setError(`选中的 Runtime [${targetLabel(selectedTarget, false)}] 为历史已退役代次，实例已下线，无法保存配置。请切换到【当前活跃代次】后再保存。`)
      return
    }

    if (modeDraft === 'inherit' || modeDraft === 'direct') {
      if (isModeTestPublishable(latestMatchingModeTest)) {
        await execute(`mode-publish:${selectedTargetKey}:${latestMatchingModeTest.test_id}`, '/api/control/v1/network-bindings/mode/publish', {
          test_id: latestMatchingModeTest.test_id,
          worker_instance_id: selectedTarget.worker_id,
          generation: selectedTarget.generation,
          meta: { expected_version: latestMatchingModeTest.binding_revision },
        }, `【${modeLabels[modeDraft]}】已保存并下发应用。`)
      } else if (selectedBinding?.mode === modeDraft && selectedBinding?.desired_status === 'applied') {
        setNotice(`当前 Runtime 已经生效为【${modeLabels[modeDraft]}】，无需重复保存。`)
      } else {
        setError(`尚未完成连通测试，请先点击【⚡ 测试连接】，确认通畅后再保存应用。`)
      }
    } else if (modeDraft === 'named_profile') {
      if (!selectedProfile) {
        setError('请先选择一个代理方案。')
        return
      }
      if (!selectedTargetSupportsProfile) {
        setError('当前 Runtime 未声明代理扩展能力。')
        return
      }

      // If already published, directly bind
      if (selectedProfile.published_content_version) {
        await execute(`bind:${selectedTargetKey}`, '/api/control/v1/network-bindings', {
          agent_id: selectedTarget.agent_id,
          backend_id: selectedTarget.backend.backend_id,
          profile_id: selectedProfile.profile_id,
          profile_version: selectedProfile.published_content_version,
          worker_instance_id: selectedTarget.worker_id,
          generation: selectedTarget.generation,
          meta: { expected_version: selectedBinding?.version || 0 },
        }, `代理【${selectedProfile.profile_id}】已成功绑定到当前 Runtime。`)
      } else if (selectedProfile.state === 'ready' && selectedProfile.ready_test_id) {
        // Auto-publish then bind!
        const pubResult = await execute(`publish:${selectedProfile.profile_id}`, `/api/control/v1/network-profiles/${encodeURIComponent(selectedProfile.profile_id)}/publish`, {
          meta: { expected_version: selectedProfile.state_revision },
        }, '')
        if (pubResult.ok) {
          await execute(`bind:${selectedTargetKey}`, '/api/control/v1/network-bindings', {
            agent_id: selectedTarget.agent_id,
            backend_id: selectedTarget.backend.backend_id,
            profile_id: selectedProfile.profile_id,
            profile_version: selectedProfile.current_content_version,
            worker_instance_id: selectedTarget.worker_id,
            generation: selectedTarget.generation,
            meta: { expected_version: selectedBinding?.version || 0 },
          }, `代理【${selectedProfile.profile_id}】已发布并成功绑定到当前 Runtime。`)
        }
      } else {
        setError(`代理尚未通过目标连通测试，请先点击【⚡ 测试连接】，确认通畅后再保存。`)
      }
    }
  }

  // 3. Add New Proxy
  const handleCreateProxy = async (event) => {
    event.preventDefault()
    const profileID = (createForm.id || newProfileID()).trim()
    const host = createForm.host.trim()
    const port = Number(createForm.port)
    if (!profileID || !host || !Number.isInteger(port) || port < 1 || port > 65535) {
      setError('请填写有效的主机和端口（1-65535）。')
      return
    }

    const result = await execute('create', '/api/control/v1/network-profiles', {
      profile_id: profileID,
      mode: createForm.mode,
      host,
      port,
      direct_ips: directIPs(createForm.direct_ips),
      meta: { expected_version: 0 },
    }, '代理已成功添加到代理池。')

    if (result.ok) {
      if (createForm.mode === 'only_socks5' && (createForm.username || createForm.password) && canManageSecrets) {
        await execute(`secret:${profileID}`, `/api/control/v1/network-profiles/${encodeURIComponent(profileID)}/secret`, {
          username: createForm.username,
          password: createForm.password,
          meta: { expected_version: 1 },
        }, '代理及凭据已成功添加到代理池。', true)
      }
      setSelectedProfileID(profileID)
      setModeDraft('named_profile')
      setCreateForm(EMPTY_FORM)
      setShowCreate(false)
    }
  }

  // Applied text summary
  const appliedSummary = useMemo(() => {
    if (!selectedBinding?.applied_mode) {
      return selectedBinding ? `${displayState(selectedBinding.desired_status)} (等待应用)` : '未绑定'
    }
    if (selectedBinding.applied_mode === 'named_profile') {
      return `代理: ${selectedBinding.applied_profile_id || '未知'} (v${selectedBinding.applied_profile_version || '1'})`
    }
    return modeLabels[selectedBinding.applied_mode] || selectedBinding.applied_mode
  }, [selectedBinding])

  // Current active test result
  const currentTest = modeDraft === 'named_profile' ? latestProfileTest : latestMatchingModeTest

  return (
    <section className="network-settings-simple" aria-busy={loading || busy || Boolean(localBusy)}>
      <header className="simple-header">
        <div>
          <h2>出网与代理设置</h2>
          <p>管理 Agent Runtime 的网络出口策略与代理服务器</p>
        </div>
        <div className="header-actions">
          <span className={`net-dot ${offline ? 'is-offline' : 'is-online'}`}>{offline ? '离线' : '在线'}</span>
          <button type="button" className="outline mini-btn" onClick={reload} disabled={offline || loading || Boolean(localBusy)}>
            刷新
          </button>
        </div>
      </header>

      {offline && <div className="simple-alert warning">当前网络离线，所有配置写入已禁用。</div>}
      {error && <div className="simple-alert error">{error}</div>}
      {notice && <div className="simple-alert success">{notice}</div>}

      {/* 1. Runtime 出网配置卡片 */}
      <div className="simple-card runtime-card">
        <div className="card-head">
          <div className="target-select-row">
            <span className="target-label">目标 Runtime:</span>
            <select
              className="target-dropdown"
              value={selectedTargetKey}
              onChange={(event) => setSelectedTargetKey(event.target.value)}
              disabled={!targets.length || loading}
            >
              {!targets.length && <option value="">暂无可用 Runtime</option>}
              {activeTargets.length > 0 && (
                <optgroup label="当前活跃 Runtime (当前代次)">
                  {activeTargets.map((option) => (
                    <option key={targetKey(option)} value={targetKey(option)}>
                      {targetLabel(option, true)}
                    </option>
                  ))}
                </optgroup>
              )}
              {historyTargets.length > 0 && (
                <optgroup label="历史代次 (旧 Worker 实例)">
                  {historyTargets.map((option) => (
                    <option key={targetKey(option)} value={targetKey(option)}>
                      {targetLabel(option, false)}
                    </option>
                  ))}
                </optgroup>
              )}
            </select>
          </div>
          {selectedTarget && (
            <div className="target-meta-badges">
              <span className={`gen-badge ${isTargetCurrent(selectedTarget) ? 'is-current' : 'is-history'}`}>
                {isTargetCurrent(selectedTarget) ? '● 当前代次' : '○ 历史代次'} (gen {selectedTarget.generation})
              </span>
              <span className={`simple-health health-${selectedTarget.backend?.health || 'unknown'}`}>
                Backend: {displayState(selectedTarget.backend?.health || 'unknown')}
              </span>
            </div>
          )}
        </div>

        {selectedTarget ? (
          <div className="card-body">
            {!isTargetCurrent(selectedTarget) && (
              <div className="simple-alert warning" style={{ marginBottom: '14px', fontSize: '13px' }}>
                提示：当前查看的是历史已退役的 Worker 代次（gen {selectedTarget.generation}），该实例已离线，无法下发网络测试或保存出网配置。如需配置，请在上方选择【当前活跃代次】。
              </div>
            )}

            <div className="mode-selection-group">
              <span className="field-label">出网方式:</span>
              <div className="mode-pill-row" role="radiogroup">
                {availableTargetModes.map((mode) => (
                  <button
                    type="button"
                    role="radio"
                    aria-checked={modeDraft === mode}
                    className={`mode-pill ${modeDraft === mode ? 'active selected' : ''}`}
                    key={mode}
                    onClick={() => setModeDraft(mode)}
                    disabled={writeDisabled || !isTargetCurrent(selectedTarget)}
                  >
                    {mode === 'inherit' && '系统默认 (inherit)'}
                    {mode === 'direct' && '直接出网 (direct)'}
                    {mode === 'named_profile' && '走代理服务器'}
                  </button>
                ))}
              </div>
            </div>

            {modeDraft === 'named_profile' && (
              <div className="proxy-select-wrap">
                <label htmlFor="select-proxy-node">选择代理节点:</label>
                <select
                  id="select-proxy-node"
                  className="proxy-dropdown"
                  value={selectedProfileID}
                  onChange={(e) => setSelectedProfileID(e.target.value)}
                  disabled={writeDisabled || !isTargetCurrent(selectedTarget) || !profiles.length}
                >
                  {!profiles.length && <option value="">暂无代理节点，请在下方添加</option>}
                  {profiles.map((p) => (
                    <option key={p.profile_id} value={p.profile_id}>
                      {p.profile_id} ({p.mode === 'only_socks5' ? 'SOCKS5' : 'HTTP'}, {p.host}:{p.port})
                      {p.secret_present ? ' · [含密码]' : ''}
                    </option>
                  ))}
                </select>
              </div>
            )}

            {/* 操作按钮组 */}
            <div className="actions-row">
              <button
                type="button"
                className="outline test-button"
                onClick={handleTestConnection}
                disabled={writeDisabled || !isTargetCurrent(selectedTarget) || (modeDraft === 'named_profile' && !selectedProfile)}
                title={!isTargetCurrent(selectedTarget) ? '选中的 Runtime 为历史已退役代次' : ''}
              >
                ⚡ 测试连接
              </button>
              <button
                type="button"
                className="primary apply-button"
                onClick={handleSaveAndApply}
                disabled={writeDisabled || !isTargetCurrent(selectedTarget) || (modeDraft === 'named_profile' && !selectedProfile)}
                title={!isTargetCurrent(selectedTarget) ? '选中的 Runtime 为历史已退役代次' : ''}
              >
                保存并应用
              </button>
            </div>

            {/* 状态简报行 */}
            <div className="status-brief-row">
              <div className="brief-item">
                <span className="brief-label">实际生效:</span>
                <strong className={selectedBinding?.desired_status === 'applied' ? 'text-ok' : 'text-warn'}>
                  {appliedSummary}
                </strong>
              </div>
              {currentTest && (
                <div className="brief-item">
                  <span className="brief-label">测试结果:</span>
                  <span className={`test-badge state-${currentTest.state}`}>
                    ● {displayState(currentTest.state)}
                    {currentTest.duration_ms ? ` (${currentTest.duration_ms}ms)` : ''}
                    {currentTest.diagnostic_code ? ` · ${displayDiagnostic(currentTest.diagnostic_code)}` : ''}
                  </span>
                </div>
              )}
            </div>

            {/* 活动任务运行保护提示 */}
            {selectedActiveRun && (
              <div className="active-run-banner">
                <span>
                  当前正在运行任务 <strong>{selectedActiveRun.task_id}</strong>，已固定网络快照（{activeRunNetworkText(selectedActiveRun)}）。修改将在下一个任务执行时生效。
                </span>
                {selectedActiveRun.task_id && typeof onOpenTask === 'function' && (
                  <button type="button" className="text-link" onClick={() => onOpenTask(selectedActiveRun.task_id)}>
                    查看任务 →
                  </button>
                )}
              </div>
            )}
          </div>
        ) : (
          <div className="empty-notice">暂无可配置的 Runtime 目标</div>
        )}
      </div>

      {/* 2. 代理池管理卡片 */}
      <div className="simple-card pool-card">
        <div className="pool-card-head">
          <div>
            <h3>代理池管理 ({profiles.length})</h3>
            <p>可供任何 Runtime 选用的 HTTP / SOCKS5 代理服务器</p>
          </div>
          <button
            type="button"
            className="outline add-proxy-btn"
            onClick={() => setShowCreate((v) => !v)}
            disabled={writeDisabled}
          >
            {showCreate ? '收起表单' : '+ 添加新代理'}
          </button>
        </div>

        {showCreate && (
          <form className="simple-create-form" onSubmit={handleCreateProxy}>
            <h4>添加代理服务器</h4>
            <div className="form-grid">
              <label>
                <span>方案名称 / ID</span>
                <input
                  value={createForm.id}
                  onChange={(e) => setCreateForm({ ...createForm, id: e.target.value })}
                  placeholder={newProfileID()}
                  disabled={writeDisabled}
                />
              </label>
              <label>
                <span>协议类型</span>
                <select
                  value={createForm.mode}
                  onChange={(e) => setCreateForm({ ...createForm, mode: e.target.value })}
                  disabled={writeDisabled}
                >
                  <option value="only_http_proxy">HTTP 代理</option>
                  <option value="only_socks5">SOCKS5 代理 (支持用户名密码)</option>
                </select>
              </label>
              <label>
                <span>代理服务器地址</span>
                <input
                  value={createForm.host}
                  onChange={(e) => setCreateForm({ ...createForm, host: e.target.value })}
                  placeholder="127.0.0.1 或 server"
                  required
                  disabled={writeDisabled}
                />
              </label>
              <label>
                <span>端口号</span>
                <input
                  type="number"
                  min="1"
                  max="65535"
                  value={createForm.port}
                  onChange={(e) => setCreateForm({ ...createForm, port: e.target.value })}
                  placeholder="7897"
                  required
                  disabled={writeDisabled}
                />
              </label>
              {createForm.mode === 'only_socks5' && (
                <>
                  <label>
                    <span>用户名 (可选)</span>
                    <input
                      value={createForm.username}
                      onChange={(e) => setCreateForm({ ...createForm, username: e.target.value })}
                      autoComplete="off"
                      disabled={writeDisabled || !canManageSecrets}
                    />
                  </label>
                  <label>
                    <span>密码 (可选)</span>
                    <input
                      type="password"
                      value={createForm.password}
                      onChange={(e) => setCreateForm({ ...createForm, password: e.target.value })}
                      autoComplete="new-password"
                      disabled={writeDisabled || !canManageSecrets}
                    />
                  </label>
                </>
              )}
            </div>
            <div className="form-actions">
              <button type="button" className="text-btn" onClick={() => setShowCreate(false)}>取消</button>
              <button type="submit" className="primary" disabled={writeDisabled}>确认添加到代理池</button>
            </div>
          </form>
        )}

        <div className="proxy-item-list">
          {profiles.map((p) => (
            <div className={`proxy-item-card ${p.profile_id === selectedProfileID ? 'selected' : ''}`} key={p.profile_id}>
              <div className="proxy-item-left">
                <span className={`protocol-tag ${p.mode}`}>
                  {p.mode === 'only_socks5' ? 'SOCKS5' : 'HTTP'}
                </span>
                <div className="proxy-item-info">
                  <strong>{p.profile_id}</strong>
                  <span>{p.host}:{p.port}</span>
                </div>
              </div>
              <div className="proxy-item-right">
                {p.secret_present && <span className="secret-tag">🔒 已设密码</span>}
              </div>
            </div>
          ))}
          {!profiles.length && <div className="empty-notice">代理池暂无配置</div>}
        </div>
      </div>
    </section>
  )
}
