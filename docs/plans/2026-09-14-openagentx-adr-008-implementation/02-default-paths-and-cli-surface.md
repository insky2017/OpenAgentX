---
doc_type: implementation_task
status: completed
owner: openagentx
updated_at: 2026-09-14
---

# 任务 02：默认路径与 CLI 表面

## 目标

建立单一、可测试的本地 profile/path resolver，让日常 Console、Fleet 和本地 daemon/schema 命令不再要求重复传入 socket、database 或 manifest，同时冻结最终 CLI 语法但不在替代 TUI 就绪前提前破坏现有 Attach。

## 依赖

- 任务 01 契约已冻结并经监督者放行。

## 实施步骤

1. 新建职责单一的 local profile/path package；不得在各 CLI package 复制 `os.UserHomeDir`、环境变量或路径拼接。
2. 实现优先级：显式 flag > 对应环境配置 > `OPENAGENTX_HOME` > `~/.openagentx`。
3. 提供以下 canonical 默认值：

```text
~/.openagentx/run/openagentx.sock
~/.openagentx/data/openagentx.db
~/.openagentx/fleet.yaml
~/.openagentx/workers/<agent-id>.yaml
~/.openagentx/credentials.json
```

4. 对 home 不可解析、相对/空 override、非法 Agent ID 和路径冲突 fail closed；创建目录时使用最小权限，不跟随不安全 credential 软链接。
5. 将共享 resolver 接入所有相关命令的参数解析。显式 flag 行为保持兼容，帮助文本显示有效默认来源，不打印 Token。
6. 冻结最终 Console 命令表：`console`、`console login`、`console logout`、`console attach [--agent] [--diagnostic]`；不新增全局 `auth status`。
7. 本任务只完成 parser/resolver 和兼容接线。`--once`、独立 `console status` 只标记为任务 06 原子删除，不得在全屏替代路径可用前先删导致功能断裂。
8. 错误信息须区分：无 home、无交互终端、缺凭据、socket 不存在、显式路径不存在；不得静默改用其他 installation。

## 测试矩阵

- 显式参数覆盖全部环境和 home；
- 对应环境变量覆盖 `OPENAGENTX_HOME`；
- `OPENAGENTX_HOME` 覆盖用户 home；
- `~`、尾斜线、canonical socket identity 和不同 home 隔离；
- Console/Fleet/serve/init/schema 的默认与显式参数；
- 非法 Agent ID 不能逃逸 `workers/`；
- help/错误不泄漏 credential 内容。

## 验证

```bash
go test ./internal/cli/... ./internal/fleet/... ./cmd/openagentx/...
go test ./internal/<new-path-package>/...
go build -o /tmp/openagentx-adr008-task02 ./cmd/openagentx
/tmp/openagentx-adr008-task02 --help
git diff --check
```

实际 package 路径按任务 01 冻结结果替换，并原样写入 execution log。

## 退出条件

- 所有默认路径来自单一 resolver；
- 显式参数和环境优先级有正反测试；
- 命令语法已冻结但尚未造成 Attach 功能空窗；
- 未创建或修改真实 `~/.openagentx`；
- 阶段提交和 execution log 完成后暂停。

## 完成记录

- 主实现提交：`e690bbbd11046e63841a4a869a90f6aeb42c5575`；review-fix 提交：
  `c0e8d4aeae2516c005cedbce6c5b35d1b8b07553`。
- 核心产物：唯一 `internal/localprofile` resolver、Console/Fleet/admin/serve/schema 默认路径接线、
  fail-closed 路径与 Console parser 测试，以及可移植的短 UDS 测试 fixture。
- 验证：Task 02 定向 Go 测试、`internal/client/console -count=1`、受影响 package `go vet`、
  `go build`、help smoke 和 `git diff --check` 均通过；独立 Termux review 最终在默认 `TMPDIR`
  下通过。
- 产品范围保持不变：未实现 Task 03 cursor/reducer，也未实现 CLI Token、`OAX` workspace 或
  全屏 TUI 等 Task 04+ 行为。
