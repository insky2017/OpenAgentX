# Fenix 测试命令与复现边界

- 对象固定为 `da5eb543bf17aa0c772eebbf0961e4dc2daac0e8`，第三方源码只读；仅增加被忽略的 `node_modules` 符号链接，目标是本轮独立 runner。
- 下列命令是实际执行命令；工作目录 `/tmp/oax-fenix-runner-20260926`，输出重定向到对应 evidence 日志。没有执行完整安装器或主服务。
- 复现依赖的是本轮保存的 runner npm lock，不是 upstream 全套 bun.lock；环境与包版本见 `fenix-test-environment.json`。

## 依赖准备

```sh
mkdir -p /tmp/oax-fenix-runner-20260926/home
npm install --prefix /tmp/oax-fenix-runner-20260926 --ignore-scripts --no-audit --no-fund --save-exact @oven/bun-linux-x64@1.3.14 yjs@13.6.31 nanoid@5.1.11 yaml@2.9.0 pino@10.3.1 pino-pretty@13.1.3 ioredis@5.10.1
```

输出：`fenix-test-dependency-install.log`。首次安装完成 45 个包，未运行 lifecycle。runner `node_modules` 下每个本地 workspace 包按其 `package.json.name` 符号链接到固定源码的相应 `packages/*` 目录；固定源码根 `node_modules` 符号链接到 runner 的目录。无 workspace manifest 改动。

隔离配置最终内容（另存 `fenix-test-isolated.toml`）：

```toml
# Assessment runner: no upstream host preloads.
```

首次配置 `[test]` / `preload = []` 被 Bun 拒绝，命令与下方主批次相同，日志 `fenix-tests-primary.log`，退出码 1；改成上述配置后仅复验一次。

## 主子批次

```sh
env -i PATH=/usr/bin:/bin NODE_ENV=test LOG_DIR=/tmp/oax-fenix-runner-20260926/logs /tmp/oax-fenix-runner-20260926/node_modules/@oven/bun-linux-x64/bin/bun test --config=/tmp/oax-fenix-runner-20260926/isolated.toml \
  /tmp/oax-fenix-assessment-20260926/packages/acp-link/src/__tests__/acp-dispatcher-cancel.test.ts \
  /tmp/oax-fenix-assessment-20260926/packages/chat-channel/src/channel/command-id-dedup.test.ts \
  /tmp/oax-fenix-assessment-20260926/packages/chat-channel/src/channel/permission-cas.test.ts \
  /tmp/oax-fenix-assessment-20260926/packages/chat-channel/src/channel/session-channel-action.test.ts \
  /tmp/oax-fenix-assessment-20260926/packages/chat-channel/src/__tests__/snapshot-cas-isolation.test.ts \
  /tmp/oax-fenix-assessment-20260926/packages/workflow-engine/src/__tests__/recovery/snapshot-recovery.test.ts \
  /tmp/oax-fenix-assessment-20260926/packages/workflow-engine/src/__tests__/executor/agent-executor.test.ts \
  /tmp/oax-fenix-assessment-20260926/src/__tests__/task-timeout-instanceof-error.test.ts > /home/sky/work/touzi/OneAxe/steadyflow/OpenAgentX/docs/reports/assessment/2026-09-26/evidence/fenix-tests-primary-recheck.log 2>&1
```

退出码 0，102 pass、0 fail、291 expect，8 个上游测试文件，10.16 s。没有测试名过滤，没有跳过参数；这些原文件中的 fixture 与单测语义保留，但全局 preload 被隔离配置关闭。

## Scheduler 子批次

```sh
env -i PATH=/usr/bin:/bin NODE_ENV=test LOG_DIR=/tmp/oax-fenix-runner-20260926/logs /tmp/oax-fenix-runner-20260926/node_modules/@oven/bun-linux-x64/bin/bun test --config=/tmp/oax-fenix-runner-20260926/isolated.toml \
  --preload /home/sky/work/touzi/OneAxe/steadyflow/OpenAgentX/docs/reports/assessment/2026-09-26/evidence/fenix-test-scheduler-preload.ts \
  /tmp/oax-fenix-assessment-20260926/src/__tests__/agent-executor.test.ts \
  /home/sky/work/touzi/OneAxe/steadyflow/OpenAgentX/docs/reports/assessment/2026-09-26/evidence/fenix-test-scheduler-probe.test.ts > /home/sky/work/touzi/OneAxe/steadyflow/OpenAgentX/docs/reports/assessment/2026-09-26/evidence/fenix-tests-scheduler-probe.log 2>&1
```

退出码 0，8 pass、0 fail、14 expect，85 ms；其中 2 项上游测试、6 项本轮行为探针。`fenix-test-scheduler-preload.ts` 是本轮编写的依赖替身，不是上游 fixture。探针期待观察到的原行为，因此 pass 可以意味着确认问题，不能解释为正确业务验收。

## 完成后检查

```sh
git -C /tmp/oax-fenix-assessment-20260926 status --short
git -C /tmp/oax-fenix-assessment-20260926 diff --exit-code
```

两者无输出且退出码 0。运行期间没有调用网络 Provider、数据库、浏览器或第三方服务；恢复测试唯一有意创建的 OS 子进程是其自身 `sleep 60` fixture，测试源码含 finally kill 清理。另见 `fenix-test-process-check.json` 的结束后 `/proc` 范围检查。没有为本轮创建监听主服务或 Docker 容器；没有更改 OAX 服务。

测试批次已经收口，不继续安装或扩测。所有失败、隔离方式与未覆盖项保留在 `fenix-tests-summary.md`。
