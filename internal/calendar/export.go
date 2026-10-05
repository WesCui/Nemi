package calendar

import (
	"time"

	ics "github.com/arran4/golang-ical"
)

const Note = "Nemi 事项的下一次提醒或截止时间。单次导出，不会同步后续更改；请导入日历并检查时间与通知设置。"

// Reuse the iCalendar encoder for escaping and line folding. The stable UID
// identifies this matter when a user imports the file again.
func Export(id, title string, at, now time.Time) []byte {
	cal := ics.NewCalendar()
	cal.SetProductId("-//Nemi//Personal Calendar//ZH-CN")
	cal.SetMethod(ics.MethodPublish)
	event := cal.AddEvent(id + "@nemi.local")
	event.SetDtStampTime(now)
	event.SetStartAt(at)
	event.SetEndAt(at.Add(15 * time.Minute))
	event.SetSummary(title)
	event.SetDescription(Note)
	return []byte(cal.Serialize(ics.WithNewLine("\r\n")))
}
