本次只验证 Skill 的离线准备。此 workspace 保持只读。
资料和 OAX profile 只能写在 /home/sky/.local/state/openagentx/validation/2026-10-05-terminal-overview-join/join-model-short 下。
禁止 resume/open、启动 Worker、向其他 Agent 发消息或修改默认 OAX profile。

用户已确认的本次接入资料：
稳定 Agent ID 是 join-skill-model-e2e，显示名是接入验证领域。
工作目录是 /home/sky/.local/state/openagentx/validation/2026-10-05-terminal-overview-join/join-model-short/workspace。职责：只负责该验证工程资料整理，不负责支付、部署或其他业务领域。
协作对象明确为 pay-domain，只记录将来咨询接口的意向，本轮不发信。
交接记录：已完成阅读本工程 README；下一步等待用户正式启用；未知事项为尚未配置协作权限。
保留本次 Codex 当前 thread，请由 helper 自动捕获，不在 brief 中手填 thread_id。
本轮唯一允许的 profile 是 /home/sky/.local/state/openagentx/validation/2026-10-05-terminal-overview-join/join-model-short/profile。必须给 prepare helper 传 --profile-home 指向它。
私密 brief 放在 /home/sky/.local/state/openagentx/validation/2026-10-05-terminal-overview-join/join-model-short/brief.json；本工作目录只读。
只做 prepare，不能 resume/open 或创建任何业务任务。完成后报告 prepared、ready 与 thread 来源。