# 现有指挥者接管交接（2026-10-07）

## 身份与宿主边界

用户明确指定的现有指挥者工作目录是 `/home/sky/docs`；该目录已经实查存在。不得把 OAX 历史 `orchestrator` 注册身份直接认作这一指挥者：数据库中的旧身份工作目录是 `/home/sky/work/touzi/OneAxe/steadyflow`，无对应 external session binding，当前 Fleet 无在线 Worker/终端。

AGY 的项目缓存中存在 docs 项目 `20d8fe72-b970-4d40-a8b5-c46e2abd17fd`。截至本次核查，AGY 本地 conversation_summaries、conversation_metadata、annotations，以及 Codex thread 索引未找到 docs 的原会话。唯一标题为“初始化指挥者角色”的旧 AGY 会话是 `0509884c-9e3f-40cb-bf75-1648b209d8ff`，工作目录为 steadyflow，2026-08-16 最后更新，不能据此替代 docs 指挥者。当前没有 docs tmux pane，AGY remote-control daemon 为 inactive。

用户随后明确授权在 `/home/sky/docs` 新建独立 Codex 指挥者，不再要求沿用旧会话。新会话 ID：`01a115e8-e528-75f3-9e30-612d617cfb84`，模型 `gpt-6-astra` / `high`，宿主为独立 user-systemd + 原生 `codex exec`，不属于 OAX daemon 或六域 Worker。此新会话不冒用历史 `orchestrator` 身份。

首次只读沙箱因本机 `bwrap: loopback: Failed RTM_NEWADDR: Operation not permitted` 无法读取文件，失败原始输出保留。后续同 thread 采用本机既有无沙箱执行模式；默认“只读”是本次任务约束，不能声称操作系统强隔离。

首轮启动单元为 `oax-independent-commander-bootstrap-20261007.service`；私有原始证据保存在 `/home/sky/docs/.oax-commander/events/bootstrap-20261007/`。实际首轮收悉与后续同 thread 恢复的验收见末尾；启动进程本身不代表接管已完成。

## 组织协调职责与执行权

指挥者负责理解用户目标、分配领域负责人、协调依赖、核对真实验收与向用户交付；各领域仍保有其业务职责。OpenAgentX 领域负责控制面、Worker、原生桥接、Task/Run 持久化及协作协议实现。

本批部署的唯一执行者是主代理所准备的 `scripts/validation/deploy_resume_timeout.py` 独立 systemd 部署链。接收交接的指挥者先只读检查其结果和服务状态；部署链运行期间绝不并行部署或再次重启。仅在该链已退出、结果明确失败并且原执行者已释放后，才能依据现有用户授权接续有界恢复。不得清空/重放历史排队任务，不直接改数据库，不主动重做未知业务副作用。

指挥者的恢复宿主必须独立于 OAX daemon 及被重启的六域 Worker。外层普通进程可等待部署结果事件，只有新结果需要处理时调用一次既有宿主的模型入口；不使用定时 LLM 轮询，不依赖本聊天持续在线。不因仅启动一个进程就报告自动恢复已经验证。

## 当前批次与证据入口

- 产品修复初始 commit：`15f787f3b7b8f04e3fc3fcad975846ec101e0daa`，Codex 默认 `timeout=0` 不设总截止，registered resume 核验权威身份；后续 quote-service 旧 manifest 兼容修复以实际候选 SHA 为准。
- 实施工作树：`/home/sky/work/touzi/OneAxe/OpenAgentX-resume-timeout-worktree`；正式交付仓库：`/home/sky/work/touzi/OneAxe/steadyflow/OpenAgentX` 的 `main`。候选、Git 合入和实际安装分别核验。
- 31 分钟真实验收独立单元：`oax-resume-timeout-e2e-20261007.service`；证据根：`~/.local/state/openagentx/validation/2026-10-07-resume-timeout/long01`。运行中不是 PASS。
- 首轮脚本末尾 SQL 使用 `id` 而实际列为 `run_id` 的夹具问题必须保留失败记录，用只读查询补判；不得为这个夹具问题重跑模型或 31 分钟工具动作。
- 部署结果由部署脚本的 `--evidence` 目录中 `result.json`、`operations.jsonl`、必要时 `recovery.json` 给出；先找到实际 systemd 单元及 argv，不猜测目录，不把 preflight 当部署完成。
- 用户已授权必要重启，但必须保留六域原 thread、window/pane，并核验候选二进制 SHA、实际进程来源、schema、Worker readiness 和原会话恢复。
- 旧进程 30 分钟截止仍可能打断旧 Run；新默认仅在经验证的候选工件安装和对应服务重启后生效。

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

旧会话定位要求已被用户授权新建所替代。新 thread 已真实创建；首次沙箱读取失败保留，随后同 thread 组织交接及正式 CLI 状态核验通过。部署恢复的执行权尚未移交，不能报告恢复验收已通过。

## 用户入口与事件续办

- 查看身份：`oax-commander status`。
- 恢复原生终端：`oax-commander open`。默认上下文约束为只读，持有 writer 锁直至用户退出终端；事件与终端不能同时写同一 thread。
- 后台事件续办：`oax-commander send --key <稳定事件ID> --prompt <提示文件绝对路径>`。普通 CLI 在独立 user-systemd 单元中运行一次 `codex exec resume <精确thread>`；无事件时没有模型进程。`event` 是其底层前台入口。
- 稳定事件 ID 只执行一次；已有结果直接返回，已开始但未知终态不自动重试。发送时 writer 忙会明确拒绝且不提交；交接程序应在上轮退出后发送，不把拒绝当作已投递。
- 部署权仍未移交。真实维护由唯一执行者明确交接完整脚本、固定候选 SHA 和证据路径，再使用 `oax-commander send --maintenance --key <稳定ID> --prompt <交接文件>`；该事件才允许正文指定的维护操作。默认事件自动前置只读约束。宿主因 bwrap 不可用采用 `danger-full-access`，此模式是明确的任务权限边界，不冒称 OS 强制只读。

运行副本位于 `/home/sky/docs/.oax-commander/commander_session.py`；仓库源为 `scripts/operations/commander_session.py`。用户入口 `/home/sky/.local/bin/oax-commander` 指向运行副本，不依赖临时开发 worktree 存活。ROLE、交接副本、原始输出和 session.json 均保留在 docs 私有子目录。正式 OAX 注册、scope 配置或自动收 OAX Mailbox 尚未实施；这是独立原生组织指挥者入口。

## 可复现安装

在本仓库根执行；已有会话不重新 `init`，升级脚本也不改变 `session.json`：

```sh
install -d -m 700 /home/sky/docs/.oax-commander
install -m 755 scripts/operations/commander_session.py /home/sky/docs/.oax-commander/commander_session.py
install -m 600 docs/operations/independent-commander-role.md /home/sky/docs/.oax-commander/ROLE.md
install -m 600 docs/operations/2026-10-07-commander-handoff.md /home/sky/docs/.oax-commander/organization-handoff.md
install -m 755 scripts/operations/oax-commander /home/sky/.local/bin/oax-commander
```

首次新建仅在用户授权新会话且没有 `session.json` 时，以独立 systemd 单元调用 `commander_session.py init --key <首次事件ID> --prompt <首任务文件>`。本批已创建上述 thread，不重复初始化。

## 本批激活验收

截至 2026-10-07 10:36 UTC：

- 原生 Codex 本地 thread 索引确认新 thread 的 cwd、`gpt-6-astra` 与 `high`，不是只从启动参数推断。
- 首次 bootstrap 已创建 thread，但因 bwrap 失败未读交接；该业务验收为 FAILED，原始结果和 SHA 清单保留。
- `handoff-read-20261007` 在同一 thread 真正读完 ROLE、组织交接和 CONTINUE，返回 `COMMANDER_HANDOFF_READ_20261007`，准确确认六域及三条并行工作。
- `api-read-20261007` 经真实 `openagentx agent status --json` 核验六域 active/ready，区分历史 uncertain/business_effect_unverified 与可见状态；同 thread 的 `turn.completed`、退出码和最终回复齐全。
- 活跃 writer 时终端打开被拒绝；同事件同正文直接返回既有结果，不调用模型，异正文复用同 key 被拒绝。
- 两次后续事件完成后独立 systemd 单元 inactive，`status` 返回 `writer_busy=false`。没有设置周期性模型唤醒。

[脱敏验收证据](../reports/validation/2026-10-07-resume-timeout/evidence/commander01/)包含原生索引、每轮回执、成功轮最终回复、锁/幂等检查及私有原始记录 SHA-256 清单。自动化交接和只读恢复已验；用户交互终端渲染及实际维护部署仍未验，不混报为完整部署通过。
