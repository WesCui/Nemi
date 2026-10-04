package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"nemi/internal/config"
	"nemi/internal/domain"
	"nemi/internal/model"
	"nemi/internal/store"
	"nemi/internal/vault"
)

func TestAgentDataReviewApprovalAndConflict(t *testing.T) {
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
	ws, code := "data-api-"+domain.ID(), domain.ID()
	if err = s.Bootstrap(ctx, code, ws); err != nil {
		t.Fatal(err)
	}
	token, err := s.Login(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := vault.New(bytes.Repeat([]byte{42}, 32))
	a := &API{Store: s, Config: config.Config{Origin: "http://localhost:3000"}, Gateway: model.New(config.Config{Provider: "deepseek", Model: "fixture", Key: "test-only"}), Vault: v}
	handler := a.Handler()
	request := func(session, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Origin", a.Config.Origin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", domain.ID())
		r.AddCookie(&http.Cookie{Name: "nemi_session", Value: session})
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, r)
		return out
	}
	command := func(fn func(pgx.Tx) (any, int, error)) store.CommandResult {
		out, e := s.Command(ctx, ws, domain.ID(), "data-test", nil, fn)
		if e != nil {
			t.Fatal(e)
		}
		return out
	}
	chat := command(func(tx pgx.Tx) (any, int, error) {
		return s.CreateChatRun(ctx, tx, ws, "", "管理我的事项和偏好", "managed", "fixture", "")
	})
	var run struct {
		ID string `json:"run_id"`
	}
	json.Unmarshal(chat.Body, &run)
	ref := store.Ref{Workspace: ws, ID: run.ID}
	if _, err = s.AdmitRun(ctx, ref, func(string, string) int64 { return 200000 }); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ClaimModel(ctx, ref, "fixture"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.FailRun(ctx, ref, "FIXTURE_FINISHED", 0, 0, 0) })
	proposal := func(value any, e error) domain.AgentAction {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
		b, _ := json.Marshal(value)
		var out struct {
			ID string `json:"id"`
		}
		json.Unmarshal(b, &out)
		action, e := s.ReadAction(ctx, ws, out.ID)
		if e != nil {
			t.Fatal(e)
		}
		return action
	}
	approve := func(action domain.AgentAction, expected int) *httptest.ResponseRecorder {
		t.Helper()
		out := request(token, "/api/v1/agent/actions/"+action.ID+"/approve", `{"confirmed":true}`)
		if out.Code != expected {
			t.Fatalf("approval status %d, expected %d", out.Code, expected)
		}
		return out
	}
	at, until, deadline := time.Now().Add(24*time.Hour).UTC().Truncate(time.Second), time.Now().Add(7*24*time.Hour).UTC().Truncate(time.Second), time.Now().Add(30*24*time.Hour).UTC().Truncate(time.Second)
	created := command(func(tx pgx.Tx) (any, int, error) {
		return s.CreateMatter(ctx, tx, ws, domain.CreateMatter{Title: "出行准备", Source: "原始资料", Category: "travel", Deadline: &deadline, ReminderAt: &at, Repeat: "daily", Until: &until, Quiet: true})
	})
	var matter domain.Matter
	json.Unmarshal(created.Body, &matter)
	items := []domain.Item{}
	for i := 0; i < 30; i++ {
		items = append(items, domain.Item{Text: fmt.Sprintf("准备项目%d", i)})
	}
	command(func(tx pgx.Tx) (any, int, error) {
		return s.EditMatter(ctx, tx, ws, matter.ID, store.EditMatter{Expected: 1, Items: &items})
	})
	readMatter := func() domain.Matter {
		t.Helper()
		m, e := s.AgentMatter(ctx, ws, matter.ID)
		if e != nil {
			t.Fatal(e)
		}
		return m
	}
	readReminder := func() domain.Reminder {
		t.Helper()
		out, e := s.AgentReminder(ctx, ws, matter.ID)
		if e != nil {
			t.Fatal(e)
		}
		b, _ := json.Marshal(out)
		var data struct {
			Reminder domain.Reminder `json:"reminder"`
		}
		json.Unmarshal(b, &data)
		return data.Reminder
	}
	index, done, title := 17, true, "出行准备已更新"
	change := store.MatterChange{ID: matter.ID, EditMatter: store.EditMatter{Expected: 2, Title: &title}, Checklist: []store.ChecklistChange{{Index: &index, Done: &done}}, Append: []domain.Item{{Text: "带雨伞"}}}
	update := proposal(s.ProposeMatterChange(ctx, ref, change))
	duplicate := proposal(s.ProposeMatterChange(ctx, ref, change))
	if update.ID != duplicate.ID || readMatter().Revision != 2 || readMatter().Items[17].Done {
		t.Fatal("proposal mutated data or duplicated")
	}
	if out := request(token, "/api/v1/agent/actions/"+update.ID+"/approve", `{"confirmed":false}`); out.Code != 400 {
		t.Fatal("explicit review bypassed")
	}
	var wg sync.WaitGroup
	codes := make(chan int, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- request(token, "/api/v1/agent/actions/"+update.ID+"/approve", `{"confirmed":true}`).Code
		}()
	}
	wg.Wait()
	close(codes)
	for c := range codes {
		if c != 200 {
			t.Fatalf("concurrent confirmation status %d", c)
		}
	}
	changed := readMatter()
	if changed.Revision != 3 || changed.Title != title || len(changed.Items) != 31 || !changed.Items[17].Done || changed.Items[29].Text != items[29].Text || changed.Items[30].Text != "带雨伞" {
		t.Fatal("checklist update lost unrelated items or repeated")
	}
	if readReminder().Revision != 1 {
		t.Fatal("unrequested reminder change")
	}
	// A matter or reminder change invalidates the immutable review snapshot.
	staleTitle := "不应覆盖"
	stale := proposal(s.ProposeMatterChange(ctx, ref, store.MatterChange{ID: matter.ID, EditMatter: store.EditMatter{Expected: 3, Title: &staleTitle}}))
	nominal := at.Add(time.Hour)
	command(func(tx pgx.Tx) (any, int, error) {
		return s.SaveReminder(ctx, tx, ws, matter.ID, store.SaveReminder{Expected: 1, At: &nominal, Enabled: true, Quiet: true, Repeat: "daily", Until: &until})
	})
	approve(stale, 409)
	if readMatter().Title != title {
		t.Fatal("stale reminder snapshot overwrote matter")
	}
	staleReminder := proposal(s.ProposeReminderChange(ctx, ref, store.ReminderChange{ID: matter.ID, MatterRevision: 3, Expected: 2, Enabled: &done, At: &at}))
	source := "资料更新"
	command(func(tx pgx.Tx) (any, int, error) {
		return s.EditMatter(ctx, tx, ws, matter.ID, store.EditMatter{Expected: 3, Source: &source})
	})
	approve(staleReminder, 409)
	if readReminder().Revision != 2 {
		t.Fatal("stale matter snapshot changed reminder")
	}
	// Stopping a reminder changes only enabled state; recurrence is retained.
	no := false
	stop := proposal(s.ProposeReminderChange(ctx, ref, store.ReminderChange{ID: matter.ID, MatterRevision: 4, Expected: 2, Enabled: &no}))
	approve(stop, 200)
	stopped := readReminder()
	if stopped.Enabled || stopped.Revision != 3 || stopped.Repeat != "daily" || !stopped.Quiet || stopped.Until == nil || !stopped.Until.Equal(until) || !stopped.Nominal.Equal(nominal) {
		t.Fatal("stopping erased reminder settings")
	}
	// Date validation is repeated at confirmation, without any partial mutation.
	soon := time.Now().Add(time.Second)
	expiring := proposal(s.ProposeReminderChange(ctx, ref, store.ReminderChange{ID: matter.ID, MatterRevision: 4, Expected: 3, Enabled: &done, At: &soon, Repeat: "once"}))
	time.Sleep(time.Until(soon) + 20*time.Millisecond)
	approve(expiring, 422)
	if readReminder().Revision != 3 {
		t.Fatal("expired date partially applied")
	}
	restart := proposal(s.ProposeReminderChange(ctx, ref, store.ReminderChange{ID: matter.ID, MatterRevision: 4, Expected: 3, Enabled: &done, At: &at}))
	approve(restart, 200)
	archiveStatus := "ARCHIVED"
	archive := proposal(s.ProposeMatterChange(ctx, ref, store.MatterChange{ID: matter.ID, EditMatter: store.EditMatter{Expected: 4, Status: &archiveStatus}}))
	approve(archive, 200)
	if readMatter().Status != "ARCHIVED" || readMatter().Source != source || readReminder().Enabled || readReminder().Revision != 5 {
		t.Fatal("archive lost history or kept reminder active")
	}
	var cancellations int
	s.Pool.QueryRow(ctx, "SELECT count(*) FROM outbox WHERE workspace_id=$1 AND kind='cancel_reminder'", ws).Scan(&cancellations)
	if cancellations < 1 {
		t.Fatal("reminder cancellation not dispatched")
	}
	list, e := s.AgentMatters(ctx, ws)
	if e != nil || len(list) != 0 {
		t.Fatal("archived matter remained in default list")
	}
	archived, e := s.AgentMatterPage(ctx, ws, "出行", "ARCHIVED", 0)
	if e != nil || len(archived) != 1 {
		t.Fatal("archived matter cannot be found for restoration")
	}
	active := "ACTIVE"
	restore := proposal(s.ProposeMatterChange(ctx, ref, store.MatterChange{ID: matter.ID, EditMatter: store.EditMatter{Expected: 5, Status: &active}}))
	approve(restore, 200)
	if readReminder().Enabled {
		t.Fatal("restoration silently restarted reminder")
	}
	// Pending, declined, edited and deleted memories use real IDs and revisions.
	save := proposal(s.ProposeMemoryChange(ctx, ref, store.MemoryChange{Operation: "save", Category: "travel", Text: "出门优先坐地铁"}))
	memories, _ := s.AgentMemories(ctx, ws)
	if len(memories) != 0 {
		t.Fatal("preference saved without review")
	}
	firstSave := approve(save, 200)
	secondSave := approve(save, 200)
	var firstResult, secondResult map[string]string
	json.Unmarshal(firstSave.Body.Bytes(), &firstResult)
	json.Unmarshal(secondSave.Body.Bytes(), &secondResult)
	if firstResult["result_id"] == "" || secondResult["result_id"] != firstResult["result_id"] {
		t.Fatal("repeated approval changed result reference")
	}
	memories, _ = s.AgentMemories(ctx, ws)
	if len(memories) != 1 {
		t.Fatal("confirmation duplicated preference")
	}
	memory := memories[0]
	edit := proposal(s.ProposeMemoryChange(ctx, ref, store.MemoryChange{Operation: "save", ID: memory.ID, Expected: 1, Category: "travel", Text: "赶时间可以打车"}))
	removeStale := proposal(s.ProposeMemoryChange(ctx, ref, store.MemoryChange{Operation: "delete", ID: memory.ID, Expected: 1}))
	approve(edit, 200)
	approve(removeStale, 409)
	remove := proposal(s.ProposeMemoryChange(ctx, ref, store.MemoryChange{Operation: "delete", ID: memory.ID, Expected: 2}))
	if out := request(token, "/api/v1/agent/actions/"+remove.ID+"/dismiss", `{"confirmed":true}`); out.Code != 200 {
		t.Fatal("decline failed")
	}
	approve(remove, 409)
	remove = proposal(s.ProposeMemoryChange(ctx, ref, store.MemoryChange{Operation: "delete", ID: memory.ID, Expected: 2}))
	// Identical proposals are idempotent for this run; use a new turn after decline.
	if remove.Status != "DECLINED" {
		t.Fatal("same run regenerated declined action")
	}
	_, err = s.Command(ctx, ws, domain.ID(), "memory-independent-edit", nil, func(tx pgx.Tx) (any, int, error) {
		return s.SaveMemory(ctx, tx, ws, memory.ID, store.SaveMemory{Expected: 2, Category: "travel", Text: "公共交通优先", Confirmed: true})
	})
	if err != nil {
		t.Fatal(err)
	}
	remove = proposal(s.ProposeMemoryChange(ctx, ref, store.MemoryChange{Operation: "delete", ID: memory.ID, Expected: 3}))
	approve(remove, 200)
	memories, _ = s.AgentMemories(ctx, ws)
	if len(memories) != 0 {
		t.Fatal("memory record retained after delete")
	}
	var turnCount int
	s.Pool.QueryRow(ctx, "SELECT count(*) FROM chat_turns WHERE workspace_id=$1", ws).Scan(&turnCount)
	if turnCount != 1 {
		t.Fatal("deleting preference removed chat history")
	}
	// Another identity cannot review or propose against these records.
	other, otherCode := "data-other-"+domain.ID(), domain.ID()
	s.Bootstrap(ctx, otherCode, other)
	otherToken, _ := s.Login(ctx, otherCode)
	if out := request(otherToken, "/api/v1/agent/actions/"+save.ID+"/approve", `{"confirmed":true}`); out.Code != 404 {
		t.Fatal("foreign proposal exposed")
	}
	if _, e = s.AgentReminder(ctx, other, matter.ID); !errors.Is(e, domain.ErrNotFound) {
		t.Fatal("foreign reminder exposed")
	}
	if _, e = s.ProposeMatterChange(ctx, ref, store.MatterChange{ID: matter.ID, EditMatter: store.EditMatter{Expected: 6}, Checklist: []store.ChecklistChange{{Done: &done}}}); e == nil {
		t.Fatal("missing checklist index changed first item")
	}
	if _, e = s.ProposeReminderChange(ctx, ref, store.ReminderChange{ID: matter.ID, MatterRevision: 6, Expected: 5}); e == nil {
		t.Fatal("missing enabled flag disabled reminder")
	}
	if err = s.FailRun(ctx, ref, "FIXTURE_FINISHED", 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	if _, e = s.ProposeMemoryChange(ctx, ref, store.MemoryChange{Operation: "save", Category: "life", Text: "停止后不应保存"}); e == nil || e.Error() != "AGENT_NO_LONGER_ACTIVE" {
		t.Fatal("stopped worker produced proposal")
	}
}
