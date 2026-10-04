package main

import (
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/worker"
	"log/slog"
	"nemi/internal/bootstrap"
	"nemi/internal/runtime"
	"os"
)

func main() {
	c, s, t := bootstrap.Open()
	defer s.Pool.Close()
	defer t.Close()
	a := &runtime.Activities{Store: s}
	w := worker.New(t, c.ReminderQueue, worker.Options{MaxConcurrentActivityExecutionSize: 10})
	w.RegisterWorkflow(runtime.ReminderWorkflow)
	w.RegisterActivityWithOptions(a.Deliver, activity.RegisterOptions{Name: "Deliver"})
	if e := w.Run(worker.InterruptCh()); e != nil {
		slog.Error("notification worker failed")
		os.Exit(1)
	}
}
