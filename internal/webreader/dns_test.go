package webreader

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/netip"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

func TestFakeDNSOnlyFallbackStillRejectsPrivateAndMixedAnswers(t *testing.T) {
	ctx := context.Background()
	calls := 0
	fallback := func(context.Context, string) ([]netip.Addr, error) {
		calls++
		return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
	}
	if ip, err := resolveAddresses(ctx, "example.com", []netip.Addr{netip.MustParseAddr("198.18.0.1")}, fallback); err != nil || ip.String() != "1.1.1.1" || calls != 1 {
		t.Fatal(err, calls)
	}
	for _, ips := range [][]netip.Addr{{netip.MustParseAddr("127.0.0.1")}, {netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("198.18.0.1")}} {
		if _, err := resolveAddresses(ctx, "example.com", ips, fallback); err == nil {
			t.Fatal("private/mixed answer accepted")
		}
	}
	if calls != 1 {
		t.Fatal("fallback used for private DNS")
	}
	if _, err := resolveAddresses(ctx, "example.com", []netip.Addr{netip.MustParseAddr("198.18.0.1")}, func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("10.0.0.1")}, nil
	}); err == nil {
		t.Fatal("private trusted DNS answer accepted")
	}
}
func TestDoHQuestionValidation(t *testing.T) {
	wrong := false
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		data, _ := io.ReadAll(r.Body)
		var m dnsmessage.Message
		if m.Unpack(data) != nil {
			t.Fatal("invalid wire query")
		}
		m.Response = true
		if wrong {
			m.Questions[0].Name, _ = dnsmessage.NewName("other.example.")
		}
		m.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: m.Questions[0].Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.AResource{A: [4]byte{1, 1, 1, 1}}}}
		data, _ = m.Pack()
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/dns-message"}}, Body: io.NopCloser(bytes.NewReader(data)), Request: r}, nil
	})}
	ips, err := dnsQuery(context.Background(), client, "example.com")
	if err != nil || len(ips) != 1 {
		t.Fatal(err)
	}
	wrong = true
	if _, err = dnsQuery(context.Background(), client, "example.com"); err == nil {
		t.Fatal("wrong DNS question accepted")
	}
}
