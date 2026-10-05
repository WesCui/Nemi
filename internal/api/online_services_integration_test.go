package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"nemi/internal/config"
	"nemi/internal/connectors"
	"nemi/internal/domain"
	"nemi/internal/model"
	"nemi/internal/store"
	"nemi/internal/vault"
)

func TestOnlineCredentialsRequireConsentAreScopedEncryptedAndRevisioned(t *testing.T) {
	db := os.Getenv("TEST_DATABASE_URL")
	if db == "" {
		t.Skip("dedicated test database required")
	}
	if !strings.Contains(db, "/nemi_test?") {
		t.Fatal("dedicated database required")
	}
	ctx := context.Background()
	st, err := store.Open(ctx, db)
	if err != nil {
		t.Fatal("test database unavailable")
	}
	defer st.Pool.Close()
	if st.Migrate(ctx) != nil {
		t.Fatal("test schema unavailable")
	}
	ws := "online-api-" + domain.ID()
	code := domain.ID()
	if st.Bootstrap(ctx, code, ws) != nil {
		t.Fatal("test workspace unavailable")
	}
	token, _ := st.Login(ctx, code)
	v, _ := vault.New(bytes.Repeat([]byte{74}, 32))
	c := config.Config{Origin: "http://localhost:3000"}
	a := &API{Store: st, Config: c, Gateway: model.New(c), Vault: v}
	handler := a.Handler()
	request := func(method, path, body, key string, authenticated bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", c.Origin)
		r.Header.Set("Idempotency-Key", key)
		if authenticated {
			r.AddCookie(&http.Cookie{Name: "nemi_session", Value: token})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	secret := "fixture-online-key-not-live"
	for _, id := range []string{"search", "amap", "mail"} {
		p := map[string]any{"label": "测试连接", "enabled": true, "expected_revision": 0, "confirmed": false}
		if id == "mail" {
			p["email"] = "fixture@qq.com"
			p["authorization_code"] = secret
		} else {
			p["api_key"] = secret
		}
		path := "/api/v1/connections/services/" + id
		b, _ := json.Marshal(p)
		if request("PUT", path, string(b), domain.ID(), true).Code != 400 {
			t.Fatal("consent was optional")
		}
		p["confirmed"] = true
		b, _ = json.Marshal(p)
		key := domain.ID()
		if request("PUT", path, string(b), key, false).Code != 401 {
			t.Fatal("unauthenticated credential save")
		}
		r := request("PUT", path, string(b), key, true)
		if r.Code != 200 || strings.Contains(r.Body.String(), secret) {
			t.Fatal("credential save failed or leaked")
		}
		if request("PUT", path, string(b), key, true).Code != 200 {
			t.Fatal("idempotent save failed")
		}
		saved, err := st.Connection(ctx, ws, id)
		if err != nil || saved.Revision != 1 || bytes.Contains(saved.Credential, []byte(secret)) {
			t.Fatal("credential was duplicated or unencrypted")
		}
		plain, e := v.Reveal(ws+":app:"+id, saved.Credential)
		if e != nil || !bytes.Contains(plain, []byte(secret)) {
			t.Fatal("encrypted credential cannot be used")
		}
		if _, e = v.Reveal("other-workspace:app:"+id, saved.Credential); e == nil {
			t.Fatal("credential crossed workspace")
		}
		if r = request("GET", path, "", "", true); r.Code != 200 || strings.Contains(r.Body.String(), secret) || strings.Contains(r.Body.String(), "fixture@qq.com") {
			t.Fatal("state exposed private credentials")
		}
		if request("PUT", path, string(b), domain.ID(), true).Code != 409 {
			t.Fatal("stale revision accepted")
		}
		p["expected_revision"] = 1
		b, _ = json.Marshal(p)
		if request("PUT", path, string(b), domain.ID(), true).Code != 200 {
			t.Fatal("replacement failed")
		}
	}
	apps := request("GET", "/api/v1/applications", "", "", true)
	if apps.Code != 200 || strings.Contains(apps.Body.String(), secret) {
		t.Fatal("catalog leaked credentials")
	}
	var catalog struct {
		Applications []connectors.Application `json:"applications"`
	}
	json.Unmarshal(apps.Body.Bytes(), &catalog)
	for _, app := range catalog.Applications {
		if connectors.ServiceID(app.ID) && (app.State != "configured" || app.Verified) {
			t.Fatal("configured means live verified")
		}
		if app.ID == "wechat" && app.State != "planned" {
			t.Fatal("WeChat falsely connected")
		}
	}
	bad := `{"label":"不支持的邮箱","expected_revision":2,"enabled":true,"confirmed":true,"email":"user@evil.example","authorization_code":"fixture-code"}`
	if request("PUT", "/api/v1/connections/services/mail", bad, domain.ID(), true).Code != 400 {
		t.Fatal("arbitrary mail endpoint accepted")
	}
}
