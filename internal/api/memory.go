package api

import (
	"github.com/jackc/pgx/v5"
	"nemi/internal/store"
	"net/http"
)

func (a *API) saveMemory(w http.ResponseWriter, r *http.Request) {
	var p store.SaveMemory
	b, e := decode(w, r, &p)
	if e != nil {
		sendError(w, 400, "偏好格式不正确")
		return
	}
	if e = p.Validate(); e != nil {
		sendError(w, 400, e.Error())
		return
	}
	id := r.PathValue("id")
	if (id != "" && p.Expected < 1) || (id == "" && p.Expected != 0) {
		sendError(w, 400, "偏好版本不正确")
		return
	}
	a.command(w, r, b, func(tx pgx.Tx) (any, int, error) {
		return a.Store.SaveMemory(r.Context(), tx, identity(r).Workspace, id, p)
	})
}
func (a *API) deleteMemory(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Expected  int  `json:"expected_revision"`
		Confirmed bool `json:"confirmed"`
	}
	b, e := decode(w, r, &p)
	if e != nil || p.Expected < 1 || !p.Confirmed {
		sendError(w, 400, "请确认移除这条偏好")
		return
	}
	a.command(w, r, b, func(tx pgx.Tx) (any, int, error) {
		return a.Store.DeleteMemory(r.Context(), tx, identity(r).Workspace, r.PathValue("id"), p.Expected)
	})
}
