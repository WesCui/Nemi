package connectors

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestOnlineProtocolsReturnSourcesWithoutCredentials(t *testing.T) {
	key := "fixture-service-key-never-live"
	o := NewOnline()
	calls := 0
	o.Client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		calls++
		body := ""
		switch r.URL.Host + r.URL.Path {
		case "api.bochaai.com/v1/web-search":
			if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer "+key {
				t.Fatal("search authorization changed")
			}
			var p map[string]any
			json.NewDecoder(r.Body).Decode(&p)
			if p["query"] != "杭州周末" || p["freshness"] != "oneWeek" || p["count"] != float64(5) {
				t.Fatal("search request changed")
			}
			body = `{"code":200,"data":{"webPages":{"value":[{"name":"官方资料","url":"https://example.com/news","summary":"真实接口资料"},{"name":"内网","url":"https://127.0.0.1/private"},{"name":"密钥网址","url":"https://example.com/?token=secret"}]}}}`
		case "restapi.amap.com/v5/place/text":
			if r.URL.Query().Get("key") != key || r.URL.Query().Get("region") != "上海" {
				t.Fatal("map request changed")
			}
			body = `{"status":"1","pois":[{"id":"poi-1","name":"人民广场","cityname":"上海市","address":"人民大道","location":"121.475,31.228"}]}`
		case "restapi.amap.com/v3/direction/walking":
			if r.URL.Query().Get("origin") != "121.475,31.228" {
				t.Fatal("route coordinate changed")
			}
			body = `{"status":"1","route":{"paths":[{"distance":"2300","duration":"1800","steps":[{"instruction":"沿南京东路步行","distance":"2300","duration":"1800"}]}]}}`
		default:
			t.Fatal("unexpected credential destination")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	ctx := context.Background()
	c := ServiceCredential{Key: key}
	s, err := o.Search(ctx, c, "杭州周末", "oneWeek")
	if err != nil || len(s.Results) != 1 || s.Results[0].URL != "https://example.com/news" {
		t.Fatal("search source lost or unsafe source exposed")
	}
	p, err := o.Places(ctx, c, "人民广场", "上海")
	if err != nil || len(p.(map[string]any)["places"].([]Place)) != 1 {
		t.Fatal("map result unavailable")
	}
	r, err := o.Route(ctx, c, "121.475,31.228", "121.49,31.23", "walking")
	if err != nil {
		t.Fatal("route result unavailable")
	}
	b, _ := json.Marshal([]any{s, p, r})
	if strings.Contains(string(b), key) {
		t.Fatal("credential leaked")
	}
	if _, err = o.Route(ctx, c, "900,31", "121,31", "walking"); err == nil || calls != 3 {
		t.Fatal("invalid coordinates sent to platform")
	}
	if _, err = o.Search(ctx, c, "杭州", "invented-date"); err == nil || calls != 3 {
		t.Fatal("invalid freshness sent to platform")
	}
}
func TestServiceCredentialsRestrictMailboxAndFields(t *testing.T) {
	for _, email := range []string{"hello@qq.com", "hello@foxmail.com", "hello@163.com", "hello@126.com"} {
		if !(ServiceCredential{Email: email, Code: "fixture-code"}).Valid("mail") {
			t.Fatal("supported mailbox rejected")
		}
	}
	for _, email := range []string{"hello@127.0.0.1", "hello@evil.example", "User <hello@qq.com>", "hello@qq.com\r\n"} {
		if (ServiceCredential{Email: email, Code: "fixture-code"}).Valid("mail") {
			t.Fatal("arbitrary mailbox accepted")
		}
	}
	if (ServiceCredential{Key: "fixture-service-key", Email: "hello@qq.com"}).Valid("amap") {
		t.Fatal("unrelated credential fields accepted")
	}
}
