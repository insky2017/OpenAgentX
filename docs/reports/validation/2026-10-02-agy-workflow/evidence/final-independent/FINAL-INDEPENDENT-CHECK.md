# 最终独立核对（只读快照）

核对时间：2026-10-01T19:11:52.470357+00:00。源码 HEAD 为 `17d5cd22b442fb5fd02bd661b36c5db4299fea9d`。本次不运行测试、不修改服务或业务数据库；以下结论只对应本次快照，后续仍在写入的验收材料须另行封存核对。

- **安装来源一致：PASS。** `openagentx.service` 为 active/running，MainPID=`306551`。直接读取 `/proc/306551/exe` 的 SHA-256 为 `666cbd876c540d63b1214f81e0e65b7922b9a41f987ba21364d03718c7e22ccd`，匹配[最终构建清单](final-deterministic/recovery-final-artifact-manifest.json)与[安装记录](installation/installed.json)。进程实际文件为 `/home/sky/.local/bin/openagentx`；Go metadata revision 精确匹配候选且 `vcs.modified=false`。
- **实际 schema 一致：PASS。** 从该进程 argv 定位 `/home/sky/.openagentx/data/openagentx.db`，使用 SQLite `mode=ro` 加 `PRAGMA query_only=ON` 读取 `schema_meta`，得到版本 **2**，应用时间 `2026-10-01T19:05:13.326Z`，与安装记录一致。没有调用会打开迁移路径的 `schema verify`；该 CLI 的输出目前仍硬编码 v1，不能把其文案作为实际 schema 证据。
- **实际 Web 一致：PASS。** `/home/sky/.openagentx/web` 中清单列出的全部 8 个文件逐一匹配 SHA-256；从该进程实际 `http://127.0.0.1:18100` 读取这 8 个资源，均为 HTTP 200 且响应内容 SHA-256 完全匹配。此项证明服务发出的工件一致，不替代浏览器交互或业务效果验收。
- **冻结 ADR 未变化：PASS。** `git diff --exit-code 7400806 HEAD -- docs/decisions docs/adr` 无差异；同时检查工作树、暂存区相对 HEAD 无变化。
- **已完成证据和待提交文本的凭据扫描：未发现命中。** 共扫描 **1422** 个文件，含额外 5 个待提交报告/runner；用 **21** 个私有测试密码/凭据文件中读得的值在内存做精确字节比对，并检查私钥、Provider key、Bearer、凭据值、Cookie、代理 userinfo 模式。精确匹配和模式候选均为 0；不输出秘密、秘密哈希或匹配上下文。扫描中未观察到文件同时变化，但不保证快照后写入的内容。

主动排除内容扫描的目录（共 297 个文件）：`browser-14d7350-attempt01`、`browser-7400806-1002f`、`browser-f3cd7a3`、`installed-17d5cd2-onboarding01`、`installed-17d5cd2-onboarding01-resume01`、`live-17d5cd2-lease01`、`live-17d5cd2-lease02`。这些浏览器、安装交互、租约验收目录可能仍在写入，本报告不宣称其最终内容已通过扫描。二进制/图片的字节模式扫描不能证明视觉脱敏，本次纳入扫描的文件均可解码文本。

待提交清单中，没有发现业务 DB、私钥、`.env`、原始 credentials/password 文件或超过 10 MB 的可疑大文件。首次检查曾发现旧 recovery evidence 中 3 个 `.pyc`，复查时已不存在；最新清单又发现**活跃安装恢复目录**中的以下临时字节码，应等该验收封存后排除，不能据本报告直接全量 `git add`：

- `docs/reports/validation/2026-10-02-agy-workflow/evidence/installed-17d5cd2-onboarding01-resume01/harness-source/__pycache__/agy_installed.cpython-312.pyc`
- `docs/reports/validation/2026-10-02-agy-workflow/evidence/installed-17d5cd2-onboarding01-resume01/harness-source/__pycache__/agy_live.cpython-312.pyc`
- `docs/reports/validation/2026-10-02-agy-workflow/evidence/installed-17d5cd2-onboarding01-resume01/harness-source/__pycache__/agy_recovery.cpython-312.pyc`
- `docs/reports/validation/2026-10-02-agy-workflow/evidence/installed-17d5cd2-onboarding01-resume01/harness-source/__pycache__/agy_workflow.cpython-312.pyc`

建议：

1. 可提交已完成且封存的证据、对应源码 runner 与本报告；保留首次失败和复验，依据各目录 README/manifest 判断边界，不将 PASS 覆盖到未完成项。
2. 活跃目录完成后由其负责人排除 `__pycache__/*.pyc`、补最终 SHA-256、执行针对该目录的脱敏复查，再提交。不要删除仍在执行的测试输入或修改其运行环境。
3. 后续如仅提交报告/证据，安装二进制仍应记为 `17d5cd2`，不得改写为新 docs-only HEAD；来源核验以当前 manifest 和 `/proc` 证据为准。

详细可复核记录：[result.json](final-independent/result.json)、[扫描文件快照哈希](final-independent/scanned-SHA256SUMS)、[当时工作树清单](final-independent/git-status.txt)、[实际进程 Go metadata](final-independent/daemon-go-version-m.txt)、[检查脚本](final-independent/check.py)。`scanned-SHA256SUMS` 默认相对本 evidence 目录；`@worktree/` 前缀表示仓库根目录。原始资料持久保存于 `/home/sky/.local/state/openagentx/validation/2026-10-02-agy-workflow/final-independent/`。
