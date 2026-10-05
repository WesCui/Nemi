package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"nemi/internal/connectors"
)

func (a *API) serviceConnection(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !connectors.ServiceID(id) {
		sendError(w, 404, "没有此应用服务")
		return
	}
	c, err := a.Store.Connection(r.Context(), identity(r).Workspace, id)
	if errors.Is(err, pgx.ErrNoRows) {
		send(w, 200, map[string]any{"configured": false, "revision": 0, "label": ""})
		return
	}
	if err != nil {
		sendError(w, 503, "暂时无法读取连接")
		return
	}
	send(w, 200, map[string]any{"configured": c.Enabled, "revision": c.Revision, "label": c.Label, "verified_at": c.VerifiedAt})
}
func (a *API) saveServiceConnection(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !connectors.ServiceID(id) {
		sendError(w, 404, "没有此应用服务")
		return
	}
	var p struct {
		connectors.ServiceCredential
		Label     string `json:"label"`
		Expected  int    `json:"expected_revision"`
		Enabled   bool   `json:"enabled"`
		Confirmed bool   `json:"confirmed"`
	}
	raw, err := decode(w, r, &p)
	if err != nil || !p.Confirmed || p.Expected < 0 || p.Expected > 1000000 || utf8.RuneCountInString(strings.TrimSpace(p.Label)) < 1 || utf8.RuneCountInString(p.Label) > 60 || (p.Enabled && !p.ServiceCredential.Valid(id)) {
		sendError(w, 400, "请核对连接名称、专用凭据和资料读取授权")
		return
	}
	encrypted := []byte{}
	if p.Enabled {
		plain, _ := json.Marshal(p.ServiceCredential)
		encrypted, err = a.Vault.Seal(identity(r).Workspace+":app:"+id, plain)
		if err != nil {
			sendError(w, 503, "暂时无法保存凭据")
			return
		}
	}
	a.command(w, r, raw, func(tx pgx.Tx) (any, int, error) {
		return a.Store.SaveConnection(r.Context(), tx, identity(r).Workspace, id, strings.TrimSpace(p.Label), p.Expected, encrypted, p.Enabled)
	})
}
