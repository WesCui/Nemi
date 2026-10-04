package store

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"nemi/internal/domain"
	"sync"
	"testing"
	"time"
)

func waitingFixture(t *testing.T, s *Store, w string, models ...string) (Ref, Continuation, []domain.AgentAction) {
	t.Helper()
	ctx := context.Background()
	ref := agentRun(t, s, w, models...)
	actions := []domain.AgentAction{}
	for _, title := range []string{"准备甲", "准备乙"} {
		b, _ := json.Marshal(domain.AgentMatter{Title: title, Category: "life"})
		a, e := s.ProposeAction(ctx, ref, domain.ID(), "create_matter", b)
		if e != nil {
			t.Fatal(e)
		}
		actions = append(actions, a)
	}
	c, e := s.WaitForActions(ctx, ref, WaitForActions{Goal: "准备生活安排", Next: "核对保存结果并生成报告", Actions: []string{actions[0].ID, actions[1].ID}})
	if e != nil {
		t.Fatal(e)
	}
	return ref, c, actions
}
func TestContinuationRequiresConsentAndAllDecisionsAndResumesOnce(t *testing.T) {
	s, w := fixture(t)
	ctx := context.Background()
	ref, c, actions := waitingFixture(t, s, w)
	command := func(fn func(pgx.Tx) (any, int, error)) {
		t.Helper()
		_, e := s.Command(ctx, w, domain.ID(), "continuation-test", nil, fn)
		if e != nil {
			t.Fatal(e)
		}
	}
	decide := func(a domain.AgentAction, status string) {
		command(func(tx pgx.Tx) (any, int, error) {
			if _, e := s.Action(ctx, tx, w, a.ID); e != nil {
				return nil, 0, e
			}
			return nil, 200, s.DecideAction(ctx, tx, w, a.ID, status, "")
		})
	}
	// Registering a wait does not grant paid continuation, even after decisions.
	if check, e := s.AdvanceContinuation(ctx, ref); e != nil || !check.Done {
		t.Fatal("unconsented work scheduled", e)
	}
	command(func(tx pgx.Tx) (any, int, error) { return s.AuthorizeContinuation(ctx, tx, ref) })
	if check, e := s.AdvanceContinuation(ctx, ref); e != nil || check.Done {
		t.Fatal("resumed while parent still running", e)
	}
	if e := s.FinishRun(ctx, ref, domain.Plan{Summary: "准备好了，请确认"}, 0, 0, 0); e != nil {
		t.Fatal(e)
	}
	decide(actions[0], "APPROVED")
	if check, e := s.AdvanceContinuation(ctx, ref); e != nil || check.Done {
		t.Fatal("resumed before all decisions", e)
	}
	decide(actions[1], "DECLINED")
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			check, e := s.AdvanceContinuation(ctx, ref)
			if e != nil {
				errs <- e
			} else if !check.Done {
				errs <- errors.New("wait not released")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	detail, e := s.Conversation(ctx, w, c.Conversation)
	if e != nil {
		t.Fatal(e)
	}
	if len(detail.Turns) != 2 || detail.Turns[1].Origin != "continuation" || detail.Turns[0].Continuation.Status != "STARTED" || detail.Turns[0].Continuation.NextRun != detail.Turns[1].RunID {
		t.Fatal("resume missing or duplicated")
	}
	var profile, config, root string
	var depth int
	e = s.Pool.QueryRow(ctx, "SELECT model_profile,model_config_id,continuation_root,continuation_depth FROM runs WHERE workspace_id=$1 AND id=$2", w, detail.Turns[1].RunID).Scan(&profile, &config, &root, &depth)
	if e != nil || profile != "fixture" || config != "" || root != ref.ID || depth != 1 {
		t.Fatal("model or lineage changed", e)
	}
	var count int
	s.Pool.QueryRow(ctx, "SELECT count(*) FROM outbox WHERE workspace_id=$1 AND kind='run' AND subject_id=$2", w, detail.Turns[1].RunID).Scan(&count)
	if count != 1 {
		t.Fatal("resume outbox duplicated")
	}
	// Model-authored resume text cannot grant new URL or source authorization.
	messages, e := s.AgentUserMessages(ctx, Ref{w, detail.Turns[1].RunID})
	if e != nil || len(messages) != 1 || messages[0].Content != "hello" {
		t.Fatal("resume treated as a new human instruction", e)
	}
	if _, e = s.Command(ctx, w, domain.ID(), "stop-chain", nil, func(tx pgx.Tx) (any, int, error) { return s.StopContinuation(ctx, tx, ref) }); e != nil {
		t.Fatal(e)
	}
	var status string
	s.Pool.QueryRow(ctx, "SELECT status FROM runs WHERE workspace_id=$1 AND id=$2", w, detail.Turns[1].RunID).Scan(&status)
	if status != "FAILED" {
		t.Fatal("stop race left child runnable")
	}
}

func TestContinuationSupersededExpiredRevokedAndFailed(t *testing.T) {
	for _, scenario := range []string{"new-message", "expired", "revoked", "parent-failed", "stopped"} {
		t.Run(scenario, func(t *testing.T) {
			s, w := fixture(t)
			ctx := context.Background()
			models := []string{}
			if scenario == "revoked" {
				m := PersonalModel{ID: domain.ID(), Label: "fixture", Provider: "deepseek", Model: "fixture", InputPrice: 2000000, OutputPrice: 4000000, Credential: []byte("test-only")}
				_, e := s.Command(ctx, w, domain.ID(), "model", nil, func(tx pgx.Tx) (any, int, error) { return s.AddModel(ctx, tx, w, m) })
				if e != nil {
					t.Fatal(e)
				}
				models = append(models, m.ID)
			}
			ref, c, actions := waitingFixture(t, s, w, models...)
			command := func(fn func(pgx.Tx) (any, int, error)) {
				t.Helper()
				_, e := s.Command(ctx, w, domain.ID(), "continuation-check", nil, fn)
				if e != nil {
					t.Fatal(e)
				}
			}
			command(func(tx pgx.Tx) (any, int, error) { return s.AuthorizeContinuation(ctx, tx, ref) })
			if scenario == "parent-failed" {
				if e := s.FailRun(ctx, ref, "FIXTURE_INTERRUPTED", 0, 0, 0); e != nil {
					t.Fatal(e)
				}
			} else {
				if e := s.FinishRun(ctx, ref, domain.Plan{Summary: "等待"}, 0, 0, 0); e != nil {
					t.Fatal(e)
				}
			}
			for _, a := range actions {
				command(func(tx pgx.Tx) (any, int, error) { return nil, 200, s.DecideAction(ctx, tx, w, a.ID, "DECLINED", "") })
			}
			switch scenario {
			case "new-message":
				command(func(tx pgx.Tx) (any, int, error) {
					return s.CreateChatRun(ctx, tx, w, c.Conversation, "改变目标", "managed", "fixture", "")
				})
			case "expired":
				_, e := s.Pool.Exec(ctx, "UPDATE agent_continuations SET expires_at=now()-interval '1 minute' WHERE workspace_id=$1 AND run_id=$2", w, ref.ID)
				if e != nil {
					t.Fatal(e)
				}
			case "revoked":
				command(func(tx pgx.Tx) (any, int, error) { return s.RemoveModel(ctx, tx, w, models[0]) })
			case "stopped":
				command(func(tx pgx.Tx) (any, int, error) { return s.StopContinuation(ctx, tx, ref) })
			}
			if check, e := s.AdvanceContinuation(ctx, ref); e != nil || !check.Done {
				t.Fatal("invalid continuation not terminal", e)
			}
			var resumed int
			s.Pool.QueryRow(ctx, "SELECT count(*) FROM chat_turns WHERE workspace_id=$1 AND origin='continuation'", w).Scan(&resumed)
			if resumed != 0 {
				t.Fatal("invalid continuation resumed")
			}
			var status string
			s.Pool.QueryRow(ctx, "SELECT status FROM agent_continuations WHERE workspace_id=$1 AND run_id=$2", w, ref.ID).Scan(&status)
			if status == "WAITING" || status == "STARTED" {
				t.Fatal("stop reason not recorded", status)
			}
		})
	}
}

func TestContinuationScopeAndReceiptWait(t *testing.T) {
	s, w := fixture(t)
	ctx := context.Background()
	ref := agentRun(t, s, w)
	a, e := s.ProposeAction(ctx, ref, domain.ID(), "send_message", []byte(`{"title":"消息"}`))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.WaitForActions(ctx, ref, WaitForActions{Goal: "处理消息", Next: "核对回执", Actions: []string{domain.ID()}}); e == nil {
		t.Fatal("guessed action accepted")
	}
	if _, e = s.WaitForActions(ctx, ref, WaitForActions{Goal: "处理消息", Next: "核对回执", Actions: []string{a.ID}}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Command(ctx, w, domain.ID(), "authorize", nil, func(tx pgx.Tx) (any, int, error) {
		return s.AuthorizeContinuation(ctx, tx, Ref{Workspace: "foreign-space", ID: ref.ID})
	}); !errors.Is(e, domain.ErrNotFound) {
		t.Fatal("foreign wait exposed", e)
	}
	if _, e = s.Command(ctx, w, domain.ID(), "authorize", nil, func(tx pgx.Tx) (any, int, error) { return s.AuthorizeContinuation(ctx, tx, ref) }); e != nil {
		t.Fatal(e)
	}
	if e = s.FinishRun(ctx, ref, domain.Plan{Summary: "等待确认"}, 0, 0, 0); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.ClaimDispatch(ctx, w, a.ID, "wecom", []byte("body")); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Command(ctx, w, domain.ID(), "approve", nil, func(tx pgx.Tx) (any, int, error) { return nil, 200, s.DecideAction(ctx, tx, w, a.ID, "APPROVED", a.ID) }); e != nil {
		t.Fatal(e)
	}
	if check, e := s.AdvanceContinuation(ctx, ref); e != nil || check.Done || check.Until.After(time.Now().Add(31*time.Second)) {
		t.Fatal("advanced before receipt or missing uncertainty timer", e)
	}
	if e = s.SettleDispatch(ctx, w, a.ID, "UNKNOWN"); e != nil {
		t.Fatal(e)
	}
	if check, e := s.AdvanceContinuation(ctx, ref); e != nil || !check.Done {
		t.Fatal("unknown result prevented review continuation", e)
	}
	var sends int
	s.Pool.QueryRow(ctx, "SELECT count(*) FROM connector_dispatches WHERE workspace_id=$1", w).Scan(&sends)
	if sends != 1 {
		t.Fatal("receipt continuation resent message")
	}
}

func TestStopContinuationLocksEntireChainBeforeEvents(t *testing.T) {
	s, w := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	ref, _, actions := waitingFixture(t, s, w)
	_, err := s.Command(ctx, w, domain.ID(), "authorize-chain", nil, func(tx pgx.Tx) (any, int, error) { return s.AuthorizeContinuation(ctx, tx, ref) })
	if err != nil {
		t.Fatal(err)
	}
	if err = s.FinishRun(ctx, ref, domain.Plan{Summary: "等待"}, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	for _, a := range actions {
		if _, err = s.Command(ctx, w, domain.ID(), "decline", nil, func(tx pgx.Tx) (any, int, error) { return nil, 200, s.DecideAction(ctx, tx, w, a.ID, "DECLINED", "") }); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.AdvanceContinuation(ctx, ref); err != nil {
		t.Fatal(err)
	}
	// An approval already owns the earlier continuation. Cancelling its active
	// child must wait for that row without owning the global event sequence lock.
	approval, err := s.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer approval.Rollback(context.Background())
	if _, err = approval.Exec(ctx, "SELECT run_id FROM agent_continuations WHERE workspace_id=$1 AND run_id=$2 FOR UPDATE", w, ref.ID); err != nil {
		t.Fatal(err)
	}
	started, done := make(chan int, 1), make(chan error, 1)
	go func() {
		_, e := s.Command(ctx, w, domain.ID(), "stop-chain", nil, func(tx pgx.Tx) (any, int, error) {
			started <- int(tx.Conn().PgConn().PID())
			return s.StopContinuation(ctx, tx, ref)
		})
		done <- e
	}()
	pid := <-started
	for {
		var blocked bool
		if err = s.Pool.QueryRow(ctx, "SELECT COALESCE(wait_event_type='Lock',false) FROM pg_stat_activity WHERE pid=$1", pid).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case err = <-done:
			t.Fatal("stop did not wait for approval", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	var free bool
	if err = approval.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(73310003)").Scan(&free); err != nil || !free {
		t.Fatal("cancellation owns event lock while waiting for approval", err)
	}
	if err = approval.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

func TestContinuationPinsPersonalModelAfterDefaultChanges(t *testing.T) {
	s, w := fixture(t)
	ctx := context.Background()
	var models []PersonalModel
	for _, label := range []string{"original", "new-default"} {
		m := PersonalModel{ID: domain.ID(), Label: label, Provider: "deepseek", Model: label, InputPrice: 1, OutputPrice: 2, Credential: []byte("test-only")}
		if _, err := s.Command(ctx, w, domain.ID(), "add-model", nil, func(tx pgx.Tx) (any, int, error) { return s.AddModel(ctx, tx, w, m) }); err != nil {
			t.Fatal(err)
		}
		models = append(models, m)
	}
	ref, c, actions := waitingFixture(t, s, w, models[0].ID)
	if err := s.FinishRun(ctx, ref, domain.Plan{Summary: "等待"}, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Command(ctx, w, domain.ID(), "switch-default", nil, func(tx pgx.Tx) (any, int, error) { return s.DefaultModel(ctx, tx, w, models[1].ID) }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Command(ctx, w, domain.ID(), "authorize", nil, func(tx pgx.Tx) (any, int, error) { return s.AuthorizeContinuation(ctx, tx, ref) }); err != nil {
		t.Fatal(err)
	}
	for _, a := range actions {
		if _, err := s.Command(ctx, w, domain.ID(), "decline", nil, func(tx pgx.Tx) (any, int, error) { return nil, 200, s.DecideAction(ctx, tx, w, a.ID, "DECLINED", "") }); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AdvanceContinuation(ctx, ref); err != nil {
		t.Fatal(err)
	}
	detail, err := s.Conversation(ctx, w, c.Conversation)
	if err != nil || len(detail.Turns) != 2 {
		t.Fatal("missing resumed turn", err)
	}
	model, err := s.RunModel(ctx, Ref{w, detail.Turns[1].RunID})
	if err != nil || model == nil || model.ID != models[0].ID {
		t.Fatal("continuation used the new default model", err)
	}
}

func TestContinuationDepthAndRootLifetimeLimits(t *testing.T) {
	for _, scenario := range []string{"depth", "age", "remaining-time"} {
		t.Run(scenario, func(t *testing.T) {
			s, w := fixture(t)
			ctx := context.Background()
			ref := agentRun(t, s, w)
			var err error
			switch scenario {
			case "depth":
				_, err = s.Pool.Exec(ctx, "UPDATE runs SET continuation_depth=8 WHERE workspace_id=$1 AND id=$2", w, ref.ID)
			case "age":
				_, err = s.Pool.Exec(ctx, "UPDATE runs SET created_at=now()-interval '8 days' WHERE workspace_id=$1 AND id=$2", w, ref.ID)
			case "remaining-time":
				if err = s.FinishRun(ctx, ref, domain.Plan{Summary: "earlier turn"}, 0, 0, 0); err != nil {
					t.Fatal(err)
				}
				_, err = s.Pool.Exec(ctx, "UPDATE runs SET created_at=now()-interval '6 days 23 hours' WHERE workspace_id=$1 AND id=$2", w, ref.ID)
				root := ref.ID
				ref = agentRun(t, s, w)
				if err == nil {
					_, err = s.Pool.Exec(ctx, "UPDATE runs SET continuation_root=$3,continuation_depth=1 WHERE workspace_id=$1 AND id=$2", w, ref.ID, root)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			a, err := s.ProposeAction(ctx, ref, domain.ID(), "create_matter", []byte(`{"title":"fixture"}`))
			if err != nil {
				t.Fatal(err)
			}
			c, err := s.WaitForActions(ctx, ref, WaitForActions{Goal: "fixture", Next: "review", Actions: []string{a.ID}})
			if scenario == "remaining-time" {
				if err != nil || !c.Expires.After(time.Now().Add(59*time.Minute)) || c.Expires.After(time.Now().Add(time.Hour)) {
					t.Fatal("wait exceeded the root lifetime", err)
				}
			} else if err == nil || err.Error() != "CONTINUATION_LIMIT" {
				t.Fatal("unbounded chain accepted", err)
			}
		})
	}
}
