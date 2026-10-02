# External Session 独立通信验收

本目录记录真实隔离实例与正式安装服务的 CLI/Unix HTTP API 通信能力。身份为 `agent apply` 创建的随机前缀测试 Agent，未为测试启动 Worker 或 Codex，未绑定或操作两个原业务会话。不能用这些证据替代原会话 E04/E05、自动唤醒或业务效果验收。

- `smoke01/`：首次候选真实主链 PASS。包含 owner 登录、双身份绑定、consultation/request、inbox/status/ack/reply、相同 key 重试及冲突、错误 token/peer、禁止回复 result、daemon 重启持久性。44 个证据文件，原始目录保留在其 manifest 的 `raw_root`。
- 所有状态变更均来自正式 CLI 或 API；SQLite 仅以只读方式独立统计任务、Run、Worker 和消息数量。首轮为 3 条消息，任务/Run/Worker 均为 0。
- 凭据文件和交互密码输入位于私有目录，既不进入 raw 记录，也不导出。各次尝试使用新目录，保留首次结果；每份导出记录有 SHA-256。
- `smoke02/`：最终候选复验 PASS，14 组断言。增加 64KiB 正文最坏 JSON 转义（65,535 个 NUL 产生约 393KiB JSON 请求）的 CLI send/inbox 完整比对，65,537 字节正文由 API 拒绝为 400；合法消息累计 4 条，任务/Run/Worker 仍均为 0。
- `smoke02/upgrade-before.json` 与 `upgrade-after.json`：正式安装 v2 数据库通过只读连接备份，候选只升级私有副本到 v3。旧有 38 张表共 279,560 行的逐表行数与行集合 SHA-256 全部保持一致；新增 2 张外部通信表。原数据库没有执行升级或其他写操作，备份本身不导出。
- 两次结果分别保留，最终导出逐项核验 raw/导出 SHA-256，并逐字检查实际 owner 密码和全部 CLI/Agent 凭据均未出现在证据中。

`installed01/` 为正式安装的独立复核，7 组断言全部通过。实际运行源码为 `6576c55`、`vcs.modified=false`，二进制 SHA-256 为 `432cbd044902351670179e7eaaf463871f59bec0d274ed8810f49eab56d7fd39`。两个随机测试身份完成 bind→send→inbox→ack→reply→status；相同 key 重试返回原消息，第二份不同 key 的 result 被 409 拒绝。独立只读数据库观察显示本轮仅有 request/result 两条 external 消息及 bound×2、sent×2、ack×1 五条外部通信 Journal，没有本轮 Task、Message、Mailbox、Run 或 Worker。

Managed Task 创建被正式 API 明确拒绝。既有 Panel 把该领域 conflict 映射为 HTTP 400，响应必须含 `external session uses messages, not managed tasks`；本次未为统一状态码修改原 API。该文案/状态码一致性事项属于非阻断后置项，不影响“拒绝且无调度写入”的实证。三个正式 OAX 服务前后 PID、starttime 和运行二进制哈希完全相同，脚本没有服务生命周期操作。`installed01/installed-build-info.json` 保存实际构建信息；该目录含清理记录共 26 份文件，哈希及实际凭据不泄漏检查均通过。

正式安装执行入口：`scripts/validation/external-session-installed.py --binary <正式二进制> --root <全新私有目录> --db <正式库> --socket <正式socket> --credentials <现有owner凭据文件> --password-file <只读私密密码文件> --evidence <全新导出目录> --commit <源码> [--unit <待只读观察的现有user service>]`。只使用随机测试身份发信，保留其通信记录供复核。

执行入口：`scripts/validation/external-session-smoke.py --binary <候选绝对路径> --root <全新私有持久目录> --evidence <全新脱敏目录> --web web/dist --commit <来源> [--upgrade-source-db <只读源库>]`。

`--upgrade-source-db` 只通过 SQLite `mode=ro` 打开源库并备份。升级命令只接触私有副本；前后逐表核验旧数据行数及无序行集合 SHA-256，不导出业务数据或数据库副本。

正式安装验收收尾只撤销 `external-installed-a-51e44952c2` 与 `external-installed-b-51e44952c2` 两个测试绑定：先读取 generation，再经正式 `external revoke` 撤销，旧专用凭据随后均返回 HTTP 401。见 `installed01/cleanup/`。保留身份、消息和 Journal 供复核；真实 `rhythm` 与 `oneaxe-pay` 绑定未操作。此项为测试凭据清理，没有重跑验收矩阵。
