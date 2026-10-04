package webreader

import (
	"context"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestPublicURLBoundaries(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.1.2.3", "169.254.169.254", "100.64.0.1", "198.18.0.2", "::1", "::ffff:127.0.0.1", "fc00::1", "2001:db8::1", "2002::1", "3fff::1"} {
		if publicIP(netip.MustParseAddr(raw)) {
			t.Fatal("allowed private/reserved", raw)
		}
	}
	for _, raw := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if !publicIP(netip.MustParseAddr(raw)) {
			t.Fatal("blocked public", raw)
		}
	}
	for _, raw := range []string{"http://example.com", "https://user:password@example.com", "https://example.com:8080", "https://example.com/?api_key=secret"} {
		u, _ := url.Parse(raw)
		if validURL(u) {
			t.Fatal("accepted unsafe URL")
		}
	}
	for _, raw := range []string{"https://127.0.0.1", "https://[::1]"} {
		if _, err := New().Read(context.Background(), raw); err == nil {
			t.Fatal("allowed loopback request")
		}
	}
	if _, err := chooseIP(context.Background(), "localhost"); err == nil {
		t.Fatal("allowed local DNS")
	}
	r := New()
	u, _ := url.Parse("http://example.com")
	if r.client.CheckRedirect(&http.Request{URL: u}, nil) == nil {
		t.Fatal("allowed insecure redirect")
	}
}
func TestBodyExtractionAndBounds(t *testing.T) {
	r := New()
	body := `<html><head><title>真实资料</title></head><body><article><h1>真实资料</h1><p>` + strings.Repeat("正文内容，供用户阅读。", 100) + `</p><script>secret-script</script></article></body></html>`
	r.client.Transport = roundTrip(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	p, err := r.Read(context.Background(), "https://example.com/article")
	if err != nil || !strings.Contains(p.Text, "正文内容") || strings.Contains(p.Text, "secret-script") || p.URL != "https://example.com/article" {
		t.Fatal(err, p)
	}
	body = strings.Repeat("x", (1<<20)+1)
	if _, err = r.Read(context.Background(), "https://example.com/article"); err == nil {
		t.Fatal("accepted oversized response")
	}
}
