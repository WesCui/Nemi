package main

import (
	"context"
	"log/slog"
	"nemi/internal/bootstrap"
	"nemi/internal/runtime"
	"os"
	"os/signal"
)

func main() {
	_, s, t := bootstrap.Open()
	defer s.Pool.Close()
	defer t.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if e := runtime.Relay(ctx, s, t); e != nil {
		slog.Error("relay failed")
		os.Exit(1)
	}
}
