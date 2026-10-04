package api

import (
	"context"
	"errors"
	ics "github.com/arran4/golang-ical"
	"github.com/jackc/pgx/v5"
	"nemi/internal/connectors"
	"nemi/internal/domain"
	"net/http"
	"strings"
	"time"
)

func (a *API) connections(w http.ResponseWriter, r *http.Request) {
	send(w, 200, map[string]any{"channels": a.Bots.Statuses()})
}
func (a *API) sendMessage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := connectors.Names[id]; !ok {
		sendError(w, 404, "没有此应用通道")
		return
	}
	var b struct {
		Text      string `json:"text"`
		Confirmed bool   `json:"confirmed"`
	}
	raw, e := decode(w, r, &b)
	if e != nil || !b.Confirmed || strings.TrimSpace(b.Text) == "" || len([]byte(b.Text)) > 1800 {
		sendError(w, 400, "请确认发送内容，正文最多 1800 字节")
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 8 || len(key) > 100 {
		sendError(w, 400, "缺少有效的发送标识")
		return
	}
	if !connectors.Valid(id, a.Bots.Bots[id]) {
		sendError(w, 409, "此群机器人尚未正确配置")
		return
	}
	workspace := identity(r).Workspace
	status, claimed, e := a.Store.ClaimDispatch(r.Context(), workspace, key, id, raw)
	if e != nil {
		if errors.Is(e, domain.ErrConflict) {
			sendError(w, 409, "发送标识已经用于另一条内容")
		} else if errors.Is(e, domain.ErrBusy) {
			sendError(w, 429, "发送较多，请稍后再试")
		} else {
			sendError(w, 503, "暂时无法记录发送请求")
		}
		return
	}
	if claimed {
		status = a.Bots.Send(r.Context(), id, b.Text)
		// Persist even when the requester disconnects, without repeating the send.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Second)
		defer cancel()
		if a.Store.SettleDispatch(ctx, workspace, key, status) != nil {
			status = "UNKNOWN"
		}
	}
	send(w, 200, map[string]string{"status": status})
}
func (a *API) calendar(w http.ResponseWriter, r *http.Request) {
	title, at, e := a.Store.CalendarTime(r.Context(), identity(r).Workspace, r.PathValue("id"))
	if e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			sendError(w, 404, "找不到此事项")
		} else if errors.Is(e, domain.ErrConflict) {
			sendError(w, 400, "请先设置提醒或截止时间")
		} else {
			sendError(w, 503, "暂时无法导出日历")
		}
		return
	}
	cal := ics.NewCalendar()
	cal.SetProductId("-//Nemi//Personal Calendar//ZH-CN")
	cal.SetMethod(ics.MethodPublish)
	event := cal.AddEvent(r.PathValue("id") + "@nemi.local")
	event.SetDtStampTime(time.Now())
	event.SetStartAt(at)
	event.SetEndAt(at.Add(15 * time.Minute))
	event.SetSummary(title)
	event.SetDescription("Nemi 事项的下一次提醒或截止时间。单次导出，不会同步后续更改；请在日历中检查时间与通知设置。")
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="nemi-event.ics"`)
	w.Write([]byte(cal.Serialize()))
}
