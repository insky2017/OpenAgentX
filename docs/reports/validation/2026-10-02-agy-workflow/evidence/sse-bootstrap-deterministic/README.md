# Web SSE 首连游标修复（D）

- 真实浏览器首次失败另见 `../installed-browser-17d5cd2-attempt01/failure.json` 与 `dom.json`：正式历史库约 267k Journal，40 次左右 200 SSE 请求仍从 0 开始，页面持续连接中。Overview 已含 latest_sequence；真正原因是前端为防非原子快照漏事件而刻意忽略此 after-read watermark，初次也退回 0。
- `01-bootstrap-reproduce.*` 保存新首连断言在原 helper 上 FAIL；不覆盖真实首次失败。
- 最小契约：Overview 在任何投影读取之前取 `live_after_sequence`，保留原末尾 `latest_sequence` 作为展示水位。前者是保守安全下界，**并非声称 Overview 全部投影是同一事务快照**；读取期间变化会在 SSE 中再回放，重复可去重，不漏变化。
- 前端用 null 区分未初始化与真实游标 0；只有首次使用 safe 下界并固化 baseline，重连继续使用已确认游标与 EventSource 原生 Last-Event-ID，不以更新的 overview 跳过断线事件。缺失/非法 safe 游标明确报错，不回退为全量历史重放。onopen 重新读取 overview、task list，并恢复 detail catch-up，补足先于 baseline 的独立列表/详情读取或断线时未完成的延迟刷新。
- `02-panel-cursor-race.*`：定向 Overview/SSE race PASS。新测试在 267814 历史尾部保留不可投影旧 Worker 事件，模拟读完 Task 后并发提交 267815，证明 pre-read cursor=267814、末尾水位267815、旧快照仍queued但SSE会交付running更新，不重放旧畸形历史；cursor查询失败返回503而非伪造0。
- `03-web-observation-retest.*`：Web 15项 PASS，覆盖首连大游标、合法0、非法字段拒绝、重连保留confirmed、history/live合并。`04-panel-race-final.*`：完整panel race PASS，包含既有认证、游标保留边界和fail-closed projection检查；强化后的空首轮测试以1秒ctx及真实Flush标记证明立即flush，不等15秒keepalive，故未改变SSE后端flush逻辑。
- `05-panel-vet.*` PASS；`06-web-build.*` PASS；补onopen task-list catch-up后最终`07-web-build-catchup.*` PASS。Go产品最后未再变化，已通过Go检查不重复。
- 不删除或跳过 after-cursor 的投影错误，不改变业务事件或权限含义。Event Journal 已清除导致的409保留原fail-closed行为；本修复恢复保留区间内断线缺口，不新增自动跨保留边界跳转。
- 源码hash与完整diff在本目录；前六项HEAD为提交前17d5cd2，最后Web构建HEAD为只提交证据/runner的5ebfede；独立核对两者cmd/internal/web/go.mod/go.sum无差异，产品修复由本批diff与hash锁定。主代理提交新候选、独立clean构建、正式历史库只读浏览器复验及安装证据继续进行，D不得替代这些I/R证据。
- 原始持久目录：`/home/sky/.local/state/openagentx/validation/2026-10-02-agy-workflow/sse-bootstrap-deterministic/`；公开副本经凭据模式检查，manifest为SHA256SUMS。
