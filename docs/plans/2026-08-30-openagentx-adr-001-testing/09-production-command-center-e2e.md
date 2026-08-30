---
doc_type: test_task
status: pending
owner: openagentx
test_id: T09
updated_at: 2026-08-30
---

# T09：手机与 PC 生产指挥台端到端

## 目标

通过 `https://agentx.oneaxe.cn/` 验证 OpenAgentX 指挥台既能监控，也能作为手机远程指挥入口。

## 用户旅程

1. 手机首次登录并安装 PWA；PC 使用另一 Session 登录。
2. 查看 Organization、Agent connectivity/availability/delivery readiness、Worker heartbeat、Backend health 和任务状态。
3. 选择逻辑 Agent，创建 Task A，查看时间线和实时状态。
4. 对 waiting_input 发送补充 Message。
5. 在待办中查看 Approval 的动作、范围、目标 run 和有效期，执行批准或拒绝。
6. 取消一个可取消 Task，确认页面、API 和 Event 一致。
7. 提交 Task B，确认 Agent 无需 tmux 自动接活。
8. 手机断网，验证所有写操作禁用且不进入后台队列；恢复联网后手工重试。
9. 退出、撤销 Session，并验证 PC/手机 Session 隔离和 idle/absolute timeout。

## 视口与质量

- 390x844、412x915、1440x900；
- 无文本裁切、控件重叠、布局跳动或不可达操作；
- SSE 长连接经 Nginx 不缓冲，HTTPS 证书、安全头和 Cookie 属性正确；
- 页面不得展示 Session Token、fencing token、密码或私密 Runtime 输出。

## 通过条件

手机能够独立完成发指令、回复、审批、取消和跟踪结果；PC 与手机看到同一持久事实，PWA 离线严格 fail closed。
