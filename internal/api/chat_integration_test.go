package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"nemi/internal/agent"
	"nemi/internal/config"
	"nemi/internal/domain"
	"nemi/internal/model"
	"nemi/internal/store"
	"nemi/internal/vault"
)

func TestChatRequiresKeyAndPreservesOwnedContext(t *testing.T) {
	db := os.Getenv("TEST_DATABASE_URL")
	if db == "" {
		t.Skip("dedicated database not configured")
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
	ws := "chat-api-" + domain.ID()
	code := "fixture-" + domain.ID()
	if err = s.Bootstrap(ctx, code, ws); err != nil {
		t.Fatal(err)
	}
	token, err := s.Login(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := vault.New(bytes.Repeat([]byte{31}, 32))
	g := model.New(config.Config{})
	a := &API{Store: s, Config: config.Config{Origin: "http://localhost:3000"}, Gateway: g, Vault: v}
	h := a.Handler()
	request := func(method, path, body, key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", a.Config.Origin)
		r.Header.Set("Idempotency-Key", key)
		r.AddCookie(&http.Cookie{Name: "nemi_session", Value: token})
		out := httptest.NewRecorder()
		h.ServeHTTP(out, r)
		return out
	}
	if out := request("POST", "/api/v1/chat/messages", `{"text":"你好"}`, domain.ID()); out.Code != 412 {
		t.Fatal("missing key accepted", out.Code)
	}
	var count int
	if err = s.Pool.QueryRow(ctx, "SELECT count(*) FROM runs WHERE workspace_id=$1", ws).Scan(&count); err != nil || count != 0 {
		t.Fatal("missing key queued a replacement", err)
	}
	if out := request("POST", "/api/v1/models", `{"label":"我的模型","provider":"deepseek","model":"fixture","api_key":"fixture-chat-key","input_price_micro_cny":2000000,"output_price_micro_cny":4000000}`, domain.ID()); out.Code != 201 {
		t.Fatal("configuration rejected", out.Code)
	}
	models, selected, err := s.Models(ctx, ws)
	if err != nil || len(models) != 1 || selected != models[0].ID {
		t.Fatal("first model not selected")
	}
	key := domain.ID()
	body := `{"text":"我喜欢清淡口味"}`
	out := request("POST", "/api/v1/chat/messages", body, key)
	if out.Code != 202 {
		t.Fatal("chat rejected", out.Code)
	}
	var created struct {
		Conversation string `json:"conversation_id"`
		Run          string `json:"run_id"`
	}
	json.Unmarshal(out.Body.Bytes(), &created)
	repeated := request("POST", "/api/v1/chat/messages", body, key)
	var duplicate struct {
		Run string `json:"run_id"`
	}
	json.Unmarshal(repeated.Body.Bytes(), &duplicate)
	if repeated.Code != 202 || duplicate.Run != created.Run {
		t.Fatal("retry created another model call")
	}
	if out = request("POST", "/api/v1/chat/messages", `{"text":"继续","conversation_id":"`+created.Conversation+`"}`, domain.ID()); out.Code != 409 {
		t.Fatal("parallel turn accepted", out.Code)
	}
	ref := store.Ref{Workspace: ws, ID: created.Run}
	calls := 0
	g.HTTP.Transport = botTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "Bearer fixture-chat-key" || r.URL.Host != "api.deepseek.com" {
			t.Error("wrong provider credential")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"finish_reason":"stop","message":{"content":"可以从清淡的蔬菜汤开始。"}}],"usage":{"prompt_tokens":50,"completion_tokens":30}}`))}, nil
	})
	personal, err := model.Resolve(ctx, s, v, g, ref)
	if err != nil || personal.Kind != "chat" {
		t.Fatal("lost captured chat model", err)
	}
	if _, err = s.AdmitRun(ctx, ref, personal.Reserve); err != nil {
		t.Fatal(err)
	}
	input, err := s.ClaimModel(ctx, ref, personal.Profile())
	if err != nil {
		t.Fatal(err)
	}
	response, err := agent.Generate(ctx, s, v, personal, ref, input.Source)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.FinishRun(ctx, ref, response.Plan, response.InputTokens, response.OutputTokens, personal.Cost(response.InputTokens, response.OutputTokens)); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("duplicate provider request")
	}
	d, err := s.Conversation(ctx, ws, created.Conversation)
	if err != nil || len(d.Turns) != 1 || d.Turns[0].Reply != "可以从清淡的蔬菜汤开始。" {
		t.Fatal("reply not durable", err)
	}
	if _, err = s.Conversation(ctx, "another-workspace", created.Conversation); err != domain.ErrNotFound {
		t.Fatal("cross-workspace chat exposed")
	}
	if out = request("POST", "/api/v1/chat/messages", `{"text":"推荐一道菜","conversation_id":"`+created.Conversation+`"}`, domain.ID()); out.Code != 202 {
		t.Fatal("follow-up rejected", out.Code)
	}
	var next struct {
		Run string `json:"run_id"`
	}
	json.Unmarshal(out.Body.Bytes(), &next)
	var snapshot string
	if err = s.Pool.QueryRow(ctx, "SELECT snapshot_source FROM runs WHERE workspace_id=$1 AND id=$2", ws, next.Run).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	var checkpoint domain.ChatContext
	if json.Unmarshal([]byte(snapshot), &checkpoint) != nil || len(checkpoint.History) != 3 || checkpoint.History[0].Content != "我喜欢清淡口味" || checkpoint.History[1].Content != "可以从清淡的蔬菜汤开始。" || checkpoint.History[2].Content != "推荐一道菜" {
		t.Fatal("follow-up lost context")
	}
	if err = s.FailRun(ctx, store.Ref{Workspace: ws, ID: next.Run}, "MODEL_HTTP_401", 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	dashboard, err := s.Dashboard(ctx, store.Identity{Workspace: ws}, "personal")
	if err != nil || len(dashboard.Matters) != 0 || len(dashboard.Runs) != 0 {
		t.Fatal("chat created a matter", err)
	}
}
