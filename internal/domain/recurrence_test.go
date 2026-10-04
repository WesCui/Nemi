package domain

import (
	"testing"
	"time"
)

func date(value string) time.Time {
	t, e := time.Parse(time.RFC3339, value)
	if e != nil {
		panic(e)
	}
	return t
}
func TestCalendarRecurrenceAndQuietHours(t *testing.T) {
	for _, tc := range []struct {
		at, now, until, repeat, want string
		quiet                        bool
	}{
		{"2026-12-31T09:00:00+08:00", "2026-12-31T10:00:00+08:00", "2027-01-03T23:59:00+08:00", "daily", "2027-01-01T09:00:00+08:00", false},
		{"2026-10-09T09:00:00+08:00", "2026-10-09T10:00:00+08:00", "2026-10-30T23:59:00+08:00", "weekdays", "2026-10-12T09:00:00+08:00", false},
		{"2026-10-02T09:00:00+08:00", "2026-10-16T10:00:00+08:00", "2026-10-30T23:59:00+08:00", "weekly", "2026-10-23T09:00:00+08:00", false},
		{"2026-10-08T07:00:00+08:00", "2026-10-09T07:30:00+08:00", "2026-10-15T23:59:00+08:00", "daily", "2026-10-09T07:00:00+08:00", true},
		{"2026-10-09T23:00:00+08:00", "2026-10-10T08:01:00+08:00", "2026-10-15T23:59:00+08:00", "weekdays", "2026-10-12T23:00:00+08:00", true},
		{"2026-10-09T09:00:00+08:00", "2026-10-20T10:00:00+08:00", "2026-10-10T23:59:00+08:00", "daily", "", false},
	} {
		t.Run(tc.repeat+tc.now, func(t *testing.T) {
			until := date(tc.until)
			next, e := NextOccurrence(date(tc.at), &until, tc.repeat, tc.quiet, date(tc.now))
			if e != nil {
				t.Fatal(e)
			}
			if tc.want == "" {
				if next != nil {
					t.Fatal("ended recurrence resumed")
				}
				return
			}
			if next == nil || !next.Equal(date(tc.want)) {
				t.Fatalf("next=%v want=%s", next, tc.want)
			}
		})
	}
}
func TestRecurrenceRequiresBoundedExplicitSchedule(t *testing.T) {
	now := date("2026-10-04T10:00:00+08:00")
	at := date("2026-10-05T09:00:00+08:00")
	until := date("2026-11-05T23:59:00+08:00")
	if ValidateRecurrence(at, nil, "daily", false, nil, now) == nil {
		t.Fatal("unbounded recurrence accepted")
	}
	if e := ValidateRecurrence(at, &until, "weekdays", false, nil, now); e != nil {
		t.Fatal(e)
	}
	weekend := date("2026-10-10T09:00:00+08:00")
	if ValidateRecurrence(weekend, &until, "weekdays", false, nil, now) == nil {
		t.Fatal("weekend first occurrence accepted")
	}
	deadline := date("2026-10-05T23:30:00+08:00")
	night := date("2026-10-05T23:00:00+08:00")
	if ValidateRecurrence(night, nil, "once", true, &deadline, now) == nil {
		t.Fatal("quiet adjustment beyond deadline accepted")
	}
}
