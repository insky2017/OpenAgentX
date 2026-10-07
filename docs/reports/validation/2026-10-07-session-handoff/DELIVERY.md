# Agent 新会话交接交付记录

## 当前状态

实现与事务集成检查完成，真实隔离 Codex 验收待执行。尚未安装、尚未重启正式服务、尚未切换任何业务 Agent 的 thread。本文件随验收更新，不能将候选源码视为运行态能力。

## 已实现

- `agent new-session` 默认只读预览，显式 apply 携带原 thread/版本和稳定幂等键。
- schema v7 活动会话指针；正式 query Task 初始化全新 Codex thread，成功结算后原子发布；身份、模型、协作对象与旧历史保留。
- 事务内拒绝忙碌、未完成咨询、旧 view/旧 Task、过期版本；失败/取消/lease 恢复保留原指针，不自动重复初始化。
- 新 Runtime capability 防止旧 Worker 忽略 ForceNew；原接入摘要不会注入已轮换的新会话。
- 清理命令支持 v7 活动会话删除闭包；不改变已保留业务历史。

## 验证记录

- Runtime、Bridge、Console API/client、Fleet 相关包通过。
- SQLite/迁移、ControlPlane、Panel、domain/API 包通过；覆盖发布/回滚、CAS/幂等、取消/恢复、待咨询、旧会话保护、owner 权限和清理闭包。
- 初次 Fleet 权限用例在工作树及未修改 main 上均失败：shell `umask 077` 将夹具要求的 0644 文件变为 0600；以 `umask 022` 运行后通过。未修改产品权限策略或此既有测试。
- 首次编译遇到并行实施中间签名不一致、CLI 临时值调用指针方法；均已修复。事务测试首次夹具错误已修正；不作为产品通过证据。
- 真实 E2E 与最终工件来源在下一步补齐；[验收计划](PLAN.md)。

## Overview 澄清

正式 `OAX:overview` 的 `%2` 仍运行总览并更新时间；同窗口额外 `%63` 为 shell。用户已确认是多开 pane，并非总览退出。本批不改总览产品逻辑、不关闭额外 shell、不操作业务终端。

## 边界

本批不提供任意历史 thread 切回/fork，不自动接续未知业务，不承诺修复原生 TUI 偶发 exit0 或取消 fallback 后旧 endpoint 重连。实际切换某个业务 Agent 需要明确目标与交接内容；本批验证使用隔离身份。
