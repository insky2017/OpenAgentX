# E20 异常协议整链：D 级通过

候选产品 commit `14d7350b86125f299615d4a8db61d844ce866758`，二进制来自 `/tmp/oax-live-14d7350/openagentx`（SHA-256 见 manifest）。本批使用独立 profile、端口、daemon 与真实 Worker，通过正式 CLI/API 配置和调度，Runtime 为本地 Python AGY 协议进程 fixture；没有调用真实模型，没有修改安装服务，因此仅为 **D**，不是 R/I。

7 种异常：空 stdout、坏 JSON、缺终态、截断 JSON、超长最终答复、矛盾双终态、exit 0 但 stderr 有错误。全部 Task 为 `uncertain`、`final_reply=false`，未产生 `query_result_delivered`。每项异常之后都提交正常 query，7 项均由同一 Worker instance/generation 领取并成功回复 `OAX-E20-NORMAL`；总计 14 个 Task/Run。

超长答复和 exit 0 stderr 的 Runtime 原始状态可为 `succeeded`，但 Task 正确保持 `uncertain/query_result_unverified`；不能把 Runtime 状态当作 Task 成功。其余异常保留具体解析诊断。首次运行即通过，没有本批首败或复验；不覆盖真实 AGY、安装服务、浏览器、模型网络或副作用。

- `manifest.json`：实际命令、候选 binary/script/fixture 哈希、时间和原始路径。
- `cases/*/task-run-journal.json`：正式 API Task、Run、Event Journal。
- `cases/*/process/`：fixture 的逐字节 stdin/stdout/stderr 与 PID/argv/cwd/exit code、原始哈希；故意保留坏 JSON 与截断，不能规范化这些字节。
- `cases/*/correlation.json`：Task/Run、fixture PID、持久原始路径、Worker instance/generation 对应关系。
- `runtime/` 是共用收集器的文本处理副本；逐字节协议核验使用上面的 `cases/*/process/`。
- `http/`、`cli/`、`daemon.*`、`worker.*`：脱敏正式入口及实际进程记录。
- `independent-verification.json`：逐字节 SHA 核验、Journal 存在、同 Worker、证据无 owner 密码核验。
- `cleanup.json`：仅停止本批拥有的进程组，均已退出，未删除证据。

持久原始资料：`/home/sky/.oax-e/e20-14d7350-r1`（权限 0700，含私有 HTTP 请求及认证；不得入库）。本目录提交副本已脱敏，所有文件由 SHA256SUMS 覆盖。

复现命令（新 profile/evidence 路径必须不存在）：

```sh
python3 scripts/e2e/agy_malformed.py --profile /home/sky/.oax-e/e20-14d7350-r1 --evidence docs/reports/validation/2026-10-02-agy-workflow/evidence/e20-14d7350-r1 --binary /tmp/oax-live-14d7350/openagentx --web /tmp/oax-live-14d7350/web/dist --commit 14d7350b86125f299615d4a8db61d844ce866758
```
