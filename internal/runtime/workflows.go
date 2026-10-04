package runtime

import (
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
	"nemi/internal/store"
	"time"
)

const RunQueue = "nemi-runtime-v1"
const ReminderQueue = "nemi-notification-v1"

type ReminderInput struct {
	Ref      store.Ref
	Revision int
	Due      time.Time
}
type Admission struct{ Ready, Done bool }

func dbOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{StartToCloseTimeout: 15 * time.Second, RetryPolicy: &temporal.RetryPolicy{InitialInterval: time.Second, MaximumInterval: 5 * time.Second, MaximumAttempts: 5}}
}
func RunWorkflow(ctx workflow.Context, ref store.Ref) error {
	ctx = workflow.WithActivityOptions(ctx, dbOptions())
	for i := 0; i < 180; i++ {
		var a Admission
		err := workflow.ExecuteActivity(ctx, "Admit", ref).Get(ctx, &a)
		if err != nil {
			return finishFailure(ctx, ref, "ADMISSION_FAILED")
		}
		if a.Done {
			return nil
		}
		if !a.Ready {
			if err = workflow.Sleep(ctx, 5*time.Second); err != nil {
				return err
			}
			continue
		}
		// Never replay paid work after an infrastructure interruption.
		modelCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: 10 * time.Minute, HeartbeatTimeout: 15 * time.Second, RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1}})
		if err = workflow.ExecuteActivity(modelCtx, "Generate", ref).Get(ctx, nil); err != nil {
			return finishFailure(ctx, ref, "MODEL_INTERRUPTED_OR_FAILED")
		}
		return nil
	}
	return finishFailure(ctx, ref, "QUEUE_TIMEOUT")
}
func finishFailure(ctx workflow.Context, r store.Ref, code string) error {
	return workflow.ExecuteActivity(ctx, "Fail", r, code).Get(ctx, nil)
}
func ReminderWorkflow(ctx workflow.Context, in ReminderInput) error {
	if delay := in.Due.Sub(workflow.Now(ctx)); delay > 0 {
		if err := workflow.Sleep(ctx, delay); err != nil {
			return err
		}
	}
	ctx = workflow.WithActivityOptions(ctx, dbOptions())
	return workflow.ExecuteActivity(ctx, "Deliver", in.Ref, in.Revision).Get(ctx, nil)
}
