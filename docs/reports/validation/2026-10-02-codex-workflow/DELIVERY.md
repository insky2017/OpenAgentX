# Codex 工作流：交付与使用

本机已安装 `6b68eeb810c2b65d629cd8a817bad25a21483d5c`，Web、daemon、AGY Worker和Codex Worker实际来源一致，`vcs.modified=false`。二进制 SHA-256：`f9b74b7df378ee05a38d3b49dc465c9168ff3ad7c70a49f81430e21b7f6b3366`。这是可用主链交付，**不表示 C01–C12 所有组合均已完成**。

## 要点

- **后台连续接单已通。** Codex app-server 接入现有 Task/Run/Mailbox/Journal；角色、目录、thread明确绑定。默认代理保存到该Agent私密环境文件，实际进程及正常重启持久性已核验。
- **已有会话可以成为长期领域Agent。** `join --prepare`登记角色与交接，当前轮结束并退出后`resume`启用。正式测试迁入旧thread，模型不调用工具即准确回忆旧内容，同时回答新角色码。不能无感接管任意正在执行的PID，也不会从标题猜thread。
- **原生终端与后台解耦。** `open --native`显示原生交互；输入仍进入正式账本，bus与native同thread竞争会排队。关闭终端后后台继续，重开可读历史；Skill只负责一次登记说明，不靠模型长期poll。
- **真实效果有证据。** 正式五Task链验证上下文/角色、精确写文件、第三轮、90秒长shell取消、取消后下一query；取消后等原写入时刻+5秒，父子进程已停且无迟到文件。其它正式服务PID/starttime保持不变。原AGY另做真实query回归。
- **缺口明确保留。** 精确取消和异常恢复还未完整解决：取消必要时终止整个专属引擎，可能影响同Agent旧后台工具，原生终端需重开；未知终态会隔离Agent，尚无一键受控解除入口。完整审批/timeout/坏代理组合与长期压力测试未全部验收。

## 现在直接使用

工作台：[http://127.0.0.1:18100](http://127.0.0.1:18100)。正式演示身份 `codex-domain-e2e` 使用独立验收目录，不与业务项目混用。

```sh
openagentx agent status codex-domain-e2e
openagentx agent open codex-domain-e2e --native
```

实际项目请用自己的稳定名称、目录与角色：

```sh
openagentx agent add --runtime codex --id research --name Research \
  --workspace /absolute/project --role /absolute/project/ROLE.md
```

给已经工作的 Agent 一段自然语言即可开始准备：

> 请运行 `openagentx agent join --help`，把当前目录和我们约定的职责登记为长期领域Agent。保存简短交接并使用`--prepare`；只有明确知道当前thread ID才传`--thread-id`。不要在当前轮里启动接管，完成后给我resume/open命令。

当前轮结束后：`openagentx agent resume <id>`，再用`openagentx agent open <id> --native`。完整步骤、默认代理和故障边界见[一页指南](../../../operations/codex-agent-entry.md)。

## 证据入口

| 内容 | 可复核记录 |
|---|---|
| 逐项覆盖与未完成项 | [C01–C12 D/R/I矩阵](COVERAGE.md) |
| 首次失败、修复与构建过程 | [执行记录](EXECUTION-LOG.md) |
| 正式来源、Web逐字节与代理重启 | [独立安装核验](evidence/installed-provenance01/README.md) |
| 旧会话迁入、三轮、真实取消与下一轮 | [正式API结果](evidence/installed-api01/verdict.json)；目录内Task/Run/Journal、PID及文件证据 |
| 原生输入、排队与重连 | [原生R与竞态](evidence/real-native02/README.md) · [正式I终端](evidence/installed-native01/SUMMARY.md) |
| 正式桌面/窄屏与断网恢复 | [真实浏览器](evidence/browser-installed01/README.md) |
| 原生取消首败与修复边界 | [协议与Adapter报告](evidence/protocol-150457/SUMMARY.md) |
| AGY兼容回归 | [同一原Task只读复核](evidence/installed-agy-regression02-review/verdict.json) |
| 当前架构与流程 | [HTML七视图](../../../design/current-architecture.html) · [Markdown/源码依据](../../../design/CURRENT_ARCHITECTURE.md) |

已安装路径：`~/.local/bin/openagentx`、`~/.openagentx/web`，服务为`openagentx.service`与`openagentx-worker@codex-domain-e2e.service`。旧binary/Web/数据库与release标记保存在私密 `~/.local/state/openagentx/validation/2026-10-02-codex-workflow/installation/rollback/`；恢复数据库不能覆盖安装后新增有效任务。所有证据保留SHA-256，凭据/数据库不入Git。

## 下一步

1. 用一个实际领域、小任务和明确产物开始日常使用；先看结果与产物，再继续下一项工作。
2. 优先补可受控解除隔离的恢复入口，以及更精确的工具取消；依据真实失败扩展测试，不再引入另一套平台。
