package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"nemi/internal/domain"
	"nemi/internal/model"
)

func (a *API) selectedGateway(ctx context.Context, tx pgx.Tx, w, id, kind string) (*model.Gateway, error) {
	if id == "__server__" {
		if !a.Gateway.Ready() {
			return nil, errors.New("MODEL_NOT_CONFIGURED")
		}
		return a.Gateway.ForKind(kind), nil
	}
	m, err := a.Store.SelectedModel(ctx, tx, w, id)
	if err != nil {
		return nil, err
	}
	if m == nil {
		if !a.Gateway.Ready() {
			return nil, errors.New("MODEL_NOT_CONFIGURED")
		}
		return a.Gateway.ForKind(kind), nil
	}
	g, err := model.Personal(*m, w, a.Vault)
	if err != nil {
		return nil, err
	}
	return g.ForKind(kind), nil
}
func (a *API) conversations(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.Conversations(r.Context(), identity(r).Workspace)
	if err != nil {
		sendError(w, 503, "暂时无法读取对话")
		return
	}
	send(w, 200, map[string]any{"conversations": list})
}
func (a *API) conversation(w http.ResponseWriter, r *http.Request) {
	d, err := a.Store.Conversation(r.Context(), identity(r).Workspace, r.PathValue("id"))
	if errors.Is(err, domain.ErrNotFound) {
		sendError(w, 404, "找不到这段对话")
		return
	}
	if err != nil {
		sendError(w, 503, "暂时无法读取对话")
		return
	}
	send(w, 200, d)
}
func (a *API) chat(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Conversation string `json:"conversation_id"`
		Text         string `json:"text"`
		Model        string `json:"model_id"`
	}
	raw, err := decode(w, r, &b)
	b.Text = strings.TrimSpace(b.Text)
	if err != nil || b.Text == "" || len(b.Text) > 6000 || !utf8.ValidString(b.Text) || len(b.Conversation) > 100 || len(b.Model) > 128 {
		sendError(w, 400, "请输入消息，单条不超过 6000 字节")
		return
	}
	a.command(w, r, raw, func(tx pgx.Tx) (any, int, error) {
		ws := identity(r).Workspace
		g, err := a.selectedGateway(r.Context(), tx, ws, b.Model, "chat")
		if err != nil {
			return nil, 0, err
		}
		return a.Store.CreateChatRun(r.Context(), tx, ws, b.Conversation, b.Text, g.Mode(), g.Profile(), g.ConfigID)
	})
}
