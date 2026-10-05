package browserreader

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestBrowserRendersScriptAndBlocksWritesAndPrivateRequests(t *testing.T) {
	if os.Getenv("NEMI_BROWSER_TEST") != "1" {
		t.Skip("installed Chromium required")
	}
	r := New()
	r.Worker = filepath.Join("..", "..", "scripts", "browser-reader.mjs")
	var calls atomic.Int32
	r.Client.Transport = fixtureTransport(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		if req.Method != "GET" || req.URL.String() != "https://fixture.nemi.test/page" || req.Header.Get("Cookie") != "" {
			t.Error("browser bypassed read-only gateway")
		}
		body := `<!doctype html><title>Fixture</title><main id="result">waiting</main><script>document.getElementById('result').textContent='JavaScript 已渲染';fetch('https://127.0.0.1/secret').catch(()=>{});fetch('https://fixture.nemi.test/write',{method:'POST',body:'never-send'}).catch(()=>{});</script><a href="https://fixture.nemi.test/next">下一页</a><a href="https://127.0.0.1/">不返回的内网链接</a>`
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	p, err := r.Read(context.Background(), "https://fixture.nemi.test/page")
	if err != nil {
		t.Fatalf("renderer failed: %v", err)
	}
	if !strings.Contains(p.Text, "JavaScript 已渲染") || len(p.Links) != 1 || calls.Load() != 1 {
		t.Fatalf("rendering or network policy failed: rendered=%t links=%d calls=%d", strings.Contains(p.Text, "JavaScript 已渲染"), len(p.Links), calls.Load())
	}
	if _, err = r.Read(context.Background(), "https://127.0.0.1/"); err == nil {
		t.Fatal("private browser destination accepted")
	}
	r.Client.Transport = fixtureTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Header: http.Header{"Content-Type": []string{"text/plain"}}, Body: io.NopCloser(strings.NewReader("unavailable")), Request: req}, nil
	})
	if _, err = r.Read(context.Background(), "https://fixture.nemi.test/unavailable"); err == nil || err.Error() != "BROWSER_READ_FAILED" {
		t.Fatalf("page failure must not imply a missing browser: %v", err)
	}
}
