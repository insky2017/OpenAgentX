# 正式安装来源与网络重启持久性独立核验

安装来源：`6b68eeb810c2b65d629cd8a817bad25a21483d5c`；二进制 SHA-256 `f9b74b7df378ee05a38d3b49dc465c9168ff3ad7c70a49f81430e21b7f6b3366`。

- root 执行重启，本验证者只读采集 BEFORE/AFTER，不操作服务、不派发 Task。daemon、AGY Worker、Codex Worker 实际 `/proc/exe` 与发布一致；app-server 是 Codex 0.160.0，其 SHA 与正式 RuntimeIdentity 一致。
- Codex Worker/app-server PID 与 starttime 均改变；目标代理环境、CODEX_HOME、NO_PROXY 及私密 `.env` hash 保持一致；正式绑定从第1代到第2代，AFTER独立Worker投影、applied回执与ready一致。
- release-clone/web/dist 的8个工件与安装目录、实际HTTP响应逐字节SHA一致；Go build info 指向发布commit且vcs.modified=false。
- 首次 version 子命令不存在，原输出保留；改用支持的 help 与 go version -m。BEFORE采集脚本未取独立Worker generation，其同名布尔值不可作为该断言；修正后的AFTER脚本和原脚本分别保存，准确边界见report.json。最初release.txt仍是旧版本，root随后更新，最终副本已保留。

本批只证明安装来源、Web静态工件与环境装配/重启持久性，不证明外网代理已穿透、实际bypass流量、模型效果或产品浏览器交互。私密原件：`/home/sky/.local/state/openagentx/evidence/2026-10-02-codex-workflow/installed-provenance01`；只采目标环境键，不读取auth.json、不保存token或认证头。
