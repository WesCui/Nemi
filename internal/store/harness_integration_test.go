package store

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"nemi/internal/domain"
)

func TestStopFencesToolsPlansAndCheckpointsButRetainsLateCharges(t *testing.T) {
	s, w := fixture(t)
	ctx := context.Background()
	ref := agentRun(t, s, w)
	if err := s.ClaimAgentStep(ctx, ref, 1, "MODEL", "model", "", 100000); err != nil {
		t.Fatal(err)
	}
	stop := func(r Ref, key string) error {
		_, err := s.Command(ctx, r.Workspace, key, "stop-test", nil, func(tx pgx.Tx) (any, int, error) { return s.StopChatRun(ctx, tx, r) })
		return err
	}
	if err := stop(Ref{Workspace: "foreign", ID: ref.ID}, domain.ID()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("foreign stop accepted", err)
	}
	key := domain.ID()
	if err := stop(ref, key); err != nil {
		t.Fatal(err)
	}
	if err := stop(ref, key); err != nil {
		t.Fatal(err)
	}
	var reserve int64
	if err := s.Pool.QueryRow(ctx, "SELECT reserved_micro_cny FROM runs WHERE workspace_id=$1 AND id=$2", w, ref.ID).Scan(&reserve); err != nil || reserve != 200000 {
		t.Fatal("unknown request lost reserve", err, reserve)
	}
	if err := s.ClaimAgentStep(ctx, ref, 2, "TOOL", "list_matters", "", 0); err == nil || err.Error() != "AGENT_NO_LONGER_ACTIVE" {
		t.Fatal("stopped worker claimed another step")
	}
	if _, err := s.UpdateTaskPlan(ctx, ref, domain.TaskPlan{Goal: "test", Steps: []domain.TaskPlanStep{{Title: "next", Status: "pending"}}}); err == nil || err.Error() != "AGENT_NO_LONGER_ACTIVE" {
		t.Fatal("stopped worker updated plan")
	}
	if err := s.SaveChatSummary(ctx, ref, 0, 1, "summary"); err == nil || err.Error() != "AGENT_NO_LONGER_ACTIVE" {
		t.Fatal("stopped worker updated checkpoint")
	}
	if _, err := s.ProposeAction(ctx, ref, domain.ID(), "create_matter", []byte(`{"title":"test"}`)); err == nil || err.Error() != "AGENT_NO_LONGER_ACTIVE" {
		t.Fatal("stopped worker proposed an action")
	}
	if err := s.SettleAgentStep(ctx, ref, 1, "SUCCEEDED", "", 100, 50, 50000); err != nil {
		t.Fatal(err)
	}
	if err := stop(ref, domain.ID()); err != nil {
		t.Fatal(err)
	}
	var status, code string
	var cost int64
	if err := s.Pool.QueryRow(ctx, "SELECT status,error_code,charged_micro_cny,reserved_micro_cny FROM runs WHERE workspace_id=$1 AND id=$2", w, ref.ID).Scan(&status, &code, &cost, &reserve); err != nil || status != "FAILED" || code != "AGENT_CANCELLED" || cost != 50000 || reserve != 0 {
		t.Fatal("late model result rewrote cancellation or charge", err)
	}
	if err := s.FinishRun(ctx, ref, domain.Plan{Summary: "late"}, 100, 50, 50000); err == nil {
		t.Fatal("late result resumed stopped run")
	}
}

func TestConcurrentAgentApprovalsClaimOneDispatch(t *testing.T) {
	s, w := fixture(t)
	ctx := context.Background()
	ref := agentRun(t, s, w)
	t.Cleanup(func() { _ = s.FailRun(ctx, ref, "FIXTURE_STOPPED", 0, 0, 0) })
	_, err := s.Command(ctx, w, domain.ID(), "config-test", nil, func(tx pgx.Tx) (any, int, error) {
		return s.SaveConnection(ctx, tx, w, "wecom", "测试群", 0, []byte("test-only-encrypted-placeholder"), true)
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(domain.AgentMessage{Title: "发送到测试群", Channel: "wecom", Text: "确认内容", Revision: 1, Recipient: "测试群"})
	a, err := s.ProposeAction(ctx, ref, domain.ID(), "send_message", b)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	type result struct {
		status  string
		claimed bool
		err     error
	}
	results := make(chan result, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, claimed, err := s.ClaimAgentMessage(ctx, w, a.ID, 1)
			results <- result{status, claimed, err}
		}()
	}
	wg.Wait()
	close(results)
	claims := 0
	for r := range results {
		if r.err != nil {
			t.Fatal(r.err)
		}
		if r.claimed {
			claims++
			if r.status != "SENDING" {
				t.Fatal("claim status changed")
			}
		} else if r.status != "UNKNOWN" {
			t.Fatal("in-flight approval looked delivered")
		}
	}
	if claims != 1 {
		t.Fatal("concurrent confirmations claimed multiple sends", claims)
	}
	if err = s.SettleDispatch(ctx, w, a.ID, "DELIVERED"); err != nil {
		t.Fatal(err)
	}
	status, claimed, err := s.ClaimAgentMessage(ctx, w, a.ID, 1)
	if err != nil || claimed || status != "DELIVERED" {
		t.Fatal("receipt retry claimed network again", err)
	}
}
