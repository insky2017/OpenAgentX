# Codex local01 实际进程与网络环境

范围：隔离 local01 的 R 级进程/环境装配核验。通过正式只读 Observe API 读取网络绑定；凭据仅在内存使用，未保存认证头、auth.json 或完整进程环境。

`report.json` 记录 Worker/app-server PID、starttime、实际 exe SHA-256、目标代理键、CODEX_HOME、私密环境文件来源及权限，逐项比对 handoff 和正式 RuntimeIdentity。provider 127.0.0.1:8080 被两种 NO_PROXY 的回环规则覆盖。

这些证据不证明外网代理流量、模型调用、实际 bypass 流量、systemd 重启或正式安装。inherit 测试的 network_effect/model_call 仍为 not_verified。实际原始目标环境子集保存在本机 0700 目录，文件 0600；入库版本仅保存必要端点与路径。未创建 Task。

私密原件：`/home/sky/.local/state/openagentx/evidence/2026-10-02-codex-workflow/network-live01`。脚本为复核来源，复验应使用新批次目录，避免覆盖首批证据。
