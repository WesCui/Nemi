package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"nemi/internal/connectors"
	"nemi/internal/domain"
)

func (a *API) approveAgentMessage(w http.ResponseWriter, r *http.Request, action domain.AgentAction) {
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 8 || len(key) > 100 {
		sendError(w, 400, "缺少有效的确认标识")
		return
	}
	var p domain.AgentMessage
	if json.Unmarshal(action.Payload, &p) != nil || connectors.Names[p.Channel] == "" || strings.TrimSpace(p.Text) == "" || len(p.Text) > 1800 {
		sendError(w, 422, "消息提案无效，请重新准备")
		return
	}
	bot, revision, _, err := a.botFor(r, p.Channel)
	if err != nil {
		sendError(w, 503, "应用凭据暂时不可用")
		return
	}
	if action.Status == "PENDING" && !connectors.Valid(p.Channel, bot) {
		sendError(w, 409, "接收群已停用，请重新连接后准备提案")
		return
	}
	ws := identity(r).Workspace
	status, claimed, err := a.Store.ClaimAgentMessage(r.Context(), ws, action.ID, revision)
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			sendError(w, 409, "接收群配置已变更或提案已取消，请让妮米重新准备后确认")
		} else if errors.Is(err, domain.ErrBusy) {
			sendError(w, 429, "发送较多，请稍后再试")
		} else {
			sendError(w, 503, "暂时无法保存发送请求")
		}
		return
	}
	if claimed {
		sender := connectors.New(map[string]connectors.Bot{p.Channel: bot})
		sender.Client = a.Bots.Client
		status = sender.Send(r.Context(), p.Channel, p.Text)
		settleCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Second)
		defer cancel()
		if a.Store.SettleDispatch(settleCtx, ws, action.ID, status) != nil {
			status = "UNKNOWN"
		}
		if status == "DELIVERED" {
			_ = a.Store.VerifyConnection(settleCtx, ws, p.Channel, revision)
		}
	}
	send(w, 200, map[string]string{"status": "APPROVED", "dispatch_status": status})
}
