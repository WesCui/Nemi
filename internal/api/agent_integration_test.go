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
	"time"

	"nemi/internal/agent"
	"nemi/internal/config"
	"nemi/internal/domain"
	"nemi/internal/model"
	"nemi/internal/store"
	"nemi/internal/vault"
)

func TestAgentProposesThenApprovalCreatesExactlyOneMatterAndReminder(t *testing.T) {
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
	ws := "agent-api-" + domain.ID()
	code := "fixture-" + domain.ID()
	if err = s.Bootstrap(ctx, code, ws); err != nil {
		t.Fatal(err)
	}
	token, err := s.Login(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := vault.New(bytes.Repeat([]byte{41}, 32))
	g := model.New(config.Config{Provider: "deepseek", Model: "fixture", Key: "fixture-agent-key", InputPrice: 2000000, OutputPrice: 4000000})
	a := &API{Store: s, Config: config.Config{Origin: "http://localhost:3000"}, Gateway: g, Vault: v}
	handler := a.Handler()
	request := func(path, body, key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", a.Config.Origin)
		r.Header.Set("Idempotency-Key", key)
		r.AddCookie(&http.Cookie{Name: "nemi_session", Value: token})
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, r)
		return out
	}
	out := request("/api/v1/chat/messages", `{"text":"请帮我创建事项并设置站内提醒"}`, domain.ID())
	if out.Code != 202 {
		t.Fatal("chat rejected", out.Code)
	}
	var created struct {
		Conversation string `json:"conversation_id"`
		Run          string `json:"run_id"`
	}
	if json.Unmarshal(out.Body.Bytes(), &created) != nil {
		t.Fatal("invalid chat output")
	}
	ref := store.Ref{Workspace: ws, ID: created.Run}
	g = g.ForKind("chat")
	if _, err = s.AdmitRun(ctx, ref, g.Reserve); err != nil {
		t.Fatal(err)
	}
	input, err := s.ClaimModel(ctx, ref, g.Profile())
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	args, _ := json.Marshal(domain.AgentMatter{Title: "取体检报告", Source: "记得带身份证", Category: "life", ReminderAt: &at, Repeat: "once"})
	calls := 0
	g.HTTP.Transport = botTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		var b struct {
			Messages []struct{ Role, Content string }
			Tools    []any
		}
		if json.NewDecoder(r.Body).Decode(&b) != nil || len(b.Tools) != 30 {
			t.Fatal("missing tools")
		}
		var choice any
		if calls == 1 {
			choice = map[string]any{"finish_reason": "tool_calls", "message": map[string]any{"tool_calls": []any{map[string]any{"id": "matter1", "type": "function", "function": map[string]string{"name": "propose_matter", "arguments": string(args)}}}}}
		} else {
			last := b.Messages[len(b.Messages)-1]
			if last.Role != "tool" || !strings.Contains(last.Content, `"status":"PENDING"`) || !strings.Contains(last.Content, "取体检报告") {
				t.Fatal("proposal not fed back")
			}
			choice = map[string]any{"finish_reason": "stop", "message": map[string]string{"content": "已准备好事项与站内提醒，请在下方确认。"}}
		}
		wire, _ := json.Marshal(map[string]any{"choices": []any{choice}, "usage": map[string]int{"prompt_tokens": 80, "completion_tokens": 40}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(wire))}, nil
	})
	result, err := agent.Generate(ctx, s, v, g, ref, input.Source)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.FinishRun(ctx, ref, result.Plan, result.InputTokens, result.OutputTokens, result.Charged); err != nil {
		t.Fatal(err)
	}
	detail, err := s.Conversation(ctx, ws, created.Conversation)
	if err != nil || len(detail.Turns) != 1 || len(detail.Turns[0].Actions) != 1 || len(detail.Turns[0].Steps) != 3 || calls != 2 {
		t.Fatal("agent history not persisted", err)
	}
	proposal := detail.Turns[0].Actions[0]
	if proposal.Status != "PENDING" {
		t.Fatal("premature approval")
	}
	var count int
	s.Pool.QueryRow(ctx, "SELECT count(*) FROM matters WHERE workspace_id=$1", ws).Scan(&count)
	if count != 0 {
		t.Fatal("agent created a matter before confirmation")
	}
	path := "/api/v1/agent/actions/" + proposal.ID + "/approve"
	if out = request(path, `{"confirmed":false}`, domain.ID()); out.Code != 400 {
		t.Fatal("missing confirmation accepted")
	}
	key := domain.ID()
	out = request(path, `{"confirmed":true}`, key)
	if out.Code != 201 {
		t.Fatal("approval rejected", out.Code)
	}
	if out = request(path, `{"confirmed":true}`, key); out.Code != 201 {
		t.Fatal("idempotent retry failed")
	}
	if out = request(path, `{"confirmed":true}`, domain.ID()); out.Code != 200 {
		t.Fatal("second confirmation failed")
	}
	if out = request("/api/v1/agent/actions/"+proposal.ID+"/dismiss", `{"confirmed":true}`, domain.ID()); out.Code != 409 {
		t.Fatal("approved proposal dismissed")
	}
	dashboard, err := s.Dashboard(ctx, store.Identity{Workspace: ws}, "managed")
	if err != nil || len(dashboard.Matters) != 1 || len(dashboard.Reminders) != 1 || !dashboard.Reminders[0].Nominal.Equal(at) {
		t.Fatal("approved effect not exact", err)
	}
	state, err := s.ConversationActionState(ctx, ref)
	if err != nil || len(state) != 1 || state[0]["status"] != "APPROVED" || state[0]["matter_id"] != dashboard.Matters[0].ID {
		t.Fatal("follow-up state stale", err)
	}
	// A later proposal can be declined; an expired date must be re-prepared.
	out = request("/api/v1/chat/messages", `{"text":"再准备一项"}`, domain.ID())
	var next struct {
		Run string `json:"run_id"`
	}
	json.Unmarshal(out.Body.Bytes(), &next)
	ref2 := store.Ref{Workspace: ws, ID: next.Run}
	if _, err = s.AdmitRun(ctx, ref2, g.Reserve); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ClaimModel(ctx, ref2, g.Profile()); err != nil {
		t.Fatal(err)
	}
	decline, err := s.ProposeAction(ctx, ref2, domain.ID(), "create_matter", args)
	if err != nil {
		t.Fatal(err)
	}
	if out = request("/api/v1/agent/actions/"+decline.ID+"/dismiss", `{"confirmed":true}`, domain.ID()); out.Code != 200 {
		t.Fatal("decline failed")
	}
	if out = request("/api/v1/agent/actions/"+decline.ID+"/approve", `{"confirmed":true}`, domain.ID()); out.Code != 409 {
		t.Fatal("declined action executed")
	}
	past := time.Now().Add(-time.Hour)
	staleArgs, _ := json.Marshal(domain.AgentMatter{Title: "过期提案", Category: "life", ReminderAt: &past, Repeat: "once"})
	stale, err := s.ProposeAction(ctx, ref2, domain.ID(), "create_matter", staleArgs)
	if err != nil {
		t.Fatal(err)
	}
	if out = request("/api/v1/agent/actions/"+stale.ID+"/approve", `{"confirmed":true}`, domain.ID()); out.Code != 422 {
		t.Fatal("past reminder accepted")
	}
	if err = s.FailRun(ctx, ref2, "FIXTURE_STOPPED", 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	// Another space cannot approve or read an action by a guessed identifier.
	other := "agent-other-" + domain.ID()
	otherCode := domain.ID()
	s.Bootstrap(ctx, otherCode, other)
	token, _ = s.Login(ctx, otherCode)
	if out = request(path, `{"confirmed":true}`, domain.ID()); out.Code != 404 {
		t.Fatal("foreign action accessible")
	}
}
