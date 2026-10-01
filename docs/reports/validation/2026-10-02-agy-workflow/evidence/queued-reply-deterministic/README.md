# 完整回复后的 queued 补充验证（D）

本批仅隔离SQLite Repository与Go race，不是真实AGY E2E。未部署、未访问真实业务数据库。

`01-before` exit 1：完整Runtime回复且已有queued补充，旧实现仍把Task终止为uncertain，复现补充不消费。`02-race` exit 0（6.822s）：新增正向/回滚及7项拒绝反例，并复验原完成判据、未知副作用、取消/完成双顺序各100次及并发barrier、native消息defer。

最小变化：Runtime succeeded、FinalReply完整非空、无截断和错误、reason=business_effect_unverified，且存在独立kind=message/lane=work pending或claimed输入时，Task进入waiting_input。保持原Run完整TurnResult及SideEffectsKnown=false；Task Error保留business_effect_unverified、completion_basis为空。不改变AssessTaskSuccess，不宣称mutation成功。

真实Repository正向流程使用正式Claim和BeginClaimedRunAttempt消费原Task；提交独立新Message；FaultBeforeCommit回滚Task/Run/Journal且Message仍pending；成功结算后只领取/解析新Message，原任务mailbox不重复领取；下一Run结束后无更多pending则uncertain；旧Finish重放不改写最新Task。拒绝反例：无final、截断、错误、实际uncertain、取消意图、native control消息、没有新消息。旧Run证据逐字段保持。

命令、时间、stdout/stderr、exit分别记录；原始持久目录 `/home/sky/.local/state/openagentx/validation/2026-10-02-agy-workflow/queued-reply-deterministic/`；证据仅合成fixture，无真实凭据。副本及SHA256SUMS入库，未由本代理commit。真实AGY queued效果仍需R/I核验。
