package webreader

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// Some local VPNs return benchmark-range fake addresses for every public name.
// Only that specific case uses a fixed, TLS-verified public DNS service. All
// destination addresses still pass the same public-IP policy before dialing.
func resolveAddresses(ctx context.Context, host string, ips []netip.Addr, fallback func(context.Context, string) ([]netip.Addr, error)) (netip.Addr, error) {
	fakeOnly := len(ips) > 0
	for _, ip := range ips {
		if !netip.MustParsePrefix("198.18.0.0/15").Contains(ip.Unmap()) {
			fakeOnly = false
		}
	}
	if fakeOnly {
		var err error
		ips, err = fallback(ctx, host)
		if err != nil {
			return netip.Addr{}, errors.New("WEB_DNS_UNAVAILABLE")
		}
	}
	if len(ips) == 0 {
		return netip.Addr{}, errors.New("WEB_DNS_UNAVAILABLE")
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return netip.Addr{}, errors.New("WEB_URL_NOT_PUBLIC")
		}
	}
	return ips[0].Unmap(), nil
}
func trustedDNS(ctx context.Context, host string) ([]netip.Addr, error) {
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 3 * time.Second, ForceAttemptHTTP2: true}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, "223.5.5.5:443")
	}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return dnsQuery(ctx, client, host)
}
func dnsQuery(ctx context.Context, client *http.Client, host string) ([]netip.Addr, error) {
	name, err := dnsmessage.NewName(strings.TrimSuffix(host, ".") + ".")
	if err != nil {
		return nil, errors.New("WEB_DNS_UNAVAILABLE")
	}
	query := dnsmessage.Message{Header: dnsmessage.Header{ID: 1, RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}}
	body, err := query.Pack()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://dns.alidns.com/dns-query", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/dns-message") {
		return nil, errors.New("WEB_DNS_UNAVAILABLE")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 12001))
	if err != nil || len(data) > 12000 {
		return nil, errors.New("WEB_DNS_UNAVAILABLE")
	}
	var answer dnsmessage.Message
	if answer.Unpack(data) != nil || answer.ID != 1 || !answer.Response || answer.Truncated || answer.RCode != dnsmessage.RCodeSuccess || len(answer.Questions) != 1 {
		return nil, errors.New("WEB_DNS_UNAVAILABLE")
	}
	q := answer.Questions[0]
	if !strings.EqualFold(q.Name.String(), name.String()) || q.Type != dnsmessage.TypeA || q.Class != dnsmessage.ClassINET {
		return nil, errors.New("WEB_DNS_UNAVAILABLE")
	}
	allowed := map[string]bool{strings.ToLower(name.String()): true}
	for i := 0; i < 8; i++ {
		for _, r := range answer.Answers {
			if c, ok := r.Body.(*dnsmessage.CNAMEResource); ok && allowed[strings.ToLower(r.Header.Name.String())] {
				allowed[strings.ToLower(c.CNAME.String())] = true
			}
		}
	}
	ips := []netip.Addr{}
	for _, r := range answer.Answers {
		if a, ok := r.Body.(*dnsmessage.AResource); ok && r.Header.Class == dnsmessage.ClassINET && allowed[strings.ToLower(r.Header.Name.String())] {
			ips = append(ips, netip.AddrFrom4(a.A))
		}
	}
	if len(ips) == 0 {
		return nil, errors.New("WEB_DNS_UNAVAILABLE")
	}
	return ips, nil
}
