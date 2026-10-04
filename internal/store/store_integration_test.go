package store

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"nemi/internal/domain"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAdmissionRetryAndDailyBudget(t *testing.T) {
	s, w := fixture(t)
	ctx := context.Background()
	newRun := func() Ref {
		m := create(t, s, w, nil)
		rr, e := s.Command(ctx, w, domain.ID(), "budget-run", nil, func(tx pgx.Tx) (any, int, error) { return s.CreateRun(ctx, tx, w, m.ID, "managed", "fixture", 1) })
		if e != nil {
			t.Fatal(e)
		}
		var r domain.Run
		json.Unmarshal(rr.Body, &r)
		// Delayed dispatch consumes today's budget rather than its creation day's.
		if _, err := s.Pool.Exec(ctx, "UPDATE runs SET created_at=now()-interval '2 days' WHERE workspace_id=$1 AND id=$2", w, r.ID); err != nil {
			t.Fatal(err)
		}
		return Ref{w, r.ID}
	}
	for i := 0; i < 3; i++ {
		r := newRun()
		calls := 0
		reserve := func(string, string) int64 { calls++; return 900000 }
		in, e := s.AdmitRun(ctx, r, reserve)
		if e != nil || !in.Ready {
			t.Fatalf("admission %v %v", in, e)
		}
		in, e = s.AdmitRun(ctx, r, reserve)
		if e != nil || !in.Ready || calls != 1 {
			t.Fatal("admission retry reserved twice")
		}
		if _, e = s.ClaimModel(ctx, r); e != nil {
			t.Fatal(e)
		}
		if e = s.FailRun(ctx, r, "FIXTURE_KNOWN_COST", 1, 1, 900000); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.AdmitRun(ctx, newRun(), func(string, string) int64 { return 900000 }); e == nil || e.Error() != "DAILY_BUDGET_EXCEEDED" {
		t.Fatal("daily budget was not enforced")
	}
}

func fixture(t *testing.T) (*Store, string) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	if !strings.Contains(url, "nemi_test") {
		t.Fatal("integration tests require a dedicated nemi_test database")
	}
	s, e := Open(context.Background(), url)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Migrate(context.Background()); e != nil {
		t.Fatal(e)
	}
	w := "test-" + domain.ID()
	if _, e = s.Pool.Exec(context.Background(), "INSERT INTO workspaces(id) VALUES($1)", w); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		s.Pool.Exec(context.Background(), "UPDATE runs SET status='FAILED' WHERE workspace_id=$1 AND status IN ('QUEUED','RUNNING')", w)
		s.Pool.Close()
	})
	return s, w
}
func create(t *testing.T, s *Store, w string, at *time.Time) domain.Matter {
	t.Helper()
	ctx := context.Background()
	r, e := s.Command(ctx, w, domain.ID(), "create", []byte("create"), func(tx pgx.Tx) (any, int, error) {
		return s.CreateMatter(ctx, tx, w, domain.CreateMatter{Title: "材料准备", Source: "准备身份证复印件", Category: "life", Confirmed: true, Timezone: "Asia/Shanghai", ReminderAt: at})
	})
	if e != nil {
		t.Fatal(e)
	}
	var m domain.Matter
	if e = json.Unmarshal(r.Body, &m); e != nil {
		t.Fatal(e)
	}
	return m
}
func TestCommandConcurrentDedupAndScope(t *testing.T) {
	s, w := fixture(t)
	ctx := context.Background()
	key := domain.ID()
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	ids := make(chan string, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := s.Command(ctx, w, key, "create", []byte("same"), func(tx pgx.Tx) (any, int, error) {
				return s.CreateMatter(ctx, tx, w, domain.CreateMatter{Title: "测试", Category: "life"})
			})
			if e != nil {
				errs <- e
				return
			}
			var m domain.Matter
			json.Unmarshal(r.Body, &m)
			ids <- m.ID
		}()
	}
	wg.Wait()
	close(errs)
	close(ids)
	for e := range errs {
		t.Error(e)
	}
	var first string
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatal("duplicate matter created")
		}
	}
	_, e := s.Command(ctx, w, key, "create", []byte("different"), func(tx pgx.Tx) (any, int, error) { t.Fatal("different request executed"); return nil, 0, nil })
	if !errors.Is(e, domain.ErrConflict) {
		t.Fatal("idempotency conflict missing")
	}
	_, e = s.Command(ctx, w, key+"scope", "patch", nil, func(tx pgx.Tx) (any, int, error) {
		return s.EditMatter(ctx, tx, "another-space", first, EditMatter{Expected: 1})
	})
	if !errors.Is(e, domain.ErrNotFound) {
		t.Fatal("cross-space access permitted")
	}
}
func TestOldReminderRevisionAndRepeatedDelivery(t *testing.T) {
	s, w := fixture(t)
	ctx := context.Background()
	at := time.Now().Add(-time.Minute)
	m := create(t, s, w, &at)
	var rid string
	s.Pool.QueryRow(ctx, "SELECT id FROM reminders WHERE workspace_id=$1 AND matter_id=$2", w, m.ID).Scan(&rid)
	r, e := s.Command(ctx, w, domain.ID(), "reminder", nil, func(tx pgx.Tx) (any, int, error) {
		return s.SaveReminder(ctx, tx, w, m.ID, SaveReminder{Expected: 1, At: &at, Enabled: true})
	})
	if e != nil {
		t.Fatal(e)
	}
	var reminder domain.Reminder
	json.Unmarshal(r.Body, &reminder)
	if e = s.DeliverReminder(ctx, Ref{w, rid}, 1); e != nil {
		t.Fatal(e)
	}
	var count int
	s.Pool.QueryRow(ctx, "SELECT count(*) FROM notifications WHERE workspace_id=$1", w).Scan(&count)
	if count != 0 {
		t.Fatal("stale reminder delivered")
	}
	for i := 0; i < 2; i++ {
		if e = s.DeliverReminder(ctx, Ref{w, rid}, 2); e != nil {
			t.Fatal(e)
		}
	}
	s.Pool.QueryRow(ctx, "SELECT count(*) FROM notifications WHERE workspace_id=$1", w).Scan(&count)
	if count != 1 {
		t.Fatal("delivery was not deduplicated")
	}
	_, e = s.Command(ctx, w, domain.ID(), "complete", nil, func(tx pgx.Tx) (any, int, error) {
		status := "COMPLETED"
		return s.EditMatter(ctx, tx, w, m.ID, EditMatter{Expected: 1, Status: &status})
	})
	if e != nil {
		t.Fatal(e)
	}
	var enabled bool
	s.Pool.QueryRow(ctx, "SELECT enabled FROM reminders WHERE workspace_id=$1 AND id=$2", w, rid).Scan(&enabled)
	if enabled {
		t.Fatal("completed matter still has a live reminder")
	}
}
func TestStalePlanCannotOverwriteUserChecklist(t *testing.T) {
	s, w := fixture(t)
	ctx := context.Background()
	m := create(t, s, w, nil)
	rr, e := s.Command(ctx, w, domain.ID(), "run", nil, func(tx pgx.Tx) (any, int, error) { return s.CreateRun(ctx, tx, w, m.ID, "fixture", "fixture:", 1) })
	if e != nil {
		t.Fatal(e)
	}
	var r domain.Run
	json.Unmarshal(rr.Body, &r)
	ref := Ref{w, r.ID}
	if _, e = s.AdmitRun(ctx, ref, func(string, string) int64 { return 0 }); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ClaimModel(ctx, ref); e != nil {
		t.Fatal(e)
	}
	items := []domain.Item{{Text: "本人确认的清单", Done: true}}
	_, e = s.Command(ctx, w, domain.ID(), "edit", nil, func(tx pgx.Tx) (any, int, error) {
		return s.EditMatter(ctx, tx, w, m.ID, EditMatter{Expected: 1, Items: &items})
	})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.FinishRun(ctx, ref, domain.Plan{Summary: "旧版本结果", Items: []string{"旧任务"}}, 0, 0, 0); e != nil {
		t.Fatal(e)
	}
	d, e := s.Dashboard(ctx, Identity{Workspace: w}, "fixture")
	if e != nil {
		t.Fatal(e)
	}
	if d.Matters[0].Items[0].Text != "本人确认的清单" {
		t.Fatal("newer checklist overwritten")
	}
	if d.Runs[0].Result == nil {
		t.Fatal("historical result missing")
	}
}
func TestUnknownAttemptRetainsBudgetAndCannotBeResubmitted(t *testing.T) {
	s, w := fixture(t)
	ctx := context.Background()
	m := create(t, s, w, nil)
	rr, e := s.Command(ctx, w, domain.ID(), "run", nil, func(tx pgx.Tx) (any, int, error) { return s.CreateRun(ctx, tx, w, m.ID, "managed", "qwen:test", 1) })
	if e != nil {
		t.Fatal(e)
	}
	var r domain.Run
	json.Unmarshal(rr.Body, &r)
	ref := Ref{w, r.ID}
	if _, e = s.AdmitRun(ctx, ref, func(string, string) int64 { return 800000 }); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ClaimModel(ctx, ref); e != nil {
		t.Fatal(e)
	}
	if e = s.FailRun(ctx, ref, "MODEL_OUTCOME_UNKNOWN", 0, 0, 0); e != nil {
		t.Fatal(e)
	}
	var reserved int64
	var attempt string
	s.Pool.QueryRow(ctx, "SELECT reserved_micro_cny,attempt_status FROM runs WHERE workspace_id=$1 AND id=$2", w, r.ID).Scan(&reserved, &attempt)
	if reserved != 800000 || attempt != "UNKNOWN" {
		t.Fatal("unknown paid request reservation released")
	}
	if _, e = s.ClaimModel(ctx, ref); e == nil {
		t.Fatal("unknown attempt resubmitted")
	}
}
