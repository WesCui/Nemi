package runtime

import (
	"context"
	"errors"
	"fmt"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"log/slog"
	"nemi/internal/store"
	"time"
)

func WorkflowID(o store.Outbox) string {
	if o.Kind == "run" {
		return "run/" + o.Workspace + "/" + o.Subject
	}
	return fmt.Sprintf("reminder/%s/%s/%d", o.Workspace, o.Subject, o.Revision)
}
func Dispatch(ctx context.Context, c client.Client, o store.Outbox) error {
	return dispatchTo(ctx, c, o, RunQueue, ReminderQueue)
}
func dispatchTo(ctx context.Context, c client.Client, o store.Outbox, runQueue, reminderQueue string) error {
	id := WorkflowID(o)
	if o.Kind == "cancel_reminder" {
		e := c.CancelWorkflow(ctx, id, "")
		var missing *serviceerror.NotFound
		if errors.As(e, &missing) {
			return nil
		}
		return e
	}
	opt := client.StartWorkflowOptions{ID: id, WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE, WorkflowExecutionTimeout: 7 * 24 * time.Hour}
	var e error
	if o.Kind == "run" {
		opt.TaskQueue = runQueue
		_, e = c.ExecuteWorkflow(ctx, opt, RunWorkflow, store.Ref{Workspace: o.Workspace, ID: o.Subject})
	} else {
		if o.Kind != "reminder" || o.Due == nil {
			return errors.New("INVALID_OUTBOX")
		}
		opt.TaskQueue = reminderQueue
		opt.WorkflowExecutionTimeout = 0
		_, e = c.ExecuteWorkflow(ctx, opt, ReminderWorkflow, ReminderInput{Ref: store.Ref{Workspace: o.Workspace, ID: o.Subject}, Revision: o.Revision, Due: *o.Due})
	}
	var duplicate *serviceerror.WorkflowExecutionAlreadyStarted
	if errors.As(e, &duplicate) {
		return nil
	}
	return e
}
func Relay(ctx context.Context, s *store.Store, c client.Client, queues ...string) error {
	runQueue, reminderQueue := RunQueue, ReminderQueue
	if len(queues) >= 2 {
		runQueue, reminderQueue = queues[0], queues[1]
	}
	workspace := ""
	if len(queues) >= 3 {
		workspace = queues[2]
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		o, e := s.LeaseOutbox(ctx, workspace)
		if e != nil {
			return e
		}
		if o == nil {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(300 * time.Millisecond):
			}
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		e = dispatchTo(callCtx, c, *o, runQueue, reminderQueue)
		cancel()
		if e != nil {
			slog.Warn("outbox dispatch deferred", "kind", o.Kind)
			if e = s.RetryOutbox(ctx, *o); e != nil {
				return e
			}
		} else {
			if e = s.CompleteOutbox(ctx, *o); e != nil {
				return e
			}
		}
	}
}
