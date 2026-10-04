package api

import (
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"nemi/internal/connectors"
	"nemi/internal/domain"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

const feishuDocsID = "feishu_documents"

func (a *API) feishuDocsState(w http.ResponseWriter, r *http.Request) {
	c, err := a.Store.Connection(r.Context(), identity(r).Workspace, feishuDocsID)
	if errors.Is(err, pgx.ErrNoRows) {
		send(w, 200, map[string]any{"configured": false, "revision": 0, "label": ""})
		return
	}
	if err != nil {
		sendError(w, 503, "暂时无法读取飞书应用配置")
		return
	}
	send(w, 200, map[string]any{"configured": c.Enabled, "revision": c.Revision, "label": c.Label, "verified_at": c.VerifiedAt})
}
func (a *API) saveFeishuDocs(w http.ResponseWriter, r *http.Request) {
	var b struct {
		AppID    string `json:"app_id"`
		Secret   string `json:"app_secret"`
		Label    string `json:"label"`
		Expected int    `json:"expected_revision"`
		Enabled  bool   `json:"enabled"`
	}
	raw, err := decode(w, r, &b)
	if err != nil || b.Expected < 0 || b.Expected > 1000000 || (b.Enabled && (!strings.HasPrefix(b.AppID, "cli_") || !modelIDPattern.MatchString(b.AppID) || !validSecret(b.Secret) || utf8.RuneCountInString(strings.TrimSpace(b.Label)) < 1 || utf8.RuneCountInString(b.Label) > 60)) {
		sendError(w, 400, "请填写飞书自建应用的名称、App ID 和 App Secret")
		return
	}
	encrypted := []byte{}
	if b.Enabled {
		plain, _ := json.Marshal(connectors.FeishuApp{ID: b.AppID, Secret: b.Secret})
		encrypted, err = a.Vault.Seal(identity(r).Workspace+":app:"+feishuDocsID, plain)
		if err != nil {
			sendError(w, 503, "应用凭据暂时无法保存")
			return
		}
	}
	a.command(w, r, raw, func(tx pgx.Tx) (any, int, error) {
		return a.Store.SaveConnection(r.Context(), tx, identity(r).Workspace, feishuDocsID, strings.TrimSpace(b.Label), b.Expected, encrypted, b.Enabled)
	})
}
func (a *API) importFeishuDoc(w http.ResponseWriter, r *http.Request) {
	var b struct {
		URL       string `json:"document_url"`
		Title     string `json:"title"`
		Expected  int    `json:"config_revision"`
		Confirmed bool   `json:"confirmed"`
	}
	raw, err := decode(w, r, &b)
	id, e := connectors.FeishuDocumentID(b.URL)
	if err != nil || e != nil || !b.Confirmed || utf8.RuneCountInString(strings.TrimSpace(b.Title)) < 1 || utf8.RuneCountInString(b.Title) > 100 {
		sendError(w, 400, "请确认文档链接与事项名称；目前支持飞书 docx 文档")
		return
	}
	// The command transaction serializes retries. Reading a specified document
	// is read-only; an interrupted read cannot create a partial matter.
	a.command(w, r, raw, func(tx pgx.Tx) (any, int, error) {
		ws := identity(r).Workspace
		c, e := a.Store.Connection(r.Context(), ws, feishuDocsID)
		if e != nil || !c.Enabled || c.Revision != b.Expected {
			return nil, 0, domain.ErrConflict
		}
		plain, e := a.Vault.Reveal(ws+":app:"+feishuDocsID, c.Credential)
		if e != nil {
			return nil, 0, e
		}
		var app connectors.FeishuApp
		if e = json.Unmarshal(plain, &app); e != nil {
			return nil, 0, e
		}
		text, e := a.Documents.Read(r.Context(), app, id)
		if e != nil {
			return nil, 0, e
		}
		// Lock the connection before the event sequence lock, as credential edits
		// do, and reject a config change or revocation during the document read.
		tag, e := tx.Exec(r.Context(), "UPDATE app_connections SET verified_at=now() WHERE workspace_id=$1 AND id=$2 AND revision=$3 AND enabled", ws, feishuDocsID, c.Revision)
		if e != nil {
			return nil, 0, e
		}
		if tag.RowsAffected() == 0 {
			return nil, 0, domain.ErrConflict
		}
		u, _ := url.Parse(b.URL)
		u.RawQuery = ""
		u.Fragment = ""
		return a.Store.CreateMatter(r.Context(), tx, ws, domain.CreateMatter{Title: strings.TrimSpace(b.Title), Source: text, Category: "work", Timezone: "Asia/Shanghai", Confirmed: true, OriginURL: u.String(), OriginProvider: "feishu"})
	})
}
