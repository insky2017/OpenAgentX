# OpenHarness 验证命令与边界

- 固定源码：`a53351bd1a2c33c2976b7df6869e3666578fdd7d`，`/tmp/oax-openharness-assessment-20260924`。
- 只读研究源码；仅在其 `cli/` 安装临时依赖，不改 tracked 文件。未启动服务、登录、调用模型或真实 CLI。
- 运行前读 `cli/package.json`、`vitest.config.ts`、`vitest.setup.ts`、三份 spec 及相关源码；process kill mocked，文件/脚本局限临时测试目录。原 setup 隔离 data/runtime/auth/DSH；净化运行环境不携带用户 token/proxy 凭据。

依赖安装（仅 clone 的 `cli/`，退出码 0，124 packages/4s）：

```sh
npm ci --ignore-scripts --no-audit --no-fund
```

第一次 PATH preflight 使用 `/usr/local/bin:/usr/bin:/bin`，本机 Node 实际在 NVM 下，退出 127，没有运行用例。原错误保留 `openharness-test-path-preflight.txt`。检查 `command -v node`、版本后按明确路径执行：

```sh
env -i PATH=/home/sky/.nvm/versions/node/v20.19.4/bin:/usr/bin:/bin LANG=C.UTF-8 \
  ./node_modules/.bin/vitest run src/orchestrator/service.spec.ts src/dsh/runtime.spec.ts \
  src/lib/stopAgentService.spec.ts --maxWorkers=1 --reporter=verbose
```

- Node 20.19.4、npm 10.8.2、Vitest 4.1.10；CI/release pin 为 Node 22.23.2。
- 2026-09-25 00:02:38 本地开始，8.48s，退出 1；119 项中 118 通过、1 失败。
- 失败原因：本机 umask 002，`service.spec.ts:286` 未指定 mode 的 mkdirSync 得到 0775，安全目录读取器正确拒绝；未执行到该 corrupt JSON fixture 的预期分支。
- 原输出保留 `openharness-targeted-tests.txt`，仅去除 ANSI 颜色编码便于审计，没有删改失败。

以局部 shell 权限默认值复跑相关原有 service 套件，没有改上游源码／测试：

```sh
umask 077
env -i PATH=/home/sky/.nvm/versions/node/v20.19.4/bin:/usr/bin:/bin LANG=C.UTF-8 \
  ./node_modules/.bin/vitest run src/orchestrator/service.spec.ts --maxWorkers=1 --reporter=verbose
```

- 2026-09-25 00:04:03 本地开始，3.69s，退出 0；46/46 通过。
- runtime 49、stop 24 的首轮通过证据复用。119 个不同用例分别有通过证据，但默认 umask 首轮未全绿，不能称全套 CI／真 Provider／桌面 E2E 通过。
- 没有为了变绿补 patch，没有复跑全仓库套件。原日志、测试源码 SHA-256、最终 clean status 见 manifest。
