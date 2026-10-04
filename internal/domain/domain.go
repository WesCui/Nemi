package domain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	_ "time/tzdata"
)

var ErrConflict = errors.New("revision or idempotency conflict")
var ErrNotFound = errors.New("not found")
var ErrBusy = errors.New("active run exists")
var ErrMemoryLimit = errors.New("memory limit reached")
var Shanghai, _ = time.LoadLocation("Asia/Shanghai")

func ID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

type Item struct {
	Text string `json:"text"`
	Done bool   `json:"done"`
}
type Plan struct {
	Summary string   `json:"summary"`
	Items   []string `json:"items"`
}
type Matter struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Source    string     `json:"source"`
	Category  string     `json:"category"`
	Status    string     `json:"status"`
	Revision  int        `json:"revision"`
	Items     []Item     `json:"items"`
	Deadline  *time.Time `json:"deadline"`
	CreatedAt time.Time  `json:"created_at"`
}
type Reminder struct {
	ID         string     `json:"id"`
	MatterID   string     `json:"matter_id"`
	Title      string     `json:"title"`
	Revision   int        `json:"revision"`
	Nominal    time.Time  `json:"nominal_at"`
	Due        time.Time  `json:"due_at"`
	Quiet      bool       `json:"quiet"`
	Enabled    bool       `json:"enabled"`
	SyncStatus string     `json:"sync_status"`
	Repeat     string     `json:"repeat"`
	Until      *time.Time `json:"repeat_until"`
}
type Run struct {
	ID           string    `json:"id"`
	MatterID     string    `json:"matter_id"`
	Status       string    `json:"status"`
	Mode         string    `json:"mode"`
	Result       *Plan     `json:"result"`
	Error        string    `json:"error"`
	CreatedAt    time.Time `json:"created_at"`
	UsedMemories int       `json:"used_memory_count"`
}
type Memory struct {
	ID        string    `json:"id"`
	Category  string    `json:"category"`
	Text      string    `json:"text"`
	Revision  int       `json:"revision"`
	UpdatedAt time.Time `json:"updated_at"`
}
type Notification struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	MatterID  string    `json:"matter_id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}
type Event struct {
	Sequence  int64     `json:"sequence"`
	Kind      string    `json:"kind"`
	SubjectID string    `json:"subject_id"`
	CreatedAt time.Time `json:"created_at"`
}
type CreateMatter struct {
	Title      string     `json:"title"`
	Source     string     `json:"source"`
	Category   string     `json:"category"`
	Deadline   *time.Time `json:"deadline"`
	ReminderAt *time.Time `json:"reminder_at"`
	Quiet      bool       `json:"quiet"`
	Timezone   string     `json:"timezone"`
	Confirmed  bool       `json:"confirmed"`
	Repeat     string     `json:"repeat"`
	Until      *time.Time `json:"repeat_until"`
}

func (c *CreateMatter) Validate(now time.Time) error {
	c.Title = strings.TrimSpace(c.Title)
	c.Source = strings.TrimSpace(c.Source)
	if len([]rune(c.Title)) < 1 || len([]rune(c.Title)) > 100 || len(c.Source) > 12000 {
		return errors.New("标题应为 1–100 字，资料不超过 12000 字节")
	}
	if c.Timezone != "Asia/Shanghai" || !c.Confirmed {
		return errors.New("请确认事项与时间（北京时间）")
	}
	if c.Category != "life" && c.Category != "travel" && c.Category != "work" {
		return errors.New("未知事项类型")
	}
	for _, t := range []*time.Time{c.Deadline, c.ReminderAt} {
		if t != nil && (!t.After(now) || t.After(now.AddDate(2, 0, 0))) {
			return errors.New("日期应在未来两年内")
		}
	}
	if c.Deadline != nil && c.ReminderAt != nil && c.ReminderAt.After(*c.Deadline) {
		return errors.New("提醒时间不能晚于截止时间")
	}
	c.Repeat = NormalizeRepeat(c.Repeat)
	if c.ReminderAt != nil {
		return ValidateRecurrence(*c.ReminderAt, c.Until, c.Repeat, c.Quiet, c.Deadline, now)
	}
	if c.Repeat != "once" || c.Until != nil {
		return errors.New("请先设置提醒时间")
	}
	return nil
}

// EffectiveDue implements the previewed 22:00–08:00 quiet window in China time.
func EffectiveDue(nominal time.Time, quiet bool) time.Time {
	t := nominal.In(Shanghai)
	if quiet && t.Hour() >= 22 {
		return time.Date(t.Year(), t.Month(), t.Day()+1, 8, 0, 0, 0, Shanghai)
	}
	if quiet && t.Hour() < 8 {
		return time.Date(t.Year(), t.Month(), t.Day(), 8, 0, 0, 0, Shanghai)
	}
	return t
}
