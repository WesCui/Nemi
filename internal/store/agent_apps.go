package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"nemi/internal/domain"
)

func (s *Store) ReadAction(ctx context.Context, w, id string) (domain.AgentAction, error) {
	var a domain.AgentAction
	err := s.Pool.QueryRow(ctx, "SELECT id,run_id,kind,payload,status,result_id FROM agent_actions WHERE workspace_id=$1 AND id=$2", w, id).Scan(&a.ID, &a.RunID, &a.Kind, &a.Payload, &a.Status, &a.ResultID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return a, err
}

// Approval, pinned recipient validation and dispatch claim commit together.
// Action ID is the stable dispatch key even when the browser changes its key.
func (s *Store) ClaimAgentMessage(ctx context.Context, w, id string, currentRevision int) (string, bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback(ctx)
	a, err := s.Action(ctx, tx, w, id)
	if err != nil {
		return "", false, err
	}
	if a.Kind != "send_message" || a.Status == "DECLINED" {
		return "", false, domain.ErrConflict
	}
	var p domain.AgentMessage
	if json.Unmarshal(a.Payload, &p) != nil {
		return "", false, domain.ErrConflict
	}
	if a.Status == "PENDING" {
		var revision int
		var enabled bool
		var label string
		if err = tx.QueryRow(ctx, "SELECT revision,enabled,label FROM app_connections WHERE workspace_id=$1 AND id=$2 FOR SHARE", w, p.Channel).Scan(&revision, &enabled, &label); err != nil {
			return "", false, domain.ErrConflict
		}
		if !enabled || revision != p.Revision || revision != currentRevision || label != p.Recipient {
			return "", false, domain.ErrConflict
		}
	}
	status, claimed, err := claimDispatchTx(ctx, tx, w, id, p.Channel, a.Payload)
	if err != nil {
		return "", false, err
	}
	if a.Status == "PENDING" {
		if err = s.DecideAction(ctx, tx, w, id, "APPROVED", id); err != nil {
			return "", false, err
		}
	}
	return status, claimed, tx.Commit(ctx)
}
