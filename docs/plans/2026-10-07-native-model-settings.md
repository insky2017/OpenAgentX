# 受管 Codex 模型切换修复

- 用户结果：原生 `/model` 可切换模型和推理强度；选择仅影响当前 Agent 的尚未开始执行的任务，已冻结 Run 不变。重开终端和重启服务后保留设置。
- 正式链路：TUI → Native Bridge → 已认证 Console API → Agent 专属 execution_profiles → BeginAttempt 冻结 → Codex turn/start。复用现有数据库表，无 schema 迁移；不写全局 Codex config.toml。
- 兼容范围：Codex 0.160.1 的 thread/settings/update、模型相关 config/batchWrite、config/read 和 thread/resume 投影。模型目录和推理强度从真实 app-server model/list 获取；其他配置写入仍明确拒绝。
- 排队语义：设置在 Run 开始规划时读取；已经开始执行的 Run、其他 Agent、角色/目录/审批策略不变。设置采用版本比较与事务审计，失效目录不得静默回落。
- 验收：真实隔离原生 TUI 选择、后续任务文件副作用、Task/Run/Journal 与 Codex rollout 参数对照；重开终端、重启后持久化；非法设置、并发版本冲突及事务回滚用必要集成测试验证。首次失败保留，不以菜单或 exit 0 代替成功。
- 发布：验证后提交、合入并推送 main，独立 clone 构建并核验来源，原子安装。用户本轮明确授权部署后重启受影响 OAX 服务，此授权覆盖旧清理批次的禁止重启限制。
- 切换：先记录线程、窗口、服务和活动任务；先完成工件与恢复脚本，再按依赖关系重启。不得误重试 uncertain 业务任务，不修改业务工程。运行来源与 CLI 安装来源分别记录，正式环境重启与验收单独出具证据。
