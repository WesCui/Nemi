package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
)

func TestPaginationKeepsLastModelRequestForAnswer(t *testing.T) {
	l := &testLedger{}
	g := testGateway()
	s := &state{ledger: l, g: g}
	calls, reads := 0, 0
	g.HTTP.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		var b struct {
			Messages []wireMessage
			Tools    []any
		}
		if json.NewDecoder(r.Body).Decode(&b) != nil {
			t.Fatal("invalid request")
		}
		if calls == 6 {
			if len(b.Tools) != 0 || !strings.Contains(b.Messages[0].Content, "未读部分") {
				t.Fatal("last request not reserved for honest answer")
			}
			return response(`{"choices":[{"finish_reason":"stop","message":{"content":"已读取前五页，后续页面尚未读取；以下为已读内容的总结。"}}],"usage":{"prompt_tokens":40,"completion_tokens":20}}`), nil
		}
		wire := fmt.Sprintf(`{"choices":[{"finish_reason":"tool_calls","message":{"tool_calls":[{"id":"page%d","type":"function","function":{"name":"paged_read","arguments":"{\"page\":%d}"}}]}}],"usage":{"prompt_tokens":40,"completion_tokens":20}}`, calls, calls)
		return response(wire), nil
	})
	read := &agentTool{info: &schema.ToolInfo{Name: "paged_read", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{"page": {Type: schema.Integer}})}, s: s, run: func(context.Context, string) (any, error) {
		reads++
		return map[string]any{"page": reads, "has_more": true}, nil
	}}
	engine, err := react.NewAgent(context.Background(), &react.AgentConfig{ToolCallingModel: &chatModel{s: s}, ToolsConfig: compose.ToolsNodeConfig{Tools: []tool.BaseTool{read}, ExecuteSequentially: true}, MaxStep: 12})
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.Generate(context.Background(), []*schema.Message{{Role: schema.System, Content: instruction}, {Role: schema.User, Content: "读取一份很长的资料并总结"}})
	if err != nil || result == nil || !strings.Contains(result.Content, "尚未读取") || reads != 5 || calls != 6 || l.claims != 11 {
		t.Fatal("pagination exhausted answer slot or exceeded budget")
	}
}
