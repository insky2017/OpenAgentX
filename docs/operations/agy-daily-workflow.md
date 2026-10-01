# AGY 日常工作：从添加 Agent 到继续结果

本页对应 `codex/agy-workflow` 候选；实际安装与通过状态以[执行记录](../reports/validation/2026-10-02-agy-workflow/EXECUTION-LOG.md)为准。

## 第一次：添加一个领域 Agent

```sh
openagentx agent add
```

依次填写名称、已存在的工作目录和职责文档路径（也可直接输入职责描述）。按提示使用现有 owner 密码。程序生成或复用身份、Worker 与 Fleet 配置，启动该 Agent 的后台服务并准备网络；完成后给出工作台链接。日常使用不用打开 tmux、编辑 YAML 或手动发布网络。AGY 自身须已登录。

已有定义可直接纳管：

```sh
openagentx agent add --identity /absolute/path/to/identity.yaml
```

明确模型时使用 `--model`；每次执行默认最多 30 分钟，新建时可通过 `--timeout 1h` 调整。已有配置复用，参数不会静默覆盖现有服务。角色文件修改从下一次运行生效，每次运行保存当时的内容摘要和工作目录。

## 平时：打开、交付、继续

```sh
openagentx agent open <agent-id>
openagentx agent status
```

工作台选“问答”可得到完整回复；选默认的“执行任务”可读写该工作目录内的材料。执行结束后看结果并检查实际文件，点击“接受结果”或“结果有问题”，需要时填写验收备注。人工接受会独立留痕，原始运行记录保留。

继续修改时点“继续此工作”：新任务会引用上次目标和结果，并显示来源 Task。运行中可以补充，标记为“下轮处理”；不会声称即时中断模型。角色和目标应写清产物路径，便于后续任务找到文件。若执行停止情况未确认，先处理该事实，勿把重发旧指令当作恢复。

关闭浏览器或退出 Console 后，后台继续工作；重新打开即可回到结果。终端用户可用 `agent open <agent-id> --console`；Console 支持 `/continue <内容>`、`/accept [备注]`、`/result-reject [备注]`，用 `/help` 查看现有指令。

## 停止与恢复

```sh
openagentx agent pause <agent-id>
openagentx agent resume <agent-id>
```

“暂停”停止接新任务，当前任务结束后后台退出；“恢复”重新启动并等待当前服务代次的网络就绪。取消当前任务使用工作台的“取消任务”，与暂停分开。页面保留已产生的效果，不把取消解释为撤销文件修改。

若没有就绪，先看 `agent status` 给出的原因和下一步。需要日志时：

```sh
journalctl --user -u openagentx.service -n 60 --no-pager
journalctl --user -u openagentx-worker@<agent-id>.service -n 100 --no-pager
```

测试证据、失败记录及版本来源见[实施与证据记录](../reports/validation/2026-10-02-agy-workflow/EXECUTION-LOG.md)。本轮仅验 AGY；保留的 CodeBuddy 不代表已经完成真实验收。
