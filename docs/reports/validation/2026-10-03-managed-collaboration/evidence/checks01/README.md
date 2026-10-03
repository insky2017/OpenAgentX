# 源码验证证据

全量 `go test ./...`、`go vet ./...` 和核心五包 `go test -race` 通过；不代表真实模型 E2E。

首次全量测试在 Console 三 pane 布局中未能读到完整 `status succeeded`，虽然 Task/Runtime 结果已成功。该次工具输出未导出文件，不能声称保留了完整首次原始日志。随后持久复验保留如下：

- `console-retest.log`：证据路径过长超过 Unix socket 限制，未进入 Console 验收；调整到短的持久路径。
- `console-retest02.log`：保留同一个状态截断失败。只调整 focused summary 未覆盖事件行，仍失败。
- `console-retest03.log`：将事件状态放在 ID/version 前，使窄窗仍能显示结果；原断言不变，通过。
- `go-test-all-final.log`：完整回归通过。其后固定 backend 的最小补丁另有定向回归，核心 race 包含最终补丁并通过。

Console 原始 PTY 与夹具位于 `/home/sky/.local/state/oax-mc03/`（失败）和 `/home/sky/.local/state/oax-mc04/`（成功）；不导出其中身份与私密配置。本目录只导出测试日志，不含凭据。
