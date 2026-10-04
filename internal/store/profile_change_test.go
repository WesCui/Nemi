package store

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"nemi/internal/domain"
	"testing"
)

func TestProfileMismatchBeforeSubmissionReleasesReserve(t *testing.T) {
	s, w := fixture(t)
	ctx := context.Background()
	m := create(t, s, w, nil)
	r, e := s.Command(ctx, w, domain.ID(), "old-profile", nil, func(tx pgx.Tx) (any, int, error) { return s.CreateRun(ctx, tx, w, m.ID, "managed", "old-profile", 1) })
	if e != nil {
		t.Fatal(e)
	}
	var run domain.Run
	json.Unmarshal(r.Body, &run)
	ref := Ref{w, run.ID}
	if _, e = s.AdmitRun(ctx, ref, func(string, string) int64 { return 800000 }); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ClaimModel(ctx, ref, "new-profile"); e == nil || e.Error() != "MODEL_CONFIG_CHANGED" {
		t.Fatal("changed profile reached model claim")
	}
	var attempt string
	if e = s.Pool.QueryRow(ctx, "SELECT attempt_status FROM runs WHERE workspace_id=$1 AND id=$2", w, run.ID).Scan(&attempt); e != nil || attempt != "RESERVED" {
		t.Fatal("unsubmitted claim wasn't rolled back", e)
	}
	if e = s.FailRun(ctx, ref, "MODEL_CONFIG_CHANGED", 0, 0, 0); e != nil {
		t.Fatal(e)
	}
	var reserved int64
	if e = s.Pool.QueryRow(ctx, "SELECT attempt_status,reserved_micro_cny FROM runs WHERE workspace_id=$1 AND id=$2", w, run.ID).Scan(&attempt, &reserved); e != nil || attempt != "SETTLED" || reserved != 0 {
		t.Fatal("unsubmitted request retained unknown budget", e)
	}
}
