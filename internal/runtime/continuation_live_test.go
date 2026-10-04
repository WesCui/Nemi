package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"io"
	"nemi/internal/config"
	"nemi/internal/domain"
	"nemi/internal/model"
	"nemi/internal/store"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLiveContinuationSurvivesWorkerReplacementWithoutReplayingParent(t *testing.T) {
	db, addr := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_TEMPORAL_ADDRESS")
	if db == "" || addr == "" {
		t.Skip("dedicated DB and Temporal required")
	}
	if !strings.Contains(db, "/nemi_test?") {
		t.Fatal("dedicated DB required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	s, e := store.Open(ctx, db)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Pool.Close()
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	c, e := client.Dial(client.Options{HostPort: addr})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	w := "continuation-live-" + domain.ID()
	if _, e = s.Pool.Exec(ctx, "INSERT INTO workspaces(id) VALUES($1)", w); e != nil {
		t.Fatal(e)
	}
	g := model.New(config.Config{Provider: "deepseek", Model: "fixture", Key: "test-only", InputPrice: 2000000, OutputPrice: 4000000}).ForKind("chat")
	out, e := s.Command(ctx, w, domain.ID(), "chat", nil, func(tx pgx.Tx) (any, int, error) {
		return s.CreateChatRun(ctx, tx, w, "", "保存事项后核对结果", "managed", g.Profile(), "")
	})
	if e != nil {
		t.Fatal(e)
	}
	var run struct {
		ID string `json:"run_id"`
	}
	json.Unmarshal(out.Body, &run)
	ref := store.Ref{Workspace: w, ID: run.ID}
	if _, e = s.AdmitRun(ctx, ref, g.Reserve); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ClaimModel(ctx, ref, g.Profile()); e != nil {
		t.Fatal(e)
	}
	args, _ := json.Marshal(domain.AgentMatter{Title: "真实数据库事项", Source: "Worker替换后仍保留", Category: "life"})
	action, e := s.ProposeAction(ctx, ref, domain.ID(), "create_matter", args)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.WaitForActions(ctx, ref, store.WaitForActions{Goal: "保存并核对", Next: "读取已保存事项并交付结果", Actions: []string{action.ID}}); e != nil {
		t.Fatal(e)
	}
	if e = s.FinishRun(ctx, ref, domain.Plan{Summary: "等待用户确认"}, 0, 0, 0); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Command(ctx, w, domain.ID(), "consent", nil, func(tx pgx.Tx) (any, int, error) { return s.AuthorizeContinuation(ctx, tx, ref) }); e != nil {
		t.Fatal(e)
	}
	var modelCalls atomic.Int32
	g.HTTP.Transport = personalTransport(func(r *http.Request) (*http.Response, error) {
		count := modelCalls.Add(1)
		var b struct {
			Messages []struct{ Role, Content string }
		}
		if json.NewDecoder(r.Body).Decode(&b) != nil {
			return nil, fmt.Errorf("wire invalid")
		}
		var choice any
		if count == 1 {
			if !strings.Contains(b.Messages[0].Content, "确认后续接") {
				return nil, fmt.Errorf("missing resume context")
			}
			choice = map[string]any{"finish_reason": "tool_calls", "message": map[string]any{"tool_calls": []any{map[string]any{"id": "read_saved", "type": "function", "function": map[string]string{"name": "list_matters", "arguments": "{}"}}}}}
		} else if count == 2 {
			last := b.Messages[len(b.Messages)-1]
			if last.Role != "tool" || !strings.Contains(last.Content, "真实数据库事项") {
				return nil, fmt.Errorf("saved state not visible")
			}
			choice = map[string]any{"finish_reason": "stop", "message": map[string]string{"content": "已核对真实保存结果"}}
		} else {
			return nil, fmt.Errorf("paid model replayed")
		}
		wire, _ := json.Marshal(map[string]any{"choices": []any{choice}, "usage": map[string]int{"prompt_tokens": 50, "completion_tokens": 30}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(wire)))}, nil
	})
	queue := "nemi-continuation-test-" + domain.ID()
	newWorker := func() worker.Worker {
		wk := worker.New(c, queue, worker.Options{})
		wk.RegisterWorkflow(ContinuationWorkflow)
		wk.RegisterWorkflow(RunWorkflow)
		wk.RegisterActivity(&Activities{Store: s, Gateway: g})
		return wk
	}
	wk := newWorker()
	if e = wk.Start(); e != nil {
		t.Fatal(e)
	}
	stopped := false
	defer func() {
		if !stopped {
			wk.Stop()
		}
	}()
	start := store.Outbox{Workspace: w, Kind: "continuation", Subject: ref.ID}
	if e = dispatchTo(ctx, c, start, queue, queue); e != nil {
		t.Fatal(e)
	}
	defer c.CancelWorkflow(context.Background(), WorkflowID(start), "")
	waiting := false
	for i := 0; i < 30 && !waiting; i++ {
		iter := c.GetWorkflowHistory(ctx, WorkflowID(start), "", false, enums.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
		for iter.HasNext() {
			ev, err := iter.Next()
			if err != nil {
				t.Fatal(err)
			}
			if ev.EventType == enums.EVENT_TYPE_TIMER_STARTED {
				waiting = true
				break
			}
		}
		if !waiting {
			time.Sleep(100 * time.Millisecond)
		}
	}
	if !waiting || modelCalls.Load() != 0 {
		t.Fatal("durable wait missing or model called before approval")
	}
	wk.Stop()
	stopped = true
	// Commit approval while no Worker is running, then persist the wake signal.
	if _, e = s.Command(ctx, w, domain.ID(), "approve", nil, func(tx pgx.Tx) (any, int, error) {
		a, err := s.Action(ctx, tx, w, action.ID)
		if err != nil {
			return nil, 0, err
		}
		var p domain.AgentMatter
		json.Unmarshal(a.Payload, &p)
		saved, code, err := s.CreateMatter(ctx, tx, w, p.Create())
		if err != nil {
			return nil, 0, err
		}
		if err = s.DecideAction(ctx, tx, w, a.ID, "APPROVED", saved.(domain.Matter).ID); err != nil {
			return nil, 0, err
		}
		return saved, code, nil
	}); e != nil {
		t.Fatal(e)
	}
	signal := store.Outbox{Workspace: w, Kind: "continuation_signal", Subject: ref.ID, Revision: 2}
	if e = dispatchTo(ctx, c, signal, queue, queue); e != nil {
		t.Fatal(e)
	}
	replacement := newWorker()
	if e = replacement.Start(); e != nil {
		t.Fatal(e)
	}
	defer replacement.Stop()
	if e = c.GetWorkflow(ctx, WorkflowID(start), "").Get(ctx, nil); e != nil {
		t.Fatal(e)
	}
	var next string
	if e = s.Pool.QueryRow(ctx, "SELECT next_run_id FROM agent_continuations WHERE workspace_id=$1 AND run_id=$2", w, ref.ID).Scan(&next); e != nil || next == "" {
		t.Fatal("resume not committed", e)
	}
	child := store.Outbox{Workspace: w, Kind: "run", Subject: next}
	for i := 0; i < 2; i++ {
		if e = dispatchTo(ctx, c, child, queue, queue); e != nil {
			t.Fatal(e)
		}
	}
	if e = c.GetWorkflow(ctx, WorkflowID(child), "").Get(ctx, nil); e != nil {
		t.Fatal(e)
	}
	if modelCalls.Load() != 2 {
		t.Fatal("model calls replayed or missing", modelCalls.Load())
	}
	var status, result string
	if e = s.Pool.QueryRow(ctx, "SELECT status,result->>'summary' FROM runs WHERE workspace_id=$1 AND id=$2", w, next).Scan(&status, &result); e != nil || status != "SUCCEEDED" || result != "已核对真实保存结果" {
		t.Fatal("resume result missing", e)
	}
	// A duplicate late signal to the completed workflow is acknowledged, so it
	// cannot poison the outbox with an endless retry after a valid resume.
	if e = dispatchTo(ctx, c, signal, queue, queue); e != nil {
		t.Fatal("closed workflow signal not acknowledged", e)
	}
}
