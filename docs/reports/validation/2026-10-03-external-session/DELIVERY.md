# 原会话协作：本轮交付

**已安装 OAX 持久通信通道，尚未完成两个原 Desktop 会话自动协作。** 本轮没有把两个原 thread 迁入 Worker，没有向原会话插入指令，也没有代替它们生成业务请求或答复。

## 已完成

- `external bind/status/send/inbox/ack/reply/revoke`：专用 Agent 身份、双向 peer 范围、持久消息、关联结果及 Journal；发送身份不冒用 owner。
- 同 key 同载荷幂等、异载荷拒绝；请求只能有一条结果，结果不能继续触发结果。读入确认、回复及业务独立验收是不同层次。
- external 与 managed Worker/SessionBinding 双向互斥；有未结束 managed Task 时拒绝换绑，external Agent 不接受 managed Task，避免永远排队。
- v2→v3 正式迁移；既有正式库的只读备份升级后，原 38 表、279,560 行逐表数据指纹不变。正式安装已有回退备份。
- 本机已登记两个真实领域身份，映射精确使用用户给定的目录及 thread，专用凭据位于各自 `~/.openagentx/external/<agent>/credentials.json`，未打印、未入库。

| Agent | 业务目录 | 原 thread | 当前状态 |
|---|---|---|---|
| rhythm | /home/sky/work/touzi/OneAxe/rhythm | 01a0b4e3-7b2e-7822-8ae1-9f0e812115a9 | 绑定有效，原会话未初始化，0 业务消息 |
| oneaxe-pay | /home/sky/work/touzi/OneAxe/oneaxe-pay-service | 01a0e016-d951-77d1-bc7e-d13662f4823c | 绑定有效，原会话未初始化，0 业务消息 |

## 原宿主阻断

共享 Codex daemon 0.160.0 对两 thread 返回 `notLoaded`；只能读历史，不是已证明的原活跃接收入口。当前 Desktop 0.153.1 使用独立 stdio app-server，界面排队由 `queued-follow-ups` 与窗口所有权/发送锁处理。虽有 `thread/queue/*` schema，未找到让外部共享 daemon 投递后被原 Desktop 消费的受支持接线。

内部直接消息路由使用 `send-now`，忙时可能 steer，且本轮工具没有开放该入口。因此它也不满足“当前轮不受打断，结束后自动处理”。本轮没有通过私有桥、修改应用状态文件、send-keys 或第二个 app-server 绕过这一边界。

**`rhythm-pay-handoff-01` 尚未发送。** 原会话自己请求→Pay 自己答复→Rhythm 自动续办的 E04/E05 未通过，不能用真实通信 API 测试替代。当前 `inbox --watch` 仅观察消息，不唤醒模型。

## 如何开始用

先在两个原会话分别输入一次：

```text
Rhythm：请读取 /home/sky/.openagentx/external/rhythm/START.md，按说明加入 OAX 通信，保留当前开发任务。
Pay：请读取 /home/sky/.openagentx/external/oneaxe-pay/START.md，按说明加入 OAX 通信，保留当前开发任务。
```

这可初始化会话内通信工具，不能使已结束轮次自行醒来。进一步实现无人转述的完整目标，需要 Desktop 提供正式 queue-only 外部入口；另一条已具备运行基础的路线是待当前工作结束后显式迁入 OAX managed runtime。后者改变宿主使用方式，本轮未替用户执行。

日用命令与语义：[使用说明](../../../operations/external-session-entry.md)。本机接入文字的可审阅源文：[双角色说明](../../../examples/external-session/README.md)。

## 安装与证据

- 产品源码：`6576c559ae852304b04a7bf149583a471b112597`。
- 安装二进制：`/home/sky/.local/bin/openagentx`。
- SHA-256：`432cbd044902351670179e7eaaf463871f59bec0d274ed8810f49eab56d7fd39`。
- Go 构建来自独立干净本地 clone，`vcs.revision=6576c55… / vcs.modified=false`；嵌套工作树下 Go 1.22 曾误标上级仓库，未使用该版本戳作为交付依据。
- 正式数据库 schema v3；Web 资源本轮无变更。daemon 与两个既有演示 Worker 的 `/proc/<pid>/exe` 哈希均等于安装工件；原 Desktop 宿主 PID/start/hash 未变。
- 私密回退备份：`~/.local/state/openagentx/validation/2026-10-03-external-session/installation01/rollback/`。v2 二进制不支持 v3 数据库，不能只换旧 binary；已有新消息时不可覆盖回退旧库丢数据。
- 全量 `go test -race ./...` 通过；首次遗留 schema 版本硬编码失败与复验均保留。隔离真实通信 14 组断言及升级副本验证通过。
- [正式安装验收](evidence/installed01/result.json)7组通过：独立测试身份实际往返、重复请求/结果拒绝与防误派Task；只读核对没有生成managed Task/Run/Worker。验证后撤销两个测试绑定，真实Rhythm/Pay绑定保留。日志、CLI/API响应、数据库独立核验和SHA均入库。

逐项状态见 [COVERAGE](COVERAGE.md)，命令与日志见 [EXECUTION-LOG](EXECUTION-LOG.md)。本轮不新增支付业务效果、原线程 E2E、前端或模型执行验收结论；AGY/Codex 原运行主链证据保留在 2026-10-02 交付中。
