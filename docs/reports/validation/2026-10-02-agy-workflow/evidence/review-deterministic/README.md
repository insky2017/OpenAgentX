# 人工验收、继续任务与就绪投影验证（D）

本批为隔离 SQLite Repository、正式 HTTP Handler 与 Go race 检测，**不是 AGY 真实 E2E（R/I）**。没有实际 Runtime、浏览器、部署或真实业务数据库操作。人工验收与继续测试使用真实 SQLite；就绪代际/恢复/忙碌场景使用受控后端快照，另有真实 SQLite 未 applied/租约过期 HTTP 集成。

## 首次失败与复验

- `01-initial`：exit 1。HTTP 测试通过；Repository 的 `active-terminal-task` 反例发现活动 Run 的 NULL result_json 在领域检查之前被 Scan 到 string，错误为 `converting NULL to string is unsupported`。主代理随后将 active Run 检查提前。
- `02-race`：修复后的人工验收定向 race，exit 0；sqlite 3.057s、panel 5.147s。
- `03-continue-readiness`：exit 1，编译阶段失败；共享代码 `TaskReadModel.DeadlineAt` 正从 time.Time 迁移为 *time.Time，console 投影尚未同步。该次未执行测试，不能解释为业务用例失败；原始错误保留。
- `04-final-race`：console 类型同步后，人工验收、继续和就绪受影响范围一起复验，exit 0；sqlite 2.851s、panel 7.528s，无 race 报告。

实际命令、工作目录、起止时间与退出码分别记录在各 `.result.json`，stdout/stderr 各自保存。源码版本、Go 版本和最终相关文件 SHA-256 在 `environment.json`。

## 断言范围

人工验收：accepted/rejected × 已知/未知业务副作用四组合持久保存；Task.status/result/error/completion_basis 与整个 Run 对象不被人工评价改写；SHA-256、Run ID/version、Task新版本绑定；旧CAS/旧RunVersion/错误Run拒绝；相同命令重放仅一条Journal，不同决策/说明/Task/Run/主体冲突；12并发重放仅一次评价；noRun/active/Task终态但Run活跃/uncertain/failed/canceled Run拒绝；FaultAfterStateWrite、FaultBeforeCommit 和真实Journal FK错误均回滚Task.version与Journal，随后可正常提交。

HTTP：接受/拒绝后官方任务详情刷新呈现持久 review；旧accepted命令重放不覆盖较新的rejected；401/403、缺失/错误CSRF、幂等头不匹配不改状态；stale Task/Run与幂等payload冲突返回409；active/uncertain Run拒绝。

继续：新Task引用父Task并携带目标/结果上下文，保留旧Task与Run原样；新Task queued且无Run，重放不重复创建；非终态父级、缺失/不存在父级、其他Agent/组织、形状合法但不支持的execution override均拒绝且无Journal或Task副作用。

就绪：租约恰好到期不可执行；最新generation未applied时不能回退借用旧generation的healthy；healthy恢复可开始；running/waiting_approval/cancel_requested状态允许排队但不可立即开始；其他Agent忙碌及本Agent历史uncertain不阻塞新任务；后端查询失败和draining fail closed；真实Repository无已应用网络及租约过期在HTTP概览都显示未就绪。

## 证据与交付边界

原始持久目录：`/home/sky/.local/state/openagentx/validation/2026-10-02-agy-workflow/review-deterministic/`。仓库副本经检查仅含合成fixture，无真实凭据，保留完整stdout/stderr。`SHA256SUMS`覆盖本目录全部证据。

本代理只新增四个review/continue/readiness测试文件；产品修复由主代理完成；均未由本代理提交Git。真实 AGY 执行/产物、浏览器交互、安装后二进制及网络应用仍由主代理独立完成R/I验收。
