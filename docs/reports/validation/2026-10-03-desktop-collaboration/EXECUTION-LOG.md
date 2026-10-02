# 原Desktop自动续办与职责校验执行记录

## 基线

- 产品HEAD `407529a`；已安装源码 `6576c55` / schema v3；开始时产品及阅读工作树均clean。
- 用户已批准原生heartbeat试点、职责校验和中断恢复。首轮业务试点只读，不修改Rhythm/Pay仓库或业务运行服务。
- 用户提交的手动原会话通信证据等待本轮独立API/rollout核实；自动唤醒仍待验。
- 本机初始观测：Pay原thread已出现完整task_complete；Rhythm最近一轮尚未出现task_complete，不能用磁盘无更新推断空闲。
- 当前用户仍使用原Desktop，两thread均gpt-6-astra/ultra。会话历史cwd与业务仓库不同，使用用户给定的业务目录及精确thread绑定，不按cwd猜测身份。

## 本轮执行

本文件随实际结果追加。原始证据位于 `~/.local/state/openagentx/validation/2026-10-03-desktop-collaboration/`；各层证据分开判定。

### 原会话手动接入已获确证

[manual01](evidence/manual01/README.md)保存正式CLI/API、原thread工具记录及独立产物对照：咨询seq3与关联result seq4均已由对方ACK；正文逐字节相等。历史回执pending保留原样，另存当前acknowledged快照。旧交付报告中“未初始化/0消息/首条咨询未发送”是上批交付时点，现已被该证据更新。原会话真实往返及同轮继续成立，自动闲时续办不能由此推导。

### 原生heartbeat触发通过，执行未通过，已暂停

[heartbeat01](evidence/heartbeat01/README.md)保存正式创建/更新/暂停回执、两版提示词、原thread输入与终态证据。原版让Agent读本轮指令文件，复验版直接要求运行status/inbox；两版共8轮均有原生自动触发但0工具调用，空回复或复述历史结论。不是消息丢失：实际heartbeat内容在原thread输入记录中存在；具体模型执行根因尚未证实，不归因成确定的接口bug。

用户指出轮询长上下文成本，实测8轮输入905,850（缓存902,656）、输出845；计数为独立turn usage，不重复累加历史总量。已用正式工具暂停oax-rhythm与oax-oneaxe-pay；无后续LLM轮询。未创建业务试点新咨询，未改两个业务工程或原宿主。是否有聊天回复不能作为自动协作通过标准。

### 职责与恢复实施

增加owner管理的组织职责目录、版本CAS、声明scope路由/接单校验、独立receipt、已ACK未完成recover和一次关联forward；配置与角色说明共享目录。接入Skill明确逐轮读取目录和本人未决请求status，防澄清回执未入新收件时漏看；确认返回状态才继续。普通消息权限不等于原Desktop工具隔离。功能已提交`8c9ceff`，具体验证见下。


### 源码与隔离真实验证

[checks01](evidence/checks01/README.md)：复审发现accepted重试的两条幂等返回可绕过最新目录，首次3项失败保留；将校验移至返回前，最终一次全量`go test -race ./...` exit 0。[domain-e2e01](evidence/domain-e2e01/result.json)首次夹具将legacy未ACK消息误判为空收件箱，保留FAIL；修正断言后[domain-e2e02](evidence/domain-e2e02/result.json)13组真实daemon/CLI通过，覆盖v3→v4、职责/权限/CAS、幂等、澄清后重新接单、ACK未完重启恢复、关联转交和无managed任务。

### 正式构建、安装与回收

[release01](evidence/release01/manifest.json)从干净独立checkout构建`8c9ceffb82f5145fc0f3f105e0de13ed945fa9fe`，`vcs.modified=false`，安装二进制SHA为`10943bd6cb83a849eeb90ab565ced0cabfef89e4218511966a0903d43034e392`。[installation01](evidence/installation01/README.md)记录安装前两演示Agent无active_run/pending_task，只重启OAX三服务；二进制一致、schema v4。旧4条消息旧列逐行哈希相同，两个Desktop宿主PID/starttime/SHA未变。Skill和两个START已更新，原Agent尚未实际读入新版Skill。

正式验收保留三次记录：installed01因密码文件自身模式不满足过严断言而前置退出；文件实际处于0700私密父目录中，修正验证器后installed02发现指定的历史密码与当前owner不匹配。两次均在创建测试身份/消息前退出。只读核验已有安装密码来源与当前owner摘要一致后，以该正确来源执行installed03；未更改密码、未打印凭据。

[installed03](evidence/installed03/result.json)6组真实正式协议验收PASS。随机测试身份仅产生2条测试消息，无Task/Run/Worker；scope错误拒绝、稳定key幂等、ACK/accepted/recover、唯一reply/completed均核对。职责目录revision 1→2→3，CAS只移除本次2个scope，原10项业务规则完全保持；测试绑定撤销，旧token返回401；三服务PID/starttime/工件SHA均不变。账本保留审计，不冒充Rhythm/Pay业务或原模型E2E。

### 用户成本要求与后续选择

禁止常驻LLM轮询的裁决已写入规则和计划。[事件触发建议](../../../operations/event-driven-collaboration.md)给出普通程序监听、合批、串行接单及低上下文方向。原Desktop可靠投递入口仍受阻；OAX managed＋原生终端可作为明确切换执行权的路线，两个原会话本轮未迁移。下一阶段若实施，须以空闲零模型请求、单条消息一次执行、忙时排队和重启不重复为真实验收。
