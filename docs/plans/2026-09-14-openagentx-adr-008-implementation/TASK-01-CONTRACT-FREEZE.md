---
doc_type: implementation_contract
status: frozen
owner: openagentx
updated_at: 2026-09-14
---

# Task 01 契约冻结与测试地图

本文只冻结 ADR-008 后续任务的实现契约和测试边界，不改变当前产品运行行为。
当前行为与目标行为必须明确区分；后续任务只有在各自监督门禁放行后才能实施。

## 1. 基线 characterization

| 领域 | `c3fc1bba` 当前行为 | ADR-008 替换任务 |
|---|---|---|
| 路径 | `serve`、Console 和 Fleet 的关键路径大多要求显式 flag；没有共享 local profile resolver | Task 02 |
| Console grammar | `console` 无子命令打印 usage 并退出 `2`；`attach --once` 和行式 REPL 仍存在 | Task 02 冻结 parser，Task 06 原子替换 |
| Attach | handler 分别读取 Agent、Worker、Backend 和 Run；响应没有 Journal high-water | Task 03 |
| Follow | 首次由调用方传入 sequence，现行 REPL 固定从 `0` 开始；重连重新 Attach，但没有纯 reducer 的 generation/WorkerInstance 防回退 | Task 03 |
| Auth | Console 用密码换取 Web Session cookie，并对写操作复用 CSRF；UDS 与 Web mux 当前挂载相同 Web Auth handler | Task 04 |
| schema | `CurrentVersion=1`；空库执行 embedded target schema，既有 v1 在校验前执行事务化 `ensure...` 兼容升级 | Task 04 |
| tmux | 固定 session 为小写 `agentx`；只有 `@openagentx_managed`；受管 Agent window 被要求恰好一个 pane | Task 05 |
| TUI | `bufio.Scanner` 读取行命令，Attach/Event 以 JSON 连续写 stdout | Task 06 |
| Fleet | 已有非破坏 preflight、正式 Console 命令、graceful down 观察和 systemd config 一致性检查；仍生成用户名/密码式 Console 入口 | Task 07 |

必须删除的临时兼容路径有明确归属：Task 03 删除 sequence `0` 默认和无代际 reducer；
Task 04 删除 Console 对 Web cookie/CSRF 登录的依赖；Task 05 删除小写 `agentx` 和单 pane
限制；Task 06 在全屏替代可用的同一提交中删除 `--once`、独立 `console status`（若 parser
中仍保留）和旧 Scanner REPL；Task 07 删除 Fleet 生成命令中的 username/password 依赖。

## 2. 默认路径契约

共享 resolver 的唯一代码边界固定为 `internal/localprofile`。所有调用方传入显式 flag
是否出现的信息，不能仅用空字符串猜测用户是否提供了 override。

解析优先级固定为：

1. 显式命令 flag；
2. 对应资源环境变量；
3. `OPENAGENTX_HOME`；
4. `os.UserHomeDir()` 下的 `.openagentx`。

资源环境变量和默认值固定如下：

| 资源 | 环境变量 | 相对 OpenAgentX home 的默认值 |
|---|---|---|
| UDS | `OPENAGENTX_SOCKET_PATH` | `run/openagentx.sock` |
| SQLite | `OPENAGENTX_DATABASE_PATH` | `data/openagentx.db` |
| Fleet manifest | `OPENAGENTX_FLEET_MANIFEST` | `fleet.yaml` |
| Worker config directory | `OPENAGENTX_WORKER_CONFIG_DIR` | `workers` |
| CLI credentials | `OPENAGENTX_CREDENTIALS_PATH` | `credentials.json` |

`OPENAGENTX_HOME` 未设置时的 canonical home 是 `~/.openagentx`，其中 `~` 必须通过
`os.UserHomeDir()` 展开。显式 flag、资源环境变量和 `OPENAGENTX_HOME` 一旦设置，空值、
相对路径或无法 canonicalize 的路径均 fail closed；不得回退到下一优先级。路径解析不扫描
`/etc`、`/var/lib`、旧仓库目录或其他 installation。Worker 路径只能由通过 Agent ID 校验的
`<worker-config-dir>/<agent-id>.yaml` 生成。

## 3. Console CLI 与非交互契约

最终公开 grammar 固定为：

```text
openagentx console
openagentx console login
openagentx console logout
openagentx console attach [--agent <agent-id>] [--diagnostic]
```

- `console` 无子命令只在交互 TTY 中进入主菜单；非交互时退出 `2`，提示选择直接子命令。
- `login` 需要交互 TTY 隐藏输入密码；不得接受 password flag、argv 或环境变量；无 TTY
  退出 `2`。
- `attach` 总是持续全屏 TUI，因此也需要交互 TTY。无 TTY 时即使给出 `--agent` 也退出
  `2`，自动化观察必须使用 Observe API。
- `--agent` 只覆盖 Agent selector，不绕过 `OAX`、当前 pane `0`、控制面授权或 window
  冲突检查。
- `logout` 可以非交互执行；服务端不可达仍清除本地 credential，并以警告说明远端撤销
  未确认。
- `attach --once`、`console status`、未知子命令和多余位置参数最终均为 usage error，退出
  `2`；删除动作只能在 Task 06 的替代 TUI 同一提交发生。
- 参数/TTY/workspace precondition 为退出 `2`；认证、控制面、I/O 或 TUI 运行失败为退出
  `1`；用户正常 `/quit`、EOF 或 context cancel 在 terminal 已恢复后退出 `0`。
- stderr 可以给出修复动作，但不得打印 Token、密码、Cookie、CSRF、Secret 或未经安全
  投影的 Runtime 输出。
- Foreground Takeover 只有禁用菜单项和 `/foreground` 的固定回复
  `Foreground Takeover（规划中，暂不可用）`，不存在可执行分支。

## 4. Attach snapshot、cursor 与 reconnect 契约

`GET /api/console/v1/attach` 的现有安全状态字段保留，并新增唯一 high-water 字段：

```json
{
  "agent_id": "quote-service",
  "generation": 48,
  "worker_instance_id": "worker-current",
  "snapshot_sequence": 1204
}
```

`snapshot_sequence` 是同一 SQLite 读取事务内，在 Agent、当前 Worker、Backend health 和
active RunAttempt 快照完成读取后取得的 `MAX(event_journal.sequence)`；空 Journal 为 `0`。
不得用多个 repository 公共方法各自读取后再查询 sequence。实现边界固定为 SQLite
repository 的单一 Console snapshot 方法，handler 只消费其结果。

实时订阅继续使用正式 Observe SSE API，首次请求传
`after_sequence=<snapshot_sequence>`。只保留一个 cursor 数值，避免
`snapshot_sequence`/`live_after_sequence` 两个字段发生分歧。客户端状态为
`last_applied_sequence`：只有事件通过身份和 sequence 校验并被 reducer 接受后才推进；
重复 sequence 幂等忽略，倒退 fail closed，下一次重连从该值之后继续。

服务端若检测到 retention gap，返回 HTTP `409` 和稳定错误码
`EVENT_CURSOR_EXPIRED`。客户端必须重新 Attach 获得一致快照和新 cursor；不得回退到
`0` 或猜测缺失状态。有限历史若实现，只能通过
`GET /api/console/v1/timeline?agent_id=<id>&before_sequence=<snapshot_sequence>&limit=<n>`
读取，数量/字节受限且默认排除 heartbeat，不参与实时 cursor 推进。

纯 reducer 的当前 Worker 身份固定为
`agent_id + worker_instance_id + generation`。sequence 不倒退；旧 generation、同代但不同
WorkerInstance 的事件只能进入受限历史（若安全投影允许），不得覆盖状态；heartbeat 只
更新 age/lease/连接状态，只有有意义状态转换进入普通 Timeline。

并发证据必须覆盖：事务快照 cursor 为 N 时并发提交 N+1，最终结果要么快照包含对应状态且
cursor 不小于 N+1，要么快照不包含而 SSE 从 N 后交付该事件；不得出现状态不可解释的丢失。

## 5. CLI Token 契约

仅 UDS mux 挂载以下 versioned endpoint，Web mux 不挂载：

```text
POST /api/auth/v1/cli/login
GET  /api/auth/v1/cli/session
POST /api/auth/v1/cli/logout
```

Web 的 `/api/auth/v1/login`、Session cookie、Secure/HttpOnly/SameSite 和写操作 CSRF
校验保持原样。CLI 请求使用 `Authorization: Bearer <opaque-token>`；不得让 bearer
middleware 包住 Web mux，也不得让 Cookie/CSRF 降级为 bearer fallback。

Token 是至少 256 bit CSPRNG 的 opaque secret。服务端只保存 SHA-256 digest、公开 token
ID、principal/user、scopes、installation ID、created/absolute expiry/last used/revoked 时间；
原始 Token 只在 login 成功响应出现一次。默认 absolute TTL 为 30 天，服务端策略只能缩短，
不能 sliding 延长。

固定 scope 为：

| scope | 最低角色 | 用途 |
|---|---|---|
| `console.read` | viewer | Attach、Agent 列表、安全 Timeline |
| `console.control` | operator | dispatch、steer、cancel、approval |
| `console.diagnostic` | owner | Diagnostic Attach |
| `fleet.lifecycle` | owner | drain/stop/force-stop 等 Worker lifecycle |

login 只签发调用者角色允许的 scope。无效密码、未知/过期/撤销 Token、installation audience
不匹配统一返回 `401 CLI_UNAUTHENTICATED`，不暴露细分原因；有效 Token 但 scope/role 不足
返回 `403 CLI_FORBIDDEN`。logout 对当前有效或已撤销 Token 幂等，客户端无论响应如何都删除
本地副本。

服务端 installation ID 是持久化随机标识。credential store 以 canonical socket、
installation ID 和 username 为键，目录权限不宽于 `0700`、文件不宽于 `0600`；socket 或
installation 不匹配时在发送 Token 前 fail closed。owner 密码变更必须调用按 user 撤销全部
CLI Token 的服务能力。

## 6. tmux marker、冲突与绑定状态机

session 固定为大小写敏感的 `OAX`。window-scoped marker 固定为：

```text
@openagentx_managed=1
@openagentx_agent_id=<exact-agent-id>
```

`overview` 只有 managed marker 且 agent marker 为空；Agent window 必须名称、agent marker
和控制面授权 Agent 三者精确一致才是 compatible。旧 `agentx` 不自动迁移或合并。

结构化证明固定使用 tmux argv/format 查询：

- 当前上下文：`display-message -p` 读取 session name、window name、pane index 和两个
  window option；
- workspace 枚举：`list-windows -t =OAX -F` 读取精确 window name 和 markers；
- pane 证明：对已唯一解析的精确 window 执行 `list-panes -F '#{pane_index}'`，证明 pane
  `0` 存在并保留所有 pane `1+`。

产品不得查询、暴露或依赖 `%pane_id`、window index、active pane、pane process 或 pane
内容作为身份；不得使用 `send-keys`、`paste-buffer`、`capture-pane`。pane index 只用于
验证调用点是否为稳定 pane `0`，不成为 Agent 身份。

绑定状态机冻结为：

```text
unmanaged-compatible -> preflighted -> bound-consistent
bound-same-agent --------------------> bound-consistent
bound-other-agent -> confirmation-required -> preflighted
any state + ambiguity/conflict ------> rejected-without-mutation
mutation failure --------------------> compensated 或 partial-failure
```

冲突类别至少包括：tmux 外、非 `OAX`、当前 pane 非 `0`、pane `0` 缺失、重复 window name、
重复 agent marker、marker 非法、name/marker 不一致、当前 window 已绑定另一 Agent、另一合法
受管 window 已占目标 Agent、unmanaged window 占目标名称、Agent 未经控制面授权。除
`bound-other-agent` 可在 TUI 明确确认后重绑外，其余全部 fail closed。

绑定按“再次全量 preflight -> 设置当前 window markers -> rename 当前 window -> 结构化重读”
执行。任一步失败都尝试恢复原 name/options；补偿失败必须返回显著 partial-failure 和修复提示，
不得继续 Attach。无目标参数的 tmux mutation 只作用于调用者当前 window；Fleet reconcile
仍不能重命名未知 window。

## 7. TUI framework 与 model/I/O 边界

固定依赖组合：

| module | version | Go directive | license | 用途 |
|---|---|---:|---|---|
| `github.com/charmbracelet/bubbletea` | `v1.3.4` | `1.18` | MIT | program、message、command、resize、terminal lifecycle |
| `github.com/charmbracelet/bubbles` | `v0.20.0` | `1.18` | MIT | textarea、viewport、list/help 等可测试组件 |
| `github.com/charmbracelet/lipgloss` | `v1.1.0` | `1.18` | MIT | 布局、样式和 overlay 合成 |

该组合已在仓库外临时 module 中以 Go `1.22.4`、`GOTOOLCHAIN=local` 完成 package compile
probe。未选择最新 Bubble Tea/Bubbles，是因为探针确认其当前最新版本分别要求 Go `1.24.0`
和 `1.24.2`。依赖只在 Task 06 实施时加入 `go.mod`。

纯 model/reducer 持有：terminal dimensions、focus、overlay、输入 draft/cursor、Agent selector、
受限 Timeline、权威 Attach snapshot、Worker identity/generation、last-applied cursor、连接状态、
CLI session 非秘密元数据和 command pending/result。`Update` 只处理 typed message 并返回
command，`View` 不做 I/O。

UDS API、SSE reader/reconnect timer、credential store、tmux query/bind、clock、signal 和 terminal
启动/恢复全部通过 interface-backed command/message 注入。测试直接驱动 model message 序列，
断言输入在 event/resize/overlay 时不变；不得在 reducer/View 内访问网络、文件、tmux 或
`TurnHandle`。

## 8. schema v1 兼容升级契约

Task 04 保持 `CurrentVersion=1`，同时修改空库 `001_target_schema.sql` 和既有 v1 reopen 路径。
既有路径在 `ValidateCurrent` 前调用新的事务化、幂等 `ensureCLITokenTables`，创建
`installation_metadata`、`cli_tokens` 及索引；随后把它们加入 required schema 校验。

升级必须在单一事务内完成；任意 DDL、installation ID 初始化或索引创建失败全部回滚。
测试必须覆盖空库、现有完整 v1 reopen、重复 Apply、部分/损坏表拒绝以及故障注入后无半表。
不得仅修改 target schema，也不得把 v1 DB 当成需要离线重建的新 schema version。

## 9. 强制设计问题结论

1. 一致 snapshot 和 high-water 在 SQLite Repository 的单一读取事务中取得；handler/service
   不拼接多次独立读取。
2. CLI bearer handler 只注册到 `runDaemon` 的 `unixMux`；`webMux` 保留现有 cookie+CSRF。
3. `cli_tokens` 同时进入 target schema、v1 `ensure...` 事务和 required schema/reopen 测试。
4. 当前上下文由 `display-message -p` 证明，pane `0` 存在性由 exact-window `list-panes`
   证明，marker/name 唯一性由 `list-windows` 证明；不使用 window index 或 `%pane_id`。
5. 尺寸、焦点、draft、timeline、snapshot/cursor/connection 和 command UI 状态属于纯 model；
   API/SSE/tmux/filesystem/clock/signal/terminal 属于注入的 command/message I/O。

## 10. Task 02-08 测试地图

| Task | 复用现有测试 | 必须新增/扩展的验收 |
|---|---|---|
| 02 path/CLI | `cmd/openagentx/main_test.go`、`internal/cli/console/command_test.go`、`internal/cli/fleet/command_test.go`、`internal/fleet/manifest_test.go` | `internal/localprofile` 优先级/路径逃逸/不同 home；所有 CLI 默认与显式 flag；非交互和 help 泄漏检查 |
| 03 cursor/reducer | `internal/api/console/handler_test.go`、`internal/client/console/client_test.go`、`internal/persistence/sqlite/observe_task_query_test.go`、`internal/safeoutput/projector_test.go` | snapshot/N+1 并发、reconnect/gap/重复/倒退、generation+WorkerInstance reducer、heartbeat 合并和脱敏 |
| 04 CLI Token | `internal/api/auth/handler_test.go`、`internal/auth/web/auth_test.go`、`internal/persistence/sqlite/migrations/*_test.go`、`internal/persistence/sqlite/bootstrap_repository_test.go` | UDS-only mux、scope/role/expiry/revoke/audience、DB 仅 digest、v1 reopen/回滚、credential 权限/原子替换、Web cookie+CSRF 回归 |
| 05 workspace | `internal/fleet/workspace_test.go`、`internal/cli/console/command_test.go`、`internal/cli/fleet/command_test.go` | `OAX`、pane `0+1+2`、structured query、全部冲突类、每个 mutation 失败点/补偿、隔离 `tmux -L` 集成 |
| 06 TUI | `internal/cli/console/command_test.go`、`internal/client/console/client_test.go`、`internal/api/console/handler_test.go` | pure Update/View、输入不被 event 冲刷、selector/menu/overlay/resize/reconnect、官方 API 单次调用、terminal restore、旧 REPL/`--once` 删除 |
| 07 Fleet | `internal/cli/fleet/command_test.go`、`internal/fleet/*_test.go`、`internal/worker/runner_test.go`、`deploy/systemd/openagentx-worker_template_test.sh` | 首次/重复/冲突 init、user-systemd canonical config、无 credential argv、Console/tmux 退出后 Worker 独立、busy graceful down、多 pane 保留 |
| 08 integration | `internal/transport/unixhttp/server_test.go`、`internal/api/panel/integration_http_test.go`、Web/PWA 既有测试与 release scanner | 临时 home+SQLite+UDS+隔离 tmux+fake systemd E2E、全量/race/Web build、secret scan、真实环境只读 preflight 和 provenance |

所有 characterization/后续测试必须使用临时 home、临时 SQLite/UDS、fake runner 或唯一
`tmux -L` server。除 Task 08 获得单独批准外，不操作真实 service、数据库、socket、默认 tmux
server 或已安装二进制。
