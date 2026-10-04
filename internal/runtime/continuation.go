package runtime

import (
	"go.temporal.io/sdk/workflow"
	"nemi/internal/store"
	"time"
)

const ContinuationSignal = "approval_changed"

// Only DB work is retried. Each resumed run has its own durable paid-call
// claims; recovery of this workflow cannot replay the parent model request.
func ContinuationWorkflow(ctx workflow.Context, ref store.Ref) error {
	options := dbOptions()
	options.RetryPolicy.MaximumAttempts = 0
	options.RetryPolicy.MaximumInterval = time.Minute
	ctx = workflow.WithActivityOptions(ctx, options)
	changes := workflow.GetSignalChannel(ctx, ContinuationSignal)
	for i := 0; i < 128; i++ {
		var result store.ContinuationCheck
		if err := workflow.ExecuteActivity(ctx, "AdvanceContinuation", ref).Get(ctx, &result); err != nil {
			return err
		}
		if result.Done {
			return nil
		}
		if result.Until.IsZero() {
			return workflow.NewContinueAsNewError(ctx, ContinuationWorkflow, ref)
		}
		timerCtx, cancel := workflow.WithCancel(ctx)
		timer := workflow.NewTimer(timerCtx, result.Until.Sub(workflow.Now(ctx)))
		selector := workflow.NewSelector(ctx)
		selector.AddReceive(changes, func(ch workflow.ReceiveChannel, more bool) { var revision int; ch.Receive(ctx, &revision) })
		selector.AddFuture(timer, func(workflow.Future) {})
		selector.Select(ctx)
		cancel()
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return workflow.NewContinueAsNewError(ctx, ContinuationWorkflow, ref)
}
