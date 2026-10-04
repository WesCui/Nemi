package api

import (
	"errors"
	"github.com/jackc/pgx/v5"
	"nemi/internal/domain"
	"nemi/internal/store"
	"net/http"
)

func (a *API) continuation(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Confirmed bool `json:"confirmed"`
	}
	raw, err := decode(w, r, &input)
	if err != nil || !input.Confirmed || len(r.PathValue("id")) != 32 {
		sendError(w, 400, "请确认这次继续或停止操作")
		return
	}
	a.command(w, r, raw, func(tx pgx.Tx) (any, int, error) {
		ref := store.Ref{Workspace: identity(r).Workspace, ID: r.PathValue("id")}
		switch r.PathValue("decision") {
		case "start":
			out, code, e := a.Store.AuthorizeContinuation(r.Context(), tx, ref)
			if e != nil && e.Error() == "CONTINUATION_EXPIRED" {
				return nil, 0, domain.ErrConflict
			}
			return out, code, e
		case "stop":
			return a.Store.StopContinuation(r.Context(), tx, ref)
		}
		return nil, 0, errors.New("ACTION_INVALID")
	})
}
