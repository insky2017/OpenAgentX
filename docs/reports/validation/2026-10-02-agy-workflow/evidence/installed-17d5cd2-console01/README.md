# 已安装 Console 日用链：I/R 首次通过

候选 `17d5cd22b442fb5fd02bd661b36c5db4299fea9d`，安装 binary SHA-256 `666cbd876c540d63b1214f81e0e65b7922b9a41f987ba21364d03718c7e22ccd`。真实 user-systemd daemon PID 306551、测试 Worker PID 343986，前后 PID/starttime/运行中 exe hash 均一致；Agent `agy-onboarding-e2e`、Worker `worker-49949502-6067-466b-8901-d41da1a48261`、generation 3。

本批独占测试Agent，以安装的 `~/.local/bin/openagentx console attach --agent agy-onboarding-e2e` 在真实专属tmux OAX/pane0运行，实际80×24 PTY键盘输入。tmux仅承载终端，不替代调度；没有fake Runtime、systemctl stub或HTTP派发。GET-only Console API复用现有installation-bound CLI credential，不存Authorization头或token。服务未改配置、未重启。

## 实际通过

1. TUI `/dispatch --intent query` 请求用工具读取workspace `input.txt`、输出ROLE约定标记；输入问题不含文件内容或ROLE标记。Task `task-fd9c1ca3-870c-4b02-acbd-0bbaf4d53e26`、Run `run-ecd1b455-7f92-4609-a642-badd2066325f` succeeded/query_result_delivered/FinalReply。精确完整结果为文件行 `OAX-FILE-d85eadcfbb` 和角色行 `OAX-ROLE-NEW-ff09d70390`，实际屏幕与正式TaskSnapshot一致。
2. TUI `/accept`→`/result-reject`。正式API分别保存accepted/rejected、同Run/结果版本与原Task succeeded；实际80×24屏幕显示两种人工结论。本例验收的是query回复，不能替代写产物验收。
3. TUI `/continue --intent query` 创建新Task `task-d1538e2a-e0fd-4253-b670-f5b9f722701f`、Run `run-8945eeaa-798f-4aed-93e6-2045a46f1a2a`；parent指向首Task，含Previous work reference。完整回复 `OAX-CONSOLE-364249-CONTINUED`，同一Worker generation执行成功。
4. `/quit`退出→实际重开安装Console→`/tasks`列表→输入完整Task ID过滤→Enter结束过滤→Enter选中。`11-reopened-result-0.screen.txt` 显示对应完成Task、Task outcome和Runtime reply；重新读取快照与退出前Task完全一致、Task集合无增加。上批D重选未收敛的事实不删除；本批独立I/R真实返回首次通过。
5. 发任务前15.0006秒空闲采样，Worker `/proc/stat` CPU ticks 265→265，前后均在线ready、无活动Run。只证明本次15秒空闲未忙等，不证明长时间负载或跨多个Run lease。

## 证据与边界

- `invocation.json`：实际命令、时间、脚本hash和journal采集命令；脚本原件在持久raw目录。
- `console.pty.log`、`pty-inputs.json`、`*.screen.txt`：实际PTY原输出、按键及当前屏幕快照（不是从历史回显推断界面）。
- `http/`、`02/04/05/06/12-*.json`：正式Console GET响应与独立快照；携带snapshot_sequence，PTY保留消费后的cursor。主代理已补充 `formal-api/`：两项任务的完整正式Observe Task/Run/Journal，独立核对succeeded与各Run。
- `before/after-service-processes.json`、`idle-sample.json`：安装服务进程/哈希/空闲采样；`daemon/worker.journal.*` 为此批时间窗口的实际journal输出，两unit均返回 `-- No entries --`；不把它们称为AGY逐事件输出日志。
- `provenance.json`：workspace input实际hash、预期文件内容、角色标记与候选来源；`cleanup.json`仅关闭专属tmux/PTY，正式daemon/Worker保持在线交回Web批次。

本批首次运行PASS。覆盖E04问答与角色/文件、E05 query结果接受/拒绝、E06 query继续、E13完成后退出返回及80×24、E22 Console子链、E10短空闲采样。未覆盖运行中退出、overview入口、mutation实际产物、认证失效恢复、多个lease长运行、浏览器窄屏。运行时限/ROLE接线以正式Run投影为据，不能由模型复述扩展为任意业务验证。

持久原始资料：`/home/sky/.local/state/openagentx/evidence/installed-17d5cd2-console01`，包括原始PTY、harness、journal；公开目录所有文件在SHA256SUMS中，运行脚本确认现有CLI token未出现在PTY或提交证据。
