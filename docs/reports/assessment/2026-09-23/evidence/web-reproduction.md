# Web 动态证据复现前置

- 脚本后缀 `.txt` 防止误执行，均为本次具体输入的记录；不是已发布的通用测试框架。执行前复制到临时目录并检查硬编码路径、Task ID 和预期状态。
- 使用独立 fixture root。本批 root 为 `/tmp/oax-web-assessment`，该目录仍保留审计材料；不要在现有 root 重跑初始化脚本。新一轮先改脚本 root 与证据输出路径，创建权限 0700 的新目录。
- `web-fixture-setup.py.txt` 展示正式 CLI `init`/`agent apply` 和标准库 PTY 密码交互；密码由随机数生成，仅落 root/password（0600），无值写进脚本。必须使用本轮相同或另行记录的 binary revision。
- 独立 Web source copy 基于 `34053c0`，只读借用 node_modules，`npm run build` 后 daemon 的 `--web-dir` 指向该 copy/dist。daemon 使用独立 DB、socket、loopback 端口。两 fake Worker 配置由 setup 生成；只有 A 经网络 UI 应用 inherit，B 故意保留未绑定用于 queued/cancel 场景。
- 本批调用 Playwright Core 的 `createRequire` 起点为 agent-browser 安装路径，Chrome 显式 `/usr/bin/google-chrome`；复现机路径不同时需改对应路径。无需读取用户 Chrome profile。
- `state.json` 是通过真实 UI 登录后生成的受控 browser storageState，须 0600；脚本只引用它，仓库没有归档此文件。它失效时通过隔离密码正常 UI 登录更新，禁止从生产会话复制。
- fake 主链先发送首次 Task，再从网络页 test→publish→applied，再执行 `web-probe-journey.mjs.txt` 的回复/第二项。faults 脚本内第一项 Task ID、mobile 后取消 Task ID 应替换为该次实际创建 ID；旧 ID 是本次证据的索引，不是通用 fixture 常量。
- `web-probe-mobile.mjs.txt` 包含真实触控视口、安全内容、offline/online、不重放、queued 取消；最终脚本 exit 0 表示观察完成，不代表取消成功，必须检查记录的 HTTP/DOM 断言。
- `web-probe-faults.mjs.txt` 仅在浏览器拦截一次 overview/login 503，其他 API 放行；执行末尾 logout 会使该 storageState 会话失效，联合测试前应重新登录生成 state。
- `web-probe-real-e2e.mjs.txt` 需要 Runtime 代理先正式注册 `web-real-agy`，在独立 workspace 启动正式 `agy-graft` Worker；配置/进程与环境版本证据见 `runtime-web-setup.json`。脚本完成真实 UI 网络 test/publish、两 Task，只有第一项 Runtime succeeded 才发送下一项。Task uncertain 不升级为 success。
- 联合文件原文、SHA、DB/Journal 独立核验见 `runtime-web-final.json`。全部脚本只访问本批临时入口；不要把 root、port 或 socket 换成生产值执行。
- 本批临时 daemon、fake Worker、AGY Worker 与 Chrome 均已关闭。复现结束也应按 Worker→daemon 顺序终止本轮自有进程并核实退出；保存的密码/state 不进入证据或 Git。
