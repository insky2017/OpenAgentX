# Desktop 原宿主外部输入队列：只读有界核实

## 要点

1. **当前未找到满足约束的可用正式连接路线。** 本轮未投递、未连接 private pipe、未调用 turn/start/queue/add、未启动宿主或 Worker、未修改业务线程、数据库、配置或源码。所有结论是静态实现与本机进程证据，不能写成原宿主实际消费 PASS。
2. **运行中的原宿主是内嵌0.153.1，而不是共享0.160 daemon。** `/proc/1632229/exe --version`与磁盘资源二进制均返回`codex-cli 0.153.1`；进程命令无`--listen`，help默认stdio。版本、PID/PPid/starttime/SHA及FD记录在`running-version.json`、`host-provenance.json`。只读版本子命令不会启动模型宿主。
3. **0.153.1确有App Server队列协议，不能说“没有队列API”。** 从实际内嵌二进制生成的实验schema包含`thread/queue/add,list,update,delete,reorder,start`。add有`threadId/clientUserMessageId/input`；list为只读队列分页。见`schema/ClientRequest.json`。但协议存在不证明它能连接已有stdio宿主，也不证明另一宿主提交后原Desktop会消费。未做业务thread排队试验；跨宿主DB队列消费仍无证据。
4. **Desktop UI队列走自己的应用global state，并有原窗口所有权仲裁。** `app-initial-c8dbea294abe.js`中`QUEUED_FOLLOW_UPS=queued-follow-ups`，storage调用`get-global-state/set-global-state`并持有`navigator.locks`的`codex-queued-follow-up-state`锁；协调器检查owner/follower、`tryAcquireStartTurn`、`queued-follow-up-send-lock-acquire/release`，然后调用`startTurn/steerTurn`。消费由turnCompleted、readiness和global-state变更唤醒。见`queue-engine.txt`、`queue-storage-contexts.json`、`send-tool-and-storage.json`。这不是读同一个Codex state DB即可等价实现的入口。
5. **打包JS未发现UI调用App Server queue/add/list/start的接线。** 扫描`.vite/build/*.js`与`webview/assets/*.js`发现该族字符串仅`thread/queue/changed`，它位于标为false的通知表并加入opt-out集合。见`source-search.json`。这支持“未证明有接线”，不应放大为所有内部核心都不存在跨进程机制。
6. **内置send_message_to_thread不是只排队能力，且当前未开放为可调用工具。** 定义具有`threadId/hostId/prompt/model/thinking`，没有queue-only参数。路由`XYi → Bqi → Rqi → sendFollowUpMessage → Iln → sendPreparedMessage`固定`send-now`，活跃turn尝试steer；无活跃轮时可开始下一轮。见`thread-tool.txt`、`send-message-submission.json`、`followup-final-route.json`、`queue-engine.txt`。native MCP通过app-owned pipe转发tools/list/call，并要求调用线程元数据；这里没有把内部pipe包装成OAX连接器，也未伪造元数据或绕过工具权限。
7. **官方文档当前请求403。** `https://developers.openai.com/codex/app-server/`抓取失败见`official-fetch-error.txt`；本结论依赖当前本机help/schema/打包源码，不声称官方文档确认了支持性。全局memory仅用于证据分层提醒，现状重新只读核对。

## 下一步

- 当前两个业务thread维持原状；不能因为共享0.160 daemon读取notLoaded、共享DB或schema含queue/add就投递试点。
- 若产品要支持本约束，需要原宿主正式暴露可访问、可认证、具备queue-only语义与原窗口仲裁的入口，或正式开放且文档明确排队语义的App工具。当前不能靠private IPC、直接改global state/SQLite、浏览器UI注入或裸turn/start补齐。

目标业务thread仅作为任务范围：`01a0b4e3-7b2e-7822-8ae1-9f0e812115a9`、`01a0e016-d951-77d1-bc7e-d13662f4823c`。本代理未读取其对话内容或投递。

源包：`/usr/lib/chatgpt/resources/app.asar`，SHA-256 `17ef298f24961f79a56b87c343e70c489c995afafc8dd97bdcb8214d5db08593`。源码摘录中的offset是提取后UTF-8文本字符偏移，不是行号；完整提取JS保留在本目录便于定位，不建议将完整产品包入仓。
