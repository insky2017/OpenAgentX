# 受管原生终端与同线程排队验收

- candidate02 的 `openagentx agent open codex-local-e2e --native` 经真实 PTY 打开 Codex；没有裸调用 Codex resume，也没有 tmux 输入。local01 后台保持 candidate01，所以不是最终安装验收。
- 首次 `real-native01` 的终端横幅为 0.153.1，其退出前未采集 frontend /proc/exe，保留版本来源限制。写文件、Task/Run、退出操作事实仍保存；不能把首轮标成 0.160。
- 本次 `real-native02` 直接核对 frontend PID 2377419 的 /proc/exe、SHA 与 --version，确认 0.160.0；实际 SHA 与 backend 运行 inode 相同。重开看到前次原生文件结果和关闭期间 API query 的完整回显。
- `real-native-contention01` 在原生真实工具运行时向同一 thread 正式提交 bus query；该输入 queued、零 Run。原生 Run 于 08:16:29.706803952Z 结束，bus Run 于 08:16:29.710451529Z 开始，无重叠；两轮在同一 Worker/generation 执行。
- 文件 `native-queue-result01.txt` 精确为 `NATIVE-QUEUE-END01\n`；bus query 结果为 `NATIVE-QUEUE-END01|BUS-AFTER-NATIVE01`；原生 TUI 同时显示两轮。mutation Task 仍 uncertain，Run succeeded；没有把独立文件通过改成 Task 自动成功。
- 两次前台均用 Ctrl-D 自然退出，code0；后台 daemon/Worker PID、starttime、exe SHA保持。工具进程独立检查无残留。本轮未测试取消。

证据关联：`../real-native01-observe/` 为首原生 Task/Run/Journal，`../real-native-next01/` 为关闭期间正式 API 同thread query，`../real-native-contention01/` 为排队中间态与最终 Task/Run/Journal/文件/时序核验。每目录含 SHA256SUMS；私有原始目录与本目录同名，位于 `/home/sky/.local/state/openagentx/validation/2026-10-02-codex-workflow/`。

下一步：主代理完成取消修复与最终候选/安装验收后更新发布结论。
