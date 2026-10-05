package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"nemi/internal/browserreader"
	"nemi/internal/connectors"
	"nemi/internal/domain"
)

func (s *Store) ApplicationCatalog(ctx context.Context, w, query string) ([]connectors.Application, error) {
	connections, err := s.AgentConnections(ctx, w)
	if err != nil {
		return nil, err
	}
	out := []connectors.Application{}
	query = strings.ToLower(strings.TrimSpace(query))
	for _, app := range connectors.Catalog() {
		if app.ID == "browser" && browserreader.Available() {
			app.State = "available"
		}
		if query != "" && !strings.Contains(strings.ToLower(app.ID+" "+app.Name+" "+app.Summary), query) {
			continue
		}
		for _, c := range connections {
			if c["id"] != app.ID {
				continue
			}
			app.Label, _ = c["label"].(string)
			app.Revision, _ = c["revision"].(int)
			verified, _ := c["verified_at"].(*time.Time)
			app.Verified = verified != nil
			if enabled, _ := c["enabled"].(bool); enabled {
				app.State = "configured"
			} else {
				app.Label = ""
			}
		}
		out = append(out, app)
	}
	return out, nil
}

// The target and revision come from a server-created proposal, never from the
// approval request. Clearing credentials and deciding the action share a TX.
func (s *Store) ApplyDisconnectAction(ctx context.Context, tx pgx.Tx, w string, action domain.AgentAction) (string, error) {
	var p domain.AgentConnection
	if action.Kind != "disconnect_app" || json.Unmarshal(action.Payload, &p) != nil || !connectors.Connectable(p.AppID) || p.Revision < 1 {
		return "", errors.New("ACTION_INVALID")
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,5))", w+":"+p.AppID); err != nil {
		return "", err
	}
	var revision int
	var enabled bool
	var label string
	if err := tx.QueryRow(ctx, "SELECT revision,enabled,label FROM app_connections WHERE workspace_id=$1 AND id=$2 FOR UPDATE", w, p.AppID).Scan(&revision, &enabled, &label); err != nil {
		return "", domain.ErrConflict
	}
	if !enabled || revision != p.Revision || label != p.Label {
		return "", domain.ErrConflict
	}
	_, _, err := s.SaveConnection(ctx, tx, w, p.AppID, label, revision, []byte{}, false)
	return p.AppID, err
}
