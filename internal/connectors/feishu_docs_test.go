package connectors

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestOfficialFeishuSDKAuthAndReadContracts(t *testing.T) {
	f := NewFeishuDocuments()
	calls := 0
	f.Client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Scheme != "https" || r.URL.Host != "open.feishu.cn" {
			t.Fatal("credential sent to nonofficial host")
		}
		body := ""
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			var app FeishuApp
			if err := json.NewDecoder(r.Body).Decode(&app); err != nil {
				t.Fatal(err)
			}
			if app.ID != "cli_fixture" || app.Secret != "test-only-app-secret" {
				t.Fatal("wrong application authentication")
			}
			body = `{"code":0,"tenant_access_token":"fixture-token","expire":7200}`
		case "/open-apis/docx/v1/documents/abcdefgh123/raw_content":
			if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer fixture-token" {
				t.Fatal("wrong read authorization")
			}
			body = `{"code":0,"data":{"content":"准备资料\n确认时间\n记录结果"}}`
		default:
			t.Fatal("unexpected SDK operation")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	text, err := f.Read(context.Background(), FeishuApp{"cli_fixture", "test-only-app-secret"}, "abcdefgh123")
	if err != nil || !strings.Contains(text, "准备资料") || calls != 2 {
		t.Fatal("authorized document read failed", err, calls)
	}
}
func TestDocumentLinksCannotChooseCredentialDestination(t *testing.T) {
	for _, raw := range []string{"http://corp.feishu.cn/docx/abcdefgh", "https://corp.feishu.cn.evil.example/docx/abcdefgh", "https://user@corp.feishu.cn/docx/abcdefgh", "https://corp.feishu.cn:443/docx/abcdefgh", "https://127.0.0.1/docx/abcdefgh", "https://corp.feishu.cn/wiki/abcdefgh", "https://corp.feishu.cn/docx/abc%2Fdefgh"} {
		if _, err := FeishuDocumentID(raw); err == nil {
			t.Fatal("unsafe document link accepted")
		}
	}
	if id, err := FeishuDocumentID("https://corp.feishu.cn/docx/abcdefgh?from=share"); err != nil || id != "abcdefgh" {
		t.Fatal("valid docx handoff rejected")
	}
}
