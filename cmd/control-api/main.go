package main

import (
	"context"
	"log/slog"
	"nemi/internal/api"
	"nemi/internal/config"
	"nemi/internal/model"
	"nemi/internal/store"
	"net/http"
	"os"
	"os/signal"
	"time"
)

func main() {
	c, e := config.Load()
	if e != nil {
		slog.Error(e.Error())
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	s, e := store.Open(ctx, c.DB)
	if e != nil {
		slog.Error("database unavailable")
		os.Exit(1)
	}
	defer s.Pool.Close()
	if e = s.Migrate(ctx); e != nil {
		slog.Error("migration failed")
		os.Exit(1)
	}
	if e = s.Bootstrap(ctx, c.Invite); e != nil {
		slog.Error("identity initialization failed")
		os.Exit(1)
	}
	h := api.NewHub(s)
	go h.Run(ctx)
	a := &api.API{Store: s, Hub: h, Config: c, Gateway: model.New(c)}
	server := &http.Server{Addr: c.Listen, Handler: a.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	slog.Info("Nemi control API ready", "address", c.Listen, "model_provider", c.Provider)
	if e = server.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		slog.Error("HTTP server failed")
		os.Exit(1)
	}
}
