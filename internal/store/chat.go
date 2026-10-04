package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"nemi/internal/domain"
)

type Conversation struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updated_at"`
}
type ChatTurn struct {
	Steps     []domain.AgentStep   `json:"steps"`
	Actions   []domain.AgentAction `json:"actions"`
	RunID     string               `json:"run_id"`
	Text      string               `json:"text"`
	Reply     string               `json:"reply"`
	Status    string               `json:"status"`
	Error     string               `json:"error"`
	Model     string               `json:"model"`
	CreatedAt time.Time            `json:"created_at"`
}
type ChatDetail struct {
	Conversation Conversation `json:"conversation"`
	Turns        []ChatTurn   `json:"turns"`
}

func (s *Store) Conversations(ctx context.Context, w string) ([]Conversation, error) {
	rows, err := s.Pool.Query(ctx, "SELECT id,title,updated_at FROM conversations WHERE workspace_id=$1 ORDER BY updated_at DESC,id DESC LIMIT 100", w)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Conversation{}
	for rows.Next() {
		var c Conversation
		if err = rows.Scan(&c.ID, &c.Title, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Store) Conversation(ctx context.Context, w, id string) (ChatDetail, error) {
	d := ChatDetail{Turns: []ChatTurn{}}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return d, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, "SELECT id,title,updated_at FROM conversations WHERE workspace_id=$1 AND id=$2", w, id).Scan(&d.Conversation.ID, &d.Conversation.Title, &d.Conversation.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, domain.ErrNotFound
	}
	if err != nil {
		return d, err
	}
	rows, err := tx.Query(ctx, `SELECT t.run_id,t.user_text,COALESCE(r.result->>'summary',''),r.status,r.error_code,COALESCE(m.label,'服务端模型'),t.created_at
 FROM chat_turns t JOIN runs r ON r.workspace_id=t.workspace_id AND r.id=t.run_id
 LEFT JOIN personal_models m ON m.workspace_id=r.workspace_id AND m.id=r.model_config_id
 WHERE t.workspace_id=$1 AND t.conversation_id=$2 ORDER BY t.position`, w, id)
	if err != nil {
		return d, err
	}
	defer rows.Close()
	for rows.Next() {
		t := ChatTurn{Steps: []domain.AgentStep{}, Actions: []domain.AgentAction{}}
		if err = rows.Scan(&t.RunID, &t.Text, &t.Reply, &t.Status, &t.Error, &t.Model, &t.CreatedAt); err != nil {
			return d, err
		}
		d.Turns = append(d.Turns, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return d, err
	}
	index := map[string]int{}
	for i, t := range d.Turns {
		index[t.RunID] = i
	}
	steps, err := tx.Query(ctx, `SELECT s.run_id,s.position,s.kind,s.name,s.status,s.error_code FROM agent_steps s JOIN chat_turns t ON t.workspace_id=s.workspace_id AND t.run_id=s.run_id WHERE t.workspace_id=$1 AND t.conversation_id=$2 ORDER BY t.position,s.position`, w, id)
	if err != nil {
		return d, err
	}
	for steps.Next() {
		var rid string
		var s domain.AgentStep
		if err = steps.Scan(&rid, &s.Position, &s.Kind, &s.Name, &s.Status, &s.Error); err != nil {
			steps.Close()
			return d, err
		}
		i := index[rid]
		d.Turns[i].Steps = append(d.Turns[i].Steps, s)
	}
	err = steps.Err()
	steps.Close()
	if err != nil {
		return d, err
	}
	actions, err := tx.Query(ctx, `SELECT a.id,a.run_id,a.kind,a.payload,a.status,a.result_id FROM agent_actions a JOIN chat_turns t ON t.workspace_id=a.workspace_id AND t.run_id=a.run_id WHERE t.workspace_id=$1 AND t.conversation_id=$2 ORDER BY t.position,a.created_at,a.id`, w, id)
	if err != nil {
		return d, err
	}
	defer actions.Close()
	for actions.Next() {
		var a domain.AgentAction
		if err = actions.Scan(&a.ID, &a.RunID, &a.Kind, &a.Payload, &a.Status, &a.ResultID); err != nil {
			return d, err
		}
		i := index[a.RunID]
		d.Turns[i].Actions = append(d.Turns[i].Actions, a)
	}
	return d, actions.Err()
}

// A conversation turn and the immutable model/context snapshot commit with the
// outbox. HTTP retries read the same run; only one turn can be active per chat.
func (s *Store) CreateChatRun(ctx context.Context, tx pgx.Tx, w, conversation, text, mode, profile, modelID string) (any, int, error) {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", w+":chat-admission"); err != nil {
		return nil, 0, err
	}
	var pending int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM runs WHERE workspace_id=$1 AND kind='chat' AND status IN ('QUEUED','RUNNING')", w).Scan(&pending); err != nil {
		return nil, 0, err
	}
	if pending >= 10 {
		return nil, 0, errors.New("CHAT_QUEUE_FULL")
	}
	if conversation == "" {
		var count int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM conversations WHERE workspace_id=$1", w).Scan(&count); err != nil {
			return nil, 0, err
		}
		if count >= 100 {
			return nil, 0, errors.New("CHAT_LIMIT_REACHED")
		}
		conversation = domain.ID()
		title := []rune(strings.TrimSpace(text))
		if len(title) > 40 {
			title = title[:40]
		}
		if _, err := tx.Exec(ctx, "INSERT INTO conversations(workspace_id,id,title) VALUES($1,$2,$3)", w, conversation, string(title)); err != nil {
			return nil, 0, err
		}
	}
	var found string
	if err := tx.QueryRow(ctx, "SELECT id FROM conversations WHERE workspace_id=$1 AND id=$2 FOR UPDATE", w, conversation).Scan(&found); errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, domain.ErrNotFound
	} else if err != nil {
		return nil, 0, err
	}
	var active bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM runs WHERE workspace_id=$1 AND conversation_id=$2 AND status IN ('QUEUED','RUNNING'))", w, conversation).Scan(&active); err != nil {
		return nil, 0, err
	}
	if active {
		return nil, 0, domain.ErrBusy
	}
	var position int
	if err := tx.QueryRow(ctx, "SELECT COALESCE(max(position),0)+1 FROM chat_turns WHERE workspace_id=$1 AND conversation_id=$2", w, conversation).Scan(&position); err != nil {
		return nil, 0, err
	}
	if position > 200 {
		return nil, 0, errors.New("CHAT_TURN_LIMIT")
	}
	rows, err := tx.Query(ctx, `SELECT t.user_text,r.result->>'summary' FROM chat_turns t JOIN runs r ON r.workspace_id=t.workspace_id AND r.id=t.run_id
 WHERE t.workspace_id=$1 AND t.conversation_id=$2 AND r.status='SUCCEEDED' ORDER BY t.position DESC LIMIT 10`, w, conversation)
	if err != nil {
		return nil, 0, err
	}
	pairs := [][2]string{}
	for rows.Next() {
		var p [2]string
		if err = rows.Scan(&p[0], &p[1]); err != nil {
			rows.Close()
			return nil, 0, err
		}
		pairs = append(pairs, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	history := []domain.ChatMessage{}
	for i := len(pairs) - 1; i >= 0; i-- {
		history = append(history, domain.ChatMessage{Role: "user", Content: pairs[i][0]}, domain.ChatMessage{Role: "assistant", Content: pairs[i][1]})
	}
	history = append(history, domain.ChatMessage{Role: "user", Content: text})
	snapshot, _ := json.Marshal(history)
	for len(snapshot) > 12000 && len(history) > 1 {
		history = history[2:]
		snapshot, _ = json.Marshal(history)
	}
	if len(snapshot) > 12000 {
		return nil, 0, errors.New("CHAT_INPUT_TOO_LARGE")
	}
	id := domain.ID()
	_, err = tx.Exec(ctx, `INSERT INTO runs(workspace_id,id,matter_id,matter_revision,snapshot_title,snapshot_source,status,mode,model_profile,kind,conversation_id,model_config_id)
 VALUES($1,$2,NULL,0,'',$3,'QUEUED',$4,$5,'chat',$6,$7)`, w, id, string(snapshot), mode, profile, conversation, modelID)
	if err != nil {
		return nil, 0, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO chat_turns(workspace_id,conversation_id,run_id,position,user_text) VALUES($1,$2,$3,$4,$5)", w, conversation, id, position, text); err != nil {
		return nil, 0, err
	}
	if _, err = tx.Exec(ctx, "UPDATE conversations SET updated_at=now() WHERE workspace_id=$1 AND id=$2", w, conversation); err != nil {
		return nil, 0, err
	}
	if err = enqueue(ctx, tx, w, "run", id, 0, nil); err != nil {
		return nil, 0, err
	}
	if err = event(ctx, tx, w, "chat.queued", id); err != nil {
		return nil, 0, err
	}
	return map[string]string{"conversation_id": conversation, "run_id": id, "status": "QUEUED"}, 202, nil
}
