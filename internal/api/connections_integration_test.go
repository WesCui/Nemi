package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	ics "github.com/arran4/golang-ical"
	"io"
	"nemi/internal/config"
	"nemi/internal/connectors"
	"nemi/internal/domain"
	"nemi/internal/model"
	"nemi/internal/store"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type botTransport func(*http.Request) (*http.Response, error)

func (f botTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestConnectorConfirmationAndCalendarAPI(t *testing.T) {
	db := os.Getenv("TEST_DATABASE_URL")
	if db == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}
	if !strings.Contains(db, "nemi_test") {
		t.Fatal("dedicated database required")
	}
	ctx := context.Background()
	s, e := store.Open(ctx, db)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Pool.Close()
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	id := "api-test-" + domain.ID()
	code := "fixture-only-" + domain.ID()
	hash := sha256.Sum256([]byte(code))
	if _, e = s.Pool.Exec(ctx, "INSERT INTO workspaces(id) VALUES($1)", id); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, "INSERT INTO users(id,workspace_id,invite_hash,display_name) VALUES($1,$1,$2,'测试')", id, hash[:]); e != nil {
		t.Fatal(e)
	}
	token, e := s.Login(ctx, code)
	if e != nil {
		t.Fatal(e)
	}
	cookie := &http.Cookie{Name: "nemi_session", Value: token}
	c := config.Config{Provider: "demo", Origin: "http://localhost:3000"}
	sender := connectors.New(map[string]connectors.Bot{"feishu": {URL: "https://open.feishu.cn/open-apis/bot/v2/hook/test-credential", Label: "协议测试群"}})
	var calls atomic.Int32
	sender.Client.Transport = botTransport(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":0}`))}, nil
	})
	a := &API{Store: s, Config: c, Gateway: model.New(c), Bots: sender}
	handler := a.Handler()
	request := func(method, path, body, key, origin string, authenticated bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		r.Header.Set("Idempotency-Key", key)
		if authenticated {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/api/v1/connections", "/api/v1/matters/missing/calendar"} {
		if request("GET", path, "", "", "", false).Code != 401 {
			t.Fatal("unauthenticated app data exposed")
		}
	}
	w := request("GET", "/api/v1/connections", "", "", "", true)
	if w.Code != 200 || strings.Contains(w.Body.String(), "test-credential") {
		t.Fatal("status leaked credentials")
	}
	path := "/api/v1/connections/feishu/messages"
	body := `{"text":"确认后的清单","confirmed":true}`
	key := domain.ID()
	if request("POST", path, `{"text":"内容","confirmed":false}`, domain.ID(), c.Origin, true).Code != 400 {
		t.Fatal("unconfirmed message sent")
	}
	if request("POST", path, body, key, "https://untrusted.example", true).Code != 403 {
		t.Fatal("cross-origin send")
	}
	if request("POST", path, `{"text":"内容","confirmed":true,"webhook":"https://evil.example"}`, domain.ID(), c.Origin, true).Code != 400 {
		t.Fatal("user supplied webhook accepted")
	}
	tooLong, _ := json.Marshal(map[string]any{"text": strings.Repeat("字", 601), "confirmed": true})
	if request("POST", path, string(tooLong), domain.ID(), c.Origin, true).Code != 400 {
		t.Fatal("byte limit ignored")
	}
	if calls.Load() != 0 {
		t.Fatal("invalid input reached network")
	}
	for i := 0; i < 2; i++ {
		w = request("POST", path, body, key, c.Origin, true)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "DELIVERED") {
			t.Fatal("missing accepted receipt", w.Code)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("idempotent send repeated network")
	}
	if request("POST", "/api/v1/connections/wecom/messages", body, domain.ID(), c.Origin, true).Code != 409 {
		t.Fatal("unconfigured channel allowed")
	}
	matterID := domain.ID()
	at := time.Date(2026, 10, 10, 2, 0, 0, 0, time.UTC)
	if _, e = s.Pool.Exec(ctx, "INSERT INTO matters(workspace_id,id,title,source,category,status,deadline) VALUES($1,$2,$3,'private source','life','ACTIVE',$4)", id, matterID, "材料准备\nBEGIN:VEVENT\nSUMMARY:Injected", at); e != nil {
		t.Fatal(e)
	}
	w = request("GET", "/api/v1/matters/"+matterID+"/calendar", "", "", "", true)
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/calendar") || strings.Contains(w.Body.String(), "private source") {
		t.Fatal("invalid calendar response")
	}
	calendar, e := ics.ParseCalendar(strings.NewReader(w.Body.String()))
	if e != nil || len(calendar.Events()) != 1 {
		t.Fatal("calendar injection or invalid serialization", e)
	}
	start, e := calendar.Events()[0].GetStartAt()
	if e != nil || !start.Equal(at) {
		t.Fatal("calendar time changed", e)
	}
	if request("GET", "/api/v1/matters/missing/calendar", "", "", "", true).Code != 404 {
		t.Fatal("unknown calendar exported")
	}
}
