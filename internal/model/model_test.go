package model

import (
	"context"
	"io"
	"nemi/internal/config"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestDomesticRequestContracts(t *testing.T) {
	for provider, host := range map[string]string{"qwen": "dashscope.aliyuncs.com", "deepseek": "api.deepseek.com", "kimi": "api.moonshot.cn", "doubao": "ark.cn-beijing.volces.com", "glm": "open.bigmodel.cn"} {
		g := New(config.Config{Provider: provider, Model: "contract-fixture", Key: "test-only-placeholder"})
		g.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Scheme != "https" || r.URL.Host != host || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test-only-placeholder" {
				t.Fatal("invalid domestic request contract")
			}
			b, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(b), `"max_tokens":2048`) || !strings.Contains(string(b), `"stream":false`) {
				t.Fatal("unbounded request")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"finish_reason":"stop","message":{"content":"{\"summary\":\"核对材料\",\"items\":[\"确认要求\",\"整理资料\",\"补齐缺项\"]}"}}],"usage":{"prompt_tokens":50,"completion_tokens":30}}`))}, nil
		})
		if _, e := g.Generate(context.Background(), "报名", "用户资料"); e != nil {
			t.Fatal(e)
		}
	}
}
func TestProfileFreezesPriceAndPromptContract(t *testing.T) {
	a := New(config.Config{Provider: "qwen", Model: "fixture", InputPrice: 1, OutputPrice: 2})
	b := New(config.Config{Provider: "qwen", Model: "fixture", InputPrice: 3, OutputPrice: 2})
	if a.Profile() == b.Profile() {
		t.Fatal("price change not reflected in profile")
	}
}

func TestDecodeRejectsInvalidAndTruncatedPlans(t *testing.T) {
	valid := []byte(`{"choices":[{"finish_reason":"stop","message":{"content":"{\"summary\":\"先核对材料\",\"items\":[\"确认要求\",\"整理资料\",\"补齐缺项\"]}"}}],"usage":{"prompt_tokens":50,"completion_tokens":30}}`)
	o, e := Decode(valid)
	if e != nil || len(o.Plan.Items) != 3 || o.InputTokens != 50 {
		t.Fatalf("%+v %v", o, e)
	}
	for _, bad := range []string{`{}`, `{"choices":[{"finish_reason":"length","message":{"content":"{}"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`, `{"choices":[{"finish_reason":"stop","message":{"content":"{\"summary\":\"ok\",\"items\":[\"a\",\"b\",\"c\"],\"tool\":\"pay\"}"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`} {
		if _, e = Decode([]byte(bad)); e == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
func TestCostRoundsUpAndReservesOutput(t *testing.T) {
	g := New(config.Config{InputPrice: 2000000, OutputPrice: 4000000})
	if g.Cost(1, 1) != 6 {
		t.Fatal("incorrect integer pricing")
	}
	if g.Reserve("材料", "报名要求") < 8192 {
		t.Fatal("output not reserved")
	}
}
