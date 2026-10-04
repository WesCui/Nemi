package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	"nemi/internal/config"
	"nemi/internal/domain"
	"nemi/internal/model"
	"nemi/internal/store"
)

type testTransport func(*http.Request) (*http.Response, error)

func (t testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return t(r) }

type testLedger struct {
	claims         int
	states         []string
	deny           bool
	settleFailures int
}

func (l *testLedger) ClaimAgentStep(context.Context, store.Ref, int, string, string, string, int64) error {
	l.claims++
	if l.deny {
		return errors.New("DAILY_BUDGET_EXCEEDED")
	}
	return nil
}
func (l *testLedger) SettleAgentStep(_ context.Context, _ store.Ref, _ int, status, _ string, _, _, _ int64) error {
	if l.settleFailures > 0 {
		l.settleFailures--
		return errors.New("fixture database unavailable")
	}
	l.states = append(l.states, status)
	return nil
}

func TestSettlementRetriesDatabaseOnlyAndRetainsKnownUsage(t *testing.T) {
	l := &testLedger{settleFailures: 3}
	g := testGateway()
	s := &state{ledger: l, g: g}
	calls := 0
	g.HTTP.Transport = testTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return response(`{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}],"usage":{"prompt_tokens":40,"completion_tokens":20}}`), nil
	})
	_, err := (&chatModel{s: s}).Generate(context.Background(), []*schema.Message{{Role: schema.User, Content: "hello"}})
	if err == nil || s.failure != "AGENT_LEDGER_UNAVAILABLE" || calls != 1 || s.input != 40 || s.output != 20 || s.cost != 160 {
		t.Fatal("known usage lost or paid request retried")
	}
}
func testGateway() *model.Gateway {
	return model.New(config.Config{Provider: "deepseek", Model: "fixture", Key: "fixture-key", InputPrice: 2000000, OutputPrice: 4000000}).ForKind("chat")
}
func response(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}
}

func TestEinoLoopReturnsActualToolResultToModel(t *testing.T) {
	l := &testLedger{}
	g := testGateway()
	s := &state{ledger: l, g: g}
	calls := 0
	g.HTTP.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		var body struct {
			Messages []wireMessage `json:"messages"`
			Tools    []any         `json:"tools"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Fatal("invalid wire")
		}
		if r.URL.Host != "api.deepseek.com" || len(body.Tools) != 1 {
			t.Fatal("fixed host or tools missing")
		}
		if calls == 1 {
			return response(`{"choices":[{"finish_reason":"tool_calls","message":{"tool_calls":[{"id":"call_time","type":"function","function":{"name":"clock","arguments":"{}"}}]}}],"usage":{"prompt_tokens":40,"completion_tokens":20}}`), nil
		}
		last := body.Messages[len(body.Messages)-1]
		if last.Role != schema.Tool || last.CallID != "call_time" || last.Content != `{"now":"fixture-real-tool-result"}` {
			t.Fatal("tool output was not sent to the model")
		}
		return response(`{"choices":[{"finish_reason":"stop","message":{"content":"根据工具返回的时间回答"}}],"usage":{"prompt_tokens":60,"completion_tokens":30}}`), nil
	})
	actual := 0
	clock := &agentTool{info: &schema.ToolInfo{Name: "clock", Desc: "读取时间", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{})}, s: s, run: func(context.Context, string) (any, error) {
		actual++
		return map[string]string{"now": "fixture-real-tool-result"}, nil
	}}
	engine, err := react.NewAgent(context.Background(), &react.AgentConfig{ToolCallingModel: &chatModel{s: s}, ToolsConfig: compose.ToolsNodeConfig{Tools: []tool.BaseTool{clock}, ExecuteSequentially: true}, MaxStep: 12})
	if err != nil {
		t.Fatal(err)
	}
	answer, err := engine.Generate(context.Background(), []*schema.Message{{Role: schema.User, Content: "现在几点"}})
	if err != nil || answer.Content != "根据工具返回的时间回答" || calls != 2 || actual != 1 || l.claims != 3 || s.input != 100 || s.output != 50 || s.cost != 400 {
		t.Fatal("real model/tool loop did not settle", err, calls, actual, s.cost)
	}
	for _, status := range l.states {
		if status != "SUCCEEDED" {
			t.Fatal("step failed")
		}
	}
}
func TestModelRejectsUnregisteredToolAndPreservesUnknownUsage(t *testing.T) {
	for _, test := range []struct{ body, status, code string }{
		{`{"choices":[{"finish_reason":"tool_calls","message":{"tool_calls":[{"id":"call_bad","type":"function","function":{"name":"send_payment","arguments":"{}"}}]}}],"usage":{"prompt_tokens":40,"completion_tokens":20}}`, "FAILED", "MODEL_INVALID_TOOL_CALL"},
		{`{"choices":[{"finish_reason":"stop","message":{"content":"no usage"}}]}`, "UNKNOWN", "MODEL_INVALID_USAGE"},
	} {
		l := &testLedger{}
		g := testGateway()
		s := &state{ledger: l, g: g}
		g.HTTP.Transport = testTransport(func(*http.Request) (*http.Response, error) { return response(test.body), nil })
		_, err := (&chatModel{s: s}).Generate(context.Background(), []*schema.Message{{Role: schema.User, Content: "hello"}})
		if err == nil || s.failure != test.code || len(l.states) != 1 || l.states[0] != test.status {
			t.Fatal("invalid provider response accepted", err)
		}
	}
}
func TestBudgetDenialPreventsHTTPAndSensitiveMetadataIsNotForwarded(t *testing.T) {
	l := &testLedger{deny: true}
	g := testGateway()
	s := &state{ledger: l, g: g}
	calls := 0
	g.HTTP.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		raw, _ := io.ReadAll(r.Body)
		if strings.Contains(string(raw), "hidden-reasoning") {
			t.Fatal("private metadata sent")
		}
		return response(`{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}],"usage":{"prompt_tokens":40,"completion_tokens":20}}`), nil
	})
	m := &chatModel{s: s}
	msgs := []*schema.Message{{Role: schema.User, Content: "hi", ReasoningContent: "hidden-reasoning"}}
	if _, err := m.Generate(context.Background(), msgs); err == nil || calls != 0 {
		t.Fatal("denied request submitted")
	}
	l.deny = false
	s.failure = ""
	if _, err := m.Generate(context.Background(), msgs); err != nil || calls != 1 {
		t.Fatal(err)
	}
}
func TestOnlyUserOfferedFeishuURLIsEligible(t *testing.T) {
	u := "https://team.feishu.cn/docx/Document123"
	if offeredURL([]domain.ChatMessage{{Role: "assistant", Content: u}}, u) || offeredURL([]domain.ChatMessage{{Role: "user", Content: u + "Other"}}, u) || !offeredURL([]domain.ChatMessage{{Role: "user", Content: "请总结 " + u + "。"}}, u) {
		t.Fatal("link source not enforced")
	}
}
