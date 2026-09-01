---
doc_type: test_task
status: blocked
owner: openagentx
test_id: T09
updated_at: 2026-09-01
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

## 验证结论

- 已确认生产 HTTPS/Nginx/SSE、PWA 基础资源、认证与 Session、生产 Task/Message/取消 API、事件流和 Worker 持续在线等基础链路；`running` Task 的取消已完成 `cancel_requested → mailbox → canceled` 闭环。
- 现有归档截图覆盖 `390x844` 与 `1440x900`，但本轮仅保存到 `412x915` 登录页；没有一份可审计的认证后手机完整交互链路，也没有 PC 与手机独立 Session 对同一持久事实的成对证据。因此不能将手机/PC 指挥台全旅程、页面敏感信息隔离或离线写操作 fail-closed 标为本关已通过。
- “真实长运行 AGY turn 被取消”同样阻塞：测试任务在执行 `sleep 30` 前，因 AGY eligibility 请求 DNS 失败而退出；因此不能证明真实长运行进程已被中断。
- T09 暂不标记整体 PASS；需补齐生产浏览器双 Session/离线证据，并在不修改取消语义的前提下恢复 AGY eligibility 网络后重跑真实长运行取消子项。
