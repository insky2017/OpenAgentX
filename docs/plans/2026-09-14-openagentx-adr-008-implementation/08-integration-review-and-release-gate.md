---
doc_type: implementation_task
status: pending
owner: openagentx
updated_at: 2026-09-14
---

# 任务 08：集成审查、实机候选与发布门禁

## 目标

对 ADR-008 完整差异做安全审查和分层验收，产出可复现的 release-candidate 证据；不自动安装、不重启真实服务、不合并到 `main`。

## 依赖

- 任务 01—07 均已通过且每阶段有独立提交与 execution log。

## 实施步骤

1. 审查 `origin/main..HEAD` 全部提交和文件，确认无受保护现场混入、无父仓修改、无 ADR-006/007 语义变更、无生成物或 Secret。
2. 建立 ADR-008 条款到代码/测试/文档的逐项 traceability matrix；任何缺项回到对应任务修复，不在 Task 08 堆叠无归属功能。
3. 运行完整自动化矩阵；保存命令、退出码、耗时和必要摘要。失败结果保留在 log，修复后追加结果，不覆盖历史。
4. 在临时目录启动隔离 daemon（临时 home/DB/UDS），初始化测试用户和 Agent，验证 login/token/attach/cursor/control/logout；测试完成后只清理自身临时目录。
5. 使用隔离 `tmux -L` server 验证 `OAX` pane `0`、pane `1+` 保留、selector、window binding、Timeline、`/status`、Diagnostic 和退出不影响 Worker。
6. 使用 fake 或隔离 user-systemd harness 验证 Fleet init/up/status/down、config 一致性和 graceful drain；不得控制真实 user service。
7. 构建到独立 cache 路径，记录 binary SHA-256、Go module sum、Git SHA 和构建命令；不覆盖 `~/.local/bin/openagentx`。
8. 对 `rtx4090` 只读检查真实服务 health、NRestarts、Linger、路径权限和仓库状态。发现 drift 只记录，不修复。
9. 建立 validation report，结论只能是 `passed-candidate` 或 `no-go`。列出仍需人工批准的备份、安装、服务升级和真实 workspace 体验步骤。
10. 提交 Task 08 文档/必要修复后暂停，由监督者独立复核并决定是否推 feature branch、合并、安装或实机验证。

## 完整验证矩阵

```bash
go test ./...
go test -race ./...
go vet ./...
go build -o <isolated-cache>/openagentx-<git-sha> ./cmd/openagentx
bash scripts/check-legacy-control-paths.sh --release
bash -n scripts/*.sh deploy/scripts/*.sh
npm run test:pwa
npm run test:observation
npm run build
git diff --check
git status --short --branch
```

如仓库实际脚本布局与命令不同，先在 execution log 记录原因和等价命令，禁止静默跳过。Web 源未受影响也至少执行与 Auth/API 契约相关的现有测试；不机械重复目标主机上的破坏性部署演练。

## 安全审查清单

- 无 `send-keys`、`paste-buffer`、`capture-pane` 业务控制；
- 无 token/password/Secret/隐藏推理/原始 stderr 泄漏；
- credential 权限和 schema 升级 fail closed；
- 所有控制动作经正式 API、权限、CAS、审计、Mailbox；
- snapshot/cursor/generation/reconnect 不丢不回退；
- tmux 冲突不 kill、不覆盖、不猜测；
- Console/tmux/SSH 退出不影响 Worker；
- graceful-stop 与 force-stop 仍严格分离。

## 退出条件

- 全量矩阵通过并有可复现证据，或明确 no-go；
- validation report、binary hash 和 traceability matrix 完整；
- feature worktree 干净，提交序列可逐阶段审查；
- 真实服务、正式 DB、真实 tmux 和已安装 binary 未改变；
- Codex 停止执行，等待监督者最终决定。
