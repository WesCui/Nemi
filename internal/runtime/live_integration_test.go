package runtime

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/activity"
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
	"testing"
	"time"
)

// Real Temporal, PG and worker replacement; the external model is a protocol fixture.
func TestLiveWorkerReplacementAndOutbox(t *testing.T) {
	addr := os.Getenv("TEST_TEMPORAL_ADDRESS")
	url := os.Getenv("TEST_DATABASE_URL")
	if addr == "" || url == "" {
		t.Skip("live Temporal integration requires TEST_TEMPORAL_ADDRESS and TEST_DATABASE_URL")
	}
	if !strings.Contains(url, "nemi_test") {
		t.Fatal("dedicated test DB required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	s, e := store.Open(ctx, url)
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
	wid := "live-" + domain.ID()
	if _, e = s.Pool.Exec(ctx, "INSERT INTO workspaces(id) VALUES($1)", wid); e != nil {
		t.Fatal(e)
	}
	due := time.Now().Add(5 * time.Second)
	until := due.Add(48 * time.Hour)
	mr, e := s.Command(ctx, wid, domain.ID(), "live-create", nil, func(tx pgx.Tx) (any, int, error) {
		return s.CreateMatter(ctx, tx, wid, domain.CreateMatter{Title: "验收周期提醒恢复", Category: "life", ReminderAt: &due, Repeat: "daily", Until: &until})
	})
	if e != nil {
		t.Fatal(e)
	}
	var m domain.Matter
	json.Unmarshal(mr.Body, &m)
	var rid string
	s.Pool.QueryRow(ctx, "SELECT id FROM reminders WHERE workspace_id=$1 AND matter_id=$2", wid, m.ID).Scan(&rid)
	queue := "nemi-live-test-" + domain.ID()
	g := model.New(config.Config{Provider: "qwen", Model: "fixture", Key: "fixture-only", InputPrice: 2000000, OutputPrice: 4000000})
	g.HTTP.Transport = personalTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"finish_reason":"stop","message":{"content":"{\"summary\":\"协议验收\",\"items\":[\"核对时间\",\"准备资料\",\"记录结果\"]}"}}],"usage":{"prompt_tokens":50,"completion_tokens":30}}`))}, nil
	})
	a := &Activities{Store: s, Gateway: g}
	newWorker := func() worker.Worker {
		w := worker.New(c, queue, worker.Options{})
		w.RegisterWorkflow(ReminderWorkflow)
		w.RegisterActivityWithOptions(a.Deliver, activity.RegisterOptions{Name: "Deliver"})
		return w
	}
	w := newWorker()
	if e = w.Start(); e != nil {
		t.Fatal(e)
	}
	wr, e := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{ID: "test/" + wid + "/reminder", TaskQueue: queue, WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE}, ReminderWorkflow, ReminderInput{Ref: store.Ref{Workspace: wid, ID: rid}, Revision: 1, Due: due})
	if e != nil {
		w.Stop()
		t.Fatal(e)
	}
	// Confirm the server persisted the timer before replacing the worker.
	timerStarted := false
	for i := 0; i < 20 && !timerStarted; i++ {
		iter := c.GetWorkflowHistory(ctx, wr.GetID(), wr.GetRunID(), false, enums.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
		for iter.HasNext() {
			ev, err := iter.Next()
			if err != nil {
				w.Stop()
				t.Fatal(err)
			}
			if ev.EventType == enums.EVENT_TYPE_TIMER_STARTED {
				timerStarted = true
				break
			}
		}
		if !timerStarted {
			time.Sleep(100 * time.Millisecond)
		}
	}
	if !timerStarted {
		w.Stop()
		t.Fatal("durable timer was not persisted")
	}
	// Remove the worker before due time; the server retains the pending timer.
	w.Stop()
	time.Sleep(6 * time.Second)
	replacement := newWorker()
	if e = replacement.Start(); e != nil {
		t.Fatal(e)
	}
	defer replacement.Stop()
	if e = wr.Get(ctx, nil); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = s.Pool.QueryRow(ctx, "SELECT count(*) FROM notifications WHERE workspace_id=$1", wid).Scan(&count); e != nil || count != 1 {
		t.Fatalf("recovery count=%d error=%v", count, e)
	}
	// The committed delivery and next occurrence share one transaction. The
	// new workflow can be submitted twice, without replacing old histories.
	var nextDue time.Time
	var revision int
	if e = s.Pool.QueryRow(ctx, "SELECT revision,due_at FROM reminders WHERE workspace_id=$1 AND id=$2", wid, rid).Scan(&revision, &nextDue); e != nil || revision != 2 || !nextDue.After(time.Now()) {
		t.Fatalf("next recurrence %d %s %v", revision, nextDue, e)
	}
	nextOutbox := store.Outbox{Workspace: wid, Kind: "reminder", Subject: rid, Revision: revision, Due: &nextDue}
	for i := 0; i < 2; i++ {
		if e = dispatchTo(ctx, c, nextOutbox, queue, queue); e != nil {
			t.Fatal(e)
		}
	}
	defer c.CancelWorkflow(context.Background(), WorkflowID(nextOutbox), "")
	timerStarted = false
	for i := 0; i < 20 && !timerStarted; i++ {
		iter := c.GetWorkflowHistory(ctx, WorkflowID(nextOutbox), "", false, enums.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
		for iter.HasNext() {
			ev, err := iter.Next()
			if err != nil {
				t.Fatal(err)
			}
			if ev.EventType == enums.EVENT_TYPE_TIMER_STARTED {
				timerStarted = true
				break
			}
		}
		if !timerStarted {
			time.Sleep(100 * time.Millisecond)
		}
	}
	if !timerStarted {
		t.Fatal("next recurring timer not persisted")
	}
	// Run outbox dispatch twice. Workflow IDs deduplicate the engine submission.
	rr, e := s.Command(ctx, wid, domain.ID(), "live-run", nil, func(tx pgx.Tx) (any, int, error) {
		return s.CreateRun(ctx, tx, wid, m.ID, g.Mode(), g.Profile(), 1)
	})
	if e != nil {
		t.Fatal(e)
	}
	var run domain.Run
	json.Unmarshal(rr.Body, &run)
	runQueue := "nemi-live-run-test-" + domain.ID()
	rw := worker.New(c, runQueue, worker.Options{})
	rw.RegisterWorkflow(RunWorkflow)
	rw.RegisterActivity(a)
	if e = rw.Start(); e != nil {
		t.Fatal(e)
	}
	defer rw.Stop()
	o := store.Outbox{Workspace: wid, Kind: "run", Subject: run.ID}
	for i := 0; i < 2; i++ {
		if e = dispatchTo(ctx, c, o, runQueue, queue); e != nil {
			t.Fatal(e)
		}
	}
	if e = c.GetWorkflow(ctx, WorkflowID(o), "").Get(ctx, nil); e != nil {
		t.Fatal(e)
	}
	var status string
	if e = s.Pool.QueryRow(ctx, "SELECT status FROM runs WHERE workspace_id=$1 AND id=$2", wid, run.ID).Scan(&status); e != nil || status != "SUCCEEDED" {
		t.Fatalf("live run %s %v", status, e)
	}
}
