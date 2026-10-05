# 对话联网、浏览器、地图与邮箱

日期：2026-10-05 · v0.12 · schema v11 · agent-v7 / plan-v2。

本版新增六项真实工具，继续使用所选真实模型决定何时调用。工具失败或缺少授权时返回明确错误，没有模拟搜索、地图、邮件或浏览器结果。所有任务入口、连接表单、成果和后续对话都在 Nemi 内。首次取得平台凭据仍需用户在平台开通，不能凭空获得账号权限。

| 工具 | 实际实现 | 需要的配置 |
| --- | --- | --- |
| search_web | 博查 Web Search，最多五条公开 HTTPS 来源、摘要及抓取时刻；可选最近一天/周/月/年 | 在对话连接卡片填写博查 API Key；不是模型 Key |
| browse_web | 服务端独立匿名 Chromium/Chrome 渲染 JavaScript 页面，返回正文和最多五个链接；正文存为当前对话可预览、下载的加密文件 | 服务端安装 Node、Playwright 和浏览器，APP_BROWSER_ENABLED=true |
| search_places | 高德 POI 2.0 按城市查询，最多五个真实地点与 GCJ-02 坐标 | 对话填写高德 Web 服务 Key |
| plan_route | 高德步行/驾车路线、米数、预估秒数及前十二个步骤；最多两条路线 | 起终点必须来自本轮地点查询或用户原话中的坐标 |
| list_mail | QQ、Foxmail、163、126 的 IMAP 收件箱，最新在前，每页十封标题、发件人、日期与未读状态 | 邮箱地址、客户端授权码和明确资料读取同意 |
| read_mail | UID 读取选定正文，256KB 原邮件上限，返回前三千字及截断标识 | ID 包含连接版本、UIDVALIDITY、UID；变化后重列收件箱 |

模型费用与搜索/地图服务额度分别计算。本版模型账本不结算这些外部服务的实际费用；查询受有界 Agent 工具次数限制，服务侧额度仍需账号维护。未保存凭据时只能引导连接，不能产生真实搜索/地图/邮件结果。保存只表示配置存在，成功读取才写 verified_at；协议测试不能替代真实平台账号验收。

## 邮箱与连接权限

连接支持 request_connection、replace=true 的修改和 propose_disconnect 的确认停用。凭据通过专用密码字段提交，AES-256-GCM 加密并绑定空间与应用用途，不回显、不进入模型工具参数。状态接口只返回标签、版本及验证时刻。保存需要明确同意读取及向用户所选模型提供资料。

IMAP 端点限定 imap.qq.com、imap.163.com、imap.126.com 的 TLS 993，验证服务器证书并固定经过公网检查的 IP。网易提供 IMAP ID 客户端信息。EXAMINE 只读打开 INBOX，正文使用 BODY.PEEK，不标记已读、不发送、不删除、不读附件、不访问其他文件夹，也不后台扫描。HTML 邮件只提取静态文字，不加载图片或脚本。模型可能在答复引用邮件内容；停用连接不删除这些聊天记录。

每次外部读取前解密当前启用配置，结束后重查同一版本；执行期间停用或替换会阻止结果继续提供给模型。已有平台请求无法撤销。旧邮件 ID 在换邮箱或 UIDVALIDITY 变化后拒绝，避免把旧 ID 用到新账号。

## 浏览器边界

这里是 Nemi 服务端的网页阅读浏览器。本地部署由本机服务运行；部署到服务器后在服务器运行。它还不是持久云端电脑、登录接管或通用页面操作系统。

复用 Playwright，使用临时浏览器上下文，无用户浏览器配置和账号 Cookie。输入仅为 URL，不接受模型脚本。所有页面、子资源和重定向经 Go 网关读取：只接受公开 HTTPS GET/HEAD，DNS 检查后直接连接对应公网 IP；浏览器 Cookie、Authorization、Set-Cookie 均不转发。非读取请求、WebSocket、Service Worker、图片/视频/字体、下载和弹窗受到限制。浏览器普通网络设置为不可用代理，受控资源由网关完成；不会直接访问 API、数据库或内网服务。读取仍可能遇到反自动化、验证码、登录要求和页面自身的 GET 副作用，不能承诺所有网站都可用。

每 Worker 最多两个渲染进程；每页最多六十次请求、总资源八 MB、单资源一 MB、正文一万八千字符，页面等待有界，外层三十五秒超时。浏览器子进程没有应用/模型/平台密钥环境变量。此实现有进程与网络约束，但没有硬 CPU/内存限制或强 OS 沙箱；公开部署前仍需隔离执行池及资源治理。

本机可使用已有 Chrome 独立临时实例；服务器建议运行 `npm ci`、`npx playwright install --with-deps chromium` 后启用。Compose 可选择 `NEMI_DOCKERFILE=Dockerfile.browser`，同时设置 `APP_BROWSER_ENABLED=true`。镜像定义已提供，本机未完成容器构建验收，不据此声称云端已上线。

## 验证与剩余范围

后端验证包括博查和高德的真实协议合同、邮箱只读 IMAP 命令与 PEEK、静态 MIME、凭据同意/加密/版本/空间隔离。真实本机浏览器验证 JavaScript 渲染，并阻止内网与 POST 请求到达网络。前端验证三种连接卡片、确认、手机布局和凭据不进入对话。实际账号回执和模型联调证据以[实施记录](./IMPLEMENTATION.md)为准。

以下仍未接入：12306 购票/退改、美团个人订单与酒店预订、淘宝/京东个人订单和支付；个人微信、通讯录与聊天记录；腾讯文档/WPS 在线账号。搜索到公开页面不代表获得账号或交易权限。后续应分别取得平台正式接口或合适的用户接管能力，再增加目标固定、价格与期限核对、逐笔确认及真实回执；不能用通用浏览器阅读冒充交易完成。

协议参考：[博查官方开放平台](https://open.bochaai.com/)、[高德路径规划](https://lbs.amap.com/api/webservice/guide/api/direction)、[高德 POI 2.0](https://lbs.amap.com/api/webservice/guide/api-advanced/newpoisearch)、[QQ 邮箱帮助](https://service.mail.qq.com/detail/0/339)、[go-imap](https://github.com/emersion/go-imap/tree/v1)、[go-message](https://github.com/emersion/go-message)、[Playwright](https://playwright.dev/docs/api/class-browsercontext)。
