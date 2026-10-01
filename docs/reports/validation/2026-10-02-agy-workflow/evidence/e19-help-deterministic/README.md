# E19 AGY 帮助范围回归（D）

仅改变Console帮助文案、README与安装指南，明确默认AGY无可发起的preflight/原生审批入口；已有approve/reject命令与运行契约保留兼容。Web仅呈现已有审批记录，没有新增可发起承诺，未改Web。

此前相同小测试在工具返回PASS（0.018s），但没有单独保存原始日志；本次明确补跑同一小测试取证，不把先前结果伪造为原始日志。实际命令、时间、候选commit、源码和stdout/stderr哈希见01-help.result.json。stdout/stderr逐字节保存。该证据仅D帮助回归，不证明R/I或preflight审批可用。

原始持久路径：/home/sky/.local/state/openagentx/validation/2026-10-02-agy-workflow/e19-help-deterministic。
