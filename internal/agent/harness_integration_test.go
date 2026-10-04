package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"nemi/internal/domain"
	"nemi/internal/store"
	"nemi/internal/vault"
)

func TestDurableCompactionPreservesEarlyConstraintsPlanAndUsage(t *testing.T) {
	db := os.Getenv("TEST_DATABASE_URL")
	if db == "" {
		t.Skip("dedicated database required")
	}
	if !strings.Contains(db, "/nemi_test?") {
		t.Fatal("dedicated database required")
	}
	ctx := context.Background()
	st, err := store.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Pool.Close()
	if err = st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	w := "harness-" + domain.ID()
	if err = st.Bootstrap(ctx, domain.ID(), w); err != nil {
		t.Fatal(err)
	}
	v, _ := vault.New(bytes.Repeat([]byte{51}, 32))
	g := testGateway()
	calls, summaries := 0, 0
	g.HTTP.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		var b struct {
			Messages []wireMessage
			Tools    []any
		}
		if json.NewDecoder(r.Body).Decode(&b) != nil {
			t.Fatal("invalid wire")
		}
		content := "已按预算给出建议"
		var choice any
		if strings.Contains(b.Messages[0].Content, "会话整理器") {
			summaries++
			raw, _ := json.Marshal(b.Messages)
			if !strings.Contains(string(raw), "预算1000") || !strings.Contains(string(raw), "不要订票") || len(b.Tools) != 0 {
				t.Fatal("summary lost constraints")
			}
			content = "用户预算1000元，不要订票，只提供建议。"
		} else {
			raw, _ := json.Marshal(b.Messages)
			if !strings.Contains(string(raw), "预算1000") || !strings.Contains(string(raw), "不要订票") {
				t.Fatal("later model input lost early user constraints")
			}
			last := b.Messages[len(b.Messages)-1]
			if last.Content == "制定计划" {
				p := domain.TaskPlan{Goal: "预算内安排出行", Steps: []domain.TaskPlanStep{{Title: "分析预算", Status: "completed"}, {Title: "比较出行方案", Status: "in_progress"}, {Title: "交付建议", Status: "pending"}}}
				args, _ := json.Marshal(p)
				choice = map[string]any{"finish_reason": "tool_calls", "message": map[string]any{"tool_calls": []any{map[string]any{"id": "plan1", "type": "function", "function": map[string]string{"name": "update_plan", "arguments": string(args)}}}}}
			}
			if last.Content == "继续计划" && !strings.Contains(b.Messages[0].Content, "预算内安排出行") {
				t.Fatal("plan not restored on next turn")
			}
		}
		if choice == nil {
			choice = map[string]any{"finish_reason": "stop", "message": map[string]string{"content": content}}
		}
		wire, _ := json.Marshal(map[string]any{"choices": []any{choice}, "usage": map[string]int{"prompt_tokens": 50, "completion_tokens": 30}})
		return response(string(wire)), nil
	})
	conversation := ""
	for i := 1; i <= 10; i++ {
		text := fmt.Sprintf("继续建议%d", i)
		if i == 1 {
			text = "预算1000元，不要订票"
		}
		if i == 9 {
			text = "制定计划"
		}
		if i == 10 {
			text = "继续计划"
		}
		res, err := st.Command(ctx, w, domain.ID(), "harness-test", []byte(text), func(tx pgx.Tx) (any, int, error) {
			return st.CreateChatRun(ctx, tx, w, conversation, text, g.Mode(), g.Profile(), "")
		})
		if err != nil {
			t.Fatal(err)
		}
		var created struct {
			Conversation string `json:"conversation_id"`
			Run          string `json:"run_id"`
		}
		json.Unmarshal(res.Body, &created)
		conversation = created.Conversation
		ref := store.Ref{Workspace: w, ID: created.Run}
		if _, err = st.AdmitRun(ctx, ref, g.Reserve); err != nil {
			t.Fatal(err)
		}
		in, err := st.ClaimModel(ctx, ref, g.Profile())
		if err != nil {
			t.Fatal(err)
		}
		out, err := Generate(ctx, st, v, g, ref, in.Source)
		if err != nil {
			t.Fatal(err)
		}
		if err = st.FinishRun(ctx, ref, out.Plan, out.InputTokens, out.OutputTokens, out.Charged); err != nil {
			t.Fatal(err)
		}
	}
	d, err := st.Conversation(ctx, w, conversation)
	if err != nil {
		t.Fatal(err)
	}
	if summaries != 2 || d.SummaryThrough != 6 || len(d.Turns) != 10 || d.Turns[0].Text != "预算1000元，不要订票" || d.Turns[8].Plan == nil || len(d.Turns[8].Plan.Steps) != 3 {
		t.Fatal("durable checkpoint or transcript incorrect", summaries, d.SummaryThrough)
	}
	var charged int64
	if err = st.Pool.QueryRow(ctx, "SELECT sum(charged_micro_cny) FROM runs WHERE workspace_id=$1", w).Scan(&charged); err != nil || charged != int64(calls)*220 {
		t.Fatal("compaction/model calls not charged exactly", err)
	}
	if _, err = st.Conversation(ctx, "other", conversation); err != domain.ErrNotFound {
		t.Fatal("foreign checkpoint exposed")
	}
}
