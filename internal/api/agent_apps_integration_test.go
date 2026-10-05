package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"nemi/internal/agent"
	"nemi/internal/config"
	"nemi/internal/connectors"
	"nemi/internal/domain"
	"nemi/internal/model"
	"nemi/internal/store"
	"nemi/internal/vault"
)

func TestAgentConnectAndSendUseImmutableConfirmationAndNeverResend(t *testing.T) {
	db := os.Getenv("TEST_DATABASE_URL")
	if db == "" {
		t.Skip("dedicated database required")
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
	ws := "agent-apps-" + domain.ID()
	code := domain.ID()
	s.Bootstrap(ctx, code, ws)
	token, _ := s.Login(ctx, code)
	v, _ := vault.New(bytes.Repeat([]byte{61}, 32))
	g := model.New(config.Config{Provider: "deepseek", Model: "fixture", Key: "fixture-key", InputPrice: 2000000, OutputPrice: 4000000})
	bots := connectors.New(nil)
	sends := 0
	bots.Client.Transport = botTransport(func(r *http.Request) (*http.Response, error) {
		sends++
		if r.URL.Host != "qyapi.weixin.qq.com" {
			t.Error("wrong outbound host")
		}
		b, _ := io.ReadAll(r.Body)
		if strings.Contains(string(b), "未知测试") {
			return nil, errors.New("fixture connection lost")
		}
		if !strings.Contains(string(b), "核对后的工作清单") {
			t.Error("message changed after confirmation")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"errcode":0}`))}, nil
	})
	g.HTTP.Transport = botTransport(func(r *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(r.Body)
		if strings.Contains(string(raw), "private-fixture-webhook") {
			t.Fatal("app credential reached model")
		}
		var b struct {
			Messages []struct{ Role, Content string }
		}
		json.Unmarshal(raw, &b)
		last := b.Messages[len(b.Messages)-1]
		var choice any
		if strings.Contains(b.Messages[0].Content, "会话整理器") {
			choice = map[string]any{"finish_reason": "stop", "message": map[string]string{"content": "用户已配置企业微信群，发送前必须核对接收群与正文；连接修改和停用同样需要确认。"}}
		} else if last.Role == "tool" {
			choice = map[string]any{"finish_reason": "stop", "message": map[string]string{"content": "请在对话卡片中确认。"}}
		} else {
			name, args := "request_connection", `{"app_id":"wecom"}`
			if strings.HasPrefix(last.Content, "发送：") {
				name = "propose_message"
				p, _ := json.Marshal(map[string]string{"channel_id": "wecom", "text": strings.TrimPrefix(last.Content, "发送：")})
				args = string(p)
			}
			if strings.HasPrefix(last.Content, "停用：") {
				name, args = "propose_disconnect", `{"app_id":"wecom"}`
			}
			if strings.HasPrefix(last.Content, "修改：") {
				name, args = "request_connection", `{"app_id":"wecom","replace":true}`
			}
			choice = map[string]any{"finish_reason": "tool_calls", "message": map[string]any{"tool_calls": []any{map[string]any{"id": "app1", "type": "function", "function": map[string]string{"name": name, "arguments": args}}}}}
		}
		wire, _ := json.Marshal(map[string]any{"choices": []any{choice}, "usage": map[string]int{"prompt_tokens": 50, "completion_tokens": 30}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(wire))}, nil
	})
	a := &API{Store: s, Config: config.Config{Origin: "http://localhost:3000"}, Gateway: g, Vault: v, Bots: bots}
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
	conversation := ""
	turn := func(text string) domain.AgentAction {
		t.Helper()
		b, _ := json.Marshal(map[string]string{"text": text, "conversation_id": conversation})
		r := request("POST", "/api/v1/chat/messages", string(b), domain.ID())
		if r.Code != 202 {
			t.Fatal("chat rejected", r.Code)
		}
		var created struct {
			Conversation string `json:"conversation_id"`
			Run          string `json:"run_id"`
		}
		json.Unmarshal(r.Body.Bytes(), &created)
		conversation = created.Conversation
		ref := store.Ref{Workspace: ws, ID: created.Run}
		cg := g.ForKind("chat")
		if _, err = s.AdmitRun(ctx, ref, cg.Reserve); err != nil {
			t.Fatal(err)
		}
		in, err := s.ClaimModel(ctx, ref, cg.Profile())
		if err != nil {
			t.Fatal(err)
		}
		out, err := agent.Generate(ctx, s, v, cg, ref, in.Source)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.FinishRun(ctx, ref, out.Plan, out.InputTokens, out.OutputTokens, out.Charged); err != nil {
			t.Fatal(err)
		}
		d, err := s.Conversation(ctx, ws, conversation)
		if err != nil || len(d.Turns[len(d.Turns)-1].Actions) != 1 {
			t.Fatal("missing action", err)
		}
		return d.Turns[len(d.Turns)-1].Actions[0]
	}
	connection := turn("连接企业微信")
	if connection.Kind != "connect_app" || sends != 0 {
		t.Fatal("connect caused external side effect")
	}
	path := "/api/v1/agent/actions/" + connection.ID + "/approve"
	if r := request("POST", path, `{"confirmed":true}`, domain.ID()); r.Code != 409 {
		t.Fatal("unconfigured connection approved")
	}
	save := func(revision int, enabled bool) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"label": "工作测试群", "webhook": "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=private-fixture-webhook", "secret": "", "enabled": enabled, "expected_revision": revision})
		r := request("PUT", "/api/v1/connections/wecom", string(body), domain.ID())
		if r.Code != 200 {
			t.Fatal("config rejected", r.Code)
		}
	}
	save(0, true)
	if r := request("POST", path, `{"confirmed":true}`, domain.ID()); r.Code != 200 {
		t.Fatal("saved connection not approved")
	}
	message := turn("发送：核对后的工作清单")
	if message.Kind != "send_message" || sends != 0 {
		t.Fatal("agent sent before confirmation")
	}
	path = "/api/v1/agent/actions/" + message.ID + "/approve"
	if r := request("POST", path, `{"confirmed":false}`, domain.ID()); r.Code != 400 || sends != 0 {
		t.Fatal("unconfirmed message sent")
	}
	key := domain.ID()
	for _, k := range []string{key, key, domain.ID()} {
		r := request("POST", path, `{"confirmed":true}`, k)
		if r.Code != 200 || !strings.Contains(r.Body.String(), "DELIVERED") {
			t.Fatal("send receipt missing", r.Code)
		}
	}
	if sends != 1 {
		t.Fatal("confirmed action sent repeatedly", sends)
	}
	if r := request("POST", "/api/v1/agent/actions/"+message.ID+"/dismiss", `{"confirmed":true}`, domain.ID()); r.Code != 409 {
		t.Fatal("sent message dismissed")
	}
	stale := turn("发送：核对后的工作清单")
	save(1, true)
	if r := request("POST", "/api/v1/agent/actions/"+stale.ID+"/approve", `{"confirmed":true}`, domain.ID()); r.Code != 409 || sends != 1 {
		t.Fatal("changed recipient config sent stale action")
	}
	unknown := turn("发送：未知测试")
	path = "/api/v1/agent/actions/" + unknown.ID + "/approve"
	for i := 0; i < 2; i++ {
		r := request("POST", path, `{"confirmed":true}`, domain.ID())
		if r.Code != 200 || !strings.Contains(r.Body.String(), "UNKNOWN") {
			t.Fatal("unknown receipt missing")
		}
	}
	if sends != 2 {
		t.Fatal("unknown send retried")
	}
	save(2, false)
	if r := request("POST", path, `{"confirmed":true}`, domain.ID()); r.Code != 200 || sends != 2 {
		t.Fatal("late receipt caused resend")
	}
	d, err := s.Conversation(ctx, ws, conversation)
	if err != nil || d.Turns[1].Actions[0].DispatchStatus != "DELIVERED" || d.Turns[3].Actions[0].DispatchStatus != "UNKNOWN" {
		t.Fatal("receipt lost after reload", err)
	}
	// A replacement cannot complete against the original enabled connection.
	save(3, true)
	replacement := turn("修改：企业微信群")
	replacePath := "/api/v1/agent/actions/" + replacement.ID + "/approve"
	if r := request("POST", replacePath, `{"confirmed":true}`, domain.ID()); r.Code != 409 {
		t.Fatal("unchanged connection completed replacement")
	}
	save(4, true)
	if r := request("POST", replacePath, `{"confirmed":true}`, domain.ID()); r.Code != 200 {
		t.Fatal("saved replacement not approved", r.Code)
	}
	apps := request("GET", "/api/v1/applications", "", "")
	if apps.Code != 200 || strings.Contains(apps.Body.String(), "private-fixture-webhook") || strings.Contains(apps.Body.String(), "credential") {
		t.Fatal("catalog leaked credentials")
	}
	var catalog struct {
		Applications []connectors.Application `json:"applications"`
	}
	if json.Unmarshal(apps.Body.Bytes(), &catalog) != nil || len(catalog.Applications) != 15 {
		t.Fatal("catalog incomplete")
	}
	for _, app := range catalog.Applications {
		if app.ID == "wecom" && (app.State != "configured" || app.Verified) {
			t.Fatal("unverified configuration claimed verification")
		}
	}
	staleDisconnect := turn("停用：企业微信群")
	save(5, true)
	if r := request("POST", "/api/v1/agent/actions/"+staleDisconnect.ID+"/approve", `{"confirmed":true}`, domain.ID()); r.Code != 409 {
		t.Fatal("stale proposal disconnected new configuration")
	}
	oldMessage := turn("发送：核对后的工作清单")
	disconnect := turn("停用：企业微信群")
	if disconnect.Kind != "disconnect_app" {
		t.Fatal("missing disconnect proposal")
	}
	current, _ := s.Connection(ctx, ws, "wecom")
	if !current.Enabled || len(current.Credential) == 0 {
		t.Fatal("proposed disconnect changed credentials without approval")
	}
	disconnectPath := "/api/v1/agent/actions/" + disconnect.ID + "/approve"
	if r := request("POST", disconnectPath, `{"confirmed":false}`, domain.ID()); r.Code != 400 {
		t.Fatal("unconfirmed disconnect applied")
	}
	var group sync.WaitGroup
	for i := 0; i < 12; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if r := request("POST", disconnectPath, `{"confirmed":true}`, domain.ID()); r.Code != 200 {
				t.Error("concurrent disconnect failed", r.Code)
			}
		}()
	}
	group.Wait()
	after, _ := s.Connection(ctx, ws, "wecom")
	if after.Enabled || len(after.Credential) != 0 || after.Revision != current.Revision+1 {
		t.Fatal("disconnect repeated or retained credentials")
	}
	if r := request("POST", "/api/v1/agent/actions/"+oldMessage.ID+"/approve", `{"confirmed":true}`, domain.ID()); r.Code != 409 || sends != 2 {
		t.Fatal("old message sent after disconnect")
	}
	if r := request("POST", disconnectPath[:len(disconnectPath)-len("approve")]+"dismiss", `{"confirmed":true}`, domain.ID()); r.Code != 409 {
		t.Fatal("confirmed disconnect dismissed")
	}
	other := "foreign-apps-" + domain.ID()
	otherCode := domain.ID()
	s.Bootstrap(ctx, otherCode, other)
	token, _ = s.Login(ctx, otherCode)
	if r := request("POST", path, `{"confirmed":true}`, domain.ID()); r.Code != 404 || sends != 2 {
		t.Fatal("foreign action confirmed")
	}
	if r := request("POST", disconnectPath, `{"confirmed":true}`, domain.ID()); r.Code != 404 {
		t.Fatal("foreign disconnect approved")
	}
}
