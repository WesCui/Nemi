package store

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"nemi/internal/domain"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRecurringDeliveryAdvancesOnceSkipsMissedAndCanStop(t *testing.T) {
	s, w := fixture(t)
	ctx := context.Background()
	at := time.Now().Add(-72 * time.Hour)
	until := time.Now().Add(7 * 24 * time.Hour)
	m := create(t, s, w, &at)
	var rid string
	if e := s.Pool.QueryRow(ctx, "UPDATE reminders SET repeat='daily',repeat_until=$2 WHERE workspace_id=$1 RETURNING id", w, until).Scan(&rid); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- s.DeliverReminder(ctx, Ref{w, rid}, 1) }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	d, e := s.Dashboard(ctx, Identity{Workspace: w}, "demo")
	if e != nil {
		t.Fatal(e)
	}
	if len(d.Notifications) != 1 || d.Notifications[0].Status != "OVERDUE" || len(d.Reminders) != 1 || d.Reminders[0].Revision != 2 || !d.Reminders[0].Due.After(time.Now()) {
		t.Fatalf("recurrence not advanced safely: %+v", d.Reminders)
	}
	var count int
	if e = s.Pool.QueryRow(ctx, "SELECT count(*) FROM outbox WHERE workspace_id=$1 AND kind='reminder' AND revision=2", w).Scan(&count); e != nil || count != 1 {
		t.Fatal("next occurrence wasn't enqueued once", e, count)
	}
	r := d.Reminders[0]
	_, e = s.Command(ctx, w, domain.ID(), "stop-repeat", nil, func(tx pgx.Tx) (any, int, error) {
		return s.SaveReminder(ctx, tx, w, m.ID, SaveReminder{Expected: r.Revision, At: &r.Nominal, Repeat: r.Repeat, Until: r.Until, Enabled: false})
	})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.DeliverReminder(ctx, Ref{w, rid}, 2); e != nil {
		t.Fatal(e)
	}
	d, e = s.Dashboard(ctx, Identity{Workspace: w}, "demo")
	if e != nil || len(d.Notifications) != 1 || d.Reminders[0].Enabled {
		t.Fatal("stopped recurrence delivered", e)
	}
}
func TestRecurringEndDateDisablesFutureWork(t *testing.T) {
	s, w := fixture(t)
	ctx := context.Background()
	at := time.Now().Add(-time.Minute)
	create(t, s, w, &at)
	var rid string
	if e := s.Pool.QueryRow(ctx, "UPDATE reminders SET repeat='daily',repeat_until=nominal_at WHERE workspace_id=$1 RETURNING id", w).Scan(&rid); e != nil {
		t.Fatal(e)
	}
	if e := s.DeliverReminder(ctx, Ref{w, rid}, 1); e != nil {
		t.Fatal(e)
	}
	d, e := s.Dashboard(ctx, Identity{Workspace: w}, "demo")
	if e != nil || d.Reminders[0].Enabled || d.Reminders[0].SyncStatus != "ENDED" {
		t.Fatal("end date ignored", e)
	}
	if e = s.DeliverReminder(ctx, Ref{w, rid}, 1); e != nil {
		t.Fatal(e)
	}
	d, e = s.Dashboard(ctx, Identity{Workspace: w}, "demo")
	if e != nil || len(d.Notifications) != 1 {
		t.Fatal("ended occurrence delivered twice", e)
	}
}
func savePreference(t *testing.T, s *Store, w, category, text string) MemoryRef {
	t.Helper()
	ctx := context.Background()
	r, e := s.Command(ctx, w, domain.ID(), "memory", nil, func(tx pgx.Tx) (any, int, error) {
		return s.SaveMemory(ctx, tx, w, "", SaveMemory{Category: category, Text: text, Confirmed: true})
	})
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(r.Body), text) {
		t.Fatal("preference text duplicated in command response")
	}
	var ref MemoryRef
	if e = json.Unmarshal(r.Body, &ref); e != nil {
		t.Fatal(e)
	}
	return ref
}
func TestPreferenceScopeOptOutAndDeletionBeforeSubmission(t *testing.T) {
	s, w := fixture(t)
	ctx := context.Background()
	general := savePreference(t, s, w, "general", "本人偏好：清单按顺序排列")
	life := savePreference(t, s, w, "life", "本人偏好：先整理纸质资料")
	savePreference(t, s, w, "travel", "不相关出行偏好")
	other := "other-" + domain.ID()
	if _, e := s.Pool.Exec(ctx, "INSERT INTO workspaces(id) VALUES($1)", other); e != nil {
		t.Fatal(e)
	}
	savePreference(t, s, other, "general", "其他用户的偏好")
	newRun := func(use bool) (domain.Matter, Ref) {
		m := create(t, s, w, nil)
		r, e := s.Command(ctx, w, domain.ID(), "run", nil, func(tx pgx.Tx) (any, int, error) { return s.CreateRun(ctx, tx, w, m.ID, "demo", "fixture", 1, use) })
		if e != nil {
			t.Fatal(e)
		}
		var run domain.Run
		json.Unmarshal(r.Body, &run)
		return m, Ref{w, run.ID}
	}
	_, r := newRun(true)
	in, e := s.AdmitRun(ctx, r, func(_ string, source string) int64 {
		if !strings.Contains(source, general.ID) && !strings.Contains(source, "本人偏好") {
			t.Error("preference missing from reservation context")
		}
		return 100
	})
	if e != nil || !in.Ready {
		t.Fatal(e)
	}
	_, e = s.Command(ctx, w, domain.ID(), "delete-memory", nil, func(tx pgx.Tx) (any, int, error) { return s.DeleteMemory(ctx, tx, w, life.ID, 1) })
	if e != nil {
		t.Fatal(e)
	}
	in, e = s.ClaimModel(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(in.Source, "清单按顺序") || strings.Contains(in.Source, "纸质资料") || strings.Contains(in.Source, "其他用户") || strings.Contains(in.Source, "不相关") {
		t.Fatal("context ignored deletion or scope")
	}
	if e = s.FinishRun(ctx, r, domain.Plan{Summary: "测试", Items: []string{"检查要求"}}, 0, 0, 0); e != nil {
		t.Fatal(e)
	}
	_, r = newRun(false)
	if _, e = s.AdmitRun(ctx, r, func(string, string) int64 { return 0 }); e != nil {
		t.Fatal(e)
	}
	in, e = s.ClaimModel(ctx, r)
	if e != nil || strings.Contains(in.Source, "已确认偏好") {
		t.Fatal("memory opt-out ignored", e)
	}
	if e = s.FailRun(ctx, r, "FIXTURE", 0, 0, 0); e != nil {
		t.Fatal(e)
	}
	_, r = newRun(true)
	_, e = s.Command(ctx, w, domain.ID(), "edit-memory", nil, func(tx pgx.Tx) (any, int, error) {
		return s.SaveMemory(ctx, tx, w, general.ID, SaveMemory{Expected: 1, Category: "general", Text: "修改后的偏好", Confirmed: true})
	})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.AdmitRun(ctx, r, func(string, string) int64 { return 0 }); e != nil {
		t.Fatal(e)
	}
	in, e = s.ClaimModel(ctx, r)
	if e != nil || strings.Contains(in.Source, "偏好") {
		t.Fatal("a queued run silently used a changed preference", e)
	}
	_, e = s.Command(ctx, w, domain.ID(), "cross-space-delete", nil, func(tx pgx.Tx) (any, int, error) { return s.DeleteMemory(ctx, tx, other, general.ID, 2) })
	if !errors.Is(e, domain.ErrNotFound) {
		t.Fatal("cross-space preference mutation permitted")
	}
	d, e := s.Dashboard(ctx, Identity{Workspace: w}, "demo")
	if e != nil {
		t.Fatal(e)
	}
	if len(d.Memories) != 2 || len(d.Activity) == 0 {
		t.Fatal("memory/activity missing")
	}
}
func TestPreferenceSelectionHasFixedContextBudget(t *testing.T) {
	s, w := fixture(t)
	ctx := context.Background()
	for i := 0; i < 8; i++ {
		savePreference(t, s, w, "general", strings.Repeat("偏", 490))
	}
	m := create(t, s, w, nil)
	r, e := s.Command(ctx, w, domain.ID(), "bounded-memory-run", nil, func(tx pgx.Tx) (any, int, error) { return s.CreateRun(ctx, tx, w, m.ID, "demo", "fixture", 1, true) })
	if e != nil {
		t.Fatal(e)
	}
	var run domain.Run
	json.Unmarshal(r.Body, &run)
	ref := Ref{w, run.ID}
	in, e := s.AdmitRun(ctx, ref, func(string, string) int64 { return 0 })
	if e != nil || len(in.Source) > len(m.Source)+2600 {
		t.Fatal("unbounded preference context", e)
	}
	if _, e = s.ClaimModel(ctx, ref); e != nil {
		t.Fatal(e)
	}
	d, e := s.Dashboard(ctx, Identity{Workspace: w}, "demo")
	if e != nil || d.Runs[0].UsedMemories != 1 {
		t.Fatal("context budget not applied", e)
	}
}

func TestUpdatedSourceStartsNewContextAndProtectsCurrentMatter(t *testing.T) {
	s, w := fixture(t)
	ctx := context.Background()
	m := create(t, s, w, nil)
	rr, e := s.Command(ctx, w, domain.ID(), "run", nil, func(tx pgx.Tx) (any, int, error) { return s.CreateRun(ctx, tx, w, m.ID, "demo", "fixture", 1) })
	if e != nil {
		t.Fatal(e)
	}
	var run domain.Run
	json.Unmarshal(rr.Body, &run)
	ref := Ref{w, run.ID}
	if _, e = s.AdmitRun(ctx, ref, func(string, string) int64 { return 0 }); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ClaimModel(ctx, ref); e != nil {
		t.Fatal(e)
	}
	source := "新要求：两位同行，预算改为1000元"
	_, e = s.Command(ctx, w, domain.ID(), "update-source", nil, func(tx pgx.Tx) (any, int, error) {
		return s.EditMatter(ctx, tx, w, m.ID, EditMatter{Expected: 1, Source: &source})
	})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.FinishRun(ctx, ref, domain.Plan{Summary: "旧结果", Items: []string{"旧资料清单"}}, 0, 0, 0); e != nil {
		t.Fatal(e)
	}
	d, e := s.Dashboard(ctx, Identity{Workspace: w}, "demo")
	if e != nil || d.Matters[0].Source != source || len(d.Matters[0].Items) != 0 {
		t.Fatal("old run overwrote redirected matter", e)
	}
	rr, e = s.Command(ctx, w, domain.ID(), "new-run", nil, func(tx pgx.Tx) (any, int, error) { return s.CreateRun(ctx, tx, w, m.ID, "demo", "fixture", 2) })
	if e != nil {
		t.Fatal(e)
	}
	json.Unmarshal(rr.Body, &run)
	ref = Ref{w, run.ID}
	in, e := s.AdmitRun(ctx, ref, func(string, string) int64 { return 0 })
	if e != nil || in.Source != source {
		t.Fatal("new run used old context", e)
	}
}
