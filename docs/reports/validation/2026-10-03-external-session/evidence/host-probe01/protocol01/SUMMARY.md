# 共享 Codex app-server 只读连接与会话归属诊断

## 已证实

- 本机 CLI 与 daemon 为 0.160.0。Unix socket 为 `/home/sky/.codex/app-server-control/app-server-control.sock`，它使用 WebSocket。
- `codex app-server proxy --sock PATH` 仅代理原始字节，不把 JSONL 转成 WebSocket。stdin 直接写 JSON initialize 不符合传输协议；标准 HTTP Upgrade 请求立即获得 `HTTP/1.1 101 Switching Protocols`。只终止本次新建的 proxy 客户端进程，未操作 daemon 生命周期。
- Python `websockets.sync.client.unix_connect(PATH, uri="ws://localhost")` 可直接连接；然后发送 JSON `initialize`，参数 `clientInfo:{name,version},capabilities:{experimentalApi:true}`，收到响应后发送 `initialized`。
- `thread/read {threadId,includeTurns:false}` 对两个目标均返回成功，但 `status.type=notLoaded`。历史路径分别位于 `sessions/2026/09/18/` 与 `sessions/2026/09/27/`。
- `thread/loaded/list {limit:100}` 在只读查询前后均只包含 `01a0f339-e4f2-7e93-a11e-de52e9c93b6c`，nextCursor=null，不含两个目标。
- `thread/queue/list {threadId,limit:1}` 对两个目标都返回空列表，nextCursor=null。该查询未使目标加载到内存。

## 两个目标

| thread | shared daemon 状态 | queue |
| --- | --- | --- |
| 01a0b4e3-7b2e-7822-8ae1-9f0e812115a9 | notLoaded | 空 |
| 01a0e016-d951-77d1-bc7e-d13662f4823c | notLoaded | 空 |

## 契约与证据边界

本地 0.160.0 生成 schema 说明 `thread/queue/add` 参数为 `{threadId,clientUserMessageId,input}`，返回 `{queuedSubmission:{id,clientUserMessageId,input}}`；`thread/queue/start` 参数为 `{threadId,queuedSubmissionId?}`，返回 `{turn}`；`thread/queue/changed` 通知仅含 threadId。schema 文件是前次同版本隔离生成的契约副本。本次没有调用任何 queue 写方法、turn/start、thread/resume，也没有订阅或接管业务会话。

读取成功只证明 shared daemon 能访问本地历史和队列存储，不能证明它持有两个原活跃会话或能够唤醒其原 owner。Desktop 还运行独立 app-server（观察 PID 1632229，默认 stdio），不能把 shared daemon 与 Desktop 原会话 engine 混为一体。后续应定位 Desktop 正式消息入口或实际 owner 的受支持连接；不得以在 shared daemon 上 resume 同 ID 冒充接入原活跃会话。

本次官方文档 URL 抓取收到 HTTPError，结论仅依赖本机 help、同版本 schema 与实际只读 wire。原始 wire 可能含会话元数据，仅保存于私有目录，不直接入库。

## 文件

- `direct-websocket.jsonl` / `direct-websocket-summary.json`：initialize 与两目标 metadata read。
- `loaded-and-queue-read.jsonl` / `loaded-and-queue-summary.json`：内存 loaded list 与只读队列查询。
- `proxy-upgrade-response.bin`：proxy 字节透传的 101 握手响应。
- `process-transport-observation.json`：独立 app-server PID、可执行文件与 socket/pipe FD，不记录环境或配置秘密。
- `schema/`：initialize/read/loaded/queue 契约。
