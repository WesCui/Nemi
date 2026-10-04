package domain

import (
	"errors"
	rrule "github.com/teambition/rrule-go"
	"time"
)

func NormalizeRepeat(value string) string {
	if value == "" {
		return "once"
	}
	return value
}

func ValidateRecurrence(at time.Time, until *time.Time, repeat string, quiet bool, deadline *time.Time, now time.Time) error {
	repeat = NormalizeRepeat(repeat)
	if repeat != "once" && repeat != "daily" && repeat != "weekdays" && repeat != "weekly" {
		return errors.New("不支持的重复频率")
	}
	if !at.After(now) || at.After(now.AddDate(2, 0, 0)) {
		return errors.New("提醒日期应在未来两年内")
	}
	if deadline != nil && EffectiveDue(at, quiet).After(*deadline) {
		return errors.New("免打扰调整后的提醒不能晚于截止时间")
	}
	if repeat == "once" {
		if until != nil {
			return errors.New("一次性提醒不需要结束日期")
		}
		return nil
	}
	if until == nil || until.Before(at) || until.After(now.AddDate(2, 0, 0)) {
		return errors.New("周期提醒需确认结束日期，且在首次提醒至未来两年之间")
	}
	if deadline != nil && until.After(*deadline) {
		return errors.New("周期提醒结束时间不能晚于事项截止时间")
	}
	if repeat == "weekdays" && (at.In(Shanghai).Weekday() == time.Saturday || at.In(Shanghai).Weekday() == time.Sunday) {
		return errors.New("周一至周五提醒的首次日期请选择周一至周五；暂不计算法定节假日调休")
	}
	return nil
}

// Calendar math is delegated to rrule-go (RFC 5545), not implemented as durations.
// We only add our quiet-hours and missed-delivery policy. At most one overdue
// record is delivered after downtime; subsequent missed occurrences are skipped.
func NextOccurrence(at time.Time, until *time.Time, repeat string, quiet bool, now time.Time) (*time.Time, error) {
	repeat = NormalizeRepeat(repeat)
	if repeat == "once" {
		return nil, nil
	}
	if until == nil {
		return nil, errors.New("REPEAT_END_REQUIRED")
	}
	options := rrule.ROption{Freq: rrule.DAILY, Dtstart: at.In(Shanghai), Until: until.In(Shanghai)}
	switch repeat {
	case "daily":
	case "weekly":
		options.Freq = rrule.WEEKLY
	case "weekdays":
		options.Byweekday = []rrule.Weekday{rrule.MO, rrule.TU, rrule.WE, rrule.TH, rrule.FR}
	default:
		return nil, errors.New("INVALID_REPEAT")
	}
	rule, err := rrule.NewRRule(options)
	if err != nil {
		return nil, err
	}
	next := rule.Iterator()
	for i := 0; i < 800; i++ {
		candidate, ok := next()
		if !ok {
			return nil, nil
		}
		if candidate.After(at) && EffectiveDue(candidate, quiet).After(now) {
			return &candidate, nil
		}
	}
	return nil, errors.New("REPEAT_HORIZON_EXCEEDED")
}
