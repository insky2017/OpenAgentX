# 最终确定性检查与候选构建记录（D）

最新最终构建候选为 `9434479e4fd3b6802aa2e4d57610bb710200cd01`（见 `LATEST-CANDIDATE.json`）；此前真实已通过证据保留并按影响复用，旧安装二进制由新候选替代。本目录包含初次全量检查、限定影响复验、被替代的旧构建和最终构建；它不是正式 AGY/安装验收 PASS。

## 初次全量检查（14d7350）

- `01-go-race.*`：`go test -race ./... -count=1` **FAIL**。唯一失败是 `internal/api/contracts_test.go:136` 的旧断言仍期望接受已明确不支持的单次 execution override；其他包 PASS（含 Console PTY），未出现 Go race 报告。完整首次失败保留，不能改写为全量 PASS。
- `02-go-vet.*`、`03-go-mod-verify.*`、`04-legacy-release.*`：PASS。
- `06-frozen-decisions.*`：冻结的 `docs/decisions`、`docs/adr` 对基线 `7400806` 无变化。
- `05-npm-ci.*` 与 `07-web-network.*`、`08-web-observation.*`、`09-web-pwa.*`：安装与 Web 定向检查 PASS。
- 初始源码提交、tree、环境、源码哈希在 `candidate.json`、`source-SHA256SUMS`；实际命令、时间、工作树前后状态、输出哈希在各项 `.result.json`。

## 旧断言修正后的影响复验（8655051）

- 主代理仅修改 `internal/api/contracts_test.go`：合法 execution 的形状校验通过；实际 CreateTask 带不支持 override 必须拒绝；置空 override 后应允许。
- 独立核对的精确差异保留在 `10-test-only-correction.diff`，无 Go 产品/Web 变化。
- `10-api-race-retest.*`：`go test -race ./internal/api -count=1 -v` **PASS**；候选与源码哈希在 `candidate-8655051.json`、`source-8655051-SHA256SUMS`。初次全量中其余包 PASS 经仅测试差异核对可复用，不称重新跑完整全量。
- `11-go-build.*` 和 `12-web-build.*`：该候选 Go/Web 构建均 PASS，但此后正式无 capture AGY 取消复现缺陷，另有 Web 修正，故 **8655051 已被替代，未作为最终安装候选**；见 `superseded-8655051.json`。

## 上一候选 f3cd7a3 与检查复用（记录保留）

- 活动取消修复与受影响 AGY、Worker、SQLite race/vet 复验在 `../active-cancel-deterministic/`，首次 D FAIL 与修复后 PASS 分别保留；真实直连失败另见 `../live-14d7350-directcancel01/`。这里不把它们冒充安装验收。
- `candidate-f3cd7a3.json` 与 `final-candidate-changes.diff` 记录独立影响审查：8655051 后 Go 仅 adapter 和 fleet 改动；AGY 源码哈希与此前通过版本一致，fleet 在精确最终 HEAD 上重跑 `13-fleet-race-final.*`、`14-fleet-vet-final.*` 均 PASS。未再重复完整所有包。
- 两处 Web 修正（登录连接故障文案、取消阶段文案）的源文件与对应已验证提交完全一致；原 observation 14 项及相关构建日志复制为 `reused-web-*.log`。最终 `16-web-build-final.*` 重新构建 PASS；network/PWA 相关未变源码复用先前 PASS。
- `15-go-build-final.*`、`16-web-build-final.*` 在独立 clean clone 构建 Go/Web PASS；`final-artifact-go-version-m.txt` 确认精确最终 revision、`vcs.modified=false`。`final-artifact-manifest.json` 记录绝对产物路径与各文件哈希，`final-artifact-SHA256SUMS` 相对于其 artifact 根目录核验。
- 最终二进制 SHA-256：`a17a9ef559ede808a5baf994170419a8f57470c98c7ecb6a0478aa8dd438cbac`。产物根：`/home/sky/.local/state/openagentx/validation/2026-10-02-agy-workflow/artifacts/f3cd7a32fe54bf1c02bad41d6c6ef8bfc97ddec9/`。已交真实验收流程；此构建记录的 `installed=false` 表示构建时未安装，后续安装证明须另看 I 记录。
- `source-f3cd7a3-SHA256SUMS` 保存最终检出中所有跟踪文件哈希；冻结 ADR 继续未变。源码不受后续纯报告提交影响，安装必须核对本 manifest，不能按后来 docs-only HEAD 推断二进制版本。
- 有界源码独立审查在 `independent-review.json`：结果验收、继续、新消息、readiness 边界未发现新增阻断；非阻断观察由主代理裁决。源码审查不能替代真实浏览器、Runtime 或独立产物核验。

## 上一候选 17d5cd2（记录保留）

- `candidate-17d5cd2.json` 与 `recovery-final-candidate-changes.diff` 记录独立影响核对：仅 daemon 周期恢复、相关新增测试/两处注释，以及 Console 审批范围帮助文案。AGY、Worker、Web、Go依赖未变化。
- `../periodic-recovery-deterministic/` 的定向 race、daemon 整包 race、vet 全 PASS，六个源码哈希与最终候选完全相同；`../e19-help-deterministic/01-help.result.json` 的小范围帮助回归 PASS，HEAD/源码/日志哈希均核验。复用既有检查，不再重复通过包。
- `17-go-build-recovery-final.*`、`18-web-build-recovery-final.*` 在独立 clean clone 构建 PASS。`recovery-final-artifact-go-version-m.txt` 确认精确 revision 和 `vcs.modified=false`；最终 Web 文件与 f3cd7a3 逐字节哈希一致。
- 本次安装以 **`recovery-final-artifact-manifest.json`** 和 **`recovery-final-artifact-SHA256SUMS`** 为准，不使用上节旧清单。二进制 SHA-256：`666cbd876c540d63b1214f81e0e65b7922b9a41f987ba21364d03718c7e22ccd`。
- 产物根：`/home/sky/.local/state/openagentx/validation/2026-10-02-agy-workflow/artifacts/17d5cd22b442fb5fd02bd661b36c5db4299fea9d/`。已交 R/I 主流程；本构建记录仍不代表已安装，也不替代新的常驻 daemon 崩溃恢复验证。

## 最新最终候选 9434479（SSE 首连修复）

- 安装以 **`sse-final-artifact-manifest.json`** 和 **`sse-final-artifact-SHA256SUMS`** 为准。`candidate-9434479.json`、`source-9434479-SHA256SUMS`、`sse-final-candidate-changes.diff` 记录独立影响核对与源码身份；旧文件名保留历史，不能据旧清单安装。
- 改动仅 Overview pre-read `live_after_sequence` 与 Web 初次/重连接线及其测试，五个源码哈希与 `../sse-bootstrap-deterministic/` 精确一致。定向与完整panel race、Web15项、vet、最终Webbuild已通过并复用，不再重复测试。认证、retention和projection错误仍fail-closed；当前修复不是全局跳过错误。
- `19-go-build-sse-final.*`、`20-web-build-sse-final.*` 在独立 clean clone 构建 PASS，精确 revision、`vcs.modified=false`。Go SHA-256=`6725f3370b3c6bf27293a0f6368cc5bfac9e92c92682cc01a8b1b862231f75c7`；Web app.js SHA-256=`45df13da90a28fc5c83ffb5dcefa2ab750343b78cbf8b6733241fbf0039ef19e`。
- 产物根：`/home/sky/.local/state/openagentx/validation/2026-10-02-agy-workflow/artifacts/9434479e4fd3b6802aa2e4d57610bb710200cd01/`。已交主流程安装；`installed=false` 是构建时状态，安装、实际/proc以及正式历史库浏览器续接验证须看新 I 记录，不能用构建成功替代。
- `internal/runtime`、Worker、周期恢复、持久层及依赖均未变化；已通过 AGY取消/超时/恢复证据由主流程按影响复用。冻结ADR继续无变化。

原始证据持久目录：`/home/sky/.local/state/openagentx/validation/2026-10-02-agy-workflow/final-deterministic/`。本目录为经凭据模式检查的公开副本，SHA-256 清单在 `SHA256SUMS`，首次失败不覆盖。
