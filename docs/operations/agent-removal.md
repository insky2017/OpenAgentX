# 清理测试领域 Agent

`agent pause` 暂停 Worker，保留身份与历史；`agent remove --purge-history` 删除选定身份和所属 OAX 历史。总览直接读取身份表，只有完成删除后才会消失。

## 使用

```sh
# 默认仅预览；也可显式写 --dry-run
openagentx agent remove test-example --purge-history --dry-run

# 核对精确身份、记录数、文件路径和 blockers 后执行
openagentx agent remove test-example --purge-history --yes

# 多个相互通信的测试身份作为同一批清理
openagentx agent remove test-example-a test-example-b --purge-history --yes
```

必须使用已登录本机 owner 且具有 `fleet.lifecycle` 权限的 CLI 凭据。显式 `--db`、`--socket`、`--credentials` 等路径沿用本机 profile 规则，数据库 Installation 必须与认证 daemon 相同。预览不会迁移 schema、停止 Worker 或删除目标；既有认证 API 仍按原规则更新凭据使用时间。

首批保护现有六业务领域及旧 `orchestrator`。活动任务、未释放租约、其他 Agent 的引用或归属冲突会阻止执行。选中身份的空闲 Worker 可由命令提交正式 stop 请求并等待退出，随后只禁用精确匹配的服务实例。排队测试任务和不确定终态属于选定历史，可一并清理；不会重新执行这些任务。

## 会删除什么

数据库清理包括身份/profile、独占 principal、各代 Worker/backend、Task/Run、任务消息与收件箱、审批、会话绑定、外部通信、所属网络工作及事件历史。Journal 删除以已解析实体为边界，事件序号不重排，不按“操作者相同”删除其他领域历史。保留不含任务正文的删除回执。

组织角色目录等共享历史快照可能同时记载测试角色和业务角色。这类快照完整保留并在计划中列明，不能删除整条或改写 payload。当前角色所有权、其他 Agent 的任务/消息/会话引用仍属于阻断条件；历史快照中的名字不等于仍有可接单的身份。本批发现两条此类混合角色目录快照。

本机自动清理仅覆盖明确归属的 Worker YAML/env、生成身份文件、join 准备回执和配置声明的专属 Codex state 目录；删除 Fleet 中选中条目，保留其他条目和共享 systemd 模板。符号链接、变化后的文件和共享目录不能按路径猜测删除。

输出中的 `preserved_paths` 表示仍存在的外部路径。业务 workspace、Git 内的角色模板/验收报告、公共日志、共享配置与全局凭据不会随身份删除。**命令完成 OAX 清理不代表外部原生会话历史也已删除。**需要全部清理时，应在 OAX 绑定仍存在、目标 Worker 已停止时核对原生会话归属，再通过对应 Runtime 的正式删除入口处理：

- Codex：专属 thread 无 writer、无未审核派生会话后使用正式 `thread/delete`。本批核查辅助脚本为 `scripts/validation/cleanup_codex_native_history.py`，默认仅预览；不得删锁或直接改共享会话数据库。
- AGY：在原生 `/resume` 会话选择器输入完整 UUID 唯一定位，再使用原生删除操作；本批是否删除成功以文件/索引复核证据为准。
- 外部验证目录：先确认专属范围，不能因为配置中的 workspace 指向共享工程就删除整个工程。

## 本机在线升级与失败处理

现有旧版 Worker 在控制面通信错误时退出，直接重启 daemon 可能断开原生终端。本批使用本机维护入口和向后兼容 v6 迁移，不重启正式 daemon、业务 Worker 或原生终端。先安装已经验证能够打开 v6 的新二进制，再执行迁移；正在运行的旧进程继续使用原工件。

清理事务有严格时间上限，外键、身份检查和 Journal 的常规只增不改保护保持生效。数据库删除、systemd 和文件操作不是同一个事务；输出按步骤记录完成状态。任何失败必须检查最后完成步骤与剩余文件，不能把部分完成称作全部清理。具体首批对象、不中断验收与停止条件见[实施计划](../plans/2026-10-05-agent-cleanup.md)。

大历史批次触发两秒事务上限时，先核验整笔数据库删除已回滚、删除授权表为空、保留领域仍在线，再用只读预览识别各批的独占闭包。存在父子任务或相互通信引用的身份须同批；其他互不引用的身份可分批执行，每批仍重新检查阻断项并遵守原两秒上限。必要时在私密在线备份副本验证分批耗时和保留行完整性；取证后删除本轮副本，仅保留程序、校验摘要与日志。不要因超时提高上限、关闭外键或直接重试原大批次。

后续开发验收优先使用独立 profile/数据库。必须在正式安装中验证临时身份时，应在当批结束前完成明确的测试身份回收，避免重新污染业务总览。
