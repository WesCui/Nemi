package bootstrap

import (
	"context"
	"go.temporal.io/sdk/client"
	"log/slog"
	"nemi/internal/config"
	"nemi/internal/store"
	"os"
)

func Open() (config.Config, *store.Store, client.Client) {
	c, e := config.Load()
	if e != nil {
		slog.Error(e.Error())
		os.Exit(1)
	}
	s, e := store.Open(context.Background(), c.DB)
	if e != nil {
		slog.Error("database unavailable")
		os.Exit(1)
	}
	t, e := client.Dial(client.Options{HostPort: c.Temporal})
	if e != nil {
		slog.Error("Temporal unavailable", "error", e)
		os.Exit(1)
	}
	return c, s, t
}
