// Package webreader fetches passive public pages, without cookies, login,
// proxies, browser scripts, or access to internal networks.
package webreader

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	readability "codeberg.org/readeck/go-readability/v2"
	"golang.org/x/net/html/charset"
)

type Page struct {
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	Text      string    `json:"text"`
	FetchedAt time.Time `json:"fetched_at"`
}
type Reader struct{ client *http.Client }

// Client returns a new reader's public-IP-pinned transport for trusted service
// endpoints. Callers still control method, URL, headers and response bounds.
func (r *Reader) Client() *http.Client { return r.client }
func CheckURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || !validURL(u) {
		return errors.New("WEB_URL_NOT_PUBLIC")
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !publicIP(ip) {
		return errors.New("WEB_URL_NOT_PUBLIC")
	}
	return nil
}
func DialPublic(ctx context.Context, host, port string) (net.Conn, error) {
	if port != "443" && port != "993" {
		return nil, errors.New("WEB_URL_NOT_PUBLIC")
	}
	ip, err := chooseIP(ctx, host)
	if err != nil {
		return nil, err
	}
	return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port))
}

var denied = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("172.16.0.0/12"), netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("192.168.0.0/16"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("224.0.0.0/3"), netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("3fff::/20"),
}

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, p := range denied {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}
func validURL(u *url.URL) bool {
	if u == nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || (u.Port() != "" && u.Port() != "443") || len(u.String()) > 2048 {
		return false
	}
	for k := range u.Query() {
		switch strings.ToLower(k) {
		case "key", "token", "api_key", "access_token", "password", "secret", "signature":
			return false
		}
	}
	return true
}
func chooseIP(ctx context.Context, host string) (netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		if !publicIP(ip) {
			return netip.Addr{}, errors.New("WEB_URL_NOT_PUBLIC")
		}
		return ip, nil
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return netip.Addr{}, errors.New("WEB_READ_FAILED")
	}
	return resolveAddresses(ctx, host, ips, trustedDNS)
}
func New() *Reader {
	t := &http.Transport{Proxy: nil, DisableKeepAlives: true, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 8 * time.Second, MaxResponseHeaderBytes: 32 << 10}
	t.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || port != "443" {
			return nil, errors.New("WEB_URL_NOT_PUBLIC")
		}
		ip, err := chooseIP(ctx, host)
		if err != nil {
			return nil, err
		}
		// Dial the address that was checked, preserving the original host for TLS SNI.
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
	}
	return &Reader{client: &http.Client{Transport: t, Timeout: 12 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 || !validURL(req.URL) {
			return errors.New("WEB_URL_NOT_PUBLIC")
		}
		return nil
	}}}
}

type boundedText struct{ bytes.Buffer }

func (b *boundedText) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 256<<10 {
		return 0, errors.New("WEB_TOO_LARGE")
	}
	return b.Buffer.Write(p)
}
func (r *Reader) Read(ctx context.Context, raw string) (Page, error) {
	u, err := url.Parse(raw)
	if err != nil || !validURL(u) {
		return Page{}, errors.New("WEB_URL_NOT_PUBLIC")
	}
	u.Fragment = ""
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !publicIP(ip) {
		return Page{}, errors.New("WEB_URL_NOT_PUBLIC")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Page{}, errors.New("WEB_URL_NOT_PUBLIC")
	}
	req.Header.Set("User-Agent", "Nemi/0.8 (public document reader)")
	resp, err := r.client.Do(req)
	if err != nil {
		return Page{}, errors.New("WEB_READ_FAILED")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Page{}, errors.New("WEB_READ_FAILED")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return Page{}, errors.New("WEB_READ_FAILED")
	}
	if len(b) > 1<<20 {
		return Page{}, errors.New("WEB_TOO_LARGE")
	}
	typeHeader := strings.ToLower(resp.Header.Get("Content-Type"))
	p := Page{URL: resp.Request.URL.String(), FetchedAt: time.Now().UTC()}
	if strings.HasPrefix(typeHeader, "text/plain") {
		src, err := charset.NewReader(bytes.NewReader(b), typeHeader)
		if err != nil {
			return Page{}, errors.New("WEB_READ_FAILED")
		}
		text, err := io.ReadAll(io.LimitReader(src, (256<<10)+1))
		if err != nil || len(text) > 256<<10 {
			return Page{}, errors.New("WEB_TOO_LARGE")
		}
		p.Text = string(text)
		p.Title = u.Hostname()
	} else if strings.Contains(typeHeader, "text/html") || strings.Contains(typeHeader, "application/xhtml+xml") {
		src, err := charset.NewReader(bytes.NewReader(b), typeHeader)
		if err != nil {
			return Page{}, errors.New("WEB_READ_FAILED")
		}
		decoded, err := io.ReadAll(io.LimitReader(src, (2<<20)+1))
		if err != nil || len(decoded) > 2<<20 {
			return Page{}, errors.New("WEB_TOO_LARGE")
		}
		article, err := readability.FromReader(bytes.NewReader(decoded), resp.Request.URL)
		if err != nil {
			return Page{}, errors.New("WEB_READ_FAILED")
		}
		var out boundedText
		if article.RenderText(&out) != nil {
			return Page{}, errors.New("WEB_TOO_LARGE")
		}
		p.Text = out.String()
		p.Title = article.Title()
	} else {
		return Page{}, errors.New("WEB_FORMAT_UNSUPPORTED")
	}
	if strings.TrimSpace(p.Text) == "" {
		return Page{}, errors.New("WEB_EMPTY_OR_REQUIRES_BROWSER")
	}
	return p, nil
}
