package main

import (
	"go.temporal.io/sdk/worker"
	"log/slog"
	"nemi/internal/bootstrap"
	"nemi/internal/model"
	"nemi/internal/runtime"
	"os"
)

func main() {
	c, s, t := bootstrap.Open()
	defer s.Pool.Close()
	defer t.Close()
	w := worker.New(t, c.RunQueue, worker.Options{MaxConcurrentActivityExecutionSize: 5})
	w.RegisterWorkflow(runtime.RunWorkflow)
	w.RegisterActivity(&runtime.Activities{Store: s, Gateway: model.New(c)})
	if e := w.Run(worker.InterruptCh()); e != nil {
		slog.Error("runtime worker failed")
		os.Exit(1)
	}
}
