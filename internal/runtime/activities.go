package runtime

import (
	"context"
	"errors"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"nemi/internal/model"
	"nemi/internal/store"
	"time"
)

type Activities struct {
	Store   *store.Store
	Gateway *model.Gateway
}

func (a *Activities) Admit(ctx context.Context, r store.Ref) (Admission, error) {
	in, e := a.Store.AdmitRun(ctx, r, a.Gateway.Reserve)
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
	in, e := a.Store.ClaimModel(ctx, r)
	if e != nil {
		return temporal.NewNonRetryableApplicationError("model attempt unavailable", "UNKNOWN", e)
	}
	if in.Profile != a.Gateway.Profile() {
		return a.Store.FailRun(ctx, r, "MODEL_CONFIG_CHANGED", 0, 0, 0)
	}
	activity.RecordHeartbeat(ctx)
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
			}
		}
	}()
	out, e := a.Gateway.Generate(ctx, in.Title, in.Source)
	cost := a.Gateway.Cost(out.InputTokens, out.OutputTokens)
	if e != nil {
		if dbErr := a.Store.FailRun(ctx, r, e.Error(), out.InputTokens, out.OutputTokens, cost); dbErr != nil {
			return dbErr
		}
		return nil
	}
	if cost > in.Reservation && a.Gateway.Mode() != "demo" {
		if e = a.Store.FailRun(ctx, r, "USAGE_EXCEEDS_RESERVATION", out.InputTokens, out.OutputTokens, cost); e != nil {
			return e
		}
		return nil
	}
	// Only DB settlement retries here; never repeat the model call.
	for i := 0; i < 5; i++ {
		e = a.Store.FinishRun(ctx, r, out.Plan, out.InputTokens, out.OutputTokens, cost)
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
