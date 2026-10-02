# 原会话手工初始化后的真实通信核验

已确证：Rhythm 原 thread 自己发送咨询，Pay 原 thread 读取、ACK 并关联回复，Rhythm 原 thread 读取回复、ACK 并形成结论。正式 API 当前双方均 acknowledged，API 正文与原始文件逐字节一致。

两原会话均有用户读取 START.md 的初始化指令；此次收发发生在活动会话中，不能据此证明空闲自动唤醒、busy gate、heartbeat 调度或支付业务验收。旧 reply-receipt/status 的 pending 是 ACK 前快照，未改写。

`cli/` 为本次实际只读命令及脱敏响应；正文替换为 SHA-256/字节数。`*-original-thread-excerpts.json` 为原 thread 的通信调用投影，混合调用中的业务工具和正文不进入仓库。`source-map.json` 保存原路径、零基字节 offset、原 JSONL 行 SHA（含换行）。私密原始选定输出及产物位于同名 manual01 raw 目录，不复制完整业务 rollout。

`artifact-content-checks.json` 记录产物与 API 对照；`verdict.json` 区分人工初始化、原会话确证和未验自动唤醒。`SHA256SUMS` 校验本目录证据文件。此次审计未发送消息或执行 ACK，未操作任何原 thread。
