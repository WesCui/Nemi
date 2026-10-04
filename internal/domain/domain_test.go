package domain

import (
	"testing"
	"time"
)

func TestQuietWindow(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		quiet    bool
	}{{"2026-12-31T23:30:00+08:00", "2027-01-01T08:00:00+08:00", true}, {"2026-10-04T07:59:00+08:00", "2026-10-04T08:00:00+08:00", true}, {"2026-10-04T08:00:00+08:00", "2026-10-04T08:00:00+08:00", true}, {"2026-10-04T23:30:00+08:00", "2026-10-04T23:30:00+08:00", false}} {
		in, _ := time.Parse(time.RFC3339, tc.in)
		want, _ := time.Parse(time.RFC3339, tc.want)
		if got := EffectiveDue(in, tc.quiet); !got.Equal(want) {
			t.Fatalf("%s: got %s want %s", tc.in, got, want)
		}
	}
}
func TestConfirmationRequired(t *testing.T) {
	c := CreateMatter{Title: "准备材料", Category: "life", Timezone: "Asia/Shanghai"}
	if c.Validate(time.Now()) == nil {
		t.Fatal("unconfirmed input accepted")
	}
	c.Confirmed = true
	if e := c.Validate(time.Now()); e != nil {
		t.Fatal(e)
	}
	c.Timezone = "UTC"
	if c.Validate(time.Now()) == nil {
		t.Fatal("unconfirmed timezone accepted")
	}
}
func TestReminderAfterDeadlineRejected(t *testing.T) {
	now := time.Now()
	deadline := now.Add(time.Hour)
	reminder := now.Add(2 * time.Hour)
	c := CreateMatter{Title: "准备材料", Category: "life", Timezone: "Asia/Shanghai", Confirmed: true, Deadline: &deadline, ReminderAt: &reminder}
	if c.Validate(now) == nil {
		t.Fatal("late reminder accepted")
	}
}
