# 测试领域 Agent 与所属历史清理

用户已确认清理测试身份、Worker 和历史任务/数据；前提是六个业务领域和当前 OpenAgentX 会话持续工作。正式实施结果与安装状态另写 DELIVERY，不将计划当成验收。

实施已完成，见[清理交付](../reports/validation/2026-10-05-agent-cleanup/DELIVERY.md)。正式首轮事务截止后完整回滚，按实际数据库副本验证的 8 个独立关联组执行成功；清理结束六域持续性通过。后续中断间隔自身域 runtime/native 变化单独记录，不覆盖清理时证据或推断原因。

## 本轮对象与结果

清理以下 11 个明确测试身份：

- `agy-onboarding-e2e`
- `codex-domain-e2e`
- `domain-installed-a-8304eb9e5aa7`
- `domain-installed-b-8304eb9e5aa7`
- `domain-installed-a-9730f99e2b42`
- `domain-installed-b-9730f99e2b42`
- `external-installed-a-51e44952c2`
- `external-installed-b-51e44952c2`
- `test-fake-agent`
- `test-fake-multiturn`
- `verification-runner`

保留 `openagentx`、`rhythm`、`pay-service`、`quote-service`、`identity-service`、`oneaxe-voice` 六域及旧 `orchestrator`。初次只读盘点：18 个身份；候选合计 33 Task、35 Run、6 条外部协作消息；两测试 Worker 在线，一个测试 Task 排队。执行时重新核对，不以初次统计替代执行前检查。

交付 `agent remove <id> [<id>...] --purge-history --dry-run` 及显式 `--yes` 执行：默认可预览精确身份、记录数、归属路径、服务和阻断原因。清理身份/profile、独占 Worker/运行后端、任务/执行/收件箱/消息/审批、会话绑定、网络工作、所属事件历史、专属凭据及本机运行产物。共享引用不能隐式扩展删除范围。共享业务工作目录、系统公共日志、全局凭据、共享组织/角色/配置以及 Git 中的代码和历史报告不随身份删除；专属原生 Runtime 历史使用支持的生命周期接口，不绕过 writer 锁。

## 不重启的上线边界

源码核查发现当前 `886ba7f` Worker 在 heartbeat、Mailbox/control 通信错误时退出，并关闭 Runtime；systemd 自动重启也不能保持原进程和 native 连接。因此正式 daemon、六域 Worker、Runtime 和 native **本批不重启**。

采用正式本机管理入口，复用现有 owner / `fleet.lifecycle` 鉴权、Installation 身份和仓储事务；不使用临时 SQL 脚本、关闭外键或临时移除审计保护。预览只读，身份/Installation 核对在任何迁移前完成。v6 仅添加清理相关表及实体级事务内 DELETE 授权例外；原业务表列、写入语义和事件序号保持兼容。许可不能在事务提交后残留，普通 DELETE/UPDATE 仍拒绝。先原子安装能够打开 v6 的已验收二进制，随后在线迁移；已运行旧进程继续工作，意外重启使用新工件。

旧 SQLite busy timeout 为 5 秒，旧 Worker 不容忍请求错误。迁移/清理事务目标不超过 1 秒，硬超时及回滚预算不超过 2 秒；实际测量，超预算不上线。禁止通过延长正式 Worker 超时或重启它们绕过验收。

只停止所选测试 Worker，确认无活动执行及有效租约后删除；移除精确 Fleet 条目和专属实例配置，不删除共享 systemd 模板。数据库、服务、文件操作分别记录，失败报告部分完成并支持恢复，不伪称跨资源原子性。回执仅含清理对象、数量、摘要及结果，不复制已删任务内容。

## 必要验收和证据

1. 真实隔离旧 daemon/旧 Worker/native/新 CLI：真实 Codex 任务执行期间在线迁移及清理另一测试身份，原任务与下一任务均有可独立核对产物；保留方 PID/starttime、thread、instance/generation、native 连接不变。保存命令、版本、时间、日志、Task/Run/Journal、过程快照和产物摘要。
2. SQLite/身份边界集成：错身份或错 Installation 在迁移前拒绝；活动目标/共享引用/过期计划拒绝；事务故障回滚；重复执行明确；普通事件删除继续受保护；最终外键完整且授权表为空。
3. 正式执行前后：保存六域和控制面持续性快照；只清理核准 11 身份；任务/执行/所属历史清零，候选服务不自启、Fleet 无候选项，总览只余保留身份。证据分源码验证、隔离真实运行、正式清理三层；未完成范围不得写 PASS。

任一保留方 Worker/native 退出、换代、thread 改变、API 锁超时、迁移不兼容或专属归属无法确认时，停止该上线/删除步骤并说明。不中断前提无法满足时先交付代码、隔离结果与替代方案，不重启生产碰运气。
