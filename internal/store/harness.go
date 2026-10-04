package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"nemi/internal/domain"
)

func (s *Store) AgentActive(ctx context.Context, r Ref) (bool, error) {
	var active bool
	err := s.Pool.QueryRow(ctx, "SELECT status='RUNNING' FROM runs WHERE workspace_id=$1 AND id=$2", r.Workspace, r.ID).Scan(&active)
	return active, err
}

// Link authorization uses original user text, never an LLM-generated summary.
func (s *Store) AgentUserMessages(ctx context.Context, r Ref) ([]domain.ChatMessage, error) {
	rows, err := s.Pool.Query(ctx, `SELECT t.user_text FROM chat_turns t JOIN chat_turns current ON current.workspace_id=t.workspace_id AND current.conversation_id=t.conversation_id WHERE current.workspace_id=$1 AND current.run_id=$2 AND t.position<=current.position ORDER BY t.position`, r.Workspace, r.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ChatMessage{}
	for rows.Next() {
		var text string
		if err = rows.Scan(&text); err != nil {
			return nil, err
		}
		out = append(out, domain.ChatMessage{Role: "user", Content: text})
	}
	return out, rows.Err()
}

// Lock the run before writing any harness state. Stop uses the same fence, so
// cancelled workers cannot overwrite a newer conversation checkpoint or plan.
func activeChat(ctx context.Context, tx pgx.Tx, r Ref) (string, error) {
	var conversation, status string
	err := tx.QueryRow(ctx, "SELECT conversation_id,status FROM runs WHERE workspace_id=$1 AND id=$2 AND kind='chat' FOR SHARE", r.Workspace, r.ID).Scan(&conversation, &status)
	if err != nil {
		return "", err
	}
	if status != "RUNNING" {
		return "", errors.New("AGENT_NO_LONGER_ACTIVE")
	}
	return conversation, nil
}

func (s *Store) SaveChatSummary(ctx context.Context, r Ref, expected, through int, summary string) error {
	if through <= expected || len(summary) > 6000 || strings.TrimSpace(summary) == "" || !utf8.ValidString(summary) {
		return errors.New("AGENT_INVALID_SUMMARY")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	conversation, err := activeChat(ctx, tx, r)
	if err != nil {
		return err
	}
	// Only the immutable snapshot authorizes the range to compact.
	var raw string
	if err = tx.QueryRow(ctx, "SELECT snapshot_source FROM runs WHERE workspace_id=$1 AND id=$2", r.Workspace, r.ID).Scan(&raw); err != nil {
		return err
	}
	var checkpoint domain.ChatContext
	if json.Unmarshal([]byte(raw), &checkpoint) != nil || checkpoint.SummaryThrough != expected || checkpoint.CompactThrough != through {
		return errors.New("AGENT_INVALID_SUMMARY")
	}
	tag, err := tx.Exec(ctx, "UPDATE conversations SET context_summary=$3,summary_through=$4 WHERE workspace_id=$1 AND id=$2 AND summary_through=$5", r.Workspace, conversation, summary, through, expected)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("AGENT_CHECKPOINT_CHANGED")
	}
	if err = event(ctx, tx, r.Workspace, "chat.compacted", conversation); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) UpdateTaskPlan(ctx context.Context, r Ref, p domain.TaskPlan) (any, error) {
	if strings.TrimSpace(p.Goal) == "" || utf8.RuneCountInString(p.Goal) > 200 || len(p.Steps) < 1 || len(p.Steps) > 8 {
		return nil, errors.New("INVALID_ARGUMENTS")
	}
	active := 0
	for _, step := range p.Steps {
		if strings.TrimSpace(step.Title) == "" || utf8.RuneCountInString(step.Title) > 150 {
			return nil, errors.New("INVALID_ARGUMENTS")
		}
		switch step.Status {
		case "pending", "completed":
		case "in_progress":
			active++
		default:
			return nil, errors.New("INVALID_ARGUMENTS")
		}
	}
	if active > 1 {
		return nil, errors.New("INVALID_ARGUMENTS")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = activeChat(ctx, tx, r); err != nil {
		return nil, err
	}
	b, _ := json.Marshal(p)
	if _, err = tx.Exec(ctx, "UPDATE runs SET task_plan=$3 WHERE workspace_id=$1 AND id=$2", r.Workspace, r.ID, b); err != nil {
		return nil, err
	}
	if err = event(ctx, tx, r.Workspace, "agent.plan_updated", r.ID); err != nil {
		return nil, err
	}
	return map[string]any{"plan": p, "progress_only": true}, tx.Commit(ctx)
}

func (s *Store) PreviousTaskPlan(ctx context.Context, r Ref) (*domain.TaskPlan, error) {
	var p *domain.TaskPlan
	err := s.Pool.QueryRow(ctx, `SELECT task_plan FROM runs WHERE workspace_id=$1 AND conversation_id=(SELECT conversation_id FROM runs WHERE workspace_id=$1 AND id=$2) AND id<>$2 AND task_plan IS NOT NULL ORDER BY created_at DESC,id DESC LIMIT 1`, r.Workspace, r.ID).Scan(&p)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// Idempotent and scoped. Already completed runs keep their original result.
func (s *Store) StopChatRun(ctx context.Context, tx pgx.Tx, r Ref) (any, int, error) {
	var status, code string
	err := tx.QueryRow(ctx, "SELECT status,error_code FROM runs WHERE workspace_id=$1 AND id=$2 AND kind='chat' FOR UPDATE", r.Workspace, r.ID).Scan(&status, &code)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, domain.ErrNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	if status == "QUEUED" || status == "RUNNING" {
		if err = failRunTx(ctx, tx, r, "AGENT_CANCELLED", 0, 0, 0); err != nil {
			return nil, 0, err
		}
		status, code = "FAILED", "AGENT_CANCELLED"
	}
	return map[string]string{"run_id": r.ID, "status": status, "error": code}, 200, nil
}
