# 原 Desktop heartbeat 有界失败证据

- 原生 heartbeat 确实向两个指定原 thread 注入输入并启动真实轮次；model 为 gpt-6-astra，effort 为 ultra。
- 首版引用指令文件，第二版直接要求 status/inbox 实际终端动作；两版均未观察到该轮工具调用，不能报告自动协作通过。
- 按同因有界复验规则，两自动化已通过正式工具暂停，回执见 automation-controls.json；没有修改业务 Runtime。
- turn-evidence.json 保存本轮公开 heartbeat 输入、有限 final 摘录、开始/结束及逐记录 SHA-256；未导出 reasoning。observation.json 是结构化最小内容采集。
- native-heartbeat-source.json 记录应用固定工件和追加的 developer 常量：其输入称 user message，但真实持久记录为工具输出；语义张力只是待核假设，不能据此断言缺工具或确定根因。

原始源路径与源快照哈希见 observation.json；私密观察目录为 /home/sky/.local/state/openagentx/validation/2026-10-03-desktop-collaboration/heartbeat01/observation03。

本次8轮原生唤醒累计输入905,850 tokens，其中902,656为缓存输入，输出845 tokens。缓存不等于无需模型执行，也不能把全部输入都算作未缓存费用。成本及零工具结果使其不适合作常驻轮询方案；两自动化已暂停，不再通过降低频率继续试探。数值来自选定轮次token_usage_record，见token-usage.json。
