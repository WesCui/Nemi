package store

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"
	"nemi/internal/domain"
)

func agentRun(t *testing.T, s *Store, w string, modelIDs ...string) Ref {
	t.Helper()
	ctx := context.Background()
	modelID := ""
	if len(modelIDs) > 0 {
		modelID = modelIDs[0]
	}
	out, err := s.Command(ctx, w, domain.ID(), "agent-test", nil, func(tx pgx.Tx) (any, int, error) {
		return s.CreateChatRun(ctx, tx, w, "", "hello", "managed", "fixture", modelID)
	})
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Run string `json:"run_id"`
	}
	json.Unmarshal(out.Body, &r)
	ref := Ref{w, r.Run}
	if _, err = s.AdmitRun(ctx, ref, func(string, string) int64 { return 200000 }); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ClaimModel(ctx, ref, "fixture"); err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestAgentLateKnownSettlementReleasesOnlyUnusedReserve(t *testing.T) {
	s, w := fixture(t)
	ctx := context.Background()
	ref := agentRun(t, s, w)
	if err := s.ClaimAgentStep(ctx, ref, 1, "MODEL", "model", "", 100000); err != nil {
		t.Fatal(err)
	}
	if err := s.FailRun(ctx, ref, "MODEL_INTERRUPTED_OR_FAILED", 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.SettleAgentStep(ctx, ref, 1, "SUCCEEDED", "", 100, 50, 50000); err != nil {
		t.Fatal(err)
	}
	if err := s.SettleAgentStep(ctx, ref, 1, "SUCCEEDED", "", 100, 50, 50000); err != nil {
		t.Fatal(err)
	}
	var charged, reserved int64
	var status, attempt string
	if err := s.Pool.QueryRow(ctx, "SELECT charged_micro_cny,reserved_micro_cny,status,attempt_status FROM runs WHERE workspace_id=$1 AND id=$2", w, ref.ID).Scan(&charged, &reserved, &status, &attempt); err != nil {
		t.Fatal(err)
	}
	if charged != 50000 || reserved != 0 || status != "FAILED" || attempt != "SETTLED" {
		t.Fatal("late settlement duplicated or lost charge", charged, reserved, status, attempt)
	}
}

func TestAgentChecksRevocationBeforeEveryStep(t *testing.T) {
	s, w := fixture(t)
	ctx := context.Background()
	m := PersonalModel{ID: domain.ID(), Label: "fixture", Provider: "deepseek", Model: "fixture", InputPrice: 2000000, OutputPrice: 4000000, Credential: []byte("test-only")}
	_, err := s.Command(ctx, w, domain.ID(), "test-add-model", nil, func(tx pgx.Tx) (any, int, error) { return s.AddModel(ctx, tx, w, m) })
	if err != nil {
		t.Fatal(err)
	}
	ref := agentRun(t, s, w, m.ID)
	if err = s.ClaimAgentStep(ctx, ref, 1, "MODEL", "model", m.ID, 100000); err != nil {
		t.Fatal(err)
	}
	if err = s.SettleAgentStep(ctx, ref, 1, "SUCCEEDED", "", 100, 50, 50000); err != nil {
		t.Fatal(err)
	}
	_, err = s.Command(ctx, w, domain.ID(), "test-remove-model", nil, func(tx pgx.Tx) (any, int, error) { return s.RemoveModel(ctx, tx, w, m.ID) })
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ClaimAgentStep(ctx, ref, 2, "MODEL", "model", m.ID, 100000); err == nil || err.Error() != "MODEL_CONFIG_REVOKED" {
		t.Fatal("revoked configuration submitted")
	}
	if err = s.FailRun(ctx, ref, "MODEL_CONFIG_REVOKED", 0, 0, 0); err != nil {
		t.Fatal(err)
	}
}
func TestAgentPartialChargeUnknownReserveAndKnownRejection(t *testing.T) {
	for _, unknown := range []bool{true, false} {
		s, w := fixture(t)
		ctx := context.Background()
		ref := agentRun(t, s, w)
		if err := s.ClaimAgentStep(ctx, ref, 1, "MODEL", "model", "", 100000); err != nil {
			t.Fatal(err)
		}
		if err := s.SettleAgentStep(ctx, ref, 1, "SUCCEEDED", "", 100, 50, 50000); err != nil {
			t.Fatal(err)
		}
		if err := s.ClaimAgentStep(ctx, ref, 2, "MODEL", "model", "", 190000); err != nil {
			t.Fatal(err)
		}
		if err := s.ClaimAgentStep(ctx, ref, 2, "MODEL", "model", "", 190000); err == nil || err.Error() != "AGENT_STEP_ALREADY_STARTED" {
			t.Fatal("paid step reclaimed")
		}
		status := "FAILED"
		if unknown {
			status = "UNKNOWN"
		}
		if err := s.SettleAgentStep(ctx, ref, 2, status, "FIXTURE_FAILURE", 0, 0, 0); err != nil {
			t.Fatal(err)
		}
		if err := s.FailRun(ctx, ref, "MODEL_INTERRUPTED_OR_FAILED", 0, 0, 0); err != nil {
			t.Fatal(err)
		}
		var charged, reserved, input, output int64
		var attempt string
		if err := s.Pool.QueryRow(ctx, "SELECT charged_micro_cny,reserved_micro_cny,input_tokens,output_tokens,attempt_status FROM runs WHERE workspace_id=$1 AND id=$2", w, ref.ID).Scan(&charged, &reserved, &input, &output, &attempt); err != nil {
			t.Fatal(err)
		}
		if charged != 50000 || input != 100 || output != 50 {
			t.Fatal("known charge erased")
		}
		if unknown && (reserved != 190000 || attempt != "UNKNOWN") {
			t.Fatal("unknown reserve released", reserved, attempt)
		}
		if !unknown && (reserved != 0 || attempt != "SETTLED") {
			t.Fatal("known rejection kept reservation")
		}
	}
}
func TestAgentExtensionCannotExceedRunBudget(t *testing.T) {
	s, w := fixture(t)
	ctx := context.Background()
	ref := agentRun(t, s, w)
	if err := s.ClaimAgentStep(ctx, ref, 1, "MODEL", "model", "", 1000001); err == nil || err.Error() != "RUN_BUDGET_EXCEEDED" {
		t.Fatal("run budget bypassed")
	}
	var count int
	s.Pool.QueryRow(ctx, "SELECT count(*) FROM agent_steps WHERE workspace_id=$1 AND run_id=$2", w, ref.ID).Scan(&count)
	if count != 0 {
		t.Fatal("denied step claimed")
	}
}
