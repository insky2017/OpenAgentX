# 文档浏览器核验

两页是无外部依赖的单文件 HTML。本轮用独立 Google Chrome 浏览器渲染工程文件的完整原始 HTML；验证现有交互、1280×900 和 390×844 布局，无页面脚本错误，截图已人工查看。短入口复制源为精确 `$oax-join`，接入 Tab 与图关系筛选、终端关闭机制演示均实际切换。

环境限制：CUA 没有可用浏览器；旧 agent-browser 把 file URL 错识为 HTTPS，loopback 页面导航返回 ERR_ACCESS_DENIED。最终在自有 about:blank 加载原文 HTML，保留源 SHA 和执行记录。这证明 Chrome 中的真实 DOM、布局与交互，不声称 file/HTTP 导航或网页部署已通过；相对文件链接另外静态核验。未绕过证书警告或改用户浏览器设置。

`join-overview-mobile.png` 是缩窄视口后尚未重新定位的图区域；最终总览窄屏截图为 `join-overview-mobile-positioned.png`。
