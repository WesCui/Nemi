package store

import (
	"context"
	"github.com/jackc/pgx/v5"
	"nemi/internal/domain"
	"testing"
)

func TestScopedRelayCannotConsumeAnotherTestWorkspace(t *testing.T) {
	s, w := fixture(t)
	ctx := context.Background()
	m := create(t, s, w, nil)
	if _, err := s.Command(ctx, w, domain.ID(), "scoped-run", nil, func(tx pgx.Tx) (any, int, error) { return s.CreateRun(ctx, tx, w, m.ID, "demo", "fixture", 1) }); err != nil {
		t.Fatal(err)
	}
	out, err := s.LeaseOutbox(ctx, w)
	if err != nil || out == nil || out.Workspace != w {
		t.Fatal("scoped outbox not leased", err)
	}
	if err = s.CompleteOutbox(ctx, *out); err != nil {
		t.Fatal(err)
	}
	if another, err := s.LeaseOutbox(ctx, w); err != nil || another != nil {
		t.Fatal("scoped relay consumed another workspace", err)
	}
}
