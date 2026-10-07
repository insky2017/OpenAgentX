# 宿主运维 Agent 历史交接（2026-10-07）

2026-10-08 定名为 `oaxops`，本文当前入口和状态路径已更新。下文组织协调及部署经历属于历史记录；用户决定目标和权限，宿主运维 Agent 负责授权维护和验收协助，不因工具名拥有独立指挥权。

## 当前入口与结果

指挥者在 `/home/sky/docs` 的独立原生 Codex 会话中执行；后台事件由 user-systemd 托管，**不会自动出现在 OAX tmux 窗口里**。运行 `oaxops open` 进入同一会话的前台终端，`oaxops status` 查看身份和最近结果。事件完成后进程退出，不持续调用模型。

2026-10-07 本次维护事件已实际完成联合工件验收、唯一部署链、OAX 重启后的结果核对和回派。最后同 thread `turn.completed`、exit0，writer 已释放。下文保留初次发现、交接及失败证据；当前部署结论见末尾。

## 身份与宿主边界

用户明确指定的现有指挥者工作目录是 `/home/sky/docs`；该目录已经实查存在。不得把 OAX 历史 `orchestrator` 注册身份直接认作这一指挥者：数据库中的旧身份工作目录是 `/home/sky/work/touzi/OneAxe/steadyflow`，无对应 external session binding，当前 Fleet 无在线 Worker/终端。

AGY 的项目缓存中存在 docs 项目 `20d8fe72-b970-4d40-a8b5-c46e2abd17fd`。截至本次核查，AGY 本地 conversation_summaries、conversation_metadata、annotations，以及 Codex thread 索引未找到 docs 的原会话。唯一标题为“初始化指挥者角色”的旧 AGY 会话是 `0509884c-9e3f-40cb-bf75-1648b209d8ff`，工作目录为 steadyflow，2026-08-16 最后更新，不能据此替代 docs 指挥者。当前没有 docs tmux pane，AGY remote-control daemon 为 inactive。

用户随后明确授权在 `/home/sky/docs` 新建独立 Codex 指挥者，不再要求沿用旧会话。新会话 ID：`01a115e8-e528-75f3-9e30-612d617cfb84`，模型 `gpt-6-astra` / `high`，宿主为独立 user-systemd + 原生 `codex exec`，不属于 OAX daemon 或六域 Worker。此新会话不冒用历史 `orchestrator` 身份。

首次只读沙箱因本机 `bwrap: loopback: Failed RTM_NEWADDR: Operation not permitted` 无法读取文件，失败原始输出保留。后续同 thread 采用本机既有无沙箱执行模式；默认“只读”是本次任务约束，不能声称操作系统强隔离。

首轮启动单元为 `oax-independent-commander-bootstrap-20261007.service`；私有原始证据保存在 `/home/sky/.local/state/oaxops/events/bootstrap-20261007/`。实际首轮收悉与后续同 thread 恢复的验收见末尾；启动进程本身不代表接管已完成。

## 组织协调职责与执行权

指挥者负责理解用户目标、分配领域负责人、协调依赖、核对真实验收与向用户交付；各领域仍保有其业务职责。OpenAgentX 领域负责控制面、Worker、原生桥接、Task/Run 持久化及协作协议实现。

本批主代理将唯一执行权移交给指挥者事件 `resume-timeout-rollout-20261007`。指挥者仅启动一次 `scripts/validation/rollout_resume_timeout_batch.py`，该脚本先做最终工件真实验收，再调用 `deploy_resume_timeout.py` 受控部署；指挥者跨重启等待并核对结果。部署链运行期间绝不并行部署或再次重启。仅在链已退出、结果明确失败且原执行者已释放后，才按既有用户授权有界恢复；本次 PASS，没有触发额外恢复。不得清空/重放历史任务、直接改数据库或主动重做未知业务副作用。

指挥者的恢复宿主必须独立于 OAX daemon 及被重启的六域 Worker。外层普通进程可等待部署结果事件，只有新结果需要处理时调用一次既有宿主的模型入口；不使用定时 LLM 轮询，不依赖本聊天持续在线。不因仅启动一个进程就报告自动恢复已经验证。

## 当前批次与证据入口

- 产品修复初始 commit：`15f787f3b7b8f04e3fc3fcad975846ec101e0daa`；最终安装产品 `c35d70260eeb39bec83c457ff1acf06269ddf2be`，包含无总截止、registered resume、spinner、原生输入确认。
- 实施工作树：`/home/sky/work/touzi/OneAxe/OpenAgentX-resume-timeout-worktree`；正式交付仓库：`/home/sky/work/touzi/OneAxe/steadyflow/OpenAgentX` 的 `main`。候选、Git 合入和实际安装分别核验。
- 31 分钟真实验收独立单元：`oax-resume-timeout-e2e-20261007.service` 已完成；证据根：`~/.local/state/openagentx/validation/2026-10-07-resume-timeout/long01`，只读补判 PASS，未重跑模型。
- 首轮脚本末尾 SQL 使用 `id` 而实际列为 `run_id` 的夹具问题必须保留失败记录，用只读查询补判；不得为这个夹具问题重跑模型或 31 分钟工具动作。
- 部署结果由部署脚本的 `--evidence` 目录中 `result.json`、`operations.jsonl`、必要时 `recovery.json` 给出；先找到实际 systemd 单元及 argv，不猜测目录，不把 preflight 当部署完成。
- 用户已授权必要重启，但必须保留六域原 thread、window/pane，并核验候选二进制 SHA、实际进程来源、schema、Worker readiness 和原会话恢复。
- 六域已受控重启，配置均为 `timeout: 0s`；新正式 Run 已核验无总截止。旧已冻结 Run/历史终态不改写。

## 权威领域档案快照

下列内容来自正式 SQLite 的只读 `agent_profiles` 与所指向 ROLE 文件。ROLE 中的 runtime 文案可能属于旧批次，不能代替当前 Worker/进程来源核验。文件摘要用于确认本次读取来源，不含凭据。

| Agent | 工作目录 | ROLE 来源与 SHA-256 |
| --- | --- | --- |
| `identity-service` | `/home/sky/work/touzi/OneAxe/oneaxe-identity` | `/home/sky/.openagentx/workers/identities/identity-service.role.md` / `51b0ac4d3cc417f43d9b41c8e32e49e0cdaa8f3ec45ea180b059331313ee6f4f` |
| `oneaxe-voice` | `/home/sky/work/touzi/OneAxe/oneaxe-voice` | `/home/sky/.openagentx/workers/identities/oneaxe-voice.role.md` / `2920c337e97b810b7b6152c8f93f1eed56e4d03353c3f396a242256286f28834` |
| `openagentx` | `/home/sky/work/touzi/OneAxe/steadyflow/OpenAgentX` | `/home/sky/.openagentx/workers/identities/openagentx.role.md` / `1bac57cbe299d1487a6ba4ab0975c0858bc8f92412cdd86f7bd36dfbdeb425bd` |
| `orchestrator` | `/home/sky/work/touzi/OneAxe/steadyflow` | `/home/sky/work/touzi/OneAxe/steadyflow/OpenAgentX/agents/orchestrator/ROLE.md` / `f45a1e24f4da5e3b59430bbe69b58e6f2c475a7828687381ed312b268b042c36` |
| `pay-service` | `/home/sky/work/touzi/OneAxe/oneaxe-pay-service` | `/home/sky/.openagentx/workers/identities/pay-service.role.md` / `2dd74f0257d938fe192e3180e039f719ca9f0fe40a5a1c1a790222032f5dff8c` |
| `quote-service` | `/home/sky/work/touzi/OneAxe/steadyflow` | `/home/sky/work/touzi/OneAxe/steadyflow/OpenAgentX/agents/quote-service/ROLE.md` / `8bea2d3c4d02c02ad378f9053cd15eb53e3408ee8bd2eb2cee8ea9b342f4b6c3` |
| `rhythm` | `/home/sky/work/touzi/OneAxe/rhythm` | `/home/sky/.openagentx/external/rhythm/ROLE.md` / `fd2763ac30677382360c2a1c483ec458b00cd82a2464f6e06343b0e61d2fba09` |

## 六域分工

| Agent | 所属职责 | 不替代的边界 |
| --- | --- | --- |
| quote-service | `stock_quote_service/`、`src/market/` 行情研发、集成、测试；数据通过 Quote Service 获取 | 不改无关模块，不绕过控制面向其他 Agent 发令 |
| rhythm | Rhythm 用户映射、登录、权益、应用数据库与支付接收器 | 支付登记、接口、回调协议、资金记录、凭据归 Pay；不替 Market Hub 决策 |
| openagentx | OAX daemon、Task/Run、SQLite、Worker、Codex app-server、Native Bridge、接入及协作契约 | 不越权执行业务域数据、资金或部署 |
| identity-service | 共享身份、Authentik 容器、OIDC 注册与回调准入、MFA、跨端认证网关 | 不授予业务权限或代管应用业务数据；秘密不入证据 |
| oneaxe-voice | GPU 听写、Linux/Android 客户端、Qwen/R2T2 生命周期、移动 V1 API | 协作意向不等于 peer 权限或行动授权 |
| pay-service | 支付登记、支付 API、回调协议、资金记录与受控凭据交接 | Rhythm 用户、登录、权益、应用 DB 与接收器归 Rhythm |

## 已登记 scope

当前正式 `external_role_scopes` 只登记以下两域，不得虚构其他域 scope 或把职责文案当 peer allowlist。

- `pay.callback_protocol` → `pay-service`：负责支付回调协议；不承接业务应用的接收器部署。
- `pay.credential_handoff` → `pay-service`：负责支付凭据的受控交接；消息正文不得包含凭据明文。
- `pay.fund_records` → `pay-service`：负责支付与资金记录及相应查询核对。
- `pay.payment_api` → `pay-service`：负责支付接口及其调用契约。
- `pay.registration` → `pay-service`：负责应用在支付系统中的登记。
- `rhythm.app_db_migration` → `rhythm`：负责Rhythm应用数据库迁移；支付领域Agent不替代执行。
- `rhythm.entitlement_verification` → `rhythm`：负责Rhythm用户权益核验。
- `rhythm.login_migration` → `rhythm`：负责Rhythm登录体系迁移。
- `rhythm.receiver_deployment` → `rhythm`：负责Rhythm支付回调接收器部署及应用侧验证。
- `rhythm.user_mapping` → `rhythm`：负责Rhythm业务用户映射。

跨域只读咨询按各域 `openagentx collaborate instructions --agent <agent>` 的当前结果执行。Pay/Rhythm 的历史绑定 peer 字段存在旧名 `oneaxe-pay`，须由正式接口判断当前许可，不能手工改 DB。咨询回复不构成业务变更授权。六域历史任务只作上下文，不因恢复自动重做。

## 接管验收与未完成项

1. 确认 docs 指挥者的原宿主、原会话 ID 和唯一 writer；保存可复核的身份证据。
2. 通过该宿主正式入口交付本文件，得到同一会话的明确收悉与职责确认。
3. 指挥者在 OAX 之外观察部署结果，核对必要恢复，再把结果通知用户；记录宿主连续性和恢复行为证据。

旧会话定位要求已被用户授权新建所替代。新 thread 已真实创建；首次沙箱读取失败保留，随后同 thread 组织交接及正式 CLI 状态核验通过。当前维护部署及跨 OAX 重启连续性也已通过，未模拟失败部署后的恢复分支。

## 用户入口与事件续办

- 查看身份：`oaxops status`。
- 恢复原生终端：`oaxops open`。默认上下文约束为只读，持有 writer 锁直至用户退出终端；事件与终端不能同时写同一 thread。
- 后台事件续办：`oaxops send --key <稳定事件ID> --prompt <提示文件绝对路径>`。普通 CLI 在独立 user-systemd 单元中运行一次 `codex exec resume <精确thread>`；无事件时没有模型进程。`event` 是其底层前台入口。
- 稳定事件 ID 只执行一次；已有结果直接返回，已开始但未知终态不自动重试。event 获取 writer 锁时若忙会拒绝执行；send 返回成功仅表示 systemd 单元已提交，须检查结果回执，不能据此声称事件执行成功。交接程序应在上轮退出后发送。
- 真实维护由唯一执行者明确交接完整脚本、固定候选 SHA 和证据路径，再使用 `oaxops send --maintenance --key <稳定ID> --prompt <交接文件>`；该事件只允许正文指定的维护操作。本次交接已完成，不延伸为以后任意维护授权。默认事件自动前置只读约束。宿主因 bwrap 不可用采用 `danger-full-access`，此模式是明确的任务权限边界，不冒称 OS 强制只读。

可执行文件为 `/home/sky/.local/bin/oaxops`；仓库源为 `scripts/operations/oaxops`，两者均为单文件，不依赖其他包装器或临时开发 worktree。ROLE、交接副本、原始输出和 session.json 保存在 XDG 私有状态目录。正式 OAX 注册、scope 配置或自动收 OAX Mailbox 尚未实施；这是独立原生组织指挥者入口。

## 可复现安装

在本仓库根执行；已有会话不重新 `init`，升级脚本也不改变 `session.json`：

```sh
install -d -m 700 /home/sky/.local/state/oaxops
install -m 600 docs/operations/oaxops-role.md /home/sky/.local/state/oaxops/ROLE.md
install -m 600 docs/operations/2026-10-07-oaxops-handoff.md /home/sky/.local/state/oaxops/organization-handoff.md
install -m 600 docs/operations/oaxops.md /home/sky/docs/OAXOPS.md
install -m 755 scripts/operations/oaxops /home/sky/.local/bin/oaxops
```

首次新建仅在用户授权新会话且没有 `session.json` 时，以独立 systemd 单元调用 `oaxops init --key <首次事件ID> --prompt <首任务文件>`。本批已创建上述 thread，不重复初始化。

## 本批激活验收

截至 2026-10-07 10:36 UTC：

- 原生 Codex 本地 thread 索引确认新 thread 的 cwd、`gpt-6-astra` 与 `high`，不是只从启动参数推断。
- 首次 bootstrap 已创建 thread，但因 bwrap 失败未读交接；该业务验收为 FAILED，原始结果和 SHA 清单保留。
- `handoff-read-20261007` 在同一 thread 真正读完 ROLE、组织交接和 CONTINUE，返回 `COMMANDER_HANDOFF_READ_20261007`，准确确认六域及三条并行工作。
- `api-read-20261007` 经真实 `openagentx agent status --json` 核验六域 active/ready，区分历史 uncertain/business_effect_unverified 与可见状态；同 thread 的 `turn.completed`、退出码和最终回复齐全。
- 活跃 writer 时终端打开被拒绝；同事件同正文直接返回既有结果，不调用模型，异正文复用同 key 被拒绝。
- 两次后续事件完成后独立 systemd 单元 inactive，`status` 返回 `writer_busy=false`。没有设置周期性模型唤醒。

[脱敏验收证据](../reports/validation/2026-10-07-resume-timeout/evidence/commander01/)包含原生索引、每轮回执、成功轮最终回复、锁/幂等检查及私有原始记录 SHA-256 清单。该初始阶段验证自动化交接和只读恢复；后续实际部署另列如下。用户交互终端渲染仍未专项验收。

### 全机文档索引补充交接

`bootstrap-index-20261007` 于 10:45:20 UTC 在同 thread 完成，`turn.completed`、exit 0 与最终 `DOCS_INDEX_HANDOFF_20261007` 均保留。指挥者实际读取 docs 顶层名称和普通 Markdown 标题，确认没有顶层 AGENTS、README 或旧全机总览；新 COMMANDER.md 不冒充历史索引。

识别 Voice、Colab、CLIProxyAPI、OneAxe Proxy、Sub2API 五组 README；支付账户资料仅识别标题，不展开正文。本轮限读 Authentik 查询说明、Voice README、Codex 宿主代理、Colab README 和 OneAxe Proxy 兼容入口五份必要说明，建立网络/远程访问、模型网关/开发宿主、身份/业务、云环境及本机硬件/媒体分组认知。原始运维说明的时效性仍需责任域核验，不构成当前部署或业务效果证据。

未读恢复码、凭据、令牌、备份或私有支付配置，未进入业务工程；未发协作请求或执行维护。此有限事件结束后空闲，无常驻模型轮询。详细结果见 [索引交接回复](../reports/validation/2026-10-07-resume-timeout/evidence/commander01/bootstrap-index-20261007-final.md)。

## 实际维护部署验收

历史维护事件 `resume-timeout-rollout-20261007` 对应的独立单元于10:55 UTC在上述同 thread开始、11:27 UTC完成；daemon 于11:22:39 UTC重启，指挥者独立宿主持续等待，随后实际读取部署结果、操作记录和回派回执。[事件结果](../reports/validation/2026-10-07-resume-timeout/evidence/commander-deployment01/result.json)与[最终回复](../reports/validation/2026-10-07-resume-timeout/evidence/commander-deployment01/final.md)保留同 thread、exit0、turn.completed。只执行一条部署链，没有平行部署或重复业务。

[独立核验](../reports/validation/2026-10-07-resume-timeout/evidence/deployment01/independent-verification.json)确认七服务和六 Bridge 均为已验工件，六域原 thread/window/pane 保持。指挥者回派收尾任务的回执为 queued；本报告不把该回执说成任务已完成。本轮原 OpenAgentX 已在恢复后的另一个现有任务中继续收尾。

这证明“独立指挥者能承担 OpenAgentX 自身重启期间的部署与核验”。失败部署恢复分支、无人值守故障自愈、OAX Mailbox 自动收信及跨域任务自动分发仍未验或未实现。下一次维护仍按具体授权、稳定事件 ID 和唯一 writer 交接。
