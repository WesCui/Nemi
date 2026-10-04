package connectors

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixtures() map[string]Bot {
	return map[string]Bot{
		"feishu":   {"https://open.feishu.cn/open-apis/bot/v2/hook/test-only", "demo", "测试群"},
		"wecom":    {"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=test-only", "", "测试群"},
		"dingtalk": {"https://oapi.dingtalk.com/robot/send?access_token=test-only", "this is secret", "测试群"},
	}
}
func TestOfficialSignatureVectors(t *testing.T) {
	now := time.Unix(1599360473, 0)
	for _, v := range []struct{ id, secret, timestamp, signature string }{
		{"feishu", "demo", "1599360473", "l1N0gAcBjdwBvGm1xMjOF0XSyaLRpR7tuO5dHfhAYc8="},
		{"dingtalk", "this is secret", "1599360473000", "hXZTWifRGHclNuZSKxoXc//sh51SWfVfRIhcdeKs63I="},
	} {
		timestamp, sign := Sign(v.id, v.secret, now)
		if timestamp != v.timestamp || sign != v.signature {
			t.Fatal("signature does not match independent vector", v.id)
		}
	}
}
func TestWebhookBoundariesAndRedaction(t *testing.T) {
	for id, b := range fixtures() {
		if !Valid(id, b) {
			t.Fatal("valid official endpoint rejected", id)
		}
	}
	for _, raw := range []string{"http://open.feishu.cn/open-apis/bot/v2/hook/test", "https://127.0.0.1/open-apis/bot/v2/hook/test", "https://open.feishu.cn.attacker.example/open-apis/bot/v2/hook/test", "https://open.feishu.cn:443/open-apis/bot/v2/hook/test", "https://open.feishu.cn/open-apis/bot/v2/hook/../test", "https://user@open.feishu.cn/open-apis/bot/v2/hook/test", "https://open.feishu.cn/open-apis/bot/v2/hook/test?key=secret"} {
		if Valid("feishu", Bot{URL: raw, Label: "测试群"}) {
			t.Fatal("unsafe endpoint accepted")
		}
	}
	if Valid("wecom", Bot{URL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=a&key=b", Label: "群"}) {
		t.Fatal("duplicate credential accepted")
	}
	bots := fixtures()
	bots["feishu"] = Bot{URL: "https://evil.example/secret-value", Label: "secret-value"}
	raw, _ := json.Marshal(New(bots).Statuses())
	if strings.Contains(string(raw), "secret-value") || strings.Contains(string(raw), "test-only") {
		t.Fatal("credential leaked into status")
	}
}
func TestPlatformPayloadAndReceiptContracts(t *testing.T) {
	for id, b := range fixtures() {
		t.Run(id, func(t *testing.T) {
			calls := 0
			s := New(map[string]Bot{id: b})
			s.Client.Transport = transport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "POST" || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
					t.Fatal("invalid method or content type")
				}
				var body map[string]json.RawMessage
				if json.NewDecoder(r.Body).Decode(&body) != nil {
					t.Fatal("invalid JSON")
				}
				if id == "feishu" {
					if string(body["msg_type"]) != `"text"` || body["sign"] == nil || body["timestamp"] == nil {
						t.Fatal("missing Feishu text/signature")
					}
				} else {
					if string(body["msgtype"]) != `"text"` {
						t.Fatal("missing text envelope")
					}
					if id == "dingtalk" && (r.URL.Query().Get("timestamp") == "" || r.URL.Query().Get("sign") == "") {
						t.Fatal("missing DingTalk signature")
					}
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(map[bool]string{true: `{"code":0}`, false: `{"errcode":0}`}[id == "feishu"]))}, nil
			})
			if s.Send(context.Background(), id, "测试内容") != "DELIVERED" || calls != 1 {
				t.Fatal("successful receipt was not recognized")
			}
			for _, raw := range []string{`{}`, `{"code":null,"errcode":null}`, `<html>`, strings.Repeat("x", 8193)} {
				s.Client.Transport = transport(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw))}, nil
				})
				if s.Send(context.Background(), id, "内容") != "UNKNOWN" {
					t.Fatal("ambiguous receipt marked successful")
				}
			}
			s.Client.Transport = transport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":19021,"errcode":310000}`))}, nil
			})
			if s.Send(context.Background(), id, "内容") != "REJECTED" {
				t.Fatal("rejection lost")
			}
		})
	}
}
