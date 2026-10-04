package main

import (
	"go.temporal.io/sdk/worker"
	"log/slog"
	"nemi/internal/bootstrap"
	"nemi/internal/files"
	"nemi/internal/model"
	"nemi/internal/runtime"
	"nemi/internal/vault"
	"os"
)

func main() {
	c, s, t := bootstrap.Open()
	defer s.Pool.Close()
	defer t.Close()
	w := worker.New(t, c.RunQueue, worker.Options{MaxConcurrentActivityExecutionSize: 5})
	w.RegisterWorkflow(runtime.RunWorkflow)
	v, e := vault.Open(c.VaultKey, c.VaultPath)
	if e != nil {
		slog.Error("credential vault unavailable")
		os.Exit(1)
	}
	f, e := files.New(c, s, v)
	if e != nil {
		slog.Error("file storage configuration unavailable")
		os.Exit(1)
	}
	w.RegisterActivity(&runtime.Activities{Store: s, Gateway: model.New(c), Vault: v, Files: f})
	if e := w.Run(worker.InterruptCh()); e != nil {
		slog.Error("runtime worker failed")
		os.Exit(1)
	}
}
