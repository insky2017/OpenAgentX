DOCS_INDEX_HANDOFF_20261007

- **现有总索引**：已列出顶层名称并提取普通 Markdown 标题。磁盘顶层没有 `AGENTS.md`、`README.md` 或全机总览索引；本次提供的 AGENTS 指令仍遵守。[COMMANDER.md](/home/sky/docs/COMMANDER.md) 是新指挥者入口，不是历史全机总览。
- **实际存在的分组索引**：[Voice](/home/sky/docs/oneaxe-voice/README.md)、[Colab](/home/sky/docs/colab/README.md)、[CLIProxyAPI](/home/sky/docs/cliproxyapi/README.md)、[OneAxe Proxy](/home/sky/docs/oneaxe-proxy-guard/README.md)、[Sub2API](/home/sky/docs/sub2api/README.md)。另有 `oneaxe-payment/readme.md`，标题为“支付账户资料”，仅提取标题，未展开账户正文。
- **机器／工程文档分组**：网络与远程访问（Mihomo、Tailscale、DERP、出口节点、Cloudflare、DDNS）；模型网关与开发宿主（CLIProxyAPI、Sub2API、Codex、AGY、SoL-Pi、OpenCode）；身份与业务能力（Authentik、Voice、支付）；云端工作环境（Colab、OpenClaw）；本机硬件与媒体（NVIDIA/MOK、录音、AMR）。
- **限读的五份说明及确认边界**：

| 说明入口 | 已了解内容 | 后续须谁确认 |
|---|---|---|
| [Authentik 查询](/home/sky/docs/authentik-query.md) | 查询服务入口、常驻容器查询桥、网络与权限边界 | Identity 确认现行访问授权 |
| [Voice](/home/sky/docs/oneaxe-voice/README.md) | 服务端与双客户端布局、共享 GPU、会话与模型管理边界 | Voice 确认容量和真实设备验收 |
| [Codex 宿主代理](/home/sky/docs/codex-app-server-daemon-proxy.md) | 更新请求由后台进程执行，终端代理不一定被继承 | root／OAX 确认当前宿主和执行入口 |
| [Colab](/home/sky/docs/colab/README.md) | Drive 持久化、临时算力、装配工具、SSH 与代理索引 | root 确认维护归属及当前连接约束 |
| [OneAxe Proxy](/home/sky/docs/oneaxe-proxy-guard/README.md) | 文档已迁往工程，本目录保留兼容入口 | root 确认维护归属和现行部署 |

- **未读范围**：未打开恢复码、凭据、令牌、备份或私有支付配置；其余说明仅做标题／索引级了解，未跟随链接进入业务工程。支付资料及应用接收器边界仍须分别由 Pay、Rhythm 确认。没有发送协作请求、创建子代理或执行维护。
- **证据边界**：这些是历史运维说明，不能代替当前部署状态、正式 scope 或业务成功证据；部署权仍由 root 持有。

下一步：结束本次有限交接，空闲不轮询。

