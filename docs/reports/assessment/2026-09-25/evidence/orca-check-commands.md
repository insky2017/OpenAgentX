# Orca 隔离验证说明

- 固定源码：`122b8c25d7c16f76e395bf9a65887d7c4bc5003b`，clone `/tmp/oax-orca-assessment-20260925`；源码与原有测试未修改。
- 已审查 `package.json`、`config/vitest.config.ts`、`config/scripts/vitest-host-ports-setup.ts`；未执行 `postinstall`、`prepare`、native runtime ensure、`build:cli`、installer、应用启动及账号/Provider调用。
- 运行器位于 `/tmp/oax-orca-test-runner-20260925`；依赖安装始终 `--ignore-scripts`。系统Node20只用于npm安装；实际Vitest进程使用既有Node24.13.0。
- 未加载上游全局setup（fake app environment/secret store及happy-dom适配），只选自含Node逻辑或自行创建临时SQLite的套件。未触碰用户Agent历史。
- Vitest4.1.11、Zod4.5.4和tsconfig2.0.0与上游lock相符；npm运行器Vite8.3.1与上游rolldown-vite7.3.1不同。不是官方完整workspace锁定依赖复现。

## 执行命令

```sh
git clone --depth 1 https://github.com/stablyai/orca /tmp/oax-orca-assessment-20260925
git -C /tmp/oax-orca-assessment-20260925 rev-parse HEAD
npm install --prefix /tmp/oax-orca-test-runner-20260925 --ignore-scripts --no-audit --no-fund vitest@4.1.11 zod@4.5.4
```

临时clone中的被git忽略的`node_modules`链接到该runner，仅用于模块解析，收尾已移除。测试配置保留在同目录的`orca-local-test-config.mjs`和`orca-recovery-test-config.mjs`。运行时在clone cwd执行：

```sh
/home/sky/.nvm/versions/node/v24.13.0/bin/node /tmp/oax-orca-test-runner-20260925/node_modules/vitest/vitest.mjs run --config /tmp/oax-orca-test-runner-20260925/vitest.config.mjs
```

首次9套collect失败、0断言，缺`@electron-toolkit/tsconfig/tsconfig.node.json`，原始输出`orca-local-tests.txt`。只补小型TypeScript配置包：

```sh
npm install --prefix /tmp/oax-orca-test-runner-20260925 --ignore-scripts --no-audit --no-fund @electron-toolkit/tsconfig@2.0.0
```

相同测试命令重跑，9文件85项通过，3.44s，输出`orca-local-tests-rerun.txt`。没有修改源码/断言/测试文件。

为核实重启时未知提交不重发，补充两个Journal套件和preamble套件：

```sh
/home/sky/.nvm/versions/node/v24.13.0/bin/node /tmp/oax-orca-test-runner-20260925/node_modules/vitest/vitest.mjs run --config /tmp/oax-orca-test-runner-20260925/vitest-recovery.config.mjs
```

2文件27项通过，preamble因缺`remark-parse`未收集，整批退出非零；输出`orca-recovery-tests.txt`。停止扩充依赖，没有删测试或换断言使其变绿。preamble原测试仅拟运行Markdown解析、fixture断言与`bash -n`语法检查，实际未执行；角色输入接线采用源码证据。

## 证据边界

- 11个不同文件的112项通过是纯逻辑/SQLite与fixture历史证据，未启动真实Provider/UI；SQLite实验特性warning保留。
- 上游shared setup、native/PTY runtime、所有源码类型检查及完整suite未运行；不能称全套通过。
- `orca-provenance.json`记录源文件/测试SHA-256、runner依赖与package-lock SHA、clean状态；`orca-source-excerpts.txt`带固定行号。
- 本批未启动任何Orca服务、desktop、SSH或模型进程；测试进程全部结束。clone与runner保留便于复核，不改变正式OAX环境。
