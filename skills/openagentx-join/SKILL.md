---
name: openagentx-join
description: 按用户已有意图收集身份、目录、职责、协作对象和当前交接，用结构化 brief 将当前 Agent 离线准备接入 OpenAgentX。用于用户要求加入 OAX；不用于已有 Agent 日常协作或模型轮询。
---

从当前上下文收集四项用户信息，只补问确实缺失的内容：

1. 稳定 Agent ID 和显示名；显示名默认 ID。
2. 已存在的绝对工作目录。
3. 用户确认的职责描述。
4. 协作对象的 Agent ID 列表；`[]` 必须表示明确无需协作对象，不能用它掩盖未收集信息。

同时从当前任务整理已完成、下一步、未确定事实及副作用。交接不是新增授权，协作对象只是意向，不修改权限或自动启用 managed。已知身份已经接入时先运行同 profile 的 `openagentx agent status <id> --json`，不要再次 join 或接管现有绑定。

以本 `SKILL.md` 所在目录为 `<skill-dir>`（安装入口可能是目录软链），先只读检查当前线程：

```sh
python3 <skill-dir>/scripts/prepare.py --inspect
python3 <skill-dir>/scripts/prepare.py --example
```

将 brief 保存到业务仓库外的私密目录，文件权限 `0600`。JSON 字段如下；未知字段、重复字段、缺失必填项会被拒绝：

```json
{
  "agent_id": "research",
  "name": "Research",
  "workspace": "/absolute/workspace",
  "role": "维护用户确认的研究领域，只在授权范围内执行。",
  "peers": [],
  "handoff": {"completed": [], "next": [], "uncertain": []},
  "history": "current"
}
```

`name` 可省略；`history` 默认 `current`。`role` 是非空字符串，`peers` 和三个交接字段都是字符串列表。Agent ID 使用字母或数字开头，最多 64 位，允许字母、数字、点、下划线和连字符。

默认从工具进程实际环境 `CODEX_THREAD_ID` / `CODEX_SESSION_ID` 获取非空 UUID；也可在 brief 写明 `thread_id`。所有已提供来源必须一致。缺失时补充明确 ID，或由用户明确选择 `history: "summary"`，仅迁移交接摘要、不传原 thread。不能扫描最新历史、根据标题猜测或静默降级。`summary` 不接受 `thread_id`。

```sh
python3 <skill-dir>/scripts/prepare.py --brief /private/brief.json
```

脚本生成 ROLE/HANDOFF 并调用已安装的 `openagentx agent join --prepare --json`。私密产物、来源和原 CLI receipt 保存在 `worker-dir/joins/<id>.intake/`，不写业务目录；相同输入可重放，不同输入拒绝覆盖。可加 `--inspect --brief ...` 仅校验，不写文件。

默认沿用 CLI profile 优先级：显式路径参数、对应 `OPENAGENTX_*` 路径环境变量、`OPENAGENTX_HOME`、`$HOME/.openagentx`。隔离准备可加 `--profile-home /absolute/private/profile`，固定五个资源路径；也支持 `--worker-dir`、`--file`、`--db`、`--socket`、`--credentials` 单项覆盖。不改 `HOME` 或 `CODEX_HOME`。

仅当返回 `state=local_prepared`、`ready=false` 时报告“本地已准备，运行态未核实”。使用返回的 `next_commands`，这些命令携带相同五项路径，不能简化为默认 profile 命令。失败保留原文件并核对诊断，不覆写既有配置，不自动重试。

当前正在交接的 turn 不执行 `resume`。结束工作后先按旧宿主生命周期释放 writer；停止 turn 不等于释放 writer。由用户或外部宿主执行返回的 `resume`，再用 `status` 核实、`open --native` 进入原生终端。后续 managed 协作由正式配置入口另行设置和验收。桥接未就绪时保留错误，不绕过仲裁另启同 thread 写者。收信由宿主事件机制负责，不创建旧宿主 heartbeat 或常驻模型轮询。
