package runtime

import (
	"context"
	"errors"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"nemi/internal/agent"
	"nemi/internal/model"
	"nemi/internal/store"
	"nemi/internal/vault"
	"strings"
	"time"
)

type Activities struct {
	Store   *store.Store
	Gateway *model.Gateway
	Vault   *vault.Vault
}

func (a *Activities) Admit(ctx context.Context, r store.Ref) (Admission, error) {
	g, e := model.Resolve(ctx, a.Store, a.Vault, a.Gateway, r)
	if e != nil {
		if strings.HasPrefix(e.Error(), "MODEL_") {
			return Admission{Done: true}, a.Store.FailRun(ctx, r, e.Error(), 0, 0, 0)
		}
		return Admission{}, e
	}
	in, e := a.Store.AdmitRun(ctx, r, g.Reserve)
	if e != nil {
		if e.Error() == "RUN_BUDGET_EXCEEDED" || e.Error() == "DAILY_BUDGET_EXCEEDED" || e.Error() == "MODEL_OUTCOME_UNKNOWN" {
			if err := a.Store.FailRun(ctx, r, e.Error(), 0, 0, 0); err != nil {
				return Admission{}, err
			}
			return Admission{Done: true}, nil
		}
		return Admission{}, e
	}
	return Admission{in.Ready, in.Done}, nil
}
func (a *Activities) Generate(ctx context.Context, r store.Ref) error {
	g, e := model.Resolve(ctx, a.Store, a.Vault, a.Gateway, r)
	if e != nil {
		if strings.HasPrefix(e.Error(), "MODEL_") {
			return a.Store.FailRun(ctx, r, e.Error(), 0, 0, 0)
		}
		return e
	}
	in, e := a.Store.ClaimModel(ctx, r, g.Profile())
	if e != nil {
		if e.Error() == "MODEL_CONFIG_CHANGED" || e.Error() == "MODEL_CONFIG_REVOKED" {
			return a.Store.FailRun(ctx, r, e.Error(), 0, 0, 0)
		}
		return temporal.NewNonRetryableApplicationError("model attempt unavailable", "UNKNOWN", e)
	}
	activity.RecordHeartbeat(ctx)
	executionCtx, cancelExecution := context.WithCancel(ctx)
	defer cancelExecution()
	done := make(chan struct{})
	defer close(done)
	go func() {
		tick := time.NewTicker(3 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
				activity.RecordHeartbeat(ctx)
				if g.Kind == "chat" {
					checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
					active, err := a.Store.AgentActive(checkCtx, r)
					cancel()
					if err != nil || !active {
						cancelExecution()
						return
					}
				}
			}
		}
	}()
	var out model.Output
	if g.Kind == "chat" {
		out, e = agent.Generate(executionCtx, a.Store, a.Vault, g, r, in.Source)
	} else {
		out, e = g.Generate(ctx, in.Title, in.Source)
	}
	cost := g.Cost(out.InputTokens, out.OutputTokens)
	if g.Kind == "chat" {
		cost = out.Charged
	}
	if e != nil {
		if dbErr := a.Store.FailRun(ctx, r, e.Error(), out.InputTokens, out.OutputTokens, cost); dbErr != nil {
			return dbErr
		}
		return nil
	}
	if g.Kind != "chat" && cost > in.Reservation {
		if e = a.Store.FailRun(ctx, r, "USAGE_EXCEEDS_RESERVATION", out.InputTokens, out.OutputTokens, cost); e != nil {
			return e
		}
		return nil
	}
	// Only DB settlement retries here; never repeat the model call.
	for i := 0; i < 5; i++ {
		e = a.Store.FinishRun(ctx, r, out.Plan, out.InputTokens, out.OutputTokens, cost)
		if e != nil && e.Error() == "RUN_NO_LONGER_ACTIVE" {
			return nil
		}
		if e == nil {
			return nil
		}
		if errors.Is(e, context.Canceled) {
			return e
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(i+1) * 200 * time.Millisecond):
		}
	}
	return e
}
func (a *Activities) Fail(ctx context.Context, r store.Ref, code string) error {
	return a.Store.FailRun(ctx, r, code, 0, 0, 0)
}
func (a *Activities) Deliver(ctx context.Context, r store.Ref, revision int) error {
	return a.Store.DeliverReminder(ctx, r, revision)
}
