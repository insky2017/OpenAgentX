---
doc_type: validation_report
task: 19
status: passed
updated_at: 2026-08-30
---

# Task 19 验证报告：部署与原子切换演练

## 验证命令

```text
go build -o bin/openagentx ./cmd/openagentx
./deploy/scripts/rehearse-cutover.sh --dry-run
go test ./...
go test -race ./...
go vet ./...
./scripts/check-legacy-control-paths.sh --release
python3 scripts/check_docs.py
git diff --check
```

以上命令均通过。dry-run 输出覆盖归档、目标 schema、daemon/Worker 停启顺序、systemd 状态和 Nginx reload 检查点，且未执行任何外部服务变更。

## 部署资产检查

- systemd unit 使用 canonical `openagentx` 命名、`Restart=on-failure`、最小文件系统写权限；
- Nginx 配置使用 Tailscale 私有上游、TLS 1.3、安全头、SSE 禁缓冲和一小时读取超时；
- runbook 明确证书私钥不入库、Worker 仅出站连接、旧库只读归档和 Worker ready 前关闭业务入口；
- release scanner 全 CLEAN，当前代码/配置无旧命名或终端控制路径。

## 演练边界

本环境没有运行中的生产 systemd、Nginx 或 Tailscale 证书，因此未执行 `--execute`，避免对正式主机产生外部副作用。真实环境应使用同一脚本执行一次成功切换和一次故障恢复，并将命令输出追加到发布记录。
