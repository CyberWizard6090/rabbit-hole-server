package domain

import (
	"errors"
	"sort"
	"time"
	_ "time/tzdata" // zone names must resolve on images without a system zoneinfo
)

var ErrInvalidRecurrence = errors.New("recurrence is invalid")

type RecurrenceFrequency string

const (
	RecurDaily   RecurrenceFrequency = "daily"
	RecurWeekly  RecurrenceFrequency = "weekly"
	RecurMonthly RecurrenceFrequency = "monthly"
	RecurYearly  RecurrenceFrequency = "yearly"
)

// RecurrenceAnchor says what the next date is counted from.
type RecurrenceAnchor string

const (
	// AnchorDue counts from the due date of the completed task (from its start date or the completion moment when it
	// has none), so a weekly task stays on its weekday however late it is finished. Occurrences that are already in
	// the past are skipped.
	AnchorDue RecurrenceAnchor = "due"
	// AnchorCompletion counts from the moment the task is completed ("two weeks after I am done").
	AnchorCompletion RecurrenceAnchor = "completion"
)

// RecurrenceTrigger says what creates the next task.
type RecurrenceTrigger string

const (
	// TriggerCompletion creates the next task when the current one is completed (the default).
	TriggerCompletion RecurrenceTrigger = "completion"
	// TriggerSchedule creates the next task when the due date of the current one passes, whether it is done or not, so
	// a "every Monday" task appears on time even if nobody finished the last one.
	TriggerSchedule RecurrenceTrigger = "schedule"
)

const (
	MaxRecurrenceInterval = 365
	MaxRecurrenceCount    = 1000
	recurrenceDateLayout  = "2006-01-02"
	// maxRecurrenceSteps bounds the search for the first occurrence after the completion date.
	maxRecurrenceSteps = 100000
)

// Recurrence is the rule of a repeating task. It lives on the task that is still to be done: completing that task
// creates the next one with the next dates and moves the rule to it.
type Recurrence struct {
	Frequency RecurrenceFrequency `json:"frequency"`
	// Interval counts frequency units: 2 with weekly is every other week.
	Interval int `json:"interval"`
	// Weekdays are the days of a weekly rule, 1 Monday ... 7 Sunday; empty means the weekday of the due date.
	Weekdays []int `json:"weekdays,omitempty"`
	// MonthDay is the day of a monthly rule, 1..31 or -1 for the last day; empty means the day of the due date.
	// A month without that day uses its last day.
	MonthDay int              `json:"month_day,omitempty"`
	Anchor   RecurrenceAnchor `json:"anchor"`
	// Trigger is what creates the next task; rules saved before it existed count as TriggerCompletion.
	Trigger RecurrenceTrigger `json:"trigger"`
	// TimeZone is the IANA zone in which days are counted; the time of day of the due date is kept in it.
	TimeZone string `json:"time_zone"`
	// Until is the last day (YYYY-MM-DD in TimeZone) of the series; empty means no end.
	Until string `json:"until,omitempty"`
	// Count is the number of tasks in the series including the first; 0 means no limit.
	Count int `json:"count,omitempty"`
	// Occurrence is the position of the task in the series (1 for the first). The server keeps it.
	Occurrence int `json:"occurrence"`
}

// Normalize fills the defaults and puts the weekdays in order. It does not validate.
func (r *Recurrence) Normalize() {
	if r.Interval == 0 {
		r.Interval = 1
	}
	if r.Anchor == "" {
		r.Anchor = AnchorDue
	}
	if r.Trigger == "" {
		r.Trigger = TriggerCompletion
	}
	if r.TimeZone == "" {
		r.TimeZone = "UTC"
	}
	if r.Occurrence < 1 {
		r.Occurrence = 1
	}
	if len(r.Weekdays) > 0 {
		sort.Ints(r.Weekdays)
	}
}

// Validate reports every problem of a user-supplied rule as a field problem under "recurrence.".
func (r Recurrence) Validate() error {
	var problems []FieldProblem
	add := func(field, reason string, params map[string]any) {
		problems = append(problems, FieldProblem{Field: "recurrence." + field, Reason: reason, Params: params})
	}
	switch r.Frequency {
	case RecurDaily, RecurWeekly, RecurMonthly, RecurYearly:
	default:
		add("frequency", "INVALID_VALUE", map[string]any{"allowed": []string{"daily", "weekly", "monthly", "yearly"}})
	}
	if r.Interval < 1 || r.Interval > MaxRecurrenceInterval {
		add("interval", "OUT_OF_RANGE", map[string]any{"min": 1, "max": MaxRecurrenceInterval})
	}
	switch r.Anchor {
	case AnchorDue, AnchorCompletion:
	default:
		add("anchor", "INVALID_VALUE", map[string]any{"allowed": []string{"due", "completion"}})
	}
	switch r.Trigger {
	case TriggerCompletion:
	case TriggerSchedule:
		if r.Anchor == AnchorCompletion {
			add("anchor", "NOT_WITH_SCHEDULE_TRIGGER", nil)
		}
	default:
		add("trigger", "INVALID_VALUE", map[string]any{"allowed": []string{"completion", "schedule"}})
	}
	if len(r.Weekdays) > 0 {
		seen := map[int]bool{}
		switch {
		case r.Frequency != RecurWeekly:
			add("weekdays", "ONLY_WITH_WEEKLY", nil)
		case r.Anchor == AnchorCompletion:
			add("weekdays", "NOT_WITH_COMPLETION_ANCHOR", nil)
		default:
			for _, day := range r.Weekdays {
				if day < 1 || day > 7 || seen[day] {
					add("weekdays", "INVALID_VALUE", map[string]any{"min": 1, "max": 7})
					break
				}
				seen[day] = true
			}
		}
	}
	if r.MonthDay != 0 {
		switch {
		case r.Frequency != RecurMonthly:
			add("month_day", "ONLY_WITH_MONTHLY", nil)
		case r.Anchor == AnchorCompletion:
			add("month_day", "NOT_WITH_COMPLETION_ANCHOR", nil)
		case r.MonthDay != -1 && (r.MonthDay < 1 || r.MonthDay > 31):
			add("month_day", "OUT_OF_RANGE", map[string]any{"min": 1, "max": 31, "last": -1})
		}
	}
	if _, err := time.LoadLocation(r.TimeZone); err != nil || r.TimeZone == "Local" {
		add("time_zone", "INVALID_VALUE", nil)
	}
	if r.Until != "" {
		if _, err := time.Parse(recurrenceDateLayout, r.Until); err != nil {
			add("until", "INVALID_VALUE", map[string]any{"format": recurrenceDateLayout})
		}
	}
	if r.Count < 0 || r.Count > MaxRecurrenceCount {
		add("count", "OUT_OF_RANGE", map[string]any{"min": 1, "max": MaxRecurrenceCount})
	}
	if len(problems) == 0 {
		return nil
	}
	return InvalidFields(ErrInvalidRecurrence, problems)
}

// Scheduled reports whether the rule creates the next task by the calendar instead of at completion.
func (r Recurrence) Scheduled() bool { return r.Trigger == TriggerSchedule }

// RequireDueDate rejects a scheduled rule on a task without a due date: the due date is what the schedule counts from.
func (r Recurrence) RequireDueDate(due *time.Time) error {
	if !r.Scheduled() || due != nil {
		return nil
	}
	return InvalidFields(ErrInvalidRecurrence, []FieldProblem{{Field: "recurrence.trigger", Reason: "REQUIRES_DUE_DATE"}})
}

// Next returns the due date of the task that follows one completed at completedAt, and false when the series is over
// (count reached, the date is after Until, or no date can be found). base is the date the next one is counted from for
// AnchorDue: the due date of the completed task, or its start date, or nil.
func (r Recurrence) Next(base *time.Time, completedAt time.Time) (time.Time, bool) {
	if r.Count > 0 && r.Occurrence >= r.Count {
		return time.Time{}, false
	}
	loc, err := time.LoadLocation(r.TimeZone)
	if err != nil {
		return time.Time{}, false
	}
	interval := r.Interval
	if interval < 1 {
		interval = 1
	}
	done := completedAt.In(loc)
	anchor := done
	if r.Anchor != AnchorCompletion && base != nil {
		anchor = base.In(loc)
	}

	step := r.stepper(anchor, loc, interval)
	next := step(anchor)
	if r.Anchor != AnchorCompletion {
		// Skip the occurrences that fell before the day the task was completed.
		for i := 0; !dayAfter(next, done); i++ {
			if i >= maxRecurrenceSteps {
				return time.Time{}, false
			}
			next = step(next)
		}
	}
	if r.Until != "" {
		until, err := time.ParseInLocation(recurrenceDateLayout, r.Until, loc)
		if err == nil && dayAfter(next, until) {
			return time.Time{}, false
		}
	}
	return next, true
}

// PinMonthDay fixes the day of a monthly rule that had none to the day it is counted from, so a task due on the 31st
// does not drift to the 30th after a short month. The service stores the result in the rule of the next task.
func (r Recurrence) PinMonthDay(base *time.Time, completedAt time.Time) Recurrence {
	if r.Frequency != RecurMonthly || r.MonthDay != 0 || r.Anchor == AnchorCompletion {
		return r
	}
	loc, err := time.LoadLocation(r.TimeZone)
	if err != nil {
		return r
	}
	day := completedAt.In(loc).Day()
	if base != nil {
		day = base.In(loc).Day()
	}
	r.MonthDay = day
	return r
}

func dayAfter(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	if ay != by {
		return ay > by
	}
	if am != bm {
		return am > bm
	}
	return ad > bd
}

func isoWeekday(t time.Time) int {
	if t.Weekday() == time.Sunday {
		return 7
	}
	return int(t.Weekday())
}

func (r Recurrence) stepper(anchor time.Time, loc *time.Location, interval int) func(time.Time) time.Time {
	hour, minute, second := anchor.Clock()
	at := func(year int, month time.Month, day int) time.Time {
		return time.Date(year, month, day, hour, minute, second, anchor.Nanosecond(), loc)
	}
	daysIn := func(year int, month time.Month) int {
		return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	}
	completion := r.Anchor == AnchorCompletion

	switch r.Frequency {
	case RecurWeekly:
		days := r.Weekdays
		if len(days) == 0 || completion {
			days = []int{isoWeekday(anchor)}
		}
		days = append([]int(nil), days...)
		sort.Ints(days)
		if completion {
			return func(from time.Time) time.Time {
				y, m, d := from.Date()
				return at(y, m, d+7*interval)
			}
		}
		return func(from time.Time) time.Time {
			y, m, d := from.Date()
			current := isoWeekday(from)
			for _, day := range days {
				if day > current {
					return at(y, m, d+day-current)
				}
			}
			// The next period starts on the Monday `interval` weeks after this week's Monday.
			return at(y, m, d-(current-1)+7*interval+days[0]-1)
		}
	case RecurMonthly:
		monthDay := r.MonthDay
		if monthDay == 0 || completion {
			monthDay = anchor.Day()
		}
		return func(from time.Time) time.Time {
			y, m, _ := from.Date()
			d := monthDay
			first := time.Date(y, m+time.Month(interval), 1, 0, 0, 0, 0, time.UTC)
			ty, tm := first.Year(), first.Month()
			last := daysIn(ty, tm)
			switch {
			case d == -1 || d > last:
				d = last
			case d < 1:
				d = 1
			}
			return at(ty, tm, d)
		}
	case RecurYearly:
		return func(from time.Time) time.Time {
			y, m, d := from.Date()
			ty := y + interval
			if last := daysIn(ty, m); d > last {
				d = last
			}
			return at(ty, m, d)
		}
	default:
		return func(from time.Time) time.Time {
			y, m, d := from.Date()
			return at(y, m, d+interval)
		}
	}
}
