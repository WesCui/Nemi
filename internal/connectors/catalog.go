package connectors

// Application is the shared capability contract for the UI and the agent.
// Planned integrations never acquire credentials or expose executable tools.
type Application struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Mark         string   `json:"mark"`
	Category     string   `json:"category"`
	Kind         string   `json:"kind"`
	Summary      string   `json:"summary"`
	Boundary     string   `json:"boundary"`
	Capabilities []string `json:"capabilities"`
	Examples     []string `json:"examples"`
	State        string   `json:"state"`
	Label        string   `json:"label,omitempty"`
	Revision     int      `json:"revision,omitempty"`
	Verified     bool     `json:"verified"`
}

func Catalog() []Application {
	return []Application{
		{ID: "search", Name: "联网搜索", Mark: "搜", Category: "life", Kind: "service", Summary: "查询最新资料，核对来源", Boundary: "通过博查 Web Search API 返回真实网页来源与摘要；需在对话专用字段保存博查 API Key，按提供方额度调用。结果不保证事实正确，应继续核对原文。", Capabilities: []string{"search_web"}, Examples: []string{"帮我连接联网搜索，然后搜索我需要的最新信息并附上来源。"}, State: "unconfigured"},
		{ID: "browser", Name: "网页浏览器", Mark: "览", Category: "life", Kind: "browser", Summary: "读取需要 JavaScript 的公开网页", Boundary: "在 Nemi 服务端的独立无登录浏览器中渲染公开 HTTPS 页面，返回正文、链接和资料文件。不登录、接管账号、点击交易按钮或支付；当前本地部署由本机服务端运行，上云需部署同一运行环境。", Capabilities: []string{"browse_web"}, Examples: []string{"请用浏览器读取我提供的公开网页，提取重要信息并保存资料。"}, State: "unavailable"},
		{ID: "feishu_documents", Name: "飞书文档", Mark: "文", Category: "work", Kind: "document", Summary: "读文档，整理报告和行动项", Boundary: "读取你明确提供且已授权的 docx 正文，最多 12000 字节。暂不编辑原文档，不读取聊天或日历。首次需要管理员提供应用配置并授权文档。", Capabilities: []string{"read_feishu_document"}, Examples: []string{"帮我连接飞书文档，然后读取我提供的文档，整理重点和待办。"}, State: "unconfigured"},
		{ID: "feishu", Name: "飞书群", Mark: "飞", Category: "work", Kind: "bot", Summary: "整理内容，确认后发到群", Boundary: "只向配置的接收群发送文字，正文最多 1800 字节。每条消息需确认；不读取群聊天。", Capabilities: []string{"propose_message"}, Examples: []string{"帮我连接飞书群，整理一份要发送的工作安排，发送前让我核对。"}, State: "unconfigured"},
		{ID: "wecom", Name: "企业微信", Mark: "企", Category: "work", Kind: "bot", Summary: "把资料和跟进安排发到群", Boundary: "通过群机器人发送经你确认的文字，不读取个人微信、通讯录或聊天记录。", Capabilities: []string{"propose_message"}, Examples: []string{"帮我把工作跟进安排发到企业微信群，先检查连接，发送前让我确认。"}, State: "unconfigured"},
		{ID: "dingtalk", Name: "钉钉", Mark: "钉", Category: "work", Kind: "bot", Summary: "整理清单，确认后发到群", Boundary: "通过加签群机器人发送文字，不读取消息、日历或文档；创建待办尚未接入。", Capabilities: []string{"propose_message"}, Examples: []string{"帮我连接钉钉群，准备一份工作清单，发送前让我核对。"}, State: "unconfigured"},
		{ID: "calendar", Name: "系统日历", Mark: "31", Category: "life", Kind: "calendar", Summary: "在对话里生成日历文件", Boundary: "导出已保存事项的下一次提醒或截止时间，生成一个 15 分钟的 .ics 日程。需你导入日历，不自动同步后续修改。", Capabilities: []string{"export_matter_calendar"}, Examples: []string{"帮我查找要安排的事项，把下一次提醒或截止时间生成日历文件。"}, State: "available"},
		{ID: "amap", Name: "高德地图", Mark: "高", Category: "life", Kind: "service", Summary: "搜索地点，查询步行和驾车路线", Boundary: "通过高德 Web 服务 API 查询地点与路线。需 Web 服务 Key；先核对城市及同名地点，再按真实坐标规划。预计时间基于查询时刻，不是未来路况承诺；公交、实时导航和叫车暂未接入。", Capabilities: []string{"search_places", "plan_route"}, Examples: []string{"帮我连接高德地图，查找上海的人民广场和外滩，核对地点后比较步行和驾车路线。"}, State: "unconfigured"},
		{ID: "wechat", Name: "微信", Mark: "微", Category: "life", Kind: "planned", Summary: "生活事项与订阅提醒", Boundary: "尚未接入微信账号或小程序订阅消息，不读取个人聊天。可提供选定文字让妮米处理。", Capabilities: []string{}, Examples: []string{"我会提供一段聊天文字，请帮我整理需要跟进的事。"}, State: "planned"},
		{ID: "mail", Name: "QQ / 163 邮箱", Mark: "邮", Category: "life", Kind: "service", Summary: "读收件箱，整理邮件中的安排", Boundary: "授权后通过 TLS IMAP 按需读取 QQ、Foxmail、163、126 收件箱。需邮箱地址和客户端授权码；不是邮箱登录密码。每页10封，正文最多256KB/返回3000字；不标已读、不发送、不删除、不读附件，不后台扫描。所读取资料将发送到你选定的模型。", Capabilities: []string{"list_mail", "read_mail"}, Examples: []string{"帮我连接邮箱，查看最近收到的邮件，找出需要回复和准备的事项。"}, State: "unconfigured"},
		{ID: "tencent-docs", Name: "腾讯文档", Mark: "文", Category: "work", Kind: "planned", Summary: "整理选定文档里的工作", Boundary: "尚未接入账号授权。粘贴网址不会让 Nemi 获得文档访问权限；可上传导出的文件或粘贴正文。", Capabilities: []string{}, Examples: []string{"我会上传从腾讯文档导出的文件，请帮我整理重点和行动项。"}, State: "planned"},
		{ID: "wps", Name: "WPS", Mark: "W", Category: "work", Kind: "planned", Summary: "分析办公文件，交付成果", Boundary: "WPS 在线账号尚未接入。可上传 PDF、DOCX、XLSX 或 CSV，处理所提供的文件；不自动修改在线原文件。", Capabilities: []string{}, Examples: []string{"我会上传办公资料，请帮我分析内容，再生成可以下载的成果文件。"}, State: "planned"},
		{ID: "rail", Name: "铁路 12306", Mark: "铁", Category: "life", Kind: "planned", Summary: "出发前的准备与提醒", Boundary: "订单与票务尚未接入，不代购票或退改。根据你提供的车次和时间准备行程。", Capabilities: []string{}, Examples: []string{"我会提供车次和出发时间，请帮我准备行李、证件和出发安排。"}, State: "planned"},
		{ID: "meituan", Name: "美团 / 大众点评", Mark: "团", Category: "life", Kind: "planned", Summary: "聚会与周末安排", Boundary: "个人订单、店铺搜索和评价尚未接入，不代下单或付款。可处理你提供的地点和预约资料。", Capabilities: []string{}, Examples: []string{"我会提供餐厅和预约资料，请帮我整理聚会安排和需要确认的事。"}, State: "planned"},
		{ID: "shopping", Name: "淘宝 / 京东", Mark: "购", Category: "life", Kind: "planned", Summary: "购物与售后事项", Boundary: "个人购物账号和订单尚未接入，不下单或付款。可根据你提供的记录整理收货与退换货事项。", Capabilities: []string{}, Examples: []string{"我会提供购买记录，请帮我整理收货、退换货期限和待办。"}, State: "planned"},
	}
}

func Connectable(id string) bool {
	return id == "feishu_documents" || Names[id] != "" || ServiceID(id)
}

func ConnectionIDs() []string {
	return []string{"feishu_documents", "feishu", "wecom", "dingtalk", "search", "amap", "mail"}
}
func ApplicationName(id string) string {
	for _, a := range Catalog() {
		if a.ID == id {
			return a.Name
		}
	}
	return ""
}
