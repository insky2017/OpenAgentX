# Codex 0.160.0 协议与 Adapter 验证

- 隔离 `CODEX_HOME`/workspace，现有 Provider 认证仅复制到私有原始目录；未连接用户全局 daemon。
- Unix endpoint 使用 WebSocket。首次 raw JSONL 失败保留在 `first-failure-raw-unix/`。
- 原生 TUI 成功 resume 同 app-server/thread，显示外部 RPC turn、真实文件产物；idle 后终端真实输入产生新 turn；关闭终端后后台写文件仍完成。
- 两 RPC 客户端同 thread 同时收到完成事件；active 时第二次 turn/start 回同 turn ID（steer）；wrong expectedTurnId 被拒绝。
- **首次原生 interrupt 的工具停止验证失败**：ACK 后收到 interrupted 且即时 sentinel 不存在，但延后核验文件在 60 秒后出现；见 `cleanup-verification.json`。不能将 provider interrupted 当工具已停止。
- owned Adapter 补充进程树 PID+starttime 控制后的两次真实取消复验通过：等待原 sleep 20 秒期限之后，sentinel 仍不存在，原 host/后代逐项退出，重建后同 thread resume 下一轮成功。首次成功保留在 `first-cancel-safe-fallback/`，最终复验为 `real-cancel-output.txt`（116.64 秒）与 `cancel-adapter-delayed-proof.json`。
- Codex 0.160.0 的 `thread/backgroundTerminals/list` 返回当前 itemId/processId，但未给出 OS PID，无法独立证明精确终止，因此最终实证使用自有 host 进程树回退；诊断见 `cancel-adapter-state/cancel-background-terminals-*.json`，退出身份清单见 `cancel-adapter-state/cancel-processes-*.json`。provider processId 不能冒充 OS PID。
- 回退范围是该 Agent 专属 engine 的全部已跟踪进程，包括该 engine 中先前任务留下的后台进程；不能宣称任意多 session 无影响。当前 Agent 并发 Run 上限为 1；其它 Worker、外部 host 和用户共享 Codex 不在终止范围。外部 host 工具停止无法证明、或非 command 工具缺完成记录时保持 uncertain，不假报 canceled。
- 动态工具服务器请求会广播给两个客户端，单一 owner 应答后完成；视图不能与 Worker 同时审批。空会话 resume 失败保留，先完成真实 query 后恢复成功。
- Go Adapter 自有常驻 app-server 两轮真实验收通过：独立文件核验、同 thread resume 上下文核验、完整 final item。首次 fixture 只因合法尾换行断言失败，原始结果保留后修正并全轮复验。
- 最终单元/race 见 `adapter-unit-race-final.txt`（3.100 秒，含未完成非 command 工具保持 uncertain，以及 setsid 后代退出且无关进程保留）；此前单元记录仍保留 `adapter-unit-race.txt`。真实 Adapter 输出见 `real-adapter-rerun-output.txt`。这些不替代正式 Task/Run/Journal、浏览器和已安装 Worker 的 E2E。

原始日志均保存在 manifest.raw_root，权限 0600；本目录为脱敏副本和 SHA-256 清单。认证文件及用户配置不入库。
