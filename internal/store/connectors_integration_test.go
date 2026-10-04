package store

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"nemi/internal/domain"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDispatchDurableClaimScopeAndQuota(t *testing.T) {
	s, w := fixture(t)
	ctx := context.Background()
	key := domain.ID()
	var claimed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, ok, e := s.ClaimDispatch(ctx, w, key, "feishu", []byte("one confirmed message"))
			if e != nil {
				t.Error(e)
			}
			if ok {
				claimed.Add(1)
			}
		}()
	}
	wg.Wait()
	if claimed.Load() != 1 {
		t.Fatal("multiple outbound claims")
	}
	status, ok, e := s.ClaimDispatch(ctx, w, key, "feishu", []byte("one confirmed message"))
	if e != nil || ok || status != "UNKNOWN" {
		t.Fatal("uncertain dispatch became retryable")
	}
	if _, _, e = s.ClaimDispatch(ctx, w, key, "wecom", []byte("one confirmed message")); !errors.Is(e, domain.ErrConflict) {
		t.Fatal("key reused for another connector")
	}
	if _, _, e = s.ClaimDispatch(ctx, w, key, "feishu", []byte("changed text")); !errors.Is(e, domain.ErrConflict) {
		t.Fatal("key reused for changed text")
	}
	if e = s.SettleDispatch(ctx, w, key, "DELIVERED"); e != nil {
		t.Fatal(e)
	}
	status, ok, e = s.ClaimDispatch(ctx, w, key, "feishu", []byte("one confirmed message"))
	if e != nil || ok || status != "DELIVERED" {
		t.Fatal("receipt not replayed")
	}
	_, other := fixture(t)
	if _, ok, e = s.ClaimDispatch(ctx, other, key, "feishu", []byte("one confirmed message")); e != nil || !ok {
		t.Fatal("workspace isolation lost")
	}
	for i := 1; i < 10; i++ {
		if _, _, e = s.ClaimDispatch(ctx, w, domain.ID(), "feishu", []byte("new message")); e != nil {
			t.Fatal(e)
		}
	}
	if _, _, e = s.ClaimDispatch(ctx, w, domain.ID(), "feishu", []byte("new message")); !errors.Is(e, domain.ErrBusy) {
		t.Fatal("quota not enforced")
	}
}
func TestCalendarScopeAndCurrentTime(t *testing.T) {
	s, w := fixture(t)
	ctx := context.Background()
	at := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	m := create(t, s, w, &at)
	title, got, e := s.CalendarTime(ctx, w, m.ID)
	if e != nil || title != m.Title || !got.Equal(at) {
		t.Fatal("calendar did not use scheduled time", e)
	}
	if _, _, e = s.CalendarTime(ctx, "another-workspace", m.ID); !errors.Is(e, pgx.ErrNoRows) {
		t.Fatal("calendar crossed workspace")
	}
	noTime := create(t, s, w, nil)
	if _, _, e = s.CalendarTime(ctx, w, noTime.ID); !errors.Is(e, domain.ErrConflict) {
		t.Fatal("invented calendar time")
	}
	if _, e = s.Pool.Exec(ctx, "UPDATE matters SET status='COMPLETED' WHERE workspace_id=$1 AND id=$2", w, m.ID); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.CalendarTime(ctx, w, m.ID); !errors.Is(e, pgx.ErrNoRows) {
		t.Fatal("completed matter exported")
	}
}
