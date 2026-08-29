---
doc_type: implementation_task
status: pending
owner: openagentx
updated_at: 2026-08-30
---

# 任务 16：React Mobile-first PWA

## 目标

实现正式命名为“OpenAgentX 指挥台”的 Vite + React + TypeScript PWA，使手机成为创建指令、处理待办和跟进任务的首要入口，同时提供 PC 运维视图。

## 依赖与入口

- 依赖：任务 15；
- 入口：Auth、Observe、Control、Admin API 和 SSE；
- 技术基线：React Router、TanStack Query/Table、CSS Modules/Variables、lucide-react、vite-plugin-pwa、Vitest、Testing Library、Playwright。

## 实施范围

- 建立 `web/command-center` Vite/React/TypeScript 工程和 Go embed 构建链；
- 手机底部主导航固定为 `指挥 / 待办 / 任务 / 组织`；
- 实现 Login、指挥、待办、Tasks/Detail、组织/Agent Detail；
- 实现 Mailboxes、Execution、系统、Events 和权限受控 Worker Admin 二级页面；
- 指挥入口默认主指挥者，授权后可选择 Domain Agent direct dispatch；
- ExecutionSpec 选项来自 API，默认折叠，不允许自由输入 Backend 参数；
- 实现 SSE 驱动的局部缓存更新、断线状态、幂等提交确认和不确定响应恢复；
- 实现 manifest、maskable icon、standalone、安装提示和可控版本更新；
- Service Worker 只缓存带 hash 的静态应用壳。

## 移动与桌面规则

- 从 360 CSS px 开始 mobile-first，核心触控目标至少 44x44 CSS px；
- 手机使用单列信息流、全屏详情和纵向时间线，不压缩桌面宽表；
- PC 使用侧边导航、排序/过滤/分页表格和并列详情；
- 状态同时使用文字/图标，不只依赖颜色；核心操作不依赖 hover；
- 页面采用紧凑运维布局，不使用营销 hero、装饰性卡片或卡片嵌套。

## 质量与验证

- 每完成一个页面或功能点，使用 `chrome-devtools mcp` 验证真实 DOM、渲染和交互；
- 表单提交必须收到服务端持久化 ID 才显示成功；
- 离线时全局提示，并禁用发送、审批、取消和 Worker 运维；
- 不使用 Background Sync，不缓存 API/SSE/Task/Message/Artifact/Auth；
- 组件测试覆盖权限、loading/error/empty/stale/offline 状态。

## 退出条件

- 手机可完整执行登录、创建 Task、补充 Message、审批、取消和查看结果；
- PC 可完成组织、队列、执行和 Worker 运维观察；
- PWA 可生成安装资产并由 Go embed 提供；
- 所有已完成页面都有真实浏览器验证记录。
