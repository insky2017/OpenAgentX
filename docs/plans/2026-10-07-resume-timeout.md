# Codex 执行时限与已登记 Agent 恢复修复

用户已授权修复、安装及必要重启。原计划以4小时缓解旧30分钟中断；用户进一步明确希望任务执行完毕，因此本批 Codex 默认不设总截止，仍保留显式正时限、人工取消和现有断连/终态不确定处理。正常完成由真实 Runtime 终态确认，不无限自动重试。AGY 时限不变。

## 用户结果

1. Codex `agent join/add` 默认 `timeout: 0s` 表示无总执行截止。Worker descriptor、控制面冻结规格和 Adapter 一致，不将0补为30分钟；显式正数仍生效。已冻结的运行不改写。
2. 六域现有 Codex 配置从30m调整为0s，受控重启后保持原thread、原window/pane及Agent模型设置。
3. 已登记 Agent 日常resume以经认证安装中的权威身份、角色路径、workspace及capabilities核对；不再被一次性bootstrap receipt的旧摘要或坏JSON阻断。未登记身份保留原注册保护，不重写旧资料或伪造摘要。
4. 显式超时在结果中标注 `deadline_exceeded`；不与人工取消混淆，不自动重跑已有副作用。

## 验收与证据

- 隔离真实CLI、daemon、Worker、Codex app-server执行至少31分钟本地工具任务：保留正式Task/Run/Journal、冻结timeout0/无deadline、真实开始/结束proof和日志；Runtime终态与Task业务复核状态分开记录。
- 真实SQLite集成核验未登记保护、注册后合法timeout/模型偏好变化、损坏receipt、身份/角色路径/工作目录/安装冲突。
- 关键取消状态机核验显式截止及无截止人工取消；AGY仍要求正时限。
- 正式Rhythm/Pay使用修复CLI resume成功，六域online/healthy/ready，实际运行工件SHA和原thread/pane、模型设置保持。
- 修改正式配置前私密备份；先以正式stop等待活动Run自然完成，再重启。独立脚本等待操作者自身结束后部署并回派原thread核验，避免在自身进程内自杀式重启。遇不确定终态停止切换，不强杀活动任务。
- 不新增业务任务、不修改业务代码、不直接改数据库。安装与源码提交、测试与业务结果分别报告。

## 非目标

不实现自动重跑、任务完成语义重构、自动判断推理是否卡死或通用升级平台。无总时限意味着真正无响应任务可能需要人工取消；现有连接/RPC超时和用户取消仍有效，不把未来的空闲检测写成已实现。
