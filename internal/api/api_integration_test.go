package api

import (
	"bytes"
	"context"
	"encoding/json"
	"nemi/internal/config"
	"nemi/internal/model"
	"nemi/internal/store"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestAuthenticatedConfirmationAndOrigin(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	if !strings.Contains(url, "nemi_test") {
		t.Fatal("dedicated test database required")
	}
	ctx := context.Background()
	s, e := store.Open(ctx, url)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Pool.Close()
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	code := "integration-private-invite-code"
	if e = s.Bootstrap(ctx, code); e != nil {
		t.Fatal(e)
	}
	c := config.Config{Provider: "demo", Origin: "http://localhost:3000"}
	a := &API{Store: s, Hub: NewHub(s), Config: c, Gateway: model.New(c)}
	handler := a.Handler()
	request := func(method, path, body, origin string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "test-key-123456")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/api/v1/dashboard", "", "", nil); w.Code != 401 {
		t.Fatal("unauthenticated access accepted")
	}
	w := request("POST", "/api/v1/auth/login", `{"invite_code":"`+code+`"}`, c.Origin, nil)
	if w.Code != 200 {
		t.Fatalf("login status %d", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe session cookie")
	}
	cookie := cookies[0]
	body := `{"title":"准备材料","source":"本人提供资料","category":"life","timezone":"Asia/Shanghai","confirmed":false}`
	if w = request("POST", "/api/v1/matters", body, c.Origin, cookie); w.Code != 400 {
		t.Fatal("unconfirmed input saved")
	}
	body = strings.Replace(body, `"confirmed":false`, `"confirmed":true`, 1)
	if w = request("POST", "/api/v1/matters", body, "https://untrusted.example", cookie); w.Code != 403 {
		t.Fatal("cross-origin command accepted")
	}
	body = body[:len(body)-1] + `,"workspace_id":"another-user"}`
	if w = request("POST", "/api/v1/matters", body, c.Origin, cookie); w.Code != 400 {
		t.Fatal("client workspace assertion accepted")
	}
	w = request("GET", "/api/v1/dashboard", "", "", cookie)
	var dashboard store.Dashboard
	if e = json.Unmarshal(w.Body.Bytes(), &dashboard); e != nil {
		t.Fatal(e)
	}
	if dashboard.User.Workspace != "" {
		t.Fatal("internal workspace exposed")
	}
}
