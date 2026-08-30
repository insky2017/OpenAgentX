---
doc_type: validation_report
task: 20
status: passed
updated_at: 2026-08-30
---

# Task 20 验证报告：原子发布验收与旧库归档

## 自动化验收

```text
go test ./...
go test -race ./...
go vet ./...
go build -o bin/openagentx ./cmd/openagentx
./bin/openagentx --help
./bin/openagentx schema verify --db <temporary-empty-db>
./scripts/check-legacy-control-paths.sh --release
python3 scripts/check_docs.py
git diff --check
npm run build
```

全部通过。`openagentx daemon --db <db> --socket <socket>` 在隔离临时目录启动成功，创建 0600 Unix Socket；发送 SIGINT 后进程退出且 Socket 清理完成。

## 验收矩阵

- 连续 Task、multi-turn、Message/Cancel/Approval 竞态、fencing、恢复和 UDS/mTLS Worker conformance：已有 Task 01-14 报告与全仓 test/race 证据通过；
- 密码 Session、RBAC、CSRF、SSE/PWA、三视口和离线 fail-closed：Task 15-17 报告通过；
- canonical 命名、旧路径删除和目标 schema 拒绝旧库：Task 18 报告与 release scanner 全 CLEAN；
- systemd、Nginx TLS/SSE、安全头、归档/hash/权限和恢复顺序：Task 19 dry-run 与 runbook 检查通过。

## 外部发布边界

本工作区没有生产 systemd、Nginx、Tailscale 证书或旧业务数据库，未执行会改变外部状态的真实切换。正式 go/no-go 必须在目标主机完成一次成功切换和一次故障恢复，并保存旧库 hash、服务状态、证书检查、Worker ready gate、SSE 重连及手机 PWA 证据；任何一项失败都按 runbook no-go。

## 冻结确认

- OpenAgentX ADR-001 文件无修改；
- 当前 Git 工作树仅包含本次实现提交及父仓进度记录；
- 旧命令、旧环境变量、终端注入和生命周期 Hook 不存在可执行入口。
