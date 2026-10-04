package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"nemi/internal/agent"
	"nemi/internal/config"
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

func TestAgentPausesAfterModelPlannedWaitAndConsentDoesNotApprove(t *testing.T) {
	db := os.Getenv("TEST_DATABASE_URL")
	if db == "" {
		t.Skip("dedicated database not configured")
	}
	if !strings.Contains(db, "/nemi_test?") {
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
	w, code := "continuation-api-"+domain.ID(), domain.ID()
	if e = s.Bootstrap(ctx, code, w); e != nil {
		t.Fatal(e)
	}
	token, e := s.Login(ctx, code)
	if e != nil {
		t.Fatal(e)
	}
	v, _ := vault.New(bytes.Repeat([]byte{44}, 32))
	g := model.New(config.Config{Provider: "deepseek", Model: "fixture", Key: "test-only", InputPrice: 2000000, OutputPrice: 4000000})
	a := &API{Store: s, Vault: v, Gateway: g, Config: config.Config{Origin: "http://localhost:3000"}}
	handler := a.Handler()
	request := func(path, body, session string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", a.Config.Origin)
		r.Header.Set("Idempotency-Key", domain.ID())
		r.AddCookie(&http.Cookie{Name: "nemi_session", Value: session})
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, r)
		return out
	}
	out := request("/api/v1/chat/messages", `{"text":"保存我的饮食偏好后，再整理建议"}`, token)
	if out.Code != 202 {
		t.Fatal("chat rejected", out.Code)
	}
	var created struct {
		Run          string `json:"run_id"`
		Conversation string `json:"conversation_id"`
	}
	json.Unmarshal(out.Body.Bytes(), &created)
	ref := store.Ref{Workspace: w, ID: created.Run}
	g = g.ForKind("chat")
	if _, e = s.AdmitRun(ctx, ref, g.Reserve); e != nil {
		t.Fatal(e)
	}
	input, e := s.ClaimModel(ctx, ref, g.Profile())
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	g.HTTP.Transport = botTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		var b struct {
			Messages []struct{ Role, Content string }
		}
		if json.NewDecoder(r.Body).Decode(&b) != nil {
			t.Fatal("wire invalid")
		}
		name, args := "propose_memory", `{"operation":"save","expected_revision":0,"category":"life","text":"早餐不吃辣"}`
		if calls == 2 {
			var action struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			}
			if json.Unmarshal([]byte(b.Messages[len(b.Messages)-1].Content), &action) != nil || action.Status != "PENDING" {
				t.Fatal("missing pending action")
			}
			name = "await_actions"
			encoded, _ := json.Marshal(store.WaitForActions{Goal: "保存偏好并整理饮食建议", Next: "核对当前偏好，给出早餐建议", Actions: []string{action.ID}})
			args = string(encoded)
		} else if calls != 1 {
			return nil, fmt.Errorf("model called again while awaiting human decision")
		}
		wire, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"finish_reason": "tool_calls", "message": map[string]any{"tool_calls": []any{map[string]any{"id": fmt.Sprintf("pause_%d", calls), "type": "function", "function": map[string]string{"name": name, "arguments": args}}}}}}, "usage": map[string]int{"prompt_tokens": 50, "completion_tokens": 30}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(wire))}, nil
	})
	result, e := agent.Generate(ctx, s, v, g, ref, input.Source)
	if e != nil {
		t.Fatal(e)
	}
	if calls != 2 || !strings.Contains(result.Plan.Summary, "早餐建议") {
		t.Fatal("pause did not preserve model-planned next step")
	}
	if e = s.FinishRun(ctx, ref, result.Plan, result.InputTokens, result.OutputTokens, result.Charged); e != nil {
		t.Fatal(e)
	}
	detail, e := s.Conversation(ctx, w, created.Conversation)
	if e != nil || detail.Turns[0].Continuation == nil || detail.Turns[0].Continuation.Authorized {
		t.Fatal("waiting state missing or self-authorized", e)
	}
	path := "/api/v1/chat/continuations/" + ref.ID + "/start"
	if out = request(path, `{"confirmed":false}`, token); out.Code != 400 {
		t.Fatal("consent bypassed")
	}
	if out = request(path, `{"confirmed":true}`, token); out.Code != 202 {
		t.Fatal("consent rejected", out.Code)
	}
	if out = request(path, `{"confirmed":true}`, token); out.Code != 202 {
		t.Fatal("consent retry failed")
	}
	var count int
	s.Pool.QueryRow(ctx, "SELECT count(*) FROM memories WHERE workspace_id=$1", w).Scan(&count)
	if count != 0 {
		t.Fatal("continuation consent approved operation")
	}
	if check, e := s.AdvanceContinuation(ctx, ref); e != nil || check.Done {
		t.Fatal("resumed before operation review", e)
	}
	if out = request("/api/v1/agent/actions/"+detail.Turns[0].Actions[0].ID+"/approve", `{"confirmed":true}`, token); out.Code != 200 {
		t.Fatal("operation confirmation failed", out.Code)
	}
	if check, e := s.AdvanceContinuation(ctx, ref); e != nil || !check.Done {
		t.Fatal("confirmed work did not resume", e)
	}
	detail, e = s.Conversation(ctx, w, created.Conversation)
	if e != nil || len(detail.Turns) != 2 || detail.Turns[1].Origin != "continuation" {
		t.Fatal("continued turn missing", e)
	}
	other, otherCode := "continuation-other-"+domain.ID(), domain.ID()
	s.Bootstrap(ctx, otherCode, other)
	otherToken, _ := s.Login(ctx, otherCode)
	if out = request(path, `{"confirmed":true}`, otherToken); out.Code != 404 {
		t.Fatal("foreign continuation visible")
	}
	if out = request("/api/v1/chat/continuations/"+ref.ID+"/stop", `{"confirmed":true}`, token); out.Code != 200 {
		t.Fatal("continued run did not stop", out.Code)
	}
}
