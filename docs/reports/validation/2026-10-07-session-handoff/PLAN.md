# 新会话交接独立验收

## 目标与边界

- 当前仅准备脚本，未启动模型、未执行真实验收、未声明通过。
- 使用独立 profile/daemon/Worker/Codex、随机 tmux socket、专属 workspace；不连接正式业务 pane，不重启正式服务。
- 复用 `TerminalRun` / `SettingsRun` / `codex_local_setup` 的真实环境生成与证据逻辑；固定候选二进制及 SHA 后执行。
- 所有领域写入经正式 CLI/API；SQLite 仅以 `mode=ro` 独立核对旧 Task/Run/session_bindings 哈希及绑定历史。
- 一个真实 Worker；另登记一个无 Worker 的 external peer 验证咨询。最多六个真实模型 Run，不重复完整双 Agent 协作矩阵。

## 必要真实流程

| 场景 | 可复核证据 |
| --- | --- |
| 初始化原会话 | 原生 PTY、query Task/单 Run/Journal、精确角色/workspace ACK、old thread |
| preview 零写 | CLI 输出；前后 Task 集合、active pointer、runtime state、workspace、历史表一致 |
| busy 拒绝 | 真实旧 thread 运行只读 `sleep 40`；申请失败、不新增 Task、pointer 不变 |
| 新会话交接 | 显式 expected_thread/version/idempotency key；新 thread != old；角色 SHA、模型保持；首轮仅接收交接 |
| nonce 上下文 | 自定义 handoff nonce 不出现在后续问题中；新 thread 后续普通 query 精确回显 nonce |
| 重试与 CAS | 同 key 不新增 Task/Run；旧 expected pointer 新 key 拒绝且 pointer 不变 |
| 旧 native view | 真实 PTY 提交后明确拒绝；Task 集合不变；退出再 open 的实际 resume argv 指向 new |
| managed 绑定 | 同 binding ID、peer集合、Agent 身份/凭据保持；generation +1、context task/thread 更新 |
| 切换后协作 | 无Worker peer 正式 ask；目标真实单 Run 在 new；peer 收到关联精确 nonce 答复，不验证 peer 自动续办 |
| Worker 重启 | fixture 配置 thread_id 特意保留 old；只重启自有 Worker；pointer 与实际后续 query 都仍 new |
| 历史保全 | 已完成旧 Task/Run/绑定逐行哈希一致，新旧 thread 绑定同时保留 |

## 产品接口与脚本待对齐项

- `GET /api/console/v1/agents/{id}/session?backend_id=codex` 返回 `agent_id/backend_id/thread_id/context_task_id/version/pending_task_id/updated_at`；version 0 为 durable bootstrap。
- `agent new-session ID` 只读 preview；写入需 `--handoff-file --apply --expected-thread --expected-version --key [--wait 3m]`。
- 首轮 ACK 的具体 prompt 以最终产品为准。脚本保存首轮是否含 nonce；精确 nonce 继承由后续明确问题独立验证，不把固定 ACK 与自定义回显冲突算成产品成功。
- 待候选确定后核实 CLI 输出、旧 view 拒绝文案及失败终态；任何调整保留首次失败，不自动重跑模型。

## 独立代码/集成审查

候选固定后，只审本轮关键 CAS、pending 清理与失败回滚：请求和 Task 原子性、只有确认成功才发布 pointer、失败/取消/不确定保持旧 pointer、managed更新与pointer同事务、旧 view投递按当前pointer校验、Worker启动优先durable pointer。集成夹具与真实 Runtime/PTY 证据分开记录。

## 执行入口（尚未执行）

```sh
python3 scripts/validation/agent_session_handoff_e2e.py run \
  --binary /absolute/fixed/candidate/openagentx \
  --root /absolute/private/validation/new-attempt \
  --commit FULL_SOURCE_COMMIT
```

每次必须新的私有 root；失败不自动重试、不销毁证据。完成后由验收执行者根据已记录 PID/starttime 安全清理自有进程，不触碰共享进程。入库前仅复制脱敏 evidence 与 SHA 清单，原始凭据/数据库/PTY 内容仍留私有目录。

## 候选执行前静态修正（未运行模型）

- preview 对照保留完整原始 runtime state，同时只从断言中排除 `updated_at`，因为 WorkerHealth 会独立更新该时间；thread/task/run/endpoint/PID/state 仍严格一致。
- busy query 显式使用 `runtime_session.backend_id=codex/provider_session_id=old`；成功切换后的普通 query 和重启后 query 均不带 session ref。
- 首次 managed bind 会改变 version 0 bootstrap 来源时间，因此在绑定完成后重取 expected session；不把夹具自身绑定当作 preview 副作用。
- 已核对 `internal/api/observe.go` 与 `internal/api/panel/handler.go`：`run_attempts[].instructions_sha256` 来自冻结 AgentInput；初始化完成立即验证其与角色文件 SHA 相同，再比较新会话 Run。
- 旧 view 拒绝文案匹配包含产品当前的“旧会话”。
- 成功时收集证据后自动验证空闲、Task 全部终结且无 pending handoff，再按 PID/starttime 停止自有 Worker（包括 `worker-restarted`）、daemon 和专属 tmux；不删证据。失败时仅清理已空闲 fixture，活跃/未决情况保留清理原因，可用同脚本 `cleanup` 显式续办。
- `--help`、Python AST 静态解析通过；不代表真实流程通过。
