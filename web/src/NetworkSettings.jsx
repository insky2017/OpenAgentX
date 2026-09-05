import React, { useEffect, useMemo, useRef, useState } from 'react'

import './network-settings.css'

const EMPTY_FORM = {
  mode: 'only_http_proxy',
  host: '',
  port: '8080',
  direct_ips: '',
}

const stateLabels = {
  draft: '草稿',
  testing: '测试中',
  ready: '测试通过',
  published: '已发布',
  stale: '需重新测试',
  pending: '等待 Worker',
  claimed: '执行中',
  applied: '已应用',
  failed: '失败',
  succeeded: '流程完成',
  queued: '排队中',
  starting: '启动中',
  running: '运行中',
  waiting_approval: '等待审批',
  finishing: '收尾中',
  uncertain: '结果不确定',
  canceled: '已取消',
  healthy: '正常',
  degraded: '降级',
  unavailable: '不可用',
  unknown: '未知',
}

const modeLabels = {
  inherit: '继承 Worker 环境',
  direct: '不注入代理',
  named_profile: '命名代理方案',
  only_http_proxy: 'HTTP 代理',
  only_socks5: 'SOCKS5 代理',
}

const diagnosticLabels = {
  INVALID_CONFIG: '配置无效',
  SECRET_MISSING: '缺少凭据',
  ENDPOINT_UNREACHABLE: '端点不可达',
  MATERIALIZATION_FAILED: '配置写入失败',
  RUNTIME_HEALTH_FAILED: 'Runtime 健康检查失败',
  RUNTIME_IDENTITY_CHANGED: 'Runtime 身份已变化',
  UNSUPPORTED_CAPABILITY: '能力不支持',
  NOT_VERIFIED: '未执行核验',
  INHERITED_CONFIGURATION_UNVERIFIED: '继承配置无法核验',
}

const probeLayerLabels = {
  configuration: '配置',
  secret: '凭据',
  endpoint: '代理端点',
  direct_rules: '直连规则',
  runtime_health: 'Runtime 健康',
  network_effect: '网络效果',
  model_call: '模型调用',
}

const probeStateLabels = {
  passed: '通过',
  failed: '失败',
  not_applicable: '不适用',
  not_verified: '未核验',
}

const newProfileID = () => {
  const suffix = crypto.randomUUID?.().replaceAll('-', '').slice(0, 10) || `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
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

const targetLabel = (option) => `${option.agent_id} / ${option.backend?.backend_id} · Worker ${option.worker_id} · gen ${option.generation}`

const supportsNamedProfile = (option) => option?.backend?.descriptor?.network_modes?.includes('named_profile')

const supportedNetworkModes = (option) => option?.backend?.descriptor?.network_modes || []

const supportsAnyNetworkMode = (option) => supportedNetworkModes(option)
  .some((mode) => ['inherit', 'direct', 'named_profile'].includes(mode))

const displayState = (value) => stateLabels[value] || '未知'

const displayDiagnostic = (value) => value
  ? diagnosticLabels[value] || '未识别的诊断'
  : '无诊断'

const shortDigest = (value) => value ? `${value.slice(0, 10)}...${value.slice(-6)}` : '无'

const displayTime = (value) => {
  if (!value) return '未知'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '未知' : date.toLocaleString()
}

const desiredBindingText = (binding) => {
  if (!binding) return '未绑定'
  if (binding.mode === 'named_profile') {
    return binding.profile_id && binding.profile_version
      ? `${modeLabels.named_profile} · ${binding.profile_id} v${binding.profile_version}`
      : `${modeLabels.named_profile} · 版本未知`
  }
  if (binding.mode === 'inherit' || binding.mode === 'direct') {
    return `${modeLabels[binding.mode]} · policy v${binding.policy_version || '未知'}`
  }
  return '未识别的期望状态'
}

const appliedBindingText = (binding) => {
  if (!binding?.applied_mode) return binding ? `${displayState(binding.desired_status)} · 尚未应用` : '未绑定'
  const configuration = binding.applied_mode === 'named_profile'
    ? `${modeLabels.named_profile} · ${binding.applied_profile_id || '未知方案'} v${binding.applied_profile_version || '未知'}`
    : `${modeLabels[binding.applied_mode] || binding.applied_mode} · policy v${binding.applied_policy_version || '未知'}`
  return `${configuration} · Worker ${binding.applied_worker_id || '未知'} · gen ${binding.applied_generation || '未知'}`
}

const activeRunNetworkText = (run) => {
  if (!run?.network_mode || !['inherit', 'direct', 'named_profile'].includes(run.network_mode)) return '网络模式未记录'
  if (run.network_mode === 'named_profile') {
    return `${modeLabels.named_profile} · ${run.network_profile_id || '方案未记录'} · ${run.network_profile_version ? `v${run.network_profile_version}` : '版本未记录'}`
  }
  return `${modeLabels[run.network_mode]} · ${run.network_policy_version ? `policy v${run.network_policy_version}` : 'policy 未记录'}`
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
  switch (error?.status) {
  case 400:
    return '输入无效，请检查当前内容。'
  case 401:
    return '登录状态已失效，请重新登录。'
  case 403:
    return '当前账号没有执行此操作的权限。'
  case 404:
    return '操作对象已不存在，请刷新。'
  case 409:
    return '版本已变化。非秘密输入仍已保留，请刷新后重新确认。'
  case 422:
    return '目标 Runtime 未声明所需的网络能力。'
  default:
    return '无法确认操作结果，请刷新核对后再决定是否重试。'
  }
}

function StatusFact({ label, value, tone = '' }) {
  return (
    <div className="network-fact">
      <span>{label}</span>
      <strong className={tone}>{value}</strong>
    </div>
  )
}

function ProbeResults({ results }) {
  if (!Array.isArray(results) || !results.length) {
    return <div className="network-probe-empty">无分层记录</div>
  }
  return (
    <div className="network-probe-list" aria-label="分层检查事实">
      {results.map((probe, index) => {
        const knownLayer = Object.hasOwn(probeLayerLabels, probe?.layer)
        const knownState = Object.hasOwn(probeStateLabels, probe?.state)
        const state = knownState ? probe.state : 'unknown'
        return (
          <div className="network-probe-row" key={`${knownLayer ? probe.layer : 'unknown'}-${index}`}>
            <strong>{knownLayer ? probeLayerLabels[probe.layer] : '未知检查项'}</strong>
            <span className={`probe-state state-${state}`}>{knownState ? probeStateLabels[probe.state] : '状态未知'}</span>
            <span>{displayDiagnostic(probe?.diagnostic_code)}</span>
            <span>{Number.isFinite(probe?.duration_ms) && probe.duration_ms > 0 ? `${probe.duration_ms} ms` : '耗时未记录'}</span>
          </div>
        )
      })}
    </div>
  )
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
  const versions = networkState.versions || []
  const tests = networkState.tests || []
  const modeTests = networkState.mode_tests || []
  const bindings = networkState.bindings || []
  const activeRuns = networkState.active_runs || []
  const targets = useMemo(() => runtimeOptions.filter(supportsAnyNetworkMode), [runtimeOptions])
  const [selectedProfileID, setSelectedProfileID] = useState('')
  const [selectedTargetKey, setSelectedTargetKey] = useState('')
  const [showCreate, setShowCreate] = useState(false)
  const [createID, setCreateID] = useState(() => newProfileID())
  const [createForm, setCreateForm] = useState(EMPTY_FORM)
  const [editForm, setEditForm] = useState(EMPTY_FORM)
  const [editDirty, setEditDirty] = useState(false)
  const [editBaselineRevision, setEditBaselineRevision] = useState(0)
  const [editConflict, setEditConflict] = useState(false)
  const [secretForm, setSecretForm] = useState({ username: '', password: '' })
  const [importProfileID, setImportProfileID] = useState(() => newProfileID())
  const [modeDraft, setModeDraft] = useState('')
  const [localBusy, setLocalBusy] = useState('')
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const commandAttempts = useRef(new Map())
  const editProfileID = useRef('')
  const modeTargetKey = useRef('')

  const selectedProfile = profiles.find((profile) => profile.profile_id === selectedProfileID) || null
  const selectedTarget = targets.find((option) => targetKey(option) === selectedTargetKey) || null
  const selectedBinding = selectedTarget
    ? bindings.find((binding) => binding.agent_id === selectedTarget.agent_id && binding.backend_id === selectedTarget.backend?.backend_id)
    : null
  const selectedActiveRun = selectedTarget
    ? activeRuns.find((run) => run.agent_id === selectedTarget.agent_id && run.backend_id === selectedTarget.backend?.backend_id) || null
    : null
  const activeRunBindingMatch = activeRunMatchesBinding(selectedActiveRun, selectedBinding)
  const selectedTargetModes = supportedNetworkModes(selectedTarget)
  const selectedTargetSupportsProfile = supportsNamedProfile(selectedTarget)
  const selectablePolicyModes = selectedTargetModes.filter((mode) => mode === 'inherit' || mode === 'direct')
  const bindingRevision = selectedBinding?.version || 0
  const profileVersions = versions
    .filter((version) => version.profile_id === selectedProfileID)
    .sort((left, right) => right.content_version - left.content_version)
  const profileTests = tests
    .filter((test) => test.profile_id === selectedProfileID)
    .sort((left, right) => Date.parse(right.created_at) - Date.parse(left.created_at))
  const targetModeTests = modeTests
    .filter((test) => test.agent_id === selectedTarget?.agent_id && test.backend_id === selectedTarget?.backend?.backend_id)
    .sort((left, right) => Date.parse(right.created_at) - Date.parse(left.created_at))

  const isModeTestPublishable = (test) => Boolean(selectedTarget
    && test.state === 'succeeded'
    && test.agent_id === selectedTarget.agent_id
    && test.backend_id === selectedTarget.backend?.backend_id
    && test.mode === modeDraft
    && test.worker_instance_id === selectedTarget.worker_id
    && test.generation === selectedTarget.generation
    && test.binding_revision === bindingRevision)

  const versionBelongsToSelectedBinding = (version) => Boolean(selectedTarget
    && selectedProfile
    && selectedBinding?.mode === 'named_profile'
    && selectedBinding.agent_id === selectedTarget.agent_id
    && selectedBinding.backend_id === selectedTarget.backend?.backend_id
    && version.profile_id === selectedProfile.profile_id
    && selectedBinding.profile_id === selectedProfile.profile_id)

  useEffect(() => {
    if (!profiles.length) {
      setSelectedProfileID('')
      return
    }
    if (!profiles.some((profile) => profile.profile_id === selectedProfileID)) {
      setSelectedProfileID(profiles[0].profile_id)
    }
  }, [profiles, selectedProfileID])

  useEffect(() => {
    if (!targets.length) {
      setSelectedTargetKey('')
      return
    }
    if (!targets.some((option) => targetKey(option) === selectedTargetKey)) {
      setSelectedTargetKey(targetKey(targets[0]))
    }
  }, [targets, selectedTargetKey])

  useEffect(() => {
    const nextTargetKey = selectedTarget ? targetKey(selectedTarget) : ''
    const targetChanged = modeTargetKey.current !== nextTargetKey
    modeTargetKey.current = nextTargetKey
    setModeDraft((current) => {
      if (!selectedTarget || !selectablePolicyModes.length) return ''
      if (!targetChanged && selectablePolicyModes.includes(current)) return current
      if (selectablePolicyModes.includes(selectedBinding?.mode)) return selectedBinding.mode
      return selectablePolicyModes[0]
    })
  }, [selectedTargetKey, selectedTargetModes.join('|'), selectedBinding?.mode])

  useEffect(() => {
    if (!selectedProfile) return
    const profileChanged = editProfileID.current !== selectedProfile.profile_id
    editProfileID.current = selectedProfile.profile_id
    if (!profileChanged && editDirty) return
    setEditForm({
      mode: selectedProfile.mode || 'only_http_proxy',
      host: selectedProfile.host || '',
      port: String(selectedProfile.port || ''),
      direct_ips: (selectedProfile.direct_ips || []).join('\n'),
    })
    setEditBaselineRevision(selectedProfile.state_revision)
    setEditConflict(false)
    if (profileChanged) {
      setEditDirty(false)
      setSecretForm({ username: '', password: '' })
    }
  }, [selectedProfile?.profile_id, selectedProfile?.current_content_version, selectedProfile?.state_revision, selectedProfile?.manifest_digest, editDirty])

  const writeDisabled = offline || busy || Boolean(localBusy) || !canWrite
  const ownerWriteDisabled = writeDisabled || !canManageSecrets

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
      setNotice(successMessage)
      let refreshFailed = false
      if (onReload) {
        try {
          await onReload()
        } catch {
          refreshFailed = true
          setError('操作已提交，但状态刷新失败。请手动刷新。')
        }
      }
      return { ok: true, refreshFailed }
    } catch (requestError) {
      setError(secret && (!requestError?.status || requestError.status >= 500)
        ? '无法确认凭据是否已替换，输入已清空。请刷新核对版本后重新输入。'
        : requestErrorText(requestError))
      if (secret && onReload) {
        try { await onReload() } catch { /* The explicit reload action remains available. */ }
      }
      return { ok: false, status: requestError?.status }
    } finally {
      setLocalBusy('')
    }
  }

  const submitCreate = async (event) => {
    event.preventDefault()
    const profileID = createID.trim()
    const host = createForm.host.trim()
    const port = Number(createForm.port)
    if (!profileID || !host || !Number.isInteger(port) || port < 1 || port > 65535) {
      setError('请填写有效的方案 ID、主机和端口。')
      return
    }
    const result = await execute('create', '/api/control/v1/network-profiles', {
      profile_id: profileID,
      mode: createForm.mode,
      host,
      port,
      direct_ips: directIPs(createForm.direct_ips),
      meta: { expected_version: 0 },
    }, '方案草稿已创建。')
    if (result.ok) {
      setSelectedProfileID(profileID)
      setCreateID(newProfileID())
      setCreateForm(EMPTY_FORM)
      setShowCreate(false)
    }
  }

  const submitEdit = async (event) => {
    event.preventDefault()
    if (!selectedProfile) return
    if (editBaselineRevision !== selectedProfile.state_revision) {
      setEditConflict(true)
      setError('方案版本已变化。请先重新确认编辑基线或放弃本地修改。')
      return
    }
    const port = Number(editForm.port)
    if (!editForm.host.trim() || !Number.isInteger(port) || port < 1 || port > 65535) {
      setError('请填写有效的主机和端口。')
      return
    }
    const result = await execute(`edit:${selectedProfile.profile_id}`, `/api/control/v1/network-profiles/${encodeURIComponent(selectedProfile.profile_id)}/draft`, {
      mode: editForm.mode,
      host: editForm.host.trim(),
      port,
      direct_ips: directIPs(editForm.direct_ips),
      meta: { expected_version: editBaselineRevision },
    }, '新草稿版本已保存。')
    if (result.status === 409) setEditConflict(true)
    if (result.ok && !result.refreshFailed) setEditDirty(false)
  }

  const submitSecret = async (event) => {
    event.preventDefault()
    if (!selectedProfile || !canManageSecrets || !secretForm.password) return
    const secret = { username: secretForm.username, password: secretForm.password }
    setSecretForm({ username: '', password: '' })
    await execute(`secret:${selectedProfile.profile_id}`, `/api/control/v1/network-profiles/${encodeURIComponent(selectedProfile.profile_id)}/secret`, {
      ...secret,
      meta: { expected_version: selectedProfile.state_revision },
    }, '凭据已替换，新内容需要重新测试。', true)
  }

  const testProfile = async () => {
    if (!selectedProfile || !selectedTarget || !selectedTargetSupportsProfile) return
    await execute(`test:${selectedProfile.profile_id}:${selectedTargetKey}`, `/api/control/v1/network-profiles/${encodeURIComponent(selectedProfile.profile_id)}/tests`, {
      worker_instance_id: selectedTarget.worker_id,
      generation: selectedTarget.generation,
      backend_id: selectedTarget.backend.backend_id,
      meta: { expected_version: selectedProfile.state_revision },
    }, '测试已提交。')
  }

  const publishProfile = async () => {
    if (!selectedProfile) return
    await execute(`publish:${selectedProfile.profile_id}`, `/api/control/v1/network-profiles/${encodeURIComponent(selectedProfile.profile_id)}/publish`, {
      meta: { expected_version: selectedProfile.state_revision },
    }, '内容版本已发布。')
  }

  const bindProfile = async () => {
    if (!selectedProfile || !selectedTarget || !selectedTargetSupportsProfile || !selectedProfile.published_content_version) return
    await execute(`bind:${selectedTargetKey}`, '/api/control/v1/network-bindings', {
      agent_id: selectedTarget.agent_id,
      backend_id: selectedTarget.backend.backend_id,
      profile_id: selectedProfile.profile_id,
      profile_version: selectedProfile.published_content_version,
      worker_instance_id: selectedTarget.worker_id,
      generation: selectedTarget.generation,
      meta: { expected_version: selectedBinding?.version || 0 },
    }, '绑定已提交，等待 Worker 应用。')
  }

  const rollback = async (version) => {
    if (!selectedTargetSupportsProfile || !versionBelongsToSelectedBinding(version)) {
      setError('当前目标绑定的不是所选方案，无法使用该版本回退。请先选择该绑定方案。')
      return
    }
    if (!version.published || version.current_published) return
    await execute(`rollback:${selectedTargetKey}:${version.content_version}`, '/api/control/v1/network-bindings/rollback', {
      agent_id: selectedTarget.agent_id,
      backend_id: selectedTarget.backend.backend_id,
      target_content_version: version.content_version,
      worker_instance_id: selectedTarget.worker_id,
      generation: selectedTarget.generation,
      meta: { expected_version: selectedBinding.version },
    }, `已从 v${version.content_version} 创建回退草稿，需重新测试并发布。`)
  }

  const importProfile = async (event) => {
    event.preventDefault()
    if (!selectedTarget || !selectedTargetSupportsProfile || !canManageSecrets || !importProfileID.trim()) return
    const result = await execute(`import:${selectedTargetKey}`, '/api/control/v1/network-imports', {
      profile_id: importProfileID.trim(),
      worker_instance_id: selectedTarget.worker_id,
      generation: selectedTarget.generation,
      backend_id: selectedTarget.backend.backend_id,
      meta: { expected_version: 0 },
    }, '一次性导入已提交。')
    if (result.ok) setImportProfileID(newProfileID())
  }

  const testMode = async () => {
    if (!selectedTarget || !selectablePolicyModes.includes(modeDraft)) return
    await execute(`mode-test:${selectedTargetKey}`, '/api/control/v1/network-bindings/mode/tests', {
      agent_id: selectedTarget.agent_id,
      backend_id: selectedTarget.backend.backend_id,
      mode: modeDraft,
      worker_instance_id: selectedTarget.worker_id,
      generation: selectedTarget.generation,
      meta: { expected_version: bindingRevision },
    }, `${modeLabels[modeDraft]}测试已提交。`)
  }

  const publishMode = async (test) => {
    if (!isModeTestPublishable(test)) return
    await execute(`mode-publish:${selectedTargetKey}:${test.test_id}`, '/api/control/v1/network-bindings/mode/publish', {
      test_id: test.test_id,
      worker_instance_id: selectedTarget.worker_id,
      generation: selectedTarget.generation,
      meta: { expected_version: test.binding_revision },
    }, `${modeLabels[test.mode]}已发布，等待 Worker 应用。`)
  }

  const rebaseEdit = () => {
    if (!selectedProfile) return
    setEditBaselineRevision(selectedProfile.state_revision)
    setEditConflict(false)
    setError('')
    setNotice(`本地修改已重新基于状态 r${selectedProfile.state_revision}，请复核后提交。`)
  }

  const discardEdit = () => {
    if (!selectedProfile) return
    setEditForm({
      mode: selectedProfile.mode || 'only_http_proxy',
      host: selectedProfile.host || '',
      port: String(selectedProfile.port || ''),
      direct_ips: (selectedProfile.direct_ips || []).join('\n'),
    })
    setEditBaselineRevision(selectedProfile.state_revision)
    setEditDirty(false)
    setEditConflict(false)
    setError('')
    setNotice('本地修改已放弃。')
  }

  const configurationStatus = ['ready', 'published'].includes(selectedProfile?.state)
    ? '已验证'
    : selectedProfile?.state === 'testing' ? '验证中' : '待验证'
  const testReady = selectedProfile?.state === 'ready'
  const appliedProfile = appliedBindingText(selectedBinding)
  const appliedToSelected = selectedBinding?.desired_status === 'applied'
    && selectedBinding.applied_mode === 'named_profile'
    && selectedBinding.applied_profile_id === selectedProfile?.profile_id
    && selectedBinding.applied_profile_version === selectedProfile?.published_content_version
    && selectedBinding.applied_worker_id === selectedTarget?.worker_id
    && selectedBinding.applied_generation === selectedTarget?.generation
  const runtimeHealth = selectedTarget?.backend?.health || 'unknown'

  return (
    <section className="network-settings" aria-busy={loading || busy || Boolean(localBusy)}>
      <header className="network-settings-head">
        <div>
          <p className="eyebrow">RUNTIME NETWORK</p>
          <h2>网络配置</h2>
        </div>
        <div className="network-head-actions">
          <span className={`network-connection ${offline ? 'is-offline' : ''}`}>{offline ? '离线' : '在线'}</span>
          <button type="button" className="outline" onClick={reload} disabled={offline || loading || Boolean(localBusy)}>刷新</button>
          <button type="button" className="primary" onClick={() => setShowCreate((value) => !value)} disabled={writeDisabled} aria-expanded={showCreate}>
            {showCreate ? '收起' : '新建方案'}
          </button>
        </div>
      </header>

      {offline && <div className="network-alert warning" role="status">当前离线，所有配置写入均已禁用。</div>}
      {error && <div className="network-alert error" role="alert"><span>{error}</span>{error.includes('刷新') && <button type="button" onClick={reload} disabled={offline || loading}>刷新</button>}</div>}
      {notice && <div className="network-alert success" role="status">{notice}</div>}

      {showCreate && (
        <form className="network-create" onSubmit={submitCreate}>
          <div className="network-section-title"><h3>新建方案草稿</h3><span>expected v0</span></div>
          <div className="network-form-grid">
            <label><span>方案 ID</span><input value={createID} onChange={(event) => setCreateID(event.target.value)} required disabled={writeDisabled} /></label>
            <label><span>代理模式</span><select value={createForm.mode} onChange={(event) => setCreateForm({ ...createForm, mode: event.target.value })} disabled={writeDisabled}><option value="only_http_proxy">HTTP 代理</option><option value="only_socks5">SOCKS5 代理</option></select></label>
            <label className="wide"><span>代理主机</span><input value={createForm.host} onChange={(event) => setCreateForm({ ...createForm, host: event.target.value })} placeholder="proxy.example.net" required disabled={writeDisabled} /></label>
            <label><span>端口</span><input type="number" min="1" max="65535" value={createForm.port} onChange={(event) => setCreateForm({ ...createForm, port: event.target.value })} required disabled={writeDisabled} /></label>
            <label className="full"><span>直连 IP（每行一个）</span><textarea value={createForm.direct_ips} onChange={(event) => setCreateForm({ ...createForm, direct_ips: event.target.value })} rows="3" placeholder={'10.0.0.8\n2001:db8::8'} disabled={writeDisabled} /></label>
          </div>
          <div className="network-form-actions"><button className="primary" type="submit" disabled={writeDisabled}>创建草稿</button></div>
        </form>
      )}

      <div className="network-target-row">
        <label htmlFor="network-runtime-target">目标 Agent / Backend / Worker</label>
        <select id="network-runtime-target" value={selectedTargetKey} onChange={(event) => setSelectedTargetKey(event.target.value)} disabled={!targets.length || loading}>
          {!targets.length && <option value="">暂无声明网络能力的 Runtime</option>}
          {targets.map((option) => <option key={targetKey(option)} value={targetKey(option)}>{targetLabel(option)}</option>)}
        </select>
      </div>

      <section className="network-mode-section" aria-label="目标网络模式">
        <div className="network-section-title">
          <h3>目标网络模式</h3>
          <span>{selectedTarget ? `binding r${bindingRevision} · Runtime ${displayState(selectedTarget.backend?.health || 'unknown')}` : '未选择目标'}</span>
        </div>
        {selectedTarget && (
          <>
            <div className="network-mode-overview">
              <div className="network-mode-control">
                <span className="network-control-label">待测模式</span>
                <div className="network-mode-options" role="radiogroup" aria-label="待测网络模式">
                  {selectablePolicyModes.map((mode) => (
                    <button
                      type="button"
                      role="radio"
                      aria-checked={modeDraft === mode}
                      className={modeDraft === mode ? 'selected' : ''}
                      key={mode}
                      onClick={() => setModeDraft(mode)}
                      disabled={writeDisabled}
                    >
                      {modeLabels[mode]}
                    </button>
                  ))}
                  {!selectablePolicyModes.length && <span className="network-inline-state">该 Runtime 未声明 inherit/direct 能力</span>}
                </div>
                {modeDraft === 'direct' && <small>Runtime 不注入代理，仍受主机路由与外部网络策略约束。</small>}
              </div>
              <div className="network-mode-facts">
                <StatusFact label="期望状态" value={desiredBindingText(selectedBinding)} />
                <StatusFact label="实际应用" value={appliedBindingText(selectedBinding)} tone={selectedBinding?.desired_status === 'applied' ? 'ok' : 'warn'} />
                <StatusFact label="声明能力" value={selectedTargetModes.map((mode) => modeLabels[mode] || mode).join(' / ')} />
              </div>
            </div>
            <div className="network-active-run" aria-label="当前 Backend 活动任务网络快照">
              {selectedActiveRun ? (
                <>
                  <div className="network-active-run-head">
                    <div><span>活动任务网络快照</span><strong>Task {selectedActiveRun.task_id || '未记录'}</strong></div>
                    <span className={`network-state state-${selectedActiveRun.status}`}>{displayState(selectedActiveRun.status)}</span>
                    {selectedActiveRun.task_id && typeof onOpenTask === 'function' && <button type="button" className="text-button" onClick={() => onOpenTask(selectedActiveRun.task_id)}>打开任务</button>}
                  </div>
                  <span>固定快照：{activeRunNetworkText(selectedActiveRun)} · binding r{selectedActiveRun.network_binding_revision || '未记录'}</span>
                  <span>执行 Worker：{selectedActiveRun.worker_instance_id || '未记录'} · gen {selectedActiveRun.worker_generation || '未记录'}</span>
                  <span>当前期望：{desiredBindingText(selectedBinding)}</span>
                  <strong className={activeRunBindingMatch === true ? 'match' : activeRunBindingMatch === false ? 'mismatch' : ''}>
                    对照：{activeRunBindingMatch === true ? '与当前期望一致' : activeRunBindingMatch === false ? '固定快照与当前期望不同' : selectedBinding ? '快照字段未记录，无法确认' : '当前无期望绑定，无法确认'}
                  </strong>
                </>
              ) : <div className="network-inline-state">当前 Backend 无活动 Run</div>}
            </div>
            <div className="network-command-actions network-mode-actions">
              <button className="outline" type="button" onClick={testMode} disabled={writeDisabled || !modeDraft}>测试模式</button>
            </div>
            <div className="network-mode-test-list" aria-label="模式测试历史">
              {targetModeTests.map((test) => {
                const publishable = isModeTestPublishable(test)
                const matchesSelection = test.mode === modeDraft
                  && test.worker_instance_id === selectedTarget.worker_id
                  && test.generation === selectedTarget.generation
                  && test.binding_revision === bindingRevision
                return (
                  <div className="network-mode-test-row" key={test.test_id}>
                    <span className={`network-state state-${test.state}`}>{displayState(test.state)}</span>
                    <strong>{modeLabels[test.mode] || test.mode} · policy v{test.policy_version}</strong>
                    <span>binding r{test.binding_revision} · Worker {test.worker_instance_id} · gen {test.generation}</span>
                    <span>{test.duration_ms ? `${test.duration_ms} ms` : '耗时未知'} · {displayDiagnostic(test.diagnostic_code)}</span>
                    <time>{displayTime(test.created_at)}</time>
                    <div className="network-mode-test-action">
                      <span className={publishable ? 'match' : ''}>{publishable ? '当前可发布' : matchesSelection && ['pending', 'claimed'].includes(test.state) ? '等待测试通过' : matchesSelection ? '测试未通过' : '与当前选择不匹配'}</span>
                      <button className="outline" type="button" onClick={() => publishMode(test)} disabled={writeDisabled || !publishable}>发布模式</button>
                    </div>
                    <ProbeResults results={test.probe_results} />
                  </div>
                )
              })}
              {!targetModeTests.length && <div className="network-empty compact">暂无模式测试记录</div>}
            </div>
          </>
        )}
        {!selectedTarget && <div className="network-empty compact">暂无可配置的 Runtime 目标</div>}
      </section>

      <div className="network-workspace">
        <aside className="network-profile-pane" aria-label="网络方案列表">
          <div className="network-pane-title"><strong>方案</strong><span>{profiles.length}</span></div>
          <div className="network-profile-list">
            {profiles.map((profile) => (
              <button type="button" key={profile.profile_id} className={profile.profile_id === selectedProfileID ? 'selected' : ''} onClick={() => setSelectedProfileID(profile.profile_id)} aria-pressed={profile.profile_id === selectedProfileID}>
                <span><strong>{profile.profile_id}</strong><small>{modeLabels[profile.mode] || profile.mode} · {profile.host}:{profile.port}</small></span>
                <span className={`network-state state-${profile.state}`}>{displayState(profile.state)}</span>
                <small>内容 v{profile.current_content_version} · 状态 r{profile.state_revision}</small>
              </button>
            ))}
            {!profiles.length && <div className="network-empty">暂无网络方案</div>}
          </div>
        </aside>

        <div className="network-detail-pane">
          {loading && <div className="network-empty" role="status">正在同步网络状态...</div>}
          {!loading && !selectedProfile && <div className="network-empty">选择或创建一个网络方案</div>}
          {!loading && selectedProfile && (
            <>
              <div className="network-detail-head">
                <div><span className="eyebrow">{selectedProfile.profile_id}</span><h3>{selectedProfile.host}:{selectedProfile.port}</h3></div>
                <span className={`network-state state-${selectedProfile.state}`}>{displayState(selectedProfile.state)}</span>
              </div>

              <div className="network-facts" aria-label="配置状态">
                <StatusFact label="配置有效" value={configurationStatus} tone={configurationStatus === '已验证' ? 'ok' : 'warn'} />
                <StatusFact label="测试状态" value={displayState(selectedProfile.state)} tone={testReady || selectedProfile.state === 'published' ? 'ok' : 'warn'} />
                <StatusFact label="发布内容" value={selectedProfile.published_content_version ? `v${selectedProfile.published_content_version}` : '未发布'} />
                <StatusFact label="Worker 应用" value={appliedProfile} tone={appliedToSelected ? 'ok' : 'warn'} />
                <StatusFact label="Runtime 健康" value={displayState(runtimeHealth)} tone={runtimeHealth === 'healthy' ? 'ok' : runtimeHealth === 'unknown' ? '' : 'warn'} />
              </div>

              <form className="network-section" onSubmit={submitEdit}>
                <div className="network-section-title"><h3>方案内容</h3><span>内容 v{selectedProfile.current_content_version} · 编辑基线 r{editBaselineRevision} · 当前 r{selectedProfile.state_revision}</span></div>
                {(editConflict || (editDirty && editBaselineRevision !== selectedProfile.state_revision)) && <div className="network-edit-conflict" role="alert"><span>服务端版本已变化，本地修改尚未重新基于当前版本。</span><div><button type="button" className="outline" onClick={rebaseEdit}>按当前版本重新确认</button><button type="button" className="text-button" onClick={discardEdit}>放弃本地修改</button></div></div>}
                <div className="network-form-grid">
                  <label><span>代理模式</span><select value={editForm.mode} onChange={(event) => { setEditForm({ ...editForm, mode: event.target.value }); setEditDirty(true) }} disabled={writeDisabled}><option value="only_http_proxy">HTTP 代理</option><option value="only_socks5">SOCKS5 代理</option></select></label>
                  <label className="wide"><span>代理主机</span><input value={editForm.host} onChange={(event) => { setEditForm({ ...editForm, host: event.target.value }); setEditDirty(true) }} required disabled={writeDisabled} /></label>
                  <label><span>端口</span><input type="number" min="1" max="65535" value={editForm.port} onChange={(event) => { setEditForm({ ...editForm, port: event.target.value }); setEditDirty(true) }} required disabled={writeDisabled} /></label>
                  <label className="full"><span>直连 IP（每行一个，不支持 CIDR）</span><textarea value={editForm.direct_ips} onChange={(event) => { setEditForm({ ...editForm, direct_ips: event.target.value }); setEditDirty(true) }} rows="3" disabled={writeDisabled} /></label>
                </div>
                <div className="network-form-actions"><span>manifest {shortDigest(selectedProfile.manifest_digest)}</span><button className="outline" type="submit" disabled={writeDisabled || editConflict || editBaselineRevision !== selectedProfile.state_revision}>保存为新草稿</button></div>
              </form>

              <section className="network-section">
                <div className="network-section-title"><h3>凭据</h3><span>{selectedProfile.secret_present ? '已配置' : '未配置'}</span></div>
                {selectedProfile.mode === 'only_http_proxy' && <div className="network-inline-state">HTTP 代理模式不支持认证凭据。</div>}
                {selectedProfile.mode === 'only_socks5' && !canManageSecrets && <div className="network-inline-state">仅 Owner 可替换凭据。</div>}
                {selectedProfile.mode === 'only_socks5' && canManageSecrets && (
                  <form className="network-secret-form" onSubmit={submitSecret} autoComplete="off">
                    <label><span>用户名</span><input value={secretForm.username} onChange={(event) => setSecretForm({ ...secretForm, username: event.target.value })} disabled={ownerWriteDisabled} autoComplete="off" /></label>
                    <label><span>密码</span><input type="password" value={secretForm.password} onChange={(event) => setSecretForm({ ...secretForm, password: event.target.value })} required disabled={ownerWriteDisabled} autoComplete="new-password" /></label>
                    <button className="outline" type="submit" disabled={ownerWriteDisabled || !secretForm.password}>替换凭据</button>
                  </form>
                )}
              </section>

              <section className="network-section network-command-section">
                <div className="network-section-title"><h3>测试、发布与绑定</h3><span>{selectedTarget ? targetLabel(selectedTarget) : '未选择目标'}</span></div>
                {selectedTarget && !selectedTargetSupportsProfile && <div className="network-inline-state">该 Runtime 未声明命名代理方案能力。</div>}
                <div className="network-command-actions">
                  <button className="outline" type="button" onClick={testProfile} disabled={writeDisabled || !selectedTarget || !selectedTargetSupportsProfile}>测试当前内容</button>
                  <button className="outline" type="button" onClick={publishProfile} disabled={writeDisabled || !testReady || !selectedProfile.ready_test_id}>发布已测试内容</button>
                  <button className="primary" type="button" onClick={bindProfile} disabled={writeDisabled || !selectedTarget || !selectedTargetSupportsProfile || !selectedProfile.published_content_version}>绑定发布版本</button>
                </div>
                <div className="network-version-compare">
                  <span>期望：{desiredBindingText(selectedBinding)}</span>
                  <span>实际：{appliedBindingText(selectedBinding)}</span>
                </div>
              </section>

              <section className="network-section">
                <div className="network-section-title"><h3>内容版本</h3><span>{profileVersions.length}</span></div>
                <div className="network-table-wrap">
                  <table className="network-table">
                    <thead><tr><th>版本</th><th>端点</th><th>发布事实</th><th>创建时间</th><th><span className="sr-only">操作</span></th></tr></thead>
                    <tbody>
                      {profileVersions.map((version) => <tr key={version.content_version}><td>v{version.content_version}</td><td>{modeLabels[version.mode] || version.mode}<br /><small>{version.host}:{version.port}</small></td><td>{version.current_published ? '当前发布' : version.published ? '曾发布' : '未发布'}</td><td>{displayTime(version.created_at)}</td><td><button type="button" className="text-button" onClick={() => rollback(version)} disabled={writeDisabled || !selectedTargetSupportsProfile || !versionBelongsToSelectedBinding(version) || !version.published || version.current_published}>创建回退草稿</button></td></tr>)}
                      {!profileVersions.length && <tr><td colSpan="5">暂无版本记录</td></tr>}
                    </tbody>
                  </table>
                </div>
              </section>

              <section className="network-section">
                <div className="network-section-title"><h3>分层测试</h3><span>{profileTests.length}</span></div>
                <div className="network-test-list">
                  {profileTests.map((test) => <div className="network-test-row" key={test.test_id}><span className={`network-state state-${test.state}`}>{displayState(test.state)}</span><strong>内容 v{test.content_version}</strong><span>{test.backend_id} · gen {test.generation}</span><span>{test.duration_ms ? `${test.duration_ms} ms` : '耗时未知'}</span><span>{displayDiagnostic(test.diagnostic_code)}</span><time>{displayTime(test.created_at)}</time><ProbeResults results={test.probe_results} /></div>)}
                  {!profileTests.length && <div className="network-empty compact">暂无测试记录</div>}
                </div>
              </section>
            </>
          )}
        </div>
      </div>

      <section className="network-bindings-section">
        <div className="network-section-title"><h3>Worker 绑定</h3><span>{bindings.length}</span></div>
        <div className="network-binding-list">
          {bindings.map((binding) => <div className="network-binding-row" key={`${binding.agent_id}-${binding.backend_id}`}><div><strong>{binding.agent_id} / {binding.backend_id}</strong><span className={`network-state state-${binding.desired_status}`}>{displayState(binding.desired_status)}</span></div><span>期望 {desiredBindingText(binding)} · binding r{binding.version}</span><span>实际 {appliedBindingText(binding)}</span>{binding.diagnostic && <span className="binding-diagnostic">应用诊断：{displayDiagnostic(binding.diagnostic)}</span>}</div>)}
          {!bindings.length && <div className="network-empty compact">暂无 Worker 绑定</div>}
        </div>
      </section>

      {canManageSecrets && (
        <form className="network-import-section" onSubmit={importProfile}>
          <div className="network-section-title"><h3>从选中 Worker 一次性导入</h3><span>{selectedTarget ? `gen ${selectedTarget.generation}` : '未选择目标'}</span></div>
          <label><span>新方案 ID</span><input value={importProfileID} onChange={(event) => setImportProfileID(event.target.value)} required disabled={ownerWriteDisabled} /></label>
          <button className="outline" type="submit" disabled={ownerWriteDisabled || !selectedTarget || !selectedTargetSupportsProfile || !importProfileID.trim()}>开始导入</button>
        </form>
      )}
    </section>
  )
}
