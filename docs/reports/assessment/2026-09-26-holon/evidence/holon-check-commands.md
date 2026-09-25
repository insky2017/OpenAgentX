# Holon 有界测试命令

- 固定源码：`/tmp/oax-holon-assessment-20260926`，commit `7c0ebbdfbf2f38adf41968051ed3440bc5ddb774`。
- 运行副本：`/tmp/oax-holon-runner-20260926/sdk`；`shutil.copytree` 原样复制 `packages/conversation-sdk`，原 tracked 文件逐一哈希与字节比较一致。源码树不生成 node_modules/dist。
- 无自编测试/依赖替身；原SDK测试自带fake client/fetch/SSE fixture。没有执行默认make ci或Rust/http skill测试。

## 预检（只读）

```sh
rustc --version
cargo --version
git -C /tmp/oax-holon-assessment-20260926 rev-parse HEAD
git -C /tmp/oax-holon-assessment-20260926 status --short
```

并以Python tomllib读取 `Cargo.lock`，统计registry来源package，与 `/home/sky/.cargo/registry/src/*/{name}-{version}` 目录名做精确对照：470/33/437（总量/已有/缺失）。仅目录名扫描，未读取用户凭据或服务历史，无cargo构建与缓存写入。

## 独立依赖安装

```sh
mkdir -p /tmp/oax-holon-runner-20260926
npm install --prefix /tmp/oax-holon-runner-20260926 --ignore-scripts --no-audit --no-fund --save-exact typescript@5.9.3 node-linux-x64@24.14.0
```

退出码0，两个包，6 s；原输出 `holon-test-dependency-install.log`，完整独立npm锁 `holon-test-dependency-lock.json`。SDK原依赖只有TS，Node24临时包用于匹配其>=24要求。

## 原tsconfig构建

```sh
env -i PATH=/usr/bin:/bin /tmp/oax-holon-runner-20260926/node_modules/node-linux-x64/bin/node /tmp/oax-holon-runner-20260926/node_modules/typescript/lib/tsc.js -p /tmp/oax-holon-runner-20260926/sdk/tsconfig.json > /home/sky/work/touzi/OneAxe/steadyflow/OpenAgentX/docs/reports/assessment/2026-09-26-holon/evidence/holon-tests-sdk-build.log 2>&1
```

退出码0，无诊断。没有修改tsconfig；在复制树生成dist供原测试import。

## 原SDK测试

工作目录 `/tmp/oax-holon-runner-20260926/sdk`：

```sh
env -i PATH=/usr/bin:/bin /tmp/oax-holon-runner-20260926/node_modules/node-linux-x64/bin/node --test --test-timeout=60000 /tmp/oax-holon-runner-20260926/sdk/test/*.test.mjs > /home/sky/work/touzi/OneAxe/steadyflow/OpenAgentX/docs/reports/assessment/2026-09-26-holon/evidence/holon-tests-sdk.log 2>&1
```

退出码0，53 pass /0 fail/0 skip/0 cancelled，807.845964 ms。原package test为先tsc再node --test，本轮只拆开记录并添加每测试60秒上限；无测试筛选、retry、静默跳过。没有运行专用Rust HTTP/SSE E2E阶段。

## 完成后核对

```sh
git -C /tmp/oax-holon-assessment-20260926 status --short
git -C /tmp/oax-holon-assessment-20260926 diff --exit-code
```

无输出、退出码0。环境JSON记录原源码hash、复制一致性、Node二进制hash、版本与预检。`/proc/*/cmdline`只按本轮Node绝对可执行路径筛选，结束后无匹配残留（`holon-test-process-check.json`）。不移除证据或运行副本，以便后续独立复核。

主批次首次通过，无必要复验，不再扩测。
