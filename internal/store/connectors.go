package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"github.com/jackc/pgx/v5"
	"nemi/internal/domain"
	"time"
)

// Claim commits BEFORE the network call. A crash or lost response never resends.
func (s *Store) ClaimDispatch(ctx context.Context, w, key, id string, body []byte) (string, bool, error) {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return "", false, e
	}
	defer tx.Rollback(ctx)
	status, claimed, e := claimDispatchTx(ctx, tx, w, key, id, body)
	if e != nil || !claimed {
		return status, claimed, e
	}
	return status, true, tx.Commit(ctx)
}
func claimDispatchTx(ctx context.Context, tx pgx.Tx, w, key, id string, body []byte) (string, bool, error) {
	if _, e := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "dispatch:"+w); e != nil {
		return "", false, e
	}
	h := sha256.Sum256(body)
	var oldID, status string
	var oldHash []byte
	e := tx.QueryRow(ctx, "SELECT connector_id,body_hash,status FROM connector_dispatches WHERE workspace_id=$1 AND key=$2", w, key).Scan(&oldID, &oldHash, &status)
	if e == nil {
		if oldID != id || !bytes.Equal(oldHash, h[:]) {
			return "", false, domain.ErrConflict
		}
		if status == "SENDING" {
			status = "UNKNOWN"
		}
		return status, false, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return "", false, e
	}
	var minute, day int
	e = tx.QueryRow(ctx, "SELECT count(*) FILTER(WHERE created_at>now()-interval '1 minute'),count(*) FROM connector_dispatches WHERE workspace_id=$1 AND created_at>now()-interval '24 hours'", w).Scan(&minute, &day)
	if e != nil {
		return "", false, e
	}
	if minute >= 10 || day >= 100 {
		return "", false, domain.ErrBusy
	}
	_, e = tx.Exec(ctx, "INSERT INTO connector_dispatches(workspace_id,key,connector_id,body_hash,status) VALUES($1,$2,$3,$4,'SENDING')", w, key, id, h[:])
	if e != nil {
		return "", false, e
	}
	return "SENDING", true, nil
}
func (s *Store) SettleDispatch(ctx context.Context, w, key, status string) error {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	tag, e := tx.Exec(ctx, "UPDATE connector_dispatches SET status=$3 WHERE workspace_id=$1 AND key=$2 AND status='SENDING'", w, key, status)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		return tx.Commit(ctx)
	}
	var parent string
	e = tx.QueryRow(ctx, "SELECT run_id FROM agent_actions WHERE workspace_id=$1 AND id=$2", w, key).Scan(&parent)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	if e == nil {
		if e = wakeContinuation(ctx, tx, Ref{w, parent}); e != nil {
			return e
		}
		if e = event(ctx, tx, w, "agent.message_receipt", key); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
func (s *Store) CalendarTime(ctx context.Context, w, id string) (string, time.Time, error) {
	var title string
	var at *time.Time
	e := s.Pool.QueryRow(ctx, `SELECT m.title,COALESCE((SELECT due_at FROM reminders r WHERE r.workspace_id=m.workspace_id AND r.matter_id=m.id AND r.enabled AND r.sync_status NOT IN ('FIRED','ENDED')),m.deadline) FROM matters m WHERE m.workspace_id=$1 AND m.id=$2 AND m.status='ACTIVE'`, w, id).Scan(&title, &at)
	if e != nil {
		return "", time.Time{}, e
	}
	if at == nil {
		return title, time.Time{}, domain.ErrConflict
	}
	return title, *at, nil
}
