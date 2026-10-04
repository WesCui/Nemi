package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"nemi/internal/config"
	"nemi/internal/connectors"
	"nemi/internal/domain"
	"nemi/internal/model"
	"nemi/internal/store"
	"nemi/internal/vault"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestPersonalCredentialAPIPrivacyAndConfirmedIntegration(t *testing.T) {
	db := os.Getenv("TEST_DATABASE_URL")
	if db == "" {
		t.Skip("dedicated test database not configured")
	}
	if !strings.Contains(db, "/nemi_test?") {
		t.Fatal("dedicated database required")
	}
	ctx := context.Background()
	s, err := store.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Pool.Close()
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	ws := "settings-api-" + domain.ID()
	code := "fixture-" + domain.ID()
	hash := sha256.Sum256([]byte(code))
	if _, err = s.Pool.Exec(ctx, "INSERT INTO workspaces(id) VALUES($1)", ws); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, "INSERT INTO users(id,workspace_id,invite_hash,display_name) VALUES($1,$1,$2,'测试')", ws, hash[:]); err != nil {
		t.Fatal(err)
	}
	token, err := s.Login(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := vault.New(bytes.Repeat([]byte{23}, 32))
	c := config.Config{Provider: "", Origin: "http://localhost:3000"}
	sender := connectors.New(nil)
	sends := 0
	sender.Client.Transport = botTransport(func(r *http.Request) (*http.Response, error) {
		sends++
		if r.URL.Host != "qyapi.weixin.qq.com" {
			t.Error("wrong configured destination")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"errcode":0}`))}, nil
	})
	docs := connectors.NewFeishuDocuments()
	reads := 0
	docs.Client.Transport = botTransport(func(r *http.Request) (*http.Response, error) {
		reads++
		body := `{"code":0,"tenant_access_token":"fixture-token","expire":7200}`
		if r.Method == "GET" {
			body = `{"code":0,"data":{"content":"读取的文档资料"}}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	a := &API{Store: s, Config: c, Gateway: model.New(c), Vault: v, Bots: sender, Documents: docs}
	handler := a.Handler()
	request := func(method, path, body, key string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", c.Origin)
		r.Header.Set("Idempotency-Key", key)
		r.AddCookie(&http.Cookie{Name: "nemi_session", Value: token})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	body := `{"label":"个人测试模型","provider":"kimi","model":"kimi-k2.6","api_key":"fixture-personal-key","input_price_micro_cny":2000000,"output_price_micro_cny":4000000}`
	key := domain.ID()
	saved := request("POST", "/api/v1/models", body, key)
	if saved.Code != 201 || strings.Contains(saved.Body.String(), "fixture-personal-key") {
		t.Fatal("unsafe model save response", saved.Code)
	}
	var m store.PersonalModel
	json.Unmarshal(saved.Body.Bytes(), &m)
	replay := request("POST", "/api/v1/models", body, key)
	var replayed store.PersonalModel
	json.Unmarshal(replay.Body.Bytes(), &replayed)
	if replay.Code != 201 || replayed.ID != m.ID {
		t.Fatal("credential save retry duplicated model")
	}
	var encrypted []byte
	s.Pool.QueryRow(ctx, "SELECT credential FROM personal_models WHERE workspace_id=$1 AND id=$2", ws, m.ID).Scan(&encrypted)
	if bytes.Contains(encrypted, []byte("fixture-personal-key")) {
		t.Fatal("plaintext model credential in database")
	}
	list := request("GET", "/api/v1/models", "", "")
	if list.Code != 200 || strings.Contains(list.Body.String(), "fixture-personal-key") || strings.Contains(list.Body.String(), "credential") {
		t.Fatal("credential disclosed by list")
	}
	check := request("POST", "/api/v1/models/"+m.ID+"/check", `{"confirmed":false}`, domain.ID())
	if check.Code != 400 {
		t.Fatal("unconfirmed paid check accepted")
	}
	hook := `{"label":"协议验收群","webhook":"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=fixture-webhook-key","secret":"","expected_revision":0,"enabled":true}`
	if out := request("PUT", "/api/v1/connections/wecom", hook, domain.ID()); out.Code != 200 {
		t.Fatal("connection config failed", out.Code)
	}
	if sends != 0 {
		t.Fatal("saving credentials sent a message")
	}
	if out := request("POST", "/api/v1/connections/wecom/messages", `{"text":"确认内容","confirmed":true,"config_revision":0}`, domain.ID()); out.Code != 409 {
		t.Fatal("stale destination confirmation accepted")
	}
	key = domain.ID()
	message := `{"text":"确认内容","confirmed":true,"config_revision":1}`
	if out := request("POST", "/api/v1/connections/wecom/messages", message, key); out.Code != 200 || !strings.Contains(out.Body.String(), "DELIVERED") {
		t.Fatal("configured send failed", out.Code)
	}
	request("POST", "/api/v1/connections/wecom/messages", message, key)
	if sends != 1 {
		t.Fatal("message retry sent twice")
	}
	channels := request("GET", "/api/v1/connections", "", "")
	if strings.Contains(channels.Body.String(), "fixture-webhook-key") || !strings.Contains(channels.Body.String(), "verified_at") {
		t.Fatal("unsafe connection metadata")
	}
	app := `{"label":"文档应用","app_id":"cli_fixture","app_secret":"fixture-app-secret","expected_revision":0,"enabled":true}`
	if out := request("PUT", "/api/v1/connections/feishu/documents", app, domain.ID()); out.Code != 200 {
		t.Fatal("app config failed", out.Code)
	}
	importBody := `{"title":"来自飞书的事项","document_url":"https://corp.feishu.cn/docx/abcdefgh123","config_revision":1,"confirmed":true}`
	key = domain.ID()
	out := request("POST", "/api/v1/connections/feishu/documents/import", importBody, key)
	if out.Code != 201 || !strings.Contains(out.Body.String(), "读取的文档资料") {
		t.Fatal("authorized import failed", out.Code)
	}
	request("POST", "/api/v1/connections/feishu/documents/import", importBody, key)
	if reads != 2 {
		t.Fatal("import retry repeated SDK calls")
	}
	if out = request("DELETE", "/api/v1/models/"+m.ID, `{}`, domain.ID()); out.Code != 200 {
		t.Fatal("credential removal failed")
	}
	s.Pool.QueryRow(ctx, "SELECT credential FROM personal_models WHERE workspace_id=$1 AND id=$2", ws, m.ID).Scan(&encrypted)
	if len(encrypted) != 0 {
		t.Fatal("revocation didn't erase encrypted credential")
	}
}
