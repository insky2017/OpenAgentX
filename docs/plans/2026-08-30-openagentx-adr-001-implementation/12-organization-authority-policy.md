---
doc_type: implementation_task
status: completed
owner: openagentx
updated_at: 2026-08-30
---

# 任务 12：Organization 与 AuthorityPolicy

## 目标

实现组织、岗位、角色、汇报链和权限策略，使默认 coordinated dispatch 与显式 direct dispatch 具有稳定业务寻址、授权和审计语义。

## 依赖与入口

- 依赖：任务 11 / G3；
- 入口：Task/Message/Approval/Cancel Command Service 和 Agent Profile；
- Worker Instance 不能成为组织成员或业务接收者。

## 实施范围

- 实现 Organization、OrgUnit、Position、Role、PositionAssignment 和 ReportingLine；
- 实现 AuthorityPolicy 的 Task、Message、Approval、Cancel、direct dispatch 和 ExecutionSpec override 权限；
- 实现默认主指挥者解析和 `dispatch_mode=coordinated`；
- 实现授权后的 Domain Agent direct dispatch；
- 组织变更、委派和拒绝结果追加 Event Journal；
- 提供组织树、Agent responsibility 和合法接收者 read model；
- 首版支持一个默认 Organization，但 schema/API 不写死全局单例。

## 质量与验证

- 跨组织、越级 direct dispatch、越权取消/审批/override 全部拒绝；
- Task/Message 以 `worker_instance_id` 为目标时拒绝；
- Worker 重启、迁移或 Backend 切换不改变岗位、权限和任务目标；
- ReportingLine 变更不追溯改写历史审计事实；
- policy evaluation 使用稳定 principal/action/resource/context 输入。

## 实施结果

- 新增组织契约类型：`OrgUnit`、`Position`、`Role`、`PositionAssignment` 和 `ReportingLine`。
- 新增纯函数 `AuthorityPolicy`，以稳定 principal/action/resource/context 输入校验组织边界、direct dispatch 和 ExecutionSpec override 权限。
- 明确 Worker Instance 不是组织成员，也不能作为业务目标；coordinated/direct dispatch 的越权和跨组织请求 fail closed。

## 退出条件

- coordinated/direct dispatch 正负向测试完整；
- 组织 read model 能回答谁负责什么、谁可以指挥谁；
- Command Service 成为 CLI/MCP/Web 唯一业务写入入口；
- Worker API 和 Admin API 不能携带业务 prompt。

## 验证

详见 [Task 12 验证报告](../../reports/validation/2026-08-30-openagentx-task12-organization-authority.md)。
