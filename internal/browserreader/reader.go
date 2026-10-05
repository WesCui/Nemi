// Package browserreader renders anonymous public HTTPS pages on the runtime
// server. It provides no account cookies, arbitrary script or write operations.
package browserreader

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"nemi/internal/webreader"
)

type Link struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}
type Page struct {
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	Text      string    `json:"text"`
	Links     []Link    `json:"links"`
	Truncated bool      `json:"truncated"`
	FetchedAt time.Time `json:"fetched_at"`
}
type Reader struct {
	Client *http.Client
	Worker string
	Node   string
}

var slots = make(chan struct{}, 2)

func New() *Reader {
	c := webreader.New().Client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Reader{Client: c, Worker: filepath.Join("scripts", "browser-reader.mjs"), Node: "node"}
}
func Available() bool {
	if os.Getenv("APP_BROWSER_ENABLED") != "true" {
		return false
	}
	_, e := exec.LookPath("node")
	if e != nil {
		return false
	}
	_, e = os.Stat(filepath.Join("scripts", "browser-reader.mjs"))
	return e == nil
}

type output struct{ bytes.Buffer }

func (b *output) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 100000 {
		return 0, errors.New("BROWSER_TOO_LARGE")
	}
	return b.Buffer.Write(p)
}
func (r *Reader) Read(ctx context.Context, raw string) (Page, error) {
	if webreader.CheckURL(raw) != nil {
		return Page{}, errors.New("BROWSER_URL_NOT_PUBLIC")
	}
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	case <-ctx.Done():
		return Page{}, errors.New("BROWSER_UNAVAILABLE")
	}
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	nonce := make([]byte, 32)
	if _, e := rand.Read(nonce); e != nil {
		return Page{}, errors.New("BROWSER_UNAVAILABLE")
	}
	token := hex.EncodeToString(nonce)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Page{}, errors.New("BROWSER_UNAVAILABLE")
	}
	var mu sync.Mutex
	count, total := 0, 0
	server := &http.Server{ReadHeaderTimeout: 2 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != "POST" || req.URL.Path != "/fetch" || req.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "denied", 403)
			return
		}
		var p struct {
			URL    string `json:"url"`
			Method string `json:"method"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, req.Body, 4096)).Decode(&p) != nil || (p.Method != "GET" && p.Method != "HEAD") || webreader.CheckURL(p.URL) != nil {
			http.Error(w, "denied", 403)
			return
		}
		mu.Lock()
		count++
		allowed := count <= 60 && total < 8<<20
		mu.Unlock()
		if !allowed {
			http.Error(w, "limit", 429)
			return
		}
		// Browser headers, cookies and authentication are deliberately discarded.
		request, e := http.NewRequestWithContext(ctx, p.Method, p.URL, nil)
		if e != nil {
			http.Error(w, "denied", 403)
			return
		}
		request.Header.Set("User-Agent", "Nemi/0.12 (public browser reader)")
		resp, e := r.Client.Do(request)
		if e != nil {
			http.Error(w, "unavailable", 502)
			return
		}
		defer resp.Body.Close()
		contentType := strings.ToLower(resp.Header.Get("Content-Type"))
		if !strings.Contains(contentType, "text/") && !strings.Contains(contentType, "javascript") && !strings.Contains(contentType, "json") && resp.StatusCode < 300 {
			http.Error(w, "unsupported", 415)
			return
		}
		b, e := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
		if e != nil || len(b) > 1<<20 {
			http.Error(w, "limit", 413)
			return
		}
		mu.Lock()
		total += len(b)
		allowed = total <= 8<<20
		mu.Unlock()
		if !allowed {
			http.Error(w, "limit", 413)
			return
		}
		headers := map[string]string{"content-type": contentType}
		if location := resp.Header.Get("Location"); location != "" {
			u, e := resp.Request.URL.Parse(location)
			if e != nil || webreader.CheckURL(u.String()) != nil {
				http.Error(w, "denied", 403)
				return
			}
			headers["location"] = u.String()
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": resp.StatusCode, "headers": headers, "body": b})
	})}
	go server.Serve(listener)
	defer server.Close()
	worker, err := filepath.Abs(r.Worker)
	if err != nil {
		return Page{}, errors.New("BROWSER_UNAVAILABLE")
	}
	cmd := exec.CommandContext(ctx, r.Node, worker)
	// The renderer receives no model/service keys or application environment.
	for _, k := range []string{"PATH", "SystemRoot", "LOCALAPPDATA", "USERPROFILE", "PROGRAMFILES", "PROGRAMFILES(X86)", "PROGRAMW6432", "TEMP", "TMP", "HOME", "PLAYWRIGHT_BROWSERS_PATH"} {
		if v := os.Getenv(k); v != "" {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	cmd.Cancel = func() error {
		if runtime.GOOS == "windows" {
			return exec.Command("taskkill.exe", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F").Run()
		}
		return cmd.Process.Kill()
	}
	cmd.WaitDelay = 3 * time.Second
	input, _ := json.Marshal(map[string]string{"url": raw, "gateway": "http://" + listener.Addr().String() + "/fetch", "token": token})
	cmd.Stdin = bytes.NewReader(input)
	var out output
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) || ctx.Err() != nil {
			return Page{}, errors.New("BROWSER_READ_FAILED")
		}
		return Page{}, errors.New("BROWSER_UNAVAILABLE")
	}
	var p Page
	if json.Unmarshal(out.Bytes(), &p) != nil || webreader.CheckURL(p.URL) != nil || len(p.Text) > 80000 || strings.TrimSpace(p.Text) == "" {
		return Page{}, errors.New("BROWSER_READ_FAILED")
	}
	links := []Link{}
	for _, l := range p.Links {
		if webreader.CheckURL(l.URL) == nil && len(l.URL) <= 800 {
			links = append(links, l)
			if len(links) == 5 {
				break
			}
		}
	}
	p.Links = links
	p.FetchedAt = time.Now().UTC()
	return p, nil
}
