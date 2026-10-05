# 测试 Agent 原生会话历史清理

本页确认 `codex-domain-e2e` 的 1 个 Codex thread 和 `agy-onboarding-e2e` 的 11 个 AGY conversation 已通过各自原生接口删除，并有独立只读复核。结论仅覆盖这 12 个原生对象，不据此判断 OAX 控制面记录清理或整个批次已经完成。

## 范围与归属

实际版本为 **codex-cli 0.160.0**、**AGY 1.2.16**。清理前按 OAX `session_bindings` 的 Agent 归属提取准确 ID，再与原生存储元数据交叉核对；没有其他 Agent 共用这些绑定，没有派生子会话，目标未持有可见 writer/presence 锁。Codex 取消记录文件名中的 `01a0fbc4-…` 是 turn ID，不是另一个 thread。

Codex 对象：

```text
codex-domain-e2e → 01a0fb9b-4097-79d3-902f-6603a844fa15
```

AGY 对象均归属 `agy-onboarding-e2e`：

```text
737da8cb-adeb-46f6-b6c1-7ebe9169ee12
63659f75-f7f9-46a2-857a-f74d9f5eb4e8
cc064bc9-b5ef-4e7e-a93d-ac45aa6fa669
9f5dd97c-62aa-4393-acdf-dcf0182fb2c7
c11bcc8f-375d-4b07-b078-6d10892b7b5a
154a09ec-3ca1-4ae8-b8fc-e468f5b876a8
e51ca819-7df6-466a-a60f-97142a847f7b
a10dbd1e-ca03-4137-bb3d-ec55b8358ed5
489142fd-988d-4754-b310-141fceef4700
9ec4a622-010a-4186-9776-4458bd7535dc
8b31d994-6951-4962-9eb7-3328fef57dda
```

归属、版本、无派生关系及临时 app-server 的真实只读协议验证见 [ownership-before.json](evidence/native-history/ownership-before.json)。记录中的 `$HOME` 替代本机用户主目录。

## 正式操作与复核

两测试 Worker 经正式 `agent pause` 停止后才执行删除。清理脚本再次检查目标 Worker 为 `inactive`、`MainPID=0`、cgroup 无进程，并检查独占绑定和 writer 锁；六个业务域、`orchestrator` 及 `oneaxe-pay` 别名受硬保护。

Codex 使用 [cleanup_codex_native_history.py](../../../../scripts/validation/cleanup_codex_native_history.py)，源码提交 `3b6989eb65150e9360e9c734ebf7e1f9e3d7c60c`。先 `--dry-run`，再 `--yes`；独立临时 `codex app-server --stdio` 经初始化后调用：

```json
{"method":"thread/delete","params":{"threadId":"01a0fb9b-4097-79d3-902f-6603a844fa15"}}
```

该接口会级联删除派生 thread，因此前置检查同时查询活动与归档派生关系，并要求为空。操作未手工删除锁或改写原生数据库，未关闭共享 app-server。接口契约见 [OpenAI Docs](https://developers.openai.com/codex/app-server/)。[操作证据](evidence/native-history/codex-operation.json)保留原始 helper stdout 的脱敏预演、删除回执及 helper SHA-256；上述 JSON-RPC 方法来自同一哈希源码，不冒充线上请求报文抓取。

除 helper 自带检查外，2026-10-05 05:34:12 UTC 的[独立只读复核](evidence/native-history/codex-independent-after.json)确认：目标 `threads` 行、rollout 文件、派生边均不存在，`thread_turns`、`thread_items`、`thread_history_projection_state`、`thread_realtime_items` 中目标 thread 的行数均为 0。

AGY 使用独立终端的 `/resume` picker，逐条输入完整 UUID、确认唯一候选后按 **F4 → 原生 `[Delete? (y/n)]` → `y`**。第一条先完成文件和索引复核，再继续其余十条；没有通过标题定位，没有恢复会话或启动模型任务。每条均独立核对目标数据库、brain、annotations、presence 和共享摘要索引，累计比较清理前后的全部 conversation ID 与数据库文件名。

[逐条操作与复核](evidence/native-history/agy-target-actions-and-checks.json)仅摘录目标 UUID、确认标记及结果，移除了会话标题、账户和其他 picker 行。首条 UUID 在录制执行过程前已完成筛选，公开证据保留当时的观察元数据和随后实际记录的 `x`／退格复核；不将它表述为本段录制包含完整 UUID 输入回显。其余十条均有完整 UUID 的原始筛选回显摘录。

2026-10-05 05:28:45 UTC 的[最终独立核验](evidence/native-history/agy-independent-final.json)确认：

- 11 个目标的 conversation 数据库、brain 目录、annotations 文件和 summary 索引行均已清除。
- 清理前的非目标 summary ID 和 conversation 数据库文件全部保留，没有发现额外删除。
- AGY 原生接口留下 **11 个零字节 presence 锁文件**；inode 未变、没有持锁者，未手工删锁。这些空文件不含会话历史。
- 自有 AGY PTY `30110` 正常退出，退出码为 0；未关闭其他 AGY 会话。

## 原始证据与公开范围

原始证据保留在本机私密目录 `$HOME/.local/state/openagentx/validation/2026-10-05-agent-cleanup/`：Codex 为根目录 `native-codex-*.jsonl`，AGY 为 `private/native-history/` 中的完整按键记录、清理前元数据和逐对象复核。原始 PTY 含无关会话标题，不原样入库。

[manifest-sha256.json](evidence/native-history/manifest-sha256.json)列出本页使用的私密原始文件及公开脱敏文件的字节数、SHA-256，可在本机复核导出来源。公开证据不包含会话正文、认证内容或共享日志；原始历史验收证据没有作为清理对象删除。
