package store

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"nemi/internal/domain"
	"testing"
)

func TestModelCaptureAndRevocationBeforeSubmission(t *testing.T) {
	s, w := fixture(t)
	ctx := context.Background()
	matter := create(t, s, w, nil)
	m := PersonalModel{ID: domain.ID(), Label: "fixture", Provider: "qwen", Model: "fixture", InputPrice: 1, OutputPrice: 2, Credential: []byte("encrypted-fixture")}
	cmd := func(route string, fn func(pgx.Tx) (any, int, error)) {
		t.Helper()
		if _, err := s.Command(ctx, w, domain.ID(), route, nil, fn); err != nil {
			t.Fatal(err)
		}
	}
	cmd("save-model", func(tx pgx.Tx) (any, int, error) { return s.AddModel(ctx, tx, w, m) })
	cmd("select-model", func(tx pgx.Tx) (any, int, error) { return s.DefaultModel(ctx, tx, w, m.ID) })
	out, err := s.Command(ctx, w, domain.ID(), "capture", nil, func(tx pgx.Tx) (any, int, error) {
		selected, e := s.SelectedModel(ctx, tx, w, "")
		if e != nil {
			return nil, 0, e
		}
		if selected == nil || selected.ID != m.ID {
			t.Fatal("default not selected")
		}
		out, status, e := s.CreateRun(ctx, tx, w, matter.ID, "personal", "fixed-profile", 1)
		if e == nil {
			e = s.BindRunModel(ctx, tx, w, out.(domain.Run).ID, selected.ID)
		}
		return out, status, e
	})
	if err != nil {
		t.Fatal(err)
	}
	var run domain.Run
	json.Unmarshal(out.Body, &run)
	ref := Ref{w, run.ID}
	cmd("fallback", func(tx pgx.Tx) (any, int, error) { return s.DefaultModel(ctx, tx, w, "") })
	selected, err := s.RunModel(ctx, ref)
	if err != nil || selected.ID != m.ID {
		t.Fatal("queued task followed changed default", err)
	}
	if _, err = s.AdmitRun(ctx, ref, func(string, string) int64 { return 500 }); err != nil {
		t.Fatal(err)
	}
	cmd("revoke", func(tx pgx.Tx) (any, int, error) { return s.RemoveModel(ctx, tx, w, m.ID) })
	if _, err = s.ClaimModel(ctx, ref, "fixed-profile"); err == nil || err.Error() != "MODEL_CONFIG_REVOKED" {
		t.Fatal("revoked credential reached CALLING", err)
	}
	var attempt string
	s.Pool.QueryRow(ctx, "SELECT attempt_status FROM runs WHERE workspace_id=$1 AND id=$2", w, run.ID).Scan(&attempt)
	if attempt != "RESERVED" {
		t.Fatal("revoked claim was committed")
	}
	if err = s.FailRun(ctx, ref, "MODEL_CONFIG_REVOKED", 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	var length int
	s.Pool.QueryRow(ctx, "SELECT octet_length(credential) FROM personal_models WHERE workspace_id=$1 AND id=$2", w, m.ID).Scan(&length)
	if length != 0 {
		t.Fatal("revocation retained credential")
	}
	other := w + "-other"
	s.Pool.Exec(ctx, "INSERT INTO workspaces(id) VALUES($1)", other)
	tx, _ := s.Pool.Begin(ctx)
	defer tx.Rollback(ctx)
	if _, err = s.SelectedModel(ctx, tx, other, m.ID); err == nil {
		t.Fatal("cross-workspace model available")
	}
}
