package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"nemi/internal/connectors"
	"nemi/internal/domain"
	"nemi/internal/model"
	"nemi/internal/store"
)

var modelIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:/-]{1,128}$`)

func validSecret(s string) bool {
	return len(s) >= 4 && len(s) <= 4096 && !strings.ContainsAny(s, "\r\n\t ")
}
func (a *API) models(w http.ResponseWriter, r *http.Request) {
	list, selected, err := a.Store.Models(r.Context(), identity(r).Workspace)
	if err != nil {
		sendError(w, 503, "暂时无法读取模型配置")
		return
	}
	send(w, 200, map[string]any{"models": list, "default_id": selected, "providers": model.Providers, "fallback_mode": a.Gateway.Mode()})
}
func (a *API) saveModel(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Label       string `json:"label"`
		Provider    string `json:"provider"`
		Model       string `json:"model"`
		Key         string `json:"api_key"`
		InputPrice  int64  `json:"input_price_micro_cny"`
		OutputPrice int64  `json:"output_price_micro_cny"`
	}
	raw, err := decode(w, r, &b)
	b.Label = strings.TrimSpace(b.Label)
	_, supported := model.ProviderByID(b.Provider)
	if err != nil || !supported || !modelIDPattern.MatchString(b.Model) || !validSecret(b.Key) || utf8.RuneCountInString(b.Label) < 1 || utf8.RuneCountInString(b.Label) > 60 || b.InputPrice <= 0 || b.InputPrice > 1000000000 || b.OutputPrice <= 0 || b.OutputPrice > 1000000000 {
		sendError(w, 400, "请填写服务商、模型名称、密钥与有效单价；单价范围为每百万 Token 0.000001–1000 元")
		return
	}
	m := store.PersonalModel{ID: domain.ID(), Label: b.Label, Provider: b.Provider, Model: b.Model, InputPrice: b.InputPrice, OutputPrice: b.OutputPrice}
	m.Credential, err = a.Vault.Seal(model.CredentialOwner(identity(r).Workspace, m.ID), []byte(b.Key))
	if err != nil {
		sendError(w, 503, "凭据存储暂时不可用")
		return
	}
	a.command(w, r, raw, func(tx pgx.Tx) (any, int, error) { return a.Store.AddModel(r.Context(), tx, identity(r).Workspace, m) })
}
func (a *API) defaultModel(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ID string `json:"model_id"`
	}
	raw, err := decode(w, r, &b)
	if err != nil {
		sendError(w, 400, "请选择模型")
		return
	}
	a.command(w, r, raw, func(tx pgx.Tx) (any, int, error) {
		return a.Store.DefaultModel(r.Context(), tx, identity(r).Workspace, b.ID)
	})
}
func (a *API) removeModel(w http.ResponseWriter, r *http.Request) {
	var b struct{}
	raw, err := decode(w, r, &b)
	if err != nil {
		sendError(w, 400, "请求格式不正确")
		return
	}
	a.command(w, r, raw, func(tx pgx.Tx) (any, int, error) {
		return a.Store.RemoveModel(r.Context(), tx, identity(r).Workspace, r.PathValue("id"))
	})
}

// Uses the normal durable, metered run path. No secret or request body goes into
// Temporal history, and an HTTP retry reads the original test run ID.
func (a *API) checkModel(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Confirmed bool `json:"confirmed"`
	}
	raw, err := decode(w, r, &b)
	if err != nil || !b.Confirmed {
		sendError(w, 400, "请确认进行一次真实模型调用")
		return
	}
	a.command(w, r, raw, func(tx pgx.Tx) (any, int, error) {
		ws := identity(r).Workspace
		m, e := a.Store.SelectedModel(r.Context(), tx, ws, r.PathValue("id"))
		if e != nil {
			return nil, 0, e
		}
		g, e := model.Personal(*m, ws, a.Vault)
		if e != nil {
			return nil, 0, e
		}
		matter, eStatus, e := a.Store.CreateMatter(r.Context(), tx, ws, domain.CreateMatter{Title: "模型连接检查", Source: "生成三项检查清单：核对时间、准备资料、记录结果。", Category: "life", Confirmed: true, Timezone: "Asia/Shanghai"})
		_ = eStatus
		if e != nil {
			return nil, 0, e
		}
		created := matter.(domain.Matter)
		out, status, e := a.Store.CreateRun(r.Context(), tx, ws, created.ID, g.Mode(), g.Profile(), created.Revision, false)
		if e != nil {
			return nil, 0, e
		}
		run := out.(domain.Run)
		if e = a.Store.BindRunModel(r.Context(), tx, ws, run.ID, m.ID); e != nil {
			return nil, 0, e
		}
		// Checks are kept as completed matters, with results in the activity feed.
		_, e = tx.Exec(r.Context(), "UPDATE matters SET status='COMPLETED' WHERE workspace_id=$1 AND id=$2", ws, created.ID)
		return run, status, e
	})
}
func (a *API) saveConnection(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := connectors.Names[id]; !ok {
		sendError(w, 404, "没有此应用通道")
		return
	}
	var b struct {
		Label    string `json:"label"`
		Webhook  string `json:"webhook"`
		Secret   string `json:"secret"`
		Expected int    `json:"expected_revision"`
		Enabled  bool   `json:"enabled"`
	}
	raw, err := decode(w, r, &b)
	bot := connectors.Bot{URL: b.Webhook, Secret: b.Secret, Label: strings.TrimSpace(b.Label)}
	if err != nil || b.Expected < 0 || b.Expected > 1000000 || (b.Enabled && (!connectors.Valid(id, bot) || len(b.Secret) > 4096 || strings.ContainsAny(b.Secret, "\r\n"))) {
		sendError(w, 400, "请填写官方机器人 Webhook、接收群名称和安全密钥")
		return
	}
	encrypted := []byte{}
	if b.Enabled {
		plain, _ := json.Marshal(bot)
		encrypted, err = a.Vault.Seal(identity(r).Workspace+":bot:"+id, plain)
		if err != nil {
			sendError(w, 503, "凭据存储暂时不可用")
			return
		}
	}
	a.command(w, r, raw, func(tx pgx.Tx) (any, int, error) {
		return a.Store.SaveConnection(r.Context(), tx, identity(r).Workspace, id, bot.Label, b.Expected, encrypted, b.Enabled)
	})
}
func (a *API) botFor(r *http.Request, id string) (connectors.Bot, int, *store.AppConnection, error) {
	ws := identity(r).Workspace
	saved, err := a.Store.Connection(r.Context(), ws, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return a.Bots.Bots[id], 0, nil, nil
	}
	if err != nil {
		return connectors.Bot{}, 0, nil, err
	}
	if !saved.Enabled {
		return connectors.Bot{}, saved.Revision, &saved, nil
	}
	plain, err := a.Vault.Reveal(ws+":bot:"+id, saved.Credential)
	if err != nil {
		return connectors.Bot{}, saved.Revision, &saved, err
	}
	var bot connectors.Bot
	err = json.Unmarshal(plain, &bot)
	return bot, saved.Revision, &saved, err
}
