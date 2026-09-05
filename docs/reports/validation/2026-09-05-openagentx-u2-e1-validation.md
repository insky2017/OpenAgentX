---
doc_type: validation_report
scope: U2/E1 final isolated closure
status: partial_incomplete
owner: independent-verification
prepared_at: 2026-09-05
---

# OpenAgentX U2/E1 最终功能验收

> 状态：`partial/incomplete`。本报告记录 r3 已实际发生的验证事实；它不是完整验收通过结论。历史准备材料见 [最终收口准备](2026-09-05-openagentx-u2-e1-final-closure-preparation.md) 与 [E1 准备稿](2026-09-05-openagentx-e1-validation.md)。

## 最后状态：用户停止（2026-09-06，主代理补记）

- 所有实现、补测和部署已停止，目标 `paused`；下文执行条件仅保留历史用途，不构成恢复授权。
- 此前 observer queued 的原因已经推进并修复：夹具缺模式绑定，同时真实 HTTP `UNSUPPORTED_CAPABILITY` 未映射导致 Worker 退出。四文件修复已完成定向测试及独立源码审核，但未提交。
- 停止时收取的原 Control API 流程确认，mode test `network-mode-test-9c8bff72-5bde-484a-b5c8-06ecc2c1ccf4` 成功并返回七层结果；observer generation 3 随后发布，binding 为 applied/revision 1。原 Task `task-5f9abc00-e2f3-4143-9ba2-48fbf79c6e44` 已进入 waiting_input，Run 数为 1。这是主代理取得的有限动态事实，未再独立浏览器验收。
- 该 observer 使用修复后二进制 SHA-256 `397910eb924c0e2f0eaee03864dbd799a80b57c1ab53417fdc2a50082075ff7a`；下文 AGY 任务仍绑定原 `c9fdfd...` 产物，不能混用证据。
- r3 隔离 daemon、observer 及相关子进程已停止，清理结果为 remaining_owned_pids=[]、forced_kill_count=0；生产与外部代理未修改。U2 和最后修复未提交；浏览器、错误配置、关键重放与完整 B 证据仍未完成，Gate 保持 partial/incomplete。
- 完整停止交接和资源分析见[执行复盘](2026-09-06-openagentx-execution-retrospective.md)。

## 当前状态

- r2 在 owner 凭据自动输入时第二次发生终端回显事故。该值不记录、不复述；r2 的 owner 凭据、会话、服务、Chrome profile 和由其产生的后续状态均永久废弃。
- r2 退役时，其 daemon、Worker、SOCKS5、专用 Chrome、未完成 agent-apply 和相关 wrapper 子进程已停止，当时确认 `18180`、`18081`、`18240`、`18241` 无监听。该历史检查不代表随后复用端口的 r3 当前服务状态。
- 最终功能批次已在全新 r3 fixture 中执行，使用独立 AGY-only Agent 和 fake-only observer Agent；已完成项与失败见下文。用户当前要求讨论收口与交付方案，新增验证暂停，当前仅整理记录。

## 已保留的非秘密前置事实

- r3 AGY 任务使用的二进制 SHA-256：`c9fdfd262c5703cf0de243fdbcf8b1a8b759a4f0f3e9a8b79a3b727124756314`；后续 observer 修复产物见顶部补记。
- 当前应用树指纹：`44f5c7689f36979201e6140ea89aa050d95a86e928c2a0d530c632d1bb21b5c3`；Web 资源指纹：`620607eecb2b41150179c2c7428a7a800d57f1931379496d4822215499638c39`。
- 六个受影响 Go 包与 Web build 在 r2 事故前已通过；这些只作为构建前置，不替代 r3 的真实生命周期证据。

## 独立 E1 Native 规则

- 在新私有 fixture `/tmp/openagentx-e1-r3-final-20260905`，selected `no_blackip`（SOCKS5）与 `http_config_host_port`（HTTP）均返回受控 marker，且各自代理连接计数为 `1`。
- 输入身份：wrapper SHA-256 `31935c961ea76532962068c0e1a8d0a258c80d2f74940dcff3532705ca38b8de`；mgraftcp SHA-256 `0e63ad80c491795a5e448cde3712bb57272ff249d464fb76ccdb0ae3f463f678`；curl SHA-256 `d36b655aa8d119ba070413f8552fb26dc68922e1ff06fc80d430002b4c4f7b92`。
- 脚本已按有界清理关闭受控 target、proxy、socket 和进程组。该结果只证明 wrapper/native 规则效果，不证明 AGY 模型、Task、Worker 常驻或业务副作用。

## r3 实际执行事实

### Runtime profile

- 通过 r3 指挥台创建 HTTP profile `e1-http-g4`，端点为 `127.0.0.1:7897`。
- 针对 AGY backend `named`、Worker `worker-93b6c5ba-cab0-476d-b44f-ed5b34c7f7d0`、generation `4` 完成正式 profile Test：内容 v1，状态为流程完成，耗时 886 ms，无诊断。
- Test 分层事实：configuration 通过（22 ms）、secret 不适用、endpoint 通过、direct rules 不适用、runtime health 通过（308 ms）；network effect 与 model call 均为 `not_verified`，不得外推。
- profile v1 已发布；binding r1 已应用至同一 Worker/generation，UI 显示 named profile `e1-http-g4 v1` 与 Runtime 健康正常。

### AGY Task 链

- 所有三项均使用同一 Worker `worker-93b6c5ba-cab0-476d-b44f-ed5b34c7f7d0`、generation `4`、`agy-batch/named`、named profile v1、binding r1。产品 Task 均因 `business_effect_unverified` 保持 `uncertain`；RunAttempt runtime 状态为 `succeeded`。
- Task A `task-450a7f16-2554-441d-bc93-cafeb7dcdde2`：模型返回 SHA `5e52eea3b68246156cc6544b51e29da2c65016d7ed5c0f9a086c403b8b2e1446`。外部核验显示 marker 仅 31 bytes，缺少末尾 LF，原预期 SHA `efaca48d9c60940b6dd83d7af0c23a1bc0ca7d1b5773933cee0287d1cb260eae` 不匹配。此项明确失败，未改写为通过。
- Task B `task-3b578134-5cd1-4a4b-b31c-f1715c5149c9`：在 A2 前只读原文件；作为同一常驻 Worker 接单与原 31-byte 状态的事实，不能满足 A2 后 32-byte 断言。
- A2 `task-1da1fff7-a03d-4301-b2f5-0b90ca5b46a7`：仅当前置 SHA 等于 Task A 的 31-byte SHA 时追加一个 LF。模型结果与外部 `cmp` 均确认文件为 32 bytes、精确字节匹配，SHA 为 `efaca48d9c60940b6dd83d7af0c23a1bc0ca7d1b5773933cee0287d1cb260eae`。
- 已在 UI 的 Run/Result 视图观察 Task A 与 A2 的 Worker/generation、固定网络、timeline 与 Result；未导出或补造完整 Journal 原始数据。

### 有限前端事实

- observer Task `task-5f9abc00-e2f3-4143-9ba2-48fbf79c6e44` 的内容 DOM 已显示链接、表格与“图片已隐藏”；原文与复制按钮可见。
- 该 Task 在观察窗口内保持 `queued`，未进入预期 `waiting_input`。因此没有断线恢复、消息恰一次、离线浏览器 Control POST=0 的证据。
- 创建该 observer 内容时，shell 层误解析了 code fence 的反引号，终端产生无副作用 command-not-found；当前 Task 内容不构成代码块渲染证据。未暴露凭据，也未重试创建。

## 未完成与证据缺口

- observer 后续已进入 `waiting_input`（见顶部主代理补记）；断线恢复、浏览器写保护/Control POST 计数、消息恰一次仍未完成。
- 坏配置正式失败、关键 idempotency key 重放、三视口/触控与手机代码表格、完整截图/网络 manifest 均未完成。
- 未导出 Task B 的完整 Run/Result/Journal 白名单字段；没有补造缺失 Journal、Run ID、截图或请求计数。
- 外部凭据事故包含两类外部代理/API 凭据暴露风险；原始值未记录。外部凭据轮换未确认完成。

## r3 执行条件（历史）

- 新口令必须由 `0600` 文件通过已验证的专用 helper 受控加载，严禁 `/dev/tty`、`write_stdin`、命令替换、argv 或延迟 prompt 注入。
- AGY Agent 只登记 named AGY backend；observer Agent 只登记 fake backend 并产生 `waiting_input -> succeeded` 观察生命周期。
- r3 的真实 UI named 配置、Task A/B、外部 workspace 核验、E1 selected native cases 和单一 Chrome 批次均须重新收集证据；r2 不得作为最终通过依据。
