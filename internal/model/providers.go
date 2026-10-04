package model

// Endpoints are pinned to the official Chinese services. User-supplied base
// URLs are deliberately not accepted by the credential-bearing gateway.
type Provider struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Docs     string `json:"docs"`
	Console  string `json:"console"`
	Endpoint string `json:"-"`
}

var Providers = []Provider{
	{"qwen", "通义千问", "https://help.aliyun.com/zh/model-studio/qwen-api-reference/", "https://bailian.console.aliyun.com/", "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions"},
	{"deepseek", "DeepSeek", "https://api-docs.deepseek.com/zh-cn/api/create-chat-completion", "https://platform.deepseek.com/", "https://api.deepseek.com/chat/completions"},
	{"kimi", "Kimi", "https://platform.kimi.com/docs/api/chat", "https://platform.kimi.com/", "https://api.moonshot.cn/v1/chat/completions"},
	{"doubao", "豆包 · 火山方舟", "https://docs.volcengine.com/docs/ark/chat-api", "https://console.volcengine.com/ark/", "https://ark.cn-beijing.volces.com/api/v3/chat/completions"},
	{"glm", "智谱 GLM", "https://docs.bigmodel.cn/api-reference/模型-api/对话补全", "https://open.bigmodel.cn/", "https://open.bigmodel.cn/api/paas/v4/chat/completions"},
}

func ProviderByID(id string) (Provider, bool) {
	for _, p := range Providers {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}
