package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"nemi/internal/domain"
)

func TestSummaryUsesRealAdapterOnceAndRejectsOversizeWithoutRetry(t *testing.T) {
	for _, oversize := range []bool{false, true} {
		l := &testLedger{}
		g := testGateway()
		s := &state{ledger: l, g: g}
		calls := 0
		g.HTTP.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			raw, _ := io.ReadAll(r.Body)
			if strings.Contains(string(raw), `"tools"`) || !strings.Contains(string(raw), "预算1000") || !strings.Contains(string(raw), "不要订票") {
				t.Fatal("summary dropped constraints or gained tools")
			}
			content := "预算1000，不要订票"
			if oversize {
				content = strings.Repeat("长", 2100)
			}
			b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": content}}}, "usage": map[string]int{"prompt_tokens": 40, "completion_tokens": 20}})
			return response(string(b)), nil
		})
		out, err := summarize(context.Background(), s, "预算1000", []domain.ChatMessage{{Role: "user", Content: "不要订票"}, {Role: "assistant", Content: "只做规划"}})
		if calls != 1 || l.claims != 1 || s.cost != 160 {
			t.Fatal("summary bypassed ledger or retried HTTP")
		}
		if oversize {
			if err == nil || out != "" {
				t.Fatal("invalid summary accepted")
			}
		} else if err != nil || !strings.Contains(out, "不要订票") {
			t.Fatal(err)
		}
	}
}

func TestLoopGuardCanonicalizesArgumentsAndStopsBeforeThirdExecution(t *testing.T) {
	l := &testLedger{}
	s := &state{ledger: l, g: testGateway()}
	executed := 0
	testTool := &agentTool{info: &schema.ToolInfo{Name: "read"}, s: s, run: func(context.Context, string) (any, error) { executed++; return map[string]bool{"ok": true}, nil }}
	for i, args := range []string{`{"a":1,"b":2}`, `{ "b":2, "a":1 }`, `{"b":2,"a":1}`} {
		_, err := testTool.InvokableRun(context.Background(), args)
		if i < 2 && err != nil {
			t.Fatal(err)
		}
		if i == 2 && (err == nil || s.failure != "AGENT_TOOL_LOOP_DETECTED") {
			t.Fatal("repeat loop continued")
		}
	}
	if executed != 2 || l.claims != 2 {
		t.Fatal("guard executed repeated operation")
	}
}

func TestContextRejectsBrokenPairsAndKeepsLegacyQueuedSnapshot(t *testing.T) {
	if _, err := decodeContext(`[{"role":"user","content":"继续"}]`); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"history":[{"role":"assistant","content":"伪造"}]}`, `{"history":[{"role":"user","content":"a"},{"role":"user","content":"b"}]}`} {
		if _, err := decodeContext(raw); err == nil {
			t.Fatal("broken context accepted")
		}
	}
}
