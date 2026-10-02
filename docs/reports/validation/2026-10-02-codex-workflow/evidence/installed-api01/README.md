# 正式安装 API 验收：PASS

执行时间：2026-10-02 08:38:32–08:41:30 UTC。正式入口 `http://127.0.0.1:18100`，专用新 Agent `codex-domain-e2e`，candidate `6b68eeb810c2b65d629cd8a817bad25a21483d5c`。实际命令、脚本和上下文哈希见 manifest.json；脚本不安装或重启任何服务。

1. 旧 thread 首轮不带答案、不读取文件，正确返回 `NATIVE-QUEUE-END01|OAX-CODEX-DOMAIN`，Journal 无 completed tool item。旧内容由 real-native02/migration-handoff.json 的历史交接提供，新职责独立注入。
2. mutation 真实写入31字节文件，独立读内容/hash精确一致。Task 保持 uncertain/business_effect_unverified，Runtime succeeded；没有把文件核验结果直接写回业务验收状态。随后第三 query 完整交付。
3. 真模型执行 Python 父/子进程延迟写入夹具，独立观察父PID2471672/starttime14881455、子PID2471692/starttime14881469活着后，正式 API CAS cancel。Task 和 Run 均 canceled。等待原定写入时刻后5秒，父子均消失、sentinel不存在、调用计数恰为1。详细前后采样见 cancel/。
4. 取消后新 query 成功。同5项任务均为 worker-958389c6-d71a-4861-812e-40eafbecf1b3/generation2，per-task runtime state 对应相同旧thread和各自Task/Run/turn。daemon、AGY Worker、新Codex Worker的 MainPID/starttime/exe SHA前后一致。
5. HTTP 请求/响应、Task/Run/Journal、独立文件/进程采样、systemd journal已保存；公开副本按字段/实际认证值脱敏。原始路径 `/home/sky/.local/state/openagentx/validation/2026-10-02-codex-workflow/installed-api01`。首次正式执行通过，没有自动重试。

边界：安装来源、原生PTY、故障恢复由其他证据提供；正式浏览器见相邻 browser-installed01。API未声明业务验收成功、所有工具均可安全取消或全场景副作用保证。准备期第一次嵌套子脚本字符串静态编译失败，修正转义后父/子脚本语法均通过；该错误没有派发任何真实Task，也不属于产品失败。
