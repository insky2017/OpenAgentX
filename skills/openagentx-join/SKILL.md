---
name: openagentx-join
description: OAX 接入旧名称的兼容入口。用户调用 $openagentx-join 时执行与标准 $oax-join 相同的离线准备流程；不激活 Agent。
---

这是 `$oax-join` 的兼容入口。单独输入 `$openagentx-join` 与单独输入 `$oax-join` 等效，沿用已有上下文，只补问缺项。

先将本 `SKILL.md` 的路径解析为真实路径，再读取其父目录的兄弟目录 `../oax-join/SKILL.md`，按该文件执行完整流程。安装位置可能是目录软链，不能按调用者当前工作目录解析相对路径。若共用说明缺失，停止并报告安装不完整，不猜测流程。

共用 helper 的唯一实现仍在本目录 `scripts/prepare.py`；标准短入口的 `scripts` 是指向此目录的相对软链。已有外部调用 `openagentx-join/scripts/prepare.py` 保持兼容。

接入范围仍仅为离线准备：不猜 thread ID、不改变 profile 来源，不从当前 turn 调用 resume/open 接管自己。具体 brief、线程来源、profile 与回执规则以上述共用说明为准。
