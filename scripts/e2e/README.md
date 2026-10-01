# 真实 AGY 有界验证

`agy_live.py` 使用隔离 profile、localhost HTTP、独立 Unix socket 与工作目录，通过正式 init / agent apply / HTTP API 完成连续 query。它明确采用 **API 配置网络**，不证明向导首次使用，也不证明正式安装及 user-systemd 服务链。查询必须最终 `succeeded/query_result_delivered`、精确返回本次唯一内容，同一 Worker 连续接单。

先从固定提交构建 Go/Web；并行实施期间使用 `git archive` 快照，不能把会变化的工作树构建标成固定提交。示例参数中的路径需替换成实际候选工件：

```sh
python3 scripts/e2e/agy_live.py \
  --profile /home/sky/.local/state/openagentx/validation/2026-10-02-agy-workflow/live-CANDIDATE-ATTEMPT \
  --evidence docs/reports/validation/2026-10-02-agy-workflow/evidence/live-CANDIDATE-ATTEMPT \
  --binary /absolute/path/to/candidate/openagentx \
  --web /absolute/path/to/candidate/web/dist \
  --commit CANDIDATE --keep
```

- 每次使用新的 profile 和 evidence 目录；保留所有首次失败，不覆盖原记录。
- 默认正式 `agy-graft`、`gemini-3.7-flash-low`、HTTP 代理 `http://127.0.0.1:7897`；可用显式参数修改模型和代理。Runtime 的环境 allowlist 仍生效，不能把 runner 环境误写成进程实际环境。
- `--keep` 保留专属 daemon/Worker，供真实浏览器验证；未使用时自动按 PID + `/proc` starttime 清理。
- password、原始 HTTP、Runtime stdout/stderr、配置和数据库保留在私有 profile（目录 `0700`，文件 `0600`）。归档 evidence 是递归字段脱敏后的材料。真实业务 payload 仍需人工秘密扫描，不能只凭正则宣称任意内容安全。
- `capture_agy.py` 字节转发并委托正式 `/home/sky/.local/bin/agy-graft`，会增加监督进程；因此最终 installed-smoke 必须另验无 capture 的正式配置。capture 委托链、摘要、原始 argv 与真实退出码分别落盘。
- API 成功不代表业务副作用已验证；目前仅覆盖无工具 query。mutation / review / continue / cancel 需单独扩展并独立核验文件和进程。

清理只作用于该 profile 记录且 starttime 匹配的专属进程组，保留证据、密码和数据库：

```sh
python3 scripts/e2e/agy_live.py --cleanup /absolute/path/to/private/profile
```

所有 setup 工具错误单独标记，不计作 AGY 产品失败。`docs/reports/validation/2026-10-02-agy-workflow/evidence/live-7400806-1002f` 是历史初始基线的两个真实 query 成功证据，最终候选必须重跑。

## 最终候选扩展

`agy_workflow.py` 在同一隔离实例上执行 `role,mutation,queued,cancel,next`。只有冻结并构建候选提交后才能运行；`--commit` 必须指向该构建来源。

- Role：用户 prompt 不包含角色码，要求从角色指令返回代码。
- mutation/review/continue：独立检查 `result.txt` 精确字节及 SHA-256，重复验收请求返回同一记录，历史执行状态不变，关联新 Task 修改文件。
- queued：真实工具已进入等待时提交补充，观察 work mailbox pending→accepted，第二个 Run 产生独立文件。
- cancel：真实 Python 和 sleep 子进程存活后才请求取消；验证五秒内消失，超过原延迟窗口仍无产物。
- timeout：另建独立 profile/Worker，以 `--worker-timeout 35s --cases timeout,next` 执行，35 秒 Worker 配置时限针对真实 60 秒工具，验证停止及超过原工具延迟后仍无产物。所有 Task 请求均不传 per-task execution override。
- next：同 Worker 再完成 query。

每条用例只写本轮 workspace。测试脚本由 harness 准备，但必须由真实 AGY 工具执行；harness 只读取 marker/PID、核对文件，不伪造 Task、Run、Mailbox 或完成状态。`--cases` 可只运行受影响范围，不能把子集通过写成整个计划通过。
