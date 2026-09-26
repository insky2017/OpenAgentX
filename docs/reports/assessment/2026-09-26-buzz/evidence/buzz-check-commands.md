# Buzz 测试命令与复现条件

- 固定源码 `/tmp/oax-buzz-assessment-20260926`，commit `781d39510cf23cfe224e8f521ae06a23377e06de`；只读，未给原副本写node_modules或target。
- 独立runner `/tmp/oax-buzz-runner-20260926`；所有测试仍为原文件。源码hash、复制一致性、版本与官方环境差异见 `buzz-test-environment.json`。
- 一个主要批次（Rust/Node两子批次），一次仅对缺依赖文件的必要复验；没有通过后扩测。

## 预检与复制

```sh
rustc --version
cargo --version
rustup which cargo
rustup which rustc
```

Python tomllib读取根Cargo.lock并与既有registry源目录名比较：1027锁定registry包、37精确缓存。主workspace无target，故不冷构建ACP/Relay。

Python shutil复制原 `crates/ifc-core/src` 到runner，展开workspace package元数据为独立manifest，proptest固定upstream lock版本1.11.0，复制原完整Cargo.lock。保存最终manifest/裁剪锁：`buzz-test-ifc-Cargo.toml`、`buzz-test-ifc-Cargo.lock`。源lib字节完全相同，最终依赖包版本均在upstream锁中。

## Rust 子批次

```sh
env -i PATH=/home/sky/.rustup/toolchains/stable-x86_64-unknown-linux-gnu/bin:/usr/bin:/bin CARGO_HOME=/tmp/oax-buzz-runner-20260926/cargo-home CARGO_TARGET_DIR=/tmp/oax-buzz-runner-20260926/target RUSTC=/home/sky/.rustup/toolchains/stable-x86_64-unknown-linux-gnu/bin/rustc RUSTDOC=/home/sky/.rustup/toolchains/stable-x86_64-unknown-linux-gnu/bin/rustdoc timeout 300 /home/sky/.rustup/toolchains/stable-x86_64-unknown-linux-gnu/bin/cargo test --manifest-path /tmp/oax-buzz-runner-20260926/ifc-core/Cargo.toml -- --nocapture > /home/sky/work/touzi/OneAxe/steadyflow/OpenAgentX/docs/reports/assessment/2026-09-26-buzz/evidence/buzz-tests-ifc-core.log 2>&1
```

退出码0，依赖/编译24.83s；5unit/property pass（0.03s），2doctest pass（0.38s，含1个预期compile_fail）。实际Rust1.89.0，不是项目pin1.95.0。没有执行仓库Hermit激活/升级，只有临时crate的原逻辑验证。

## Node 依赖与源复制

```sh
npm install --prefix /tmp/oax-buzz-runner-20260926 --ignore-scripts --no-audit --no-fund --save-exact typescript@6.0.3 node-linux-x64@24.14.0
```

退出码0，原输出 `buzz-test-dependency-install.log`。Python shutil复制 `desktop/src`、原 `package.json`、`test-loader.mjs`、`test-loader-hooks.mjs` 到runner/desktop；2491个文件逐一字节比较相同。

## Node 主子批次

工作目录 `/tmp/oax-buzz-runner-20260926/desktop`：

```sh
env -i PATH=/usr/bin:/bin /tmp/oax-buzz-runner-20260926/node_modules/node-linux-x64/bin/node --import ./test-loader.mjs --experimental-strip-types --test --test-timeout=30000 src/features/agents/lib/cancelTurnOutcome.test.mjs src/features/agents/managedAgentReconciliationPlan.test.mjs src/features/agents/managedAgentRuntimeStatus.test.mjs src/features/messages/lib/sendToChannelSemantics.test.mjs > /home/sky/work/touzi/OneAxe/steadyflow/OpenAgentX/docs/reports/assessment/2026-09-26-buzz/evidence/buzz-tests-desktop.log 2>&1
```

退出码1：21 pass；第4文件因 `Cannot find package 'nostr-tools' imported from .../shared/lib/pubkey.ts` 无法收集。不是8项消息逻辑断言失败，也没有将它记作跳过/通过。

## 唯一必要补依赖与复验

```sh
npm install --prefix /tmp/oax-buzz-runner-20260926 --ignore-scripts --no-audit --no-fund --save-exact nostr-tools@2.23.12
```

退出码0，版本取自upstream pnpm-lock.yaml；日志 `buzz-test-dependency-recheck-install.log`，完整最终runner npm锁 `buzz-test-node-package-lock.json`。没有增加生产依赖替身。

```sh
env -i PATH=/usr/bin:/bin /tmp/oax-buzz-runner-20260926/node_modules/node-linux-x64/bin/node --import ./test-loader.mjs --experimental-strip-types --test --test-timeout=30000 src/features/messages/lib/sendToChannelSemantics.test.mjs > /home/sky/work/touzi/OneAxe/steadyflow/OpenAgentX/docs/reports/assessment/2026-09-26-buzz/evidence/buzz-tests-desktop-recheck.log 2>&1
```

退出码0，8 pass、0fail/skip，881.88681ms。已通过21项未重跑。

## 完成后只读核对

```sh
git -C /tmp/oax-buzz-assessment-20260926 status --short
git -C /tmp/oax-buzz-assessment-20260926 diff --exit-code
```

无输出且退出码0。`/proc/*/cmdline`按可执行argv[0]位于本轮runner路径筛选，未发现残留，见 `buzz-test-process-check.json`。没有启动服务/真实Provider、读取账号或用户Agent历史；没有提交Git。
