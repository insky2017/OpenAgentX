# 原会话协作、职责与恢复交付

## 用户可用结果

- 两原会话已经完成真实咨询→Pay关联回复→Rhythm读入及双方ACK，原thread工具与正式API/产物交叉核验通过。
- 已安装源码`8c9ceff` / schema v4，SHA-256 `10943bd6cb83a849eeb90ab565ced0cabfef89e4218511966a0903d43034e392`。职责目录由owner管理，发送/接单/回复检查声明scope；ACK与处置分离，已ACK未完请求可恢复，支持一次关联转交。
- Rhythm/Pay的10项职责已登记。简短[接入Skill](../../../../skills/oax-collaborate/SKILL.md)已安装到`~/.codex/skills/oax-collaborate/`，两个START入口已更新。原Agent后续实际读取新版Skill的行为仍需单独验证。
- 规则/计划提交`6fcc80a`、功能提交`8c9ceff`。使用说明见[原会话入口](../../../operations/external-session-entry.md)。

## 自动续办未通过，周期模型轮询已暂停

Desktop原生heartbeat能启动两个原thread，但两版提示词共8轮均无工具调用，表现为空回复或复述旧进度，没有完成新的OAX咨询。不能因为聊天里有答复就判定自动协作成功。

8轮实测输入905,850，其中缓存902,656，输出845；缓存仍有成本，不估算未核实金额。用户明确指出长上下文轮询成本后，两项自动化已正式暂停。后续采用普通程序监听、消息去重/合并、仅在有可执行工作时启动模型的方向；原Desktop事件入站入口仍无已验证方案。OAX托管runtime可作为改变执行方式的另一路线，但本轮未迁移两个原会话。成本计算、工作方式选择与后续真实验收见[事件触发方案](../../../operations/event-driven-collaboration.md)。

## 验证与恢复

全量`go test -race ./...`通过；独立13组真实隔离daemon/CLI验证覆盖职责、旧数据迁移、重启恢复、幂等与转交。首轮夹具失败和accepted职责校验漏洞的首次失败均保留。正式安装从干净独立checkout构建，`vcs.modified=false`；OAX三服务工件哈希一致，两个Desktop宿主PID/start/hash保持不变，既有4条消息旧列完整保留。正式安装6组消息协议验收通过：[installed03](evidence/installed03/result.json)。使用随机测试身份，临时2项scope已按CAS清理，最终revision=3且原10项规则内容不变；测试通信凭据已撤销。详见[执行记录](EXECUTION-LOG.md)。

完整范围见[COVERAGE](COVERAGE.md)；原始证据在`~/.local/state/openagentx/validation/2026-10-03-desktop-collaboration/`，精选脱敏副本和SHA在`evidence/`。回滚副本为`installation01/rollback/`，schema降级必须同时恢复旧数据库和二进制；不可让v3二进制打开v4数据库。未操作支付资金、通知补发、业务部署或两个业务工程。
