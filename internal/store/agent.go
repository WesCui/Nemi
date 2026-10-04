package store

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"nemi/internal/domain"
	"time"
)

// Claims precede paid submissions; reservation extensions use the admission lock.
func (s *Store) ClaimAgentStep(ctx context.Context, r Ref, pos int, kind, name, configID string, required int64) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if configID != "" {
		var revoked bool
		err = tx.QueryRow(ctx, "SELECT revoked FROM personal_models WHERE workspace_id=$1 AND id=$2 FOR SHARE", r.Workspace, configID).Scan(&revoked)
		if errors.Is(err, pgx.ErrNoRows) || revoked {
			return errors.New("MODEL_CONFIG_REVOKED")
		}
		if err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(73310002)"); err != nil {
		return err
	}
	var status, attempt string
	var reserved, charged int64
	err = tx.QueryRow(ctx, "SELECT status,attempt_status,reserved_micro_cny,charged_micro_cny FROM runs WHERE workspace_id=$1 AND id=$2 FOR UPDATE", r.Workspace, r.ID).Scan(&status, &attempt, &reserved, &charged)
	if err != nil {
		return err
	}
	if status != "RUNNING" || attempt != "CALLING" {
		return errors.New("AGENT_NO_LONGER_ACTIVE")
	}
	if charged+required > 1000000 {
		return errors.New("RUN_BUDGET_EXCEEDED")
	}
	if required > reserved {
		extra := required - reserved
		var daily int64
		if err = tx.QueryRow(ctx, "SELECT COALESCE(sum(reserved_micro_cny+charged_micro_cny),0) FROM runs WHERE workspace_id=$1 AND budget_day=(SELECT budget_day FROM runs WHERE workspace_id=$1 AND id=$2)", r.Workspace, r.ID).Scan(&daily); err != nil {
			return err
		}
		if daily+extra > 3000000 {
			return errors.New("DAILY_BUDGET_EXCEEDED")
		}
		if _, err = tx.Exec(ctx, "UPDATE runs SET reserved_micro_cny=$3 WHERE workspace_id=$1 AND id=$2", r.Workspace, r.ID, required); err != nil {
			return err
		}
	}
	tag, err := tx.Exec(ctx, "INSERT INTO agent_steps(workspace_id,run_id,position,kind,name,status) VALUES($1,$2,$3,$4,$5,'CALLING') ON CONFLICT DO NOTHING", r.Workspace, r.ID, pos, kind, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("AGENT_STEP_ALREADY_STARTED")
	}
	if err = event(ctx, tx, r.Workspace, "agent.step_started", r.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) SettleAgentStep(ctx context.Context, r Ref, pos int, status, code string, input, output, cost int64) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Consistent lock order with run completion/failure and step claims.
	if _, err = tx.Exec(ctx, "SELECT id FROM runs WHERE workspace_id=$1 AND id=$2 FOR UPDATE", r.Workspace, r.ID); err != nil {
		return err
	}
	var kind string
	err = tx.QueryRow(ctx, "UPDATE agent_steps SET status=$4,error_code=$5,input_tokens=$6,output_tokens=$7,charged_micro_cny=$8 WHERE workspace_id=$1 AND run_id=$2 AND position=$3 AND status='CALLING' RETURNING kind", r.Workspace, r.ID, pos, status, code, input, output, cost).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if kind == "MODEL" {
		_, err = tx.Exec(ctx, `UPDATE runs SET charged_micro_cny=charged_micro_cny+$3,input_tokens=input_tokens+$4,output_tokens=output_tokens+$5,
 attempt_status=CASE WHEN status='FAILED' AND $6<>'UNKNOWN' AND NOT EXISTS(SELECT 1 FROM agent_steps WHERE workspace_id=$1 AND run_id=$2 AND kind='MODEL' AND status IN ('CALLING','UNKNOWN')) THEN 'SETTLED' ELSE attempt_status END,
 reserved_micro_cny=CASE WHEN status='FAILED' AND $6<>'UNKNOWN' AND NOT EXISTS(SELECT 1 FROM agent_steps WHERE workspace_id=$1 AND run_id=$2 AND kind='MODEL' AND status IN ('CALLING','UNKNOWN')) THEN 0 ELSE GREATEST(reserved_micro_cny-$3,0) END
 WHERE workspace_id=$1 AND id=$2`, r.Workspace, r.ID, cost, input, output, status)
		if err != nil {
			return err
		}
	}
	if err = event(ctx, tx, r.Workspace, "agent.step_completed", r.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) ProposeAction(ctx context.Context, r Ref, key, kind string, payload []byte) (domain.AgentAction, error) {
	out, err := s.Command(ctx, r.Workspace, key, "agent.propose", payload, func(tx pgx.Tx) (any, int, error) {
		var active bool
		if err := tx.QueryRow(ctx, "SELECT status='RUNNING' FROM runs WHERE workspace_id=$1 AND id=$2 FOR SHARE", r.Workspace, r.ID).Scan(&active); err != nil {
			return nil, 0, err
		}
		if !active {
			return nil, 0, errors.New("AGENT_NO_LONGER_ACTIVE")
		}
		a := domain.AgentAction{ID: domain.ID(), RunID: r.ID, Kind: kind, Payload: json.RawMessage(payload), Status: "PENDING"}
		_, err := tx.Exec(ctx, "INSERT INTO agent_actions(workspace_id,id,run_id,kind,payload) VALUES($1,$2,$3,$4,$5)", r.Workspace, a.ID, r.ID, kind, payload)
		if err == nil {
			err = event(ctx, tx, r.Workspace, "agent.action_pending", r.ID)
		}
		return a, 201, err
	})
	var a domain.AgentAction
	if err == nil {
		err = json.Unmarshal(out.Body, &a)
	}
	return a, err
}
func (s *Store) Action(ctx context.Context, tx pgx.Tx, w, id string) (domain.AgentAction, error) {
	var a domain.AgentAction
	err := tx.QueryRow(ctx, "SELECT id,run_id,kind,payload,status,result_id FROM agent_actions WHERE workspace_id=$1 AND id=$2 FOR UPDATE", w, id).Scan(&a.ID, &a.RunID, &a.Kind, &a.Payload, &a.Status, &a.ResultID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return a, err
}
func (s *Store) DecideAction(ctx context.Context, tx pgx.Tx, w, id, status, result string) error {
	_, err := tx.Exec(ctx, "UPDATE agent_actions SET status=$3,result_id=$4 WHERE workspace_id=$1 AND id=$2 AND status='PENDING'", w, id, status, result)
	if err == nil {
		err = event(ctx, tx, w, "agent.action_decided", id)
	}
	return err
}

// Tool reads expose bounded domain data, never sessions, model keys or app secrets.
func (s *Store) AgentMatters(ctx context.Context, w string) ([]map[string]any, error) {
	rows, err := s.Pool.Query(ctx, `SELECT m.id,m.title,m.category,m.status,m.deadline,r.nominal_at,r.due_at,r.enabled
 FROM matters m LEFT JOIN reminders r ON r.workspace_id=m.workspace_id AND r.matter_id=m.id
 WHERE m.workspace_id=$1 ORDER BY m.created_at DESC,m.id DESC LIMIT 20`, w)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, title, category, status string
		var deadline, nominal, due *time.Time
		var enabled *bool
		if err = rows.Scan(&id, &title, &category, &status, &deadline, &nominal, &due, &enabled); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "title": title, "category": category, "status": status, "deadline": deadline, "reminder_at": nominal, "effective_reminder_at": due, "reminder_enabled": enabled})
	}
	return out, rows.Err()
}
func (s *Store) AgentMatter(ctx context.Context, w, id string) (domain.Matter, error) {
	m, err := scanMatter(s.Pool.QueryRow(ctx, matterSelect+" WHERE workspace_id=$1 AND id=$2", w, id))
	if len([]rune(m.Source)) > 2000 {
		m.Source = string([]rune(m.Source)[:2000]) + "\n[资料仅显示前 2000 字]"
	}
	return m, err
}
func (s *Store) AgentMemories(ctx context.Context, w string) ([]domain.Memory, error) {
	rows, err := s.Pool.Query(ctx, "SELECT id,category,content,revision,updated_at FROM memories WHERE workspace_id=$1 ORDER BY updated_at DESC,id LIMIT 8", w)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Memory{}
	for rows.Next() {
		var m domain.Memory
		if err = rows.Scan(&m.ID, &m.Category, &m.Text, &m.Revision, &m.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func (s *Store) AgentConnections(ctx context.Context, w string) ([]map[string]any, error) {
	rows, err := s.Pool.Query(ctx, "SELECT id,label,enabled,verified_at FROM app_connections WHERE workspace_id=$1 ORDER BY id LIMIT 20", w)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, label string
		var enabled bool
		var verified *time.Time
		if err = rows.Scan(&id, &label, &enabled, &verified); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "label": label, "enabled": enabled, "verified_at": verified})
	}
	return out, rows.Err()
}
func (s *Store) ConversationActionState(ctx context.Context, r Ref) ([]map[string]any, error) {
	rows, err := s.Pool.Query(ctx, `SELECT a.id,a.payload->>'title',a.status,a.result_id FROM agent_actions a JOIN runs run ON run.workspace_id=a.workspace_id AND run.id=a.run_id
 WHERE a.workspace_id=$1 AND run.conversation_id=(SELECT conversation_id FROM runs WHERE workspace_id=$1 AND id=$2) ORDER BY a.created_at DESC,a.id LIMIT 12`, r.Workspace, r.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, title, status, result string
		if err = rows.Scan(&id, &title, &status, &result); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "title": title, "status": status, "matter_id": result})
	}
	return out, rows.Err()
}
