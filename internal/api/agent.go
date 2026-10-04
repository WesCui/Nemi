package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"nemi/internal/domain"
)

func (a *API) decideAgentAction(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Confirmed bool `json:"confirmed"`
	}
	raw, err := decode(w, r, &b)
	if err != nil || !b.Confirmed || len(r.PathValue("id")) != 32 {
		sendError(w, 400, "请确认这次操作")
		return
	}
	a.command(w, r, raw, func(tx pgx.Tx) (any, int, error) {
		ws := identity(r).Workspace
		action, err := a.Store.Action(r.Context(), tx, ws, r.PathValue("id"))
		if err != nil {
			return nil, 0, err
		}
		approve := r.PathValue("decision") == "approve"
		if !approve && r.PathValue("decision") != "dismiss" {
			return nil, 0, domain.ErrNotFound
		}
		if action.Status != "PENDING" {
			if (approve && action.Status == "APPROVED") || (!approve && action.Status == "DECLINED") {
				return map[string]string{"status": action.Status, "matter_id": action.ResultID}, 200, nil
			}
			return nil, 0, domain.ErrConflict
		}
		if !approve {
			if err = a.Store.DecideAction(r.Context(), tx, ws, action.ID, "DECLINED", ""); err != nil {
				return nil, 0, err
			}
			return map[string]string{"status": "DECLINED"}, 200, nil
		}
		if action.Kind != "create_matter" {
			return nil, 0, domain.ErrConflict
		}
		var p domain.AgentMatter
		if json.Unmarshal(action.Payload, &p) != nil {
			return nil, 0, errors.New("ACTION_INVALID")
		}
		c := p.Create()
		if c.Validate(time.Now()) != nil {
			return nil, 0, errors.New("ACTION_INVALID")
		}
		out, _, err := a.Store.CreateMatter(r.Context(), tx, ws, c)
		if err != nil {
			return nil, 0, err
		}
		matter, ok := out.(domain.Matter)
		if !ok {
			return nil, 0, errors.New("ACTION_INVALID")
		}
		if err = a.Store.DecideAction(r.Context(), tx, ws, action.ID, "APPROVED", matter.ID); err != nil {
			return nil, 0, err
		}
		return map[string]string{"status": "APPROVED", "matter_id": matter.ID}, 201, nil
	})
}
