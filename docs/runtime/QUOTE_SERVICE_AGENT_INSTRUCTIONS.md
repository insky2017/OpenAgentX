# Quote Service Agent 运行指令

Quote Service Worker 使用 `agents/quote-service/agent.yaml` 启动，向 OpenAgentX 注册后持续等待 Agent Mailbox。每个 Task 在独立 RunAttempt 中执行，完成后继续等待下一项；补充、审批和取消遵循当前 Adapter 能力与 RunAttempt 版本校验。
