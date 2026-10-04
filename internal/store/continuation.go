package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"nemi/internal/domain"
)

type Continuation struct {
	RunID        string    `json:"run_id"`
	RootID       string    `json:"root_id"`
	Conversation string    `json:"conversation_id"`
	Goal         string    `json:"goal"`
	Next         string    `json:"next_step"`
	Actions      []string  `json:"action_ids"`
	Status       string    `json:"status"`
	Authorized   bool      `json:"authorized"`
	NextRun      string    `json:"next_run_id"`
	Error        string    `json:"error"`
	Expires      time.Time `json:"expires_at"`
}
type WaitForActions struct {
	Goal    string   `json:"goal"`
	Next    string   `json:"next_step"`
	Actions []string `json:"action_ids"`
}

const continuationSelect = `SELECT run_id,root_id,conversation_id,goal,next_step,action_ids,status,authorized,next_run_id,error_code,expires_at FROM agent_continuations`

func scanContinuation(row pgx.Row) (Continuation, error) {
	var c Continuation
	var ids []byte
	err := row.Scan(&c.RunID, &c.RootID, &c.Conversation, &c.Goal, &c.Next, &ids, &c.Status, &c.Authorized, &c.NextRun, &c.Error, &c.Expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, domain.ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(ids, &c.Actions)
	}
	return c, err
}
func (s *Store) WaitForActions(ctx context.Context, r Ref, p WaitForActions) (Continuation, error) {
	p.Goal, p.Next = strings.TrimSpace(p.Goal), strings.TrimSpace(p.Next)
	if utf8.RuneCountInString(p.Goal) < 1 || utf8.RuneCountInString(p.Goal) > 200 || len(p.Next) < 1 || len(p.Next) > 1500 || !utf8.ValidString(p.Next) || len(p.Actions) < 1 || len(p.Actions) > 4 {
		return Continuation{}, errors.New("INVALID_ARGUMENTS")
	}
	sort.Strings(p.Actions)
	for i, id := range p.Actions {
		if len(id) != 32 || (i > 0 && p.Actions[i-1] == id) {
			return Continuation{}, errors.New("INVALID_ARGUMENTS")
		}
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Continuation{}, err
	}
	defer tx.Rollback(ctx)
	conversation, err := activeChat(ctx, tx, r)
	if err != nil {
		return Continuation{}, err
	}
	// Serialize wait registration with approval, including the period before
	// the continuation row exists. Both callers hold the parent run first.
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,5))", r.Workspace+":"+r.ID); err != nil {
		return Continuation{}, err
	}
	var root string
	var depth int
	var created time.Time
	if err = tx.QueryRow(ctx, "SELECT continuation_root,continuation_depth,created_at FROM runs WHERE workspace_id=$1 AND id=$2", r.Workspace, r.ID).Scan(&root, &depth, &created); err != nil {
		return Continuation{}, err
	}
	if root == "" {
		root = r.ID
	}
	var rootCreated time.Time
	var cancelled bool
	if err = tx.QueryRow(ctx, "SELECT created_at,continuation_cancelled FROM runs WHERE workspace_id=$1 AND id=$2", r.Workspace, root).Scan(&rootCreated, &cancelled); err != nil {
		return Continuation{}, err
	}
	if cancelled || depth >= 8 || time.Since(rootCreated) >= 7*24*time.Hour {
		return Continuation{}, errors.New("CONTINUATION_LIMIT")
	}
	var matched, pending int
	if err = tx.QueryRow(ctx, "SELECT count(*),count(*) FILTER(WHERE status='PENDING') FROM agent_actions WHERE workspace_id=$1 AND run_id=$2 AND id=ANY($3)", r.Workspace, r.ID, p.Actions).Scan(&matched, &pending); err != nil {
		return Continuation{}, err
	}
	if matched != len(p.Actions) {
		return Continuation{}, errors.New("INVALID_ARGUMENTS")
	}
	if pending == 0 {
		return Continuation{}, errors.New("ACTION_ALREADY_DECIDED")
	}
	ids, _ := json.Marshal(p.Actions)
	expires := minTime(created.Add(24*time.Hour), rootCreated.Add(7*24*time.Hour))
	if _, err = tx.Exec(ctx, `INSERT INTO agent_continuations(workspace_id,run_id,root_id,conversation_id,goal,next_step,action_ids,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`, r.Workspace, r.ID, root, conversation, p.Goal, p.Next, ids, expires); err != nil {
		return Continuation{}, err
	}
	c, err := scanContinuation(tx.QueryRow(ctx, continuationSelect+" WHERE workspace_id=$1 AND run_id=$2", r.Workspace, r.ID))
	if err != nil {
		return c, err
	}
	before, _ := json.Marshal(c.Actions)
	if c.Goal != p.Goal || c.Next != p.Next || string(before) != string(ids) {
		return c, domain.ErrConflict
	}
	if err = event(ctx, tx, r.Workspace, "agent.waiting_confirmation", r.ID); err != nil {
		return c, err
	}
	return c, tx.Commit(ctx)
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// Wake rows are committed with the operation/receipt/run state. Losing a relay
// response can duplicate a signal, never create a second resumed run.
func wakeContinuation(ctx context.Context, tx pgx.Tx, r Ref) error {
	var revision int
	err := tx.QueryRow(ctx, "UPDATE agent_continuations SET wake_revision=wake_revision+1 WHERE workspace_id=$1 AND run_id=$2 AND authorized RETURNING wake_revision", r.Workspace, r.ID).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return enqueue(ctx, tx, r.Workspace, "continuation_signal", r.ID, revision, nil)
}
func (s *Store) AuthorizeContinuation(ctx context.Context, tx pgx.Tx, r Ref) (any, int, error) {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", r.Workspace+":chat-admission"); err != nil {
		return nil, 0, err
	}
	c, err := scanContinuation(tx.QueryRow(ctx, continuationSelect+" WHERE workspace_id=$1 AND run_id=$2 FOR UPDATE", r.Workspace, r.ID))
	if err != nil {
		return nil, 0, err
	}
	if c.Status != "WAITING" {
		return c, 200, nil
	}
	if !c.Expires.After(time.Now()) {
		return nil, 0, errors.New("CONTINUATION_EXPIRED")
	}
	if c.Authorized {
		return c, 202, nil
	}
	if _, err = tx.Exec(ctx, "UPDATE agent_continuations SET authorized=true WHERE workspace_id=$1 AND run_id=$2", r.Workspace, r.ID); err != nil {
		return nil, 0, err
	}
	if err = enqueue(ctx, tx, r.Workspace, "continuation", r.ID, 0, nil); err != nil {
		return nil, 0, err
	}
	if err = wakeContinuation(ctx, tx, r); err != nil {
		return nil, 0, err
	}
	if err = event(ctx, tx, r.Workspace, "agent.continuation_authorized", r.ID); err != nil {
		return nil, 0, err
	}
	c.Authorized = true
	return c, 202, nil
}
func cancelConversationContinuations(ctx context.Context, tx pgx.Tx, w, conversation string) error {
	rows, err := tx.Query(ctx, "UPDATE agent_continuations SET status='CANCELLED',error_code='NEW_USER_MESSAGE' WHERE workspace_id=$1 AND conversation_id=$2 AND status='WAITING' RETURNING run_id", w, conversation)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err = wakeContinuation(ctx, tx, Ref{w, id}); err != nil {
			return err
		}
	}
	return nil
}
func failContinuation(ctx context.Context, tx pgx.Tx, r Ref, code string) error {
	if _, err := tx.Exec(ctx, "UPDATE agent_continuations SET status='BLOCKED',error_code=$3 WHERE workspace_id=$1 AND run_id=$2 AND status='WAITING'", r.Workspace, r.ID, code); err != nil {
		return err
	}
	return wakeContinuation(ctx, tx, r)
}

type ContinuationCheck struct {
	Done  bool
	Until time.Time
}

func (s *Store) AdvanceContinuation(ctx context.Context, r Ref) (ContinuationCheck, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return ContinuationCheck{}, err
	}
	defer tx.Rollback(ctx)
	// Same admission/conversation order as an explicit user turn. A new message
	// and a resume can never each admit a concurrent turn in one conversation.
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", r.Workspace+":chat-admission"); err != nil {
		return ContinuationCheck{}, err
	}
	var conversation string
	err = tx.QueryRow(ctx, "SELECT conversation_id FROM agent_continuations WHERE workspace_id=$1 AND run_id=$2", r.Workspace, r.ID).Scan(&conversation)
	if errors.Is(err, pgx.ErrNoRows) {
		return ContinuationCheck{Done: true}, nil
	}
	if err != nil {
		return ContinuationCheck{}, err
	}
	if _, err = tx.Exec(ctx, "SELECT id FROM conversations WHERE workspace_id=$1 AND id=$2 FOR UPDATE", r.Workspace, conversation); err != nil {
		return ContinuationCheck{}, err
	}
	c, err := scanContinuation(tx.QueryRow(ctx, continuationSelect+" WHERE workspace_id=$1 AND run_id=$2 FOR UPDATE", r.Workspace, r.ID))
	if err != nil {
		return ContinuationCheck{}, err
	}
	if c.Status != "WAITING" || !c.Authorized {
		return ContinuationCheck{Done: true}, nil
	}
	terminal := func(status, code string) (ContinuationCheck, error) {
		if _, e := tx.Exec(ctx, "UPDATE agent_continuations SET status=$3,error_code=$4 WHERE workspace_id=$1 AND run_id=$2", r.Workspace, r.ID, status, code); e != nil {
			return ContinuationCheck{}, e
		}
		if e := event(ctx, tx, r.Workspace, "agent.continuation_stopped", r.ID); e != nil {
			return ContinuationCheck{}, e
		}
		return ContinuationCheck{Done: true}, tx.Commit(ctx)
	}
	if !c.Expires.After(time.Now()) {
		return terminal("EXPIRED", "CONTINUATION_EXPIRED")
	}
	var rootCancelled bool
	if err = tx.QueryRow(ctx, "SELECT continuation_cancelled FROM runs WHERE workspace_id=$1 AND id=$2", r.Workspace, c.RootID).Scan(&rootCancelled); err != nil {
		return ContinuationCheck{}, err
	}
	if rootCancelled {
		return terminal("CANCELLED", "AGENT_CANCELLED")
	}
	var status, mode, profile, modelID string
	var depth int
	if err = tx.QueryRow(ctx, "SELECT status,mode,model_profile,model_config_id,continuation_depth FROM runs WHERE workspace_id=$1 AND id=$2", r.Workspace, r.ID).Scan(&status, &mode, &profile, &modelID, &depth); err != nil {
		return ContinuationCheck{}, err
	}
	if status == "FAILED" {
		return terminal("BLOCKED", "PARENT_FAILED")
	}
	if depth >= 8 {
		return terminal("BLOCKED", "CONTINUATION_LIMIT")
	}
	if status != "SUCCEEDED" {
		return ContinuationCheck{Until: c.Expires}, nil
	}
	if modelID != "" {
		var revoked bool
		err = tx.QueryRow(ctx, "SELECT revoked FROM personal_models WHERE workspace_id=$1 AND id=$2 FOR SHARE", r.Workspace, modelID).Scan(&revoked)
		if errors.Is(err, pgx.ErrNoRows) || revoked {
			return terminal("BLOCKED", "MODEL_CONFIG_REVOKED")
		}
		if err != nil {
			return ContinuationCheck{}, err
		}
	}
	var other bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM runs WHERE workspace_id=$1 AND conversation_id=$2 AND id<>$3 AND status IN ('QUEUED','RUNNING'))", r.Workspace, conversation, r.ID).Scan(&other); err != nil {
		return ContinuationCheck{}, err
	}
	if other {
		return terminal("CANCELLED", "NEW_USER_MESSAGE")
	}
	var pending, matched int
	var sendingUntil *time.Time
	err = tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE a.status='PENDING'),max(d.created_at+interval '30 seconds') FILTER(WHERE a.kind='send_message' AND a.status='APPROVED' AND d.status='SENDING' AND d.created_at+interval '30 seconds'>now()) FROM agent_actions a LEFT JOIN connector_dispatches d ON d.workspace_id=a.workspace_id AND d.key=a.id WHERE a.workspace_id=$1 AND a.run_id=$2 AND a.id=ANY($3)`, r.Workspace, r.ID, c.Actions).Scan(&matched, &pending, &sendingUntil)
	if err != nil {
		return ContinuationCheck{}, err
	}
	if matched != len(c.Actions) {
		return terminal("BLOCKED", "ACTION_MISSING")
	}
	if pending > 0 {
		return ContinuationCheck{Until: c.Expires}, nil
	}
	if sendingUntil != nil {
		return ContinuationCheck{Until: minTime(*sendingUntil, c.Expires)}, nil
	}
	resume := &domain.ResumeContext{Parent: r.ID, Root: c.RootID, Depth: depth + 1, Goal: c.Goal, Next: c.Next}
	text := "确认阶段已结束，继续既定目标：" + c.Goal + "\n下一步：" + c.Next + "\n先核对操作的实时结果；取消或未知回执不代表执行成功。"
	out, _, err := s.createChatRun(ctx, tx, r.Workspace, conversation, text, mode, profile, modelID, nil, resume)
	if err != nil {
		switch err.Error() {
		case "CHAT_QUEUE_FULL", "CHAT_TURN_LIMIT", "CHAT_CONTEXT_TOO_LARGE":
			return terminal("BLOCKED", err.Error())
		}
		return ContinuationCheck{}, err
	}
	next := out.(map[string]string)["run_id"]
	if _, err = tx.Exec(ctx, "UPDATE agent_continuations SET status='STARTED',next_run_id=$3 WHERE workspace_id=$1 AND run_id=$2", r.Workspace, r.ID, next); err != nil {
		return ContinuationCheck{}, err
	}
	if err = event(ctx, tx, r.Workspace, "agent.continuation_started", r.ID); err != nil {
		return ContinuationCheck{}, err
	}
	return ContinuationCheck{Done: true}, tx.Commit(ctx)
}
func (s *Store) StopContinuation(ctx context.Context, tx pgx.Tx, r Ref) (any, int, error) {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", r.Workspace+":chat-admission"); err != nil {
		return nil, 0, err
	}
	c, err := scanContinuation(tx.QueryRow(ctx, continuationSelect+" WHERE workspace_id=$1 AND run_id=$2", r.Workspace, r.ID))
	if err != nil {
		return nil, 0, err
	}
	if _, err = tx.Exec(ctx, "UPDATE runs SET continuation_cancelled=true WHERE workspace_id=$1 AND id=$2", r.Workspace, c.RootID); err != nil {
		return nil, 0, err
	}
	// Lock active descendants, then every existing continuation, before any
	// failure emits an event. Approval can hold an earlier continuation while
	// waiting for the event sequence lock; reversing that order would deadlock.
	rows, err := tx.Query(ctx, "SELECT id FROM runs WHERE workspace_id=$1 AND continuation_root=$2 AND status IN ('QUEUED','RUNNING') ORDER BY id FOR UPDATE", r.Workspace, c.RootID)
	if err != nil {
		return nil, 0, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	if _, err = tx.Exec(ctx, "SELECT run_id FROM agent_continuations WHERE workspace_id=$1 AND root_id=$2 ORDER BY run_id FOR UPDATE", r.Workspace, c.RootID); err != nil {
		return nil, 0, err
	}
	for _, id := range ids {
		if err = failRunTx(ctx, tx, Ref{r.Workspace, id}, "AGENT_CANCELLED", 0, 0, 0); err != nil {
			return nil, 0, err
		}
	}
	if _, err = tx.Exec(ctx, "UPDATE agent_continuations SET status='CANCELLED',error_code='AGENT_CANCELLED' WHERE workspace_id=$1 AND root_id=$2 AND (status='WAITING' OR run_id=$3)", r.Workspace, c.RootID, r.ID); err != nil {
		return nil, 0, err
	}
	if err = wakeContinuation(ctx, tx, r); err != nil {
		return nil, 0, err
	}
	if err = event(ctx, tx, r.Workspace, "agent.continuation_cancelled", r.ID); err != nil {
		return nil, 0, err
	}
	return map[string]string{"status": "CANCELLED"}, 200, nil
}
