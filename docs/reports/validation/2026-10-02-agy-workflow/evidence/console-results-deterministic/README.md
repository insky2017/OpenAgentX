# Console 继续、验收与就绪接线（D）

- `/continue [--intent query|mutation] <内容>` 对所选终态 Task 创建关联的新 Task，正式 API 接收 `parent_task_id` 与 `continue_context=true`。省略 intent 沿用所选工作，保持显式发送。
- `/accept [备注]`、`/result-reject [备注]` 绑定已加载 Task/Run 的版本调用正式验收 API。展示验收人、时间、结论、备注，保留原执行状态；未确认停止不能验收。`/reject` 仍为已有审批拒绝。
- Console TaskSnapshot 提供 review 与 parent；版本混读拒绝。reducer 允许终态的验收元数据递增，仍拒绝原状态/结果改变，并保留重复事件幂等。
- Agent 选择列表与 `/status` 使用共用就绪投影；`/status` 重新读取正式 Attach，避免把连接时旧就绪状态当成当前状态。Run截止时间继续显示。

## 验证

| 证据 | 结果 |
| --- | --- |
| 01-console-packages.log | 首次四包测试：非PTY通过，PTY持久路径过长导致Unix socket绑定失败。 |
| 02-pty-retest.log | 缩短路径后真实链成功到首Task，测试等待视口外Focused摘要失败。 |
| 03-targeted-race.log | 新API测试夹具漏填Run必填身份字段；其余新增定向race通过。 |
| 04-console-retest.log | 补全夹具后四包非PTY全部通过；compact-pane摘要等待仍失败。 |
| 05-pty-full-pane-retest.log | 按两次同因止损换全pane；夹具在异步focus完成前发下一命令，首字符被loading吞掉。 |
| 06-targeted-race-retest.log | 新增API/client/TUI/reducer回归的定向race全部通过。 |
| 07-pty-focus-sync-retest.log | 再选择同一Task的Focused摘要等待仍未收敛；保留该失败，不宣称该路径的PTY通过。 |
| 08-pty-focused-result-retest.log | 换为用户自然主路径：直接对当前已focus完成Task操作；真实PTY接受→拒绝→关联继续、API状态核验、退出后下一任务全部通过（28.98s）。 |
| pty-r2…pty-r6/ | 各次实际PTY/daemon输出，r6另有Worker日志及接受、拒绝、继续的正式API快照。 |
| metadata.json | 真实命令工具版本、fixture路径、二进制SHA-256与源码文件哈希。 |

首次失败与复验分开保存。两次相同微型pane的观察假设失败后改用全pane；最终采用已focus当前Task的操作路径，避免额外重选同一任务。已有 `/tasks` 单测通过，但本批不宣称其新真实PTY路径通过。所有执行均为隔离fixture D：真实进程、PTY和API不能替代真实AGY R或安装I证据；未操作正式服务。
