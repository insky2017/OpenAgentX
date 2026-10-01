# Agent 最小本机入口：D 层确定性证据

- 已实现 `agent add/open/status/pause/resume`（`start` 是恢复别名），向导只询问名称、workspace、职责；密码可使用TTY或0600密码文件。角色通过正式 identity profile 持久化，Worker 复用现有身份。
- `add` 生成或导入 identity、Worker config、Fleet 条目；已有匹配配置复用，冲突不覆盖。增量更新持有文件锁并原子替换；单个Agent启动不会被其它Agent的坏Worker配置阻断。
- 单Agent用户服务核对实际ExecStart、WorkingDirectory、EnvironmentFile；恢复不派发Task、不重跑uncertain。暂停使用正式持久化stop意图，等待draining→offline。网络走正式CLI身份的test→publish→当前Worker代际applied，并最终核对权威readiness。
- 新建 `OAX:overview` 执行只读 `agent status --watch`，显示readiness、当前工作、最近结果和打开/恢复命令；现有用户正在使用的overview窗口不强杀替换。
- 本目录是 **D 层**：单测、fixture，以及真实CLI/PTy/隔离daemon的入口复验。systemctl为mock；**不证明真实user-systemd、真实AGY或已安装候选通过**。

## 实际执行与首次失败

```text
go build -o /tmp/openagentx-onboarding-candidate ./cmd/openagentx
go test -v ./internal/cli/fleet ./internal/fleet ./cmd/openagentx
python3 docs/reports/validation/2026-10-02-agy-workflow/evidence/onboarding-deterministic/pty_fixture.py /tmp/openagentx-onboarding-candidate
```

- `first-test.log`：首次新增单测及既有Fleet/tmux/CLI包通过。
- `retest.log`：完善代际匹配、环境继承等后的定向测试通过。
- `first-pty-failure.log/json`：真实PTY已经完成两个Agent添加和幂等；第一次open遭遇正式Observe overview HTTP403，保留原始失败。主代理随后为overview/network overview补console.read，为两项mode写补owner+fleet.lifecycle，其它网络写保持拒绝。
- `pty-retest.log/json`：修复后的真实PTY复验通过：init→三问add→第二Agent→重复add→open --no-open→status。包含真实CLI Token登录，但证据中不输出Token、密码或Cookie。
- `daemon-retest.log`：按实际重定向采集的隔离daemon stdout/stderr；该流程无日志输出，因此文件为空，不以空日志声称Runtime证据。
- `model-native-proxy-test.log`：最终模型/环境修复后Fleet与network包通过；默认和显式`--model`生成的YAML均由真实`RunWorkerProcess`解析至公开Worker注册请求（服务端在注册后以fixture 503结束）。NATIVE_PROXY仅inherit保留，named_profile明确剥离，防止绕过已发布网络。
- `agent-final-targeted.log`：Agent入口最终定向测试通过。
- `final-test.log`：并行Console投影修改期间的构建失败（DeadlineAt字段类型暂不一致），保留；最终复验见后续`final-retest.log`。

原始证据保留：

```text
/home/sky/.local/state/openagentx/evidence/onboarding-d-20261002-015342
/home/sky/.local/state/openagentx/evidence/onboarding-d-20261002-015858
```

只将已脱敏的PTY日志、无敏感内容的结论和daemon日志复制入库；原始测试密码、CLI凭据、数据库均留在0700原始目录，未入库。SHA-256清单记录本目录文件实际内容。

## 安装候选后 I 层最短路径（由主代理安装后执行）

前提：`~/.local/bin/openagentx`及正在运行的daemon均为同一已测候选；现有owner密码文件必须600。下例中workspace使用独立真实目录，role是已存在Markdown。不要将示例占位路径原样执行。

```sh
openagentx agent add --id agy-onboarding-e2e --name 'AGY 入口验收' \
  --workspace /absolute/isolated/workspace --role /absolute/isolated/workspace/ROLE.md \
  --password-file /absolute/private/owner-password --no-open --wait 5m
openagentx agent open agy-onboarding-e2e --no-open
openagentx agent open agy-onboarding-e2e --web-url http://127.0.0.1:18100
openagentx agent pause agy-onboarding-e2e
openagentx agent resume agy-onboarding-e2e --no-open --wait 5m
journalctl --user -u openagentx-worker@agy-onboarding-e2e.service --no-pager
```

应逐次保存Task/Run/Journal/API和unit journal；暂停需用真实正在执行的Run证明drain，恢复需核对新generation及applied，并确认旧uncertain未自动重做。本D目录不预先标记这些I项通过。

## 小导航与正式服务地址修复复验

默认status按每Agent输出名称、就绪原因、当前工作、最多120字符的最近结果摘要、打开和恢复命令；`--json`保留完整诊断。`--watch`只有stdout是真TTY时清屏并重绘，重定向输出不包含刷新控制序列。

正式user service若使用`--http-addr ${OPENAGENTX_HTTP_ADDR}`，入口查询其MainPID并只从`/proc/<PID>/environ`提取该地址键，不source文件、不打印其它环境。实际子进程测试证明该场景；首次测试在子进程环境尚未就绪时读取失败，记录为`navigation-service-env-test.log`，夹具增加stdout ready握手后的复验为`navigation-service-env-retest.log`。

`navigation-pty.log/json`记录新候选三问向导、增量与幂等添加、简短status和连续两次watch同屏刷新，通过后SIGINT正常退出。原始目录：`/home/sky/.local/state/openagentx/evidence/onboarding-d-20261002-021930`。仍为D层，systemctl mock未变；真实服务安装验收由主代理执行。
