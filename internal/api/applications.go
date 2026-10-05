package api

import "net/http"

func (a *API) applications(w http.ResponseWriter, r *http.Request) {
	apps, err := a.Store.ApplicationCatalog(r.Context(), identity(r).Workspace, "")
	if err != nil {
		sendError(w, 503, "暂时无法读取应用能力")
		return
	}
	send(w, 200, map[string]any{"applications": apps})
}
