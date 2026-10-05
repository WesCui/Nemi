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
		{ID: "feishu_documents", Name: "飞书文档", Mark: "文", Category: "work", Kind: "document", Summary: "读文档，整理报告和行动项", Boundary: "读取你明确提供且已授权的 docx 正文，最多 12000 字节。暂不编辑原文档，不读取聊天或日历。首次需要管理员提供应用配置并授权文档。", Capabilities: []string{"read_feishu_document"}, Examples: []string{"帮我连接飞书文档，然后读取我提供的文档，整理重点和待办。"}, State: "unconfigured"},
		{ID: "feishu", Name: "飞书群", Mark: "飞", Category: "work", Kind: "bot", Summary: "整理内容，确认后发到群", Boundary: "只向配置的接收群发送文字，正文最多 1800 字节。每条消息需确认；不读取群聊天。", Capabilities: []string{"propose_message"}, Examples: []string{"帮我连接飞书群，整理一份要发送的工作安排，发送前让我核对。"}, State: "unconfigured"},
		{ID: "wecom", Name: "企业微信", Mark: "企", Category: "work", Kind: "bot", Summary: "把资料和跟进安排发到群", Boundary: "通过群机器人发送经你确认的文字，不读取个人微信、通讯录或聊天记录。", Capabilities: []string{"propose_message"}, Examples: []string{"帮我把工作跟进安排发到企业微信群，先检查连接，发送前让我确认。"}, State: "unconfigured"},
		{ID: "dingtalk", Name: "钉钉", Mark: "钉", Category: "work", Kind: "bot", Summary: "整理清单，确认后发到群", Boundary: "通过加签群机器人发送文字，不读取消息、日历或文档；创建待办尚未接入。", Capabilities: []string{"propose_message"}, Examples: []string{"帮我连接钉钉群，准备一份工作清单，发送前让我核对。"}, State: "unconfigured"},
		{ID: "calendar", Name: "系统日历", Mark: "31", Category: "life", Kind: "calendar", Summary: "在对话里生成日历文件", Boundary: "导出已保存事项的下一次提醒或截止时间，生成一个 15 分钟的 .ics 日程。需你导入日历，不自动同步后续修改。", Capabilities: []string{"export_matter_calendar"}, Examples: []string{"帮我查找要安排的事项，把下一次提醒或截止时间生成日历文件。"}, State: "available"},
		{ID: "amap", Name: "高德地图", Mark: "高", Category: "life", Kind: "planned", Summary: "出行计划与地点资料", Boundary: "实时地点搜索和路线接口尚未接入。可在对话提供已选地址，让妮米整理出行安排；不能声称已查询高德。", Capabilities: []string{}, Examples: []string{"我想准备一次出行，请先问我目的地和时间，根据我提供的地址整理安排。"}, State: "planned"},
		{ID: "wechat", Name: "微信", Mark: "微", Category: "life", Kind: "planned", Summary: "生活事项与订阅提醒", Boundary: "尚未接入微信账号或小程序订阅消息，不读取个人聊天。可提供选定文字让妮米处理。", Capabilities: []string{}, Examples: []string{"我会提供一段聊天文字，请帮我整理需要跟进的事。"}, State: "planned"},
		{ID: "mail", Name: "QQ / 163 邮箱", Mark: "邮", Category: "life", Kind: "planned", Summary: "从选定邮件整理安排", Boundary: "自动读取邮箱尚未接入。可粘贴选定邮件或附上文件，不要提供登录密码。", Capabilities: []string{}, Examples: []string{"我会提供一封邮件，请帮我整理时间、材料和接下来要做的事。"}, State: "planned"},
		{ID: "tencent-docs", Name: "腾讯文档", Mark: "文", Category: "work", Kind: "planned", Summary: "整理选定文档里的工作", Boundary: "尚未接入账号授权。粘贴网址不会让 Nemi 获得文档访问权限；可上传导出的文件或粘贴正文。", Capabilities: []string{}, Examples: []string{"我会上传从腾讯文档导出的文件，请帮我整理重点和行动项。"}, State: "planned"},
		{ID: "wps", Name: "WPS", Mark: "W", Category: "work", Kind: "planned", Summary: "分析办公文件，交付成果", Boundary: "WPS 在线账号尚未接入。可上传 PDF、DOCX、XLSX 或 CSV，处理所提供的文件；不自动修改在线原文件。", Capabilities: []string{}, Examples: []string{"我会上传办公资料，请帮我分析内容，再生成可以下载的成果文件。"}, State: "planned"},
		{ID: "rail", Name: "铁路 12306", Mark: "铁", Category: "life", Kind: "planned", Summary: "出发前的准备与提醒", Boundary: "订单与票务尚未接入，不代购票或退改。根据你提供的车次和时间准备行程。", Capabilities: []string{}, Examples: []string{"我会提供车次和出发时间，请帮我准备行李、证件和出发安排。"}, State: "planned"},
		{ID: "meituan", Name: "美团 / 大众点评", Mark: "团", Category: "life", Kind: "planned", Summary: "聚会与周末安排", Boundary: "个人订单、店铺搜索和评价尚未接入，不代下单或付款。可处理你提供的地点和预约资料。", Capabilities: []string{}, Examples: []string{"我会提供餐厅和预约资料，请帮我整理聚会安排和需要确认的事。"}, State: "planned"},
		{ID: "shopping", Name: "淘宝 / 京东", Mark: "购", Category: "life", Kind: "planned", Summary: "购物与售后事项", Boundary: "个人购物账号和订单尚未接入，不下单或付款。可根据你提供的记录整理收货与退换货事项。", Capabilities: []string{}, Examples: []string{"我会提供购买记录，请帮我整理收货、退换货期限和待办。"}, State: "planned"},
	}
}

func Connectable(id string) bool {
	return id == "feishu_documents" || Names[id] != ""
}
