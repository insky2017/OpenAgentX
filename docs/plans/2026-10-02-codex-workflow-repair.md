# Codex 工作流实施计划（2026-10-02）

状态：用户已确认执行。沿用 AGY 交付，新增 Codex；真实验收前不标记安装完成。

## 用户结果

一个常驻领域 Agent 可使用 Codex 接收连续任务；用户可在终端看到原生会话、断开后回来继续；正在工作的 Agent 可用一次 join 登记角色、工作目录与交接，空闲后启用。用户不必手动配置 Fleet、Token、tmux。CLI help 是稳定自初始化入口，Skill 仅作薄说明。

## 最小架构与边界

- 保留 Worker / Task / RunAttempt / Mailbox / Journal，增加 `codex-app-server`，不将 Codex 当 generic ACP，不引入另一个调度平台。
- 使用本机 Codex 0.160.0 实际 schema：thread/start、thread/resume、turn/start、turn/steer、turn/interrupt。Unix endpoint 上承载 WebSocket。协议关卡须先于依赖该语义的实现。
- AgentID 是长期角色，TaskID 是工作，RunID 是执行，threadId 是原生会话。新任务默认新会话；续接须显式且记录来源。thread 绑定尽早经受 fencing 保护的正式入口持久化。
- Worker 常驻监听。终端关闭不等于 Agent 离线。原生人工输入与总线输入必须协调并进入正式账本；不能用裸 resume 或 send-keys 绕过。
- join 分为 prepared / connected / ready，不承诺任意已有 PID 无感接管。无法控制的现有 CLI 先保存交接，当前轮结束后重连同 thread。准备状态不启动 Worker、不自动派单。
- 默认网络设置保存到 Agent 私密环境文件并作用于实际 app-server。新 Codex Agent 复用现有 Mihomo 的 mixed-port 配置，本机已核实为 7897；不在 Runtime 硬编码端口。保留本机 NO_PROXY。既有进程不假定自动继承新环境。
- 取消只作用于本 Run 的 turn，不能杀共享 daemon 或其它会话。丢失终态、副作用不明、断线不能误报成功或盲重放。

## 实施与判定顺序

1. 保存规则、计划、验收矩阵和初始状态并提交；隔离协议 probe 固定本机版本，保留失败与复验。
2. 接入 Runtime、网络与早期绑定；并行完成 add/join/resume/open 的用户入口及清楚的状态提示。
3. 完成输入仲裁、真实 Codex query/mutation/连续任务/取消/恢复，以及原生终端与总线交互；缺陷集中修复。
4. 独立验证、准确构建安装、真实浏览器/PTY核验、持久证据与 SHA-256、使用说明及架构状态更新，提交完整结果。

## 本轮取舍

借鉴 Paseo 的 app-server 会话管理、Holon 的宿主 wait/wake、Buzz 的常驻队列和职责注入。复用 OAX 持久账本，只提取小机制。WorkBuddy 保留不扩测；远程公网、多租户治理、全面安全强化不阻断本机链路。重复执行、凭据泄漏、有效数据破坏和误报成功必须处理。

现有 AGY 日用交付不是 Codex 通过证据。实现、确定性验证（D）、真实 Runtime（R）、安装及用户入口（I）分开报告。
