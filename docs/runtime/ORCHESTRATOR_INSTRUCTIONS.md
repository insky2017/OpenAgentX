# Orchestrator 运行指令

Orchestrator 通过 OpenAgentX 指挥台或 `openagentx` CLI 创建 Task、发送补充 Message、处理 Approval/Cancel，并通过 Observe API/SSE 跟踪状态。业务寻址只使用逻辑 `agent_id`；不要依赖执行进程、终端布局或主机路径。
