package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"nemi/internal/config"
	"nemi/internal/connectors"
	"nemi/internal/domain"
	"nemi/internal/model"
	"nemi/internal/store"
)

type API struct {
	Store   *store.Store
	Hub     *Hub
	Config  config.Config
	Gateway *model.Gateway
	Bots    *connectors.Sender
	mu      sync.Mutex
	logins  map[string]loginWindow
}
type loginWindow struct {
	count int
	since time.Time
}
type identityKey struct{}

func (a *API) Handler() http.Handler {
	if a.Bots == nil {
		a.Bots = connectors.New(a.Config.Bots)
	}
	a.logins = map[string]loginWindow{}
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { send(w, 200, map[string]bool{"ok": true}) })
	m.HandleFunc("POST /api/v1/auth/login", a.login)
	m.Handle("POST /api/v1/auth/logout", a.auth(http.HandlerFunc(a.logout)))
	m.Handle("GET /api/v1/me", a.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		send(w, 200, map[string]any{"user": identity(r), "model_mode": a.Gateway.Mode()})
	})))
	m.Handle("GET /api/v1/dashboard", a.auth(http.HandlerFunc(a.dashboard)))
	m.Handle("GET /api/v1/connections", a.auth(http.HandlerFunc(a.connections)))
	m.Handle("POST /api/v1/connections/{id}/messages", a.auth(http.HandlerFunc(a.sendMessage)))
	m.Handle("GET /api/v1/matters/{id}/calendar", a.auth(http.HandlerFunc(a.calendar)))
	m.Handle("GET /api/v1/events", a.auth(http.HandlerFunc(a.events)))
	m.Handle("POST /api/v1/matters", a.auth(http.HandlerFunc(a.createMatter)))
	m.Handle("PATCH /api/v1/matters/{id}", a.auth(http.HandlerFunc(a.editMatter)))
	m.Handle("PUT /api/v1/matters/{id}/reminder", a.auth(http.HandlerFunc(a.saveReminder)))
	m.Handle("POST /api/v1/matters/{id}/runs", a.auth(http.HandlerFunc(a.createRun)))
	m.Handle("POST /api/v1/memories", a.auth(http.HandlerFunc(a.saveMemory)))
	m.Handle("PUT /api/v1/memories/{id}", a.auth(http.HandlerFunc(a.saveMemory)))
	m.Handle("DELETE /api/v1/memories/{id}", a.auth(http.HandlerFunc(a.deleteMemory)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != "GET" && r.Method != "HEAD" {
			if o := r.Header.Get("Origin"); o != "" && o != a.Config.Origin {
				sendError(w, 403, "请求来源不受信任")
				return
			}
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				sendError(w, 415, "请使用 JSON 请求")
				return
			}
		}
		if r.URL.Path != "/api/v1/events" {
			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			defer cancel()
			r = r.WithContext(ctx)
		}
		m.ServeHTTP(w, r)
	})
}
func identity(r *http.Request) store.Identity {
	return r.Context().Value(identityKey{}).(store.Identity)
}
func (a *API) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := r.Cookie("nemi_session")
		if e != nil || len(c.Value) != 64 {
			sendError(w, 401, "请先登录")
			return
		}
		i, e := a.Store.Authenticate(r.Context(), c.Value)
		if e != nil {
			if errors.Is(e, pgx.ErrNoRows) {
				sendError(w, 401, "登录已过期")
			} else {
				sendError(w, 503, "暂时无法验证登录，请稍后重试")
			}
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, i)))
	})
}
func (a *API) login(w http.ResponseWriter, r *http.Request) {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	a.mu.Lock()
	v := a.logins[ip]
	if time.Since(v.since) > 15*time.Minute {
		v = loginWindow{since: time.Now()}
	}
	v.count++
	a.logins[ip] = v
	// Bounded dev-only limiter; entries are removed after their window.
	for k, x := range a.logins {
		if time.Since(x.since) > 15*time.Minute {
			delete(a.logins, k)
		}
	}
	a.mu.Unlock()
	if v.count > 20 {
		sendError(w, 429, "尝试较多，请稍后再试")
		return
	}
	var b struct {
		Code string `json:"invite_code"`
	}
	if _, e := decode(w, r, &b); e != nil {
		sendError(w, 400, "请输入邀请口令")
		return
	}
	token, e := a.Store.Login(r.Context(), b.Code)
	if e != nil {
		if errors.Is(e, domain.ErrNotFound) {
			sendError(w, 401, "邀请口令不正确")
		} else {
			sendError(w, 503, "暂时无法登录")
		}
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "nemi_session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 7 * 24 * 3600})
	send(w, 200, map[string]bool{"ok": true})
}
func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	c, _ := r.Cookie("nemi_session")
	if e := a.Store.Logout(r.Context(), c.Value); e != nil {
		sendError(w, 503, "暂时无法退出，请重试")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "nemi_session", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	send(w, 200, map[string]bool{"ok": true})
}
func decode(w http.ResponseWriter, r *http.Request, v any) ([]byte, error) {
	b, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 20000))
	if e != nil {
		return nil, e
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if e = d.Decode(v); e != nil {
		return nil, e
	}
	var extra any
	if e = d.Decode(&extra); e != io.EOF {
		return nil, errors.New("trailing JSON")
	}
	return b, nil
}
func (a *API) command(w http.ResponseWriter, r *http.Request, b []byte, fn func(pgx.Tx) (any, int, error)) {
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 8 || len(key) > 100 {
		sendError(w, 400, "缺少有效的请求标识")
		return
	}
	v, e := a.Store.Command(r.Context(), identity(r).Workspace, key, r.Method+" "+r.URL.Path, b, fn)
	if e != nil {
		switch {
		case errors.Is(e, domain.ErrConflict):
			sendError(w, 409, "内容已更新或请求标识重复，请刷新后重试")
		case errors.Is(e, domain.ErrNotFound):
			sendError(w, 404, "找不到这项事项")
		case errors.Is(e, domain.ErrBusy):
			sendError(w, 409, "这项事项已经在整理中")
		case errors.Is(e, domain.ErrMemoryLimit):
			sendError(w, 409, "最多保存 50 条偏好，请先整理已有内容")
		default:
			slog.Error("business command failed")
			sendError(w, 503, "暂时无法保存，请稍后重试")
		}
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(v.Status)
	w.Write(v.Body)
}
func (a *API) createMatter(w http.ResponseWriter, r *http.Request) {
	var c domain.CreateMatter
	b, e := decode(w, r, &c)
	if e != nil {
		sendError(w, 400, "事项格式不正确")
		return
	}
	if e = c.Validate(time.Now()); e != nil {
		sendError(w, 400, e.Error())
		return
	}
	a.command(w, r, b, func(tx pgx.Tx) (any, int, error) {
		return a.Store.CreateMatter(r.Context(), tx, identity(r).Workspace, c)
	})
}
func (a *API) editMatter(w http.ResponseWriter, r *http.Request) {
	var p store.EditMatter
	b, e := decode(w, r, &p)
	if e != nil || p.Expected < 1 {
		sendError(w, 400, "事项版本不正确")
		return
	}
	if p.Title != nil {
		*p.Title = strings.TrimSpace(*p.Title)
		if len([]rune(*p.Title)) < 1 || len([]rune(*p.Title)) > 100 {
			sendError(w, 400, "标题应为 1–100 字")
			return
		}
	}
	if p.Source != nil {
		*p.Source = strings.TrimSpace(*p.Source)
		if len(*p.Source) > 12000 {
			sendError(w, 400, "资料不超过 12000 字节")
			return
		}
	}
	if p.Status != nil && *p.Status != "ACTIVE" && *p.Status != "COMPLETED" {
		sendError(w, 400, "事项状态不正确")
		return
	}
	if p.Items != nil {
		if len(*p.Items) > 60 {
			sendError(w, 400, "清单过长")
			return
		}
		for _, item := range *p.Items {
			if strings.TrimSpace(item.Text) == "" || len([]rune(item.Text)) > 150 {
				sendError(w, 400, "清单项应为 1–150 字")
				return
			}
		}
	}
	a.command(w, r, b, func(tx pgx.Tx) (any, int, error) {
		return a.Store.EditMatter(r.Context(), tx, identity(r).Workspace, r.PathValue("id"), p)
	})
}
func (a *API) saveReminder(w http.ResponseWriter, r *http.Request) {
	var p store.SaveReminder
	b, e := decode(w, r, &p)
	if e != nil || p.Expected < 0 || !p.Confirmed || p.Timezone != "Asia/Shanghai" || p.At == nil {
		sendError(w, 400, "请确认提醒的日期和时间")
		return
	}
	if p.Enabled && (!p.At.After(time.Now()) || p.At.After(time.Now().AddDate(2, 0, 0))) {
		sendError(w, 400, "提醒日期应在未来两年内")
		return
	}
	p.Repeat = domain.NormalizeRepeat(p.Repeat)
	if p.Repeat != "once" && p.Repeat != "daily" && p.Repeat != "weekdays" && p.Repeat != "weekly" {
		sendError(w, 400, "不支持的重复频率")
		return
	}
	if p.Enabled {
		if e = domain.ValidateRecurrence(*p.At, p.Until, p.Repeat, p.Quiet, nil, time.Now()); e != nil {
			sendError(w, 400, e.Error())
			return
		}
	}
	a.command(w, r, b, func(tx pgx.Tx) (any, int, error) {
		return a.Store.SaveReminder(r.Context(), tx, identity(r).Workspace, r.PathValue("id"), p)
	})
}
func (a *API) createRun(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Expected  int  `json:"expected_revision"`
		UseMemory bool `json:"use_memory"`
	}
	b, e := decode(w, r, &p)
	if e != nil || p.Expected < 1 {
		sendError(w, 400, "事项版本不正确")
		return
	}
	a.command(w, r, b, func(tx pgx.Tx) (any, int, error) {
		return a.Store.CreateRun(r.Context(), tx, identity(r).Workspace, r.PathValue("id"), a.Gateway.Mode(), a.Gateway.Profile(), p.Expected, p.UseMemory)
	})
}
func (a *API) dashboard(w http.ResponseWriter, r *http.Request) {
	d, e := a.Store.Dashboard(r.Context(), identity(r), a.Gateway.Mode())
	if e != nil {
		sendError(w, 503, "暂时无法读取事项")
		return
	}
	send(w, 200, d)
}
func (a *API) events(w http.ResponseWriter, r *http.Request) {
	last, _ := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64)
	if last < 0 {
		last = 0
	}
	sub, unsubscribe := a.Hub.subscribe(identity(r).Workspace)
	defer unsubscribe()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	rc := http.NewResponseController(w)
	write := func(ev domain.Event) bool {
		if ev.Sequence <= last {
			return true
		}
		last = ev.Sequence
		b, _ := json.Marshal(ev)
		rc.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, e := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", ev.Sequence, b); e != nil {
			return false
		}
		return rc.Flush() == nil
	}
	for {
		es, e := a.Store.Events(r.Context(), identity(r).Workspace, last)
		if e != nil {
			return
		}
		for _, ev := range es {
			if !write(ev) {
				return
			}
		}
		if len(es) < 100 {
			break
		}
	}
	rc.Flush()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-sub.events:
			if !ok || !write(ev) {
				return
			}
		case <-ticker.C:
			c, _ := r.Cookie("nemi_session")
			if _, e := a.Store.Authenticate(r.Context(), c.Value); e != nil {
				return
			}
			rc.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if _, e := fmt.Fprint(w, ": heartbeat\n\n"); e != nil {
				return
			}
			if e := rc.Flush(); e != nil {
				return
			}
		}
	}
}
func send(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func sendError(w http.ResponseWriter, status int, message string) {
	send(w, status, map[string]string{"error": message})
}
