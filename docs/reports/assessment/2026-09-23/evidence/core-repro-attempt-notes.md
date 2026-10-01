# 复现过程边界

- 初次 HTTP 场景测试包含两处夹具预期问题：取消原先预期进入 cancel_requested，实际返回 400；Execution JSON 使用了不属于契约的 version/workspace/permission_mode 字段，strict decode 拒绝。
- 读取实际 ExecutionSpec 契约后一次修正请求形状；取消响应改为读取原始错误并核对事务回滚。没有改动产品实现。
- 最终 core-reproductions.log 的 PASS 表示复现了缺陷行为，不表示该行为通过验收。
- HTTP 认证、CSRF 和 SQLite 使用真实实现；账号、数据库、HTTP listener 是隔离 fixture。恢复测试只用 repository 方法创建专用过期状态，不直接改 SQLite，不构成真实进程崩溃或 Runtime E2E。
- 复现源码中的测试密码仅为临时 fixture 输入，不来自用户或生产配置；报告与日志没有真实凭据。
