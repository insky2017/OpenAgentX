---
doc_type: implementation_task
status: completed
owner: openagentx
updated_at: 2026-09-22
---

# 任务 08：集成审查与候选门禁

## 目标

对 ADR-009 完整提交链和 diff 做最终安全审查，重跑无缓存全量、隔离用户闭环和真实边界检查，产出
`passed-candidate` 或 `no-go` validation report；不自动发布、安装或部署。

## 依赖与范围

- Task 01—07 均为 `completed/GO`，每关有实现提交与独立 docs-only gate record；
- 审查从监督确认的 ADR-009 baseline 到 HEAD 的全部提交，不只看最后一项；
- 产品缺陷回到所属 Task 创建独立 fix 并重新 gate，Task 08 文档提交不得夹带代码。

## 验收矩阵

| 类别 | 验收内容 |
|---|---|
| 正向 | 默认路径 login/Fleet/OAX Attach/dispatch/过程/最终 reply/focused control/Diagnostic/quit 全闭环 |
| 状态/CAS/幂等 | Task/Run/Worker/version/cursor traceability；重复/重连/mode switch；控制恰好一次；Worker 可继续领取下一任务 |
| 失败 | 全部 projection/cursor、auth/scope、CAS、empty/malformed/truncated output、retention、offline 和 compact terminal 故障注入 |
| 竞态 | snapshot N/N+1、dispatch/event、terminal/output、replacement、Follow ack/cancel、mode switch old stream |
| 资源/平台 | bounded memory/output/page；临时 HOME/DB/UDS；唯一 tmux server；无真实服务/DB/default tmux mutation |
| 证据格式 | ADR 条款->代码->测试->文档 traceability；命令/退出码/耗时；候选 SHA256/provenance；失败历史和未执行项 |

## 实施步骤

1. 校验冻结 ADR-009 hash，审查 baseline..HEAD 全提交、完整 diff、依赖、生成物、Secret 和父仓边界。
2. 建立逐条 traceability matrix，确认没有 ADR-006/007、Foreground、Runtime TTY 或 tmux 控制范围混入。
3. 无缓存运行全仓 Go 普通/race、vet、module、build、Web、shell/systemd、release/secret/legacy 检查。
4. 重跑 Task 02—07 的关键故障注入和唯一 `tmux -L` + PTY 用户闭环。
5. 从 clean detached worktree 构建候选到独立 cache，记录 Git revision、`vcs.modified=false`、SHA-256、
   `go version -m` 和权限；不覆盖 installed binary。
6. 如获单独授权，只读核验目标主机 repo/service/binary/DB/socket/credential/OAX 状态；发现 drift 只记录。
7. 新建 validation report，结论只能是 `passed-candidate` 或 `no-go`，列出人工发布步骤和剩余风险。

## 完整验证

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go mod verify
go build -o <isolated-cache>/openagentx-adr009-<git-sha> ./cmd/openagentx
bash -n scripts/*.sh deploy/scripts/*.sh deploy/systemd/*.sh
bash scripts/check-legacy-control-paths.sh --release
npm run test:observation
npm run test:pwa
npm run build
git diff --check <baseline>..HEAD
git status --short --branch
```

## 退出条件

- 所有 P0/P1 关闭，P2 有明确 owner/后续任务且不破坏当前验收；
- validation report 可复现且没有把静态/fixture 证据写成真实部署 E2E；
- 候选 provenance 完整，feature worktree clean，真实运行状态未改变；
- 创建一个 docs-only Task 08 validation 提交后停止等待最终监督 gate；
- push/merge/install/restart/migrate/现场发布均需下一次明确授权。

## 完成记录（2026-09-22）

- validation 提交：`8e62f8c390743b660db2d0a9f07dce37a75da2f4`，仅 report + execution log；
  [验证报告](../../reports/validation/2026-09-20-openagentx-adr009-release-candidate.md) 结论为 `passed-candidate`。
- 本批门禁预算为 20 分钟/一次有界复核。独立验证代理确认 E2E 断言与 observation/reducer/CAS/Diagnostic
  证据映射成立，未发现新增阻断；主代理完成候选、范围、状态和文档检查后裁定 `GO`，不冒称外部监督 GO。
- 产品与测试源码自 `5bebf14` 未变，因此复用先前无缓存普通/race、Web、systemd、release、安全检查及
  临时 HOME/DB/UDS、独立 `tmux -L` + PTY 完整用户闭环。补验对每一个 Shell 文件分别执行 `bash -n`。
- 候选仍为 `/home/sky/.cache/openagentx-builds/openagentx-adr009-5bebf14`，SHA-256
  `e482b113311ba618ac45512bf3098d431e739ab61bfc681ec3d2f90f483300c2`；源码 revision 精确为
  `5bebf148c414c2ca8bd8d2e6367f3e828b2ec2af`，`vcs.modified=false`，目录/文件 `0700`。
- 候选阶段没有 P0/P1 open issue。此前 E2E 使用 fake Runtime，不代表外部 AGY/真实业务效果已通过。
  用户日常 `OAX:quote-service.0` 未做现场测试；2026-09-22 只读确认 daemon、Worker、Console 仍为
  `f49cec4`。当前源码、候选与生产运行版本的差异已明确记录，未部署、push、merge 或重启。
