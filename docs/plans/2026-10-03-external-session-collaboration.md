# 原 Codex 会话之间的 OAX 协作

## 结果与边界

用户已批准实施：Rhythm 与 Pay 两个现有 Codex 会话保留原执行宿主，通过 OAX 自主咨询、回复和后续续办。用户不再转述消息。先完成只读交接 `rhythm-pay-handoff-01`，不执行订单、退款、注册、部署、业务数据或凭据变更，不修改两个业务工程当前开发内容。

| Agent | 用户指定业务目录 | 原 thread |
|---|---|---|
| rhythm | /home/sky/work/touzi/OneAxe/rhythm | 01a0b4e3-7b2e-7822-8ae1-9f0e812115a9 |
| oneaxe-pay | /home/sky/work/touzi/OneAxe/oneaxe-pay-service | 01a0e016-d951-77d1-bc7e-d13662f4823c |

这里的映射由用户指定，历史 cwd 不能替代它。能读到 rollout 不等于连接原宿主。不能为接入另起 Worker/app-server 抢同一 thread，也不向忙碌 thread 裸发 turn/start、send-keys 或修改会话文件注入。

## 最小实现

1. **先核对原宿主。** 记录本机 CLI、Desktop、transport 与队列协议。只读核对实际已加载 thread；原宿主必须具备忙时排队、空闲启动后续轮和可核对的消息标识。共享 daemon 能读历史并不构成此证明。不重启用户共享宿主。
2. **复用 OAX 控制面。** 新增 external-session 消息模式、两张小表（会话绑定、协作消息），复用 Agent/Principal/Organization、SQLite 事务和 Journal；不创建永久占位 Task、不伪造 Run、不开第二套平台。
3. **让 Agent 简单使用。** CLI 帮助提供绑定、发信、收信、确认、关联回复及状态；绑定用 owner 授权，通信使用 Agent 专用凭据和 peer 范围，凭据仅保存在本机受限文件。既有 managed Worker 与外部会话互斥。
4. **投递不等于完成。** 消息先持久化再投递；稳定幂等键核对完整载荷。结果只能回复原始请求，由服务推导接收者，不能给结果再自动回结果。结果为 Agent 自述，独立验收另计。
5. **宿主连接器只转送。** 只有真实原宿主具备上述能力才启用常驻自动转送；外部确认丢失保留 unknown 并核对，不盲目重发。宿主阻断时仍交付可独立验证的通信入口，但自动唤醒明确保留未完成，不冒称全链通过。

## 本轮 E2E 与证据

| 用例 | 真实验收 | 失败/不变量 |
|---|---|---|
| E01 宿主定位 | 原宿主显示两条目标 thread 的真实状态 | notLoaded/仅磁盘历史不能通过 |
| E02 身份与绑定 | 正式入口登记两个领域身份及专用通信凭据 | managed/external 双向互斥、旧凭据失效、非 peer 拒绝 |
| E03 持久通信 | 实际 CLI/API 发信、收信、ack、关联 reply、Journal | 同 key 同载荷幂等，改载荷冲突，禁止回复环 |
| E04 原会话协作 | Rhythm 自行请求→Pay 自行答复→Rhythm 收结果后续办 | 必须有原会话 turn/消息证据；主代理替写不算 |
| E05 宿主排队 | 目标忙时不打断，空闲时启动后续轮，界面可见 | 稳定投递标识，丢确认不自动重复执行 |
| E06 持久与恢复 | 通信服务重启后消息/身份/回执仍在 | 进程崩溃不得把未知投递自动改为待重试 |

首条咨询核实 `steadyflow-334` 身份能否延续、`steadyflow-334-30d-v1` 商品及 `18458/internal/payment-events` 当前接收者归属。双方附各自源码/运行快照证据，明确 customer/order/operation、独立 runtime 与迁移差异。源码能力、历史运行快照与本轮真实运行分开。

执行记录：[EXECUTION-LOG](../reports/validation/2026-10-03-external-session/EXECUTION-LOG.md)。原始证据存本机持久目录，脱敏副本及 SHA-256 清单入库。保存首次失败、修复和复验；mock 与单测仅覆盖逻辑，不能替代 E01/E04/E05。结论按源码、隔离真实 API、安装、原会话和业务效果分层。

## 本轮裁决

- 必做：身份不可冒用 owner、消息不丢失、不重复执行、不假成功；原会话不被第二执行者接管。
- 有界核实：宿主的原生外部队列入口。两次同因失败即换更小验证路径；无支持能力时报告明确阻断。
- 后置：通用组织治理、完整 Exchange 工作流、新 Web 控制台、其他 Runtime 自动唤醒。后续由真实使用缺口触发，不阻碍本轮通信。
