# Codex 隔离实例：真实浏览器只读观察

结论：本次范围PASS，详见verdict.json。URL为`http://127.0.0.1:42423`，Agent为`codex-local-e2e`。从真实登录界面进入，仅打开既有query/mutation任务、滚动和刷新，未派单/验收/取消。CLI真实执行、独立产物核验和进程来源另见同批真实链证据。

桌面1440×1000与390×844 touch/mobile emulation分别保存DOM、视口几何和截图。卡片readiness与正式overview一致；query完整回复、adapter/model、mutation未独立核验说明均可见。截图07证明窄屏真实滚动后能读输出。刷新后保留选中任务。实际HTTP JS/CSS hash见08。

首次未登录session探测留下一个401控制台记录，登录后读取正常；刷新后无console error/warn。工具文件路径限制、最大化窗口resize、单次参数过长及viewport过渡快照见tooling-failures.json，不伪装为产品失败。

边界：本次不是已安装服务验收，不覆盖原生PTY、取消/断线、不声称验证busy灯色、320px或实体手机。390px固定输入区压缩结果可视高度，但可滚动到真实输出，属非阻断体验建议。
