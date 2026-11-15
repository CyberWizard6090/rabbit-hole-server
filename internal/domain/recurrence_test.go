package domain

import (
	"errors"
	"testing"
	"time"
)

func at(t *testing.T, layout, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(layout, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func utc(t *testing.T, value string) time.Time { return at(t, "2006-01-02 15:04", value) }

func rule(frequency RecurrenceFrequency, interval int, mutate ...func(*Recurrence)) Recurrence {
	r := Recurrence{Frequency: frequency, Interval: interval, Anchor: AnchorDue, Trigger: TriggerCompletion, TimeZone: "UTC", Occurrence: 1}
	for _, m := range mutate {
		m(&r)
	}
	return r
}

func TestNextOccurrence(t *testing.T) {
	cases := []struct {
		name      string
		rule      Recurrence
		base      string // due date of the completed task, empty for none
		completed string
		want      string // empty means the series is over
	}{
		{"daily", rule(RecurDaily, 1), "2026-11-10 09:00", "2026-11-10 12:00", "2026-11-11 09:00"},
		{"every third day", rule(RecurDaily, 3), "2026-11-10 09:00", "2026-11-10 12:00", "2026-11-13 09:00"},
		{"finished early keeps the schedule", rule(RecurDaily, 1), "2026-11-20 09:00", "2026-11-10 12:00", "2026-11-21 09:00"},
		{"late completion skips the missed days", rule(RecurDaily, 1), "2026-11-01 09:00", "2026-11-10 12:00", "2026-11-11 09:00"},
		{"completed on the due day gives tomorrow", rule(RecurDaily, 1), "2026-11-10 23:00", "2026-11-10 23:30", "2026-11-11 23:00"},
		{"no due date counts from completion", rule(RecurDaily, 2), "", "2026-11-10 12:00", "2026-11-12 12:00"},

		{"weekly keeps the weekday", rule(RecurWeekly, 1), "2026-11-10 09:00", "2026-11-10 12:00", "2026-11-17 09:00"},
		{"every other week", rule(RecurWeekly, 2), "2026-11-10 09:00", "2026-11-10 12:00", "2026-11-24 09:00"},
		{"weekly on Monday and Thursday from Monday", rule(RecurWeekly, 1, func(r *Recurrence) { r.Weekdays = []int{1, 4} }), "2026-11-09 09:00", "2026-11-09 12:00", "2026-11-12 09:00"},
		{"weekly on Monday and Thursday from Thursday", rule(RecurWeekly, 1, func(r *Recurrence) { r.Weekdays = []int{4, 1} }), "2026-11-12 09:00", "2026-11-12 12:00", "2026-11-16 09:00"},
		{"every other week on Friday and Sunday from Sunday", rule(RecurWeekly, 2, func(r *Recurrence) { r.Weekdays = []int{5, 7} }), "2026-11-15 09:00", "2026-11-15 12:00", "2026-11-27 09:00"},
		{"weekly from a Sunday goes to next Sunday", rule(RecurWeekly, 1), "2026-11-15 09:00", "2026-11-15 12:00", "2026-11-22 09:00"},

		{"monthly", rule(RecurMonthly, 1), "2026-11-10 09:00", "2026-11-10 12:00", "2026-12-10 09:00"},
		{"monthly clamps to a short month", rule(RecurMonthly, 1, func(r *Recurrence) { r.MonthDay = 31 }), "2026-01-31 09:00", "2026-01-31 12:00", "2026-02-28 09:00"},
		{"a pinned day returns after a short month", rule(RecurMonthly, 1, func(r *Recurrence) { r.MonthDay = 31 }), "2026-02-28 09:00", "2026-02-28 12:00", "2026-03-31 09:00"},
		{"monthly on the last day", rule(RecurMonthly, 1, func(r *Recurrence) { r.MonthDay = -1 }), "2026-11-30 09:00", "2026-11-30 12:00", "2026-12-31 09:00"},
		{"every three months across the year", rule(RecurMonthly, 3), "2026-11-15 09:00", "2026-11-15 12:00", "2027-02-15 09:00"},

		{"yearly", rule(RecurYearly, 1), "2026-11-10 09:00", "2026-11-10 12:00", "2027-11-10 09:00"},
		{"yearly on a leap day", rule(RecurYearly, 1), "2028-02-29 09:00", "2028-02-29 12:00", "2029-02-28 09:00"},

		{"after completion adds the interval to the completion day", rule(RecurDaily, 3, func(r *Recurrence) { r.Anchor = AnchorCompletion }), "2026-11-01 09:00", "2026-11-10 12:00", "2026-11-13 12:00"},
		{"weekly after completion", rule(RecurWeekly, 2, func(r *Recurrence) { r.Anchor = AnchorCompletion }), "", "2026-11-10 12:00", "2026-11-24 12:00"},
		{"monthly after completion clamps", rule(RecurMonthly, 1, func(r *Recurrence) { r.Anchor = AnchorCompletion }), "", "2026-01-31 12:00", "2026-02-28 12:00"},

		{"count reached ends the series", rule(RecurDaily, 1, func(r *Recurrence) { r.Count = 3; r.Occurrence = 3 }), "2026-11-10 09:00", "2026-11-10 12:00", ""},
		{"count not reached", rule(RecurDaily, 1, func(r *Recurrence) { r.Count = 3; r.Occurrence = 2 }), "2026-11-10 09:00", "2026-11-10 12:00", "2026-11-11 09:00"},
		{"until includes its day", rule(RecurDaily, 1, func(r *Recurrence) { r.Until = "2026-11-11" }), "2026-11-10 09:00", "2026-11-10 12:00", "2026-11-11 09:00"},
		{"until excludes later days", rule(RecurDaily, 1, func(r *Recurrence) { r.Until = "2026-11-10" }), "2026-11-10 09:00", "2026-11-10 12:00", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var base *time.Time
			if tc.base != "" {
				b := utc(t, tc.base)
				base = &b
			}
			got, ok := tc.rule.Next(base, utc(t, tc.completed))
			if tc.want == "" {
				if ok {
					t.Fatalf("series continues with %v, want it over", got)
				}
				return
			}
			if !ok || !got.Equal(utc(t, tc.want)) {
				t.Fatalf("next = %v (ok=%v), want %s", got, ok, tc.want)
			}
		})
	}
}

func TestNextKeepsTheWallClockAcrossDaylightSaving(t *testing.T) {
	// Berlin leaves summer time on 2026-10-25: 09:00 stays 09:00 local, which is one hour later in UTC.
	r := rule(RecurDaily, 1, func(r *Recurrence) { r.TimeZone = "Europe/Berlin" })
	base := time.Date(2026, 10, 24, 7, 0, 0, 0, time.UTC) // 09:00 CEST
	got, ok := r.Next(&base, base.Add(time.Hour))
	if want := time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC); !ok || !got.Equal(want) { // 09:00 CET
		t.Fatalf("next = %v, want %v", got, want)
	}
}

func TestDaysAreCountedInTheRuleTimeZone(t *testing.T) {
	// 2026-11-10 23:30 UTC is already 11 November in Tokyo, so a task due that evening UTC counts as done "today" there.
	r := rule(RecurDaily, 1, func(r *Recurrence) { r.TimeZone = "Asia/Tokyo" })
	base := utc(t, "2026-11-10 23:30")
	got, _ := r.Next(&base, utc(t, "2026-11-10 23:45"))
	if want := utc(t, "2026-11-11 23:30"); !got.Equal(want) {
		t.Fatalf("next = %v, want %v", got, want)
	}
}

func TestPinMonthDayKeepsTheDayAcrossShortMonths(t *testing.T) {
	r := rule(RecurMonthly, 1)
	base := utc(t, "2026-01-31 09:00")
	pinned := r.PinMonthDay(&base, base)
	if pinned.MonthDay != 31 {
		t.Fatalf("pinned day = %d, want 31", pinned.MonthDay)
	}
	if again := pinned.PinMonthDay(&base, base); again.MonthDay != 31 {
		t.Fatal("pinning twice changed the day")
	}
	if rule(RecurWeekly, 1).PinMonthDay(&base, base).MonthDay != 0 {
		t.Fatal("only monthly rules get a day")
	}
}

func TestRecurrenceValidation(t *testing.T) {
	good := rule(RecurWeekly, 1, func(r *Recurrence) { r.Weekdays = []int{1, 3} })
	if err := good.Validate(); err != nil {
		t.Fatalf("a good rule was rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*Recurrence)
		field  string
	}{
		{"unknown frequency", func(r *Recurrence) { r.Frequency = "hourly" }, "recurrence.frequency"},
		{"zero interval", func(r *Recurrence) { r.Interval = 0 }, "recurrence.interval"},
		{"huge interval", func(r *Recurrence) { r.Interval = 5000 }, "recurrence.interval"},
		{"unknown anchor", func(r *Recurrence) { r.Anchor = "never" }, "recurrence.anchor"},
		{"weekday zero", func(r *Recurrence) { r.Weekdays = []int{0} }, "recurrence.weekdays"},
		{"weekday eight", func(r *Recurrence) { r.Weekdays = []int{8} }, "recurrence.weekdays"},
		{"duplicate weekday", func(r *Recurrence) { r.Weekdays = []int{2, 2} }, "recurrence.weekdays"},
		{"weekdays on a daily rule", func(r *Recurrence) { r.Frequency = RecurDaily; r.Weekdays = []int{1} }, "recurrence.weekdays"},
		{"weekdays with the completion anchor", func(r *Recurrence) { r.Anchor = AnchorCompletion }, "recurrence.weekdays"},
		{"month day on a weekly rule", func(r *Recurrence) { r.MonthDay = 5 }, "recurrence.month_day"},
		{"unknown zone", func(r *Recurrence) { r.TimeZone = "Mars/Base" }, "recurrence.time_zone"},
		{"local zone", func(r *Recurrence) { r.TimeZone = "Local" }, "recurrence.time_zone"},
		{"bad until", func(r *Recurrence) { r.Until = "tomorrow" }, "recurrence.until"},
		{"unknown trigger", func(r *Recurrence) { r.Trigger = "hourly" }, "recurrence.trigger"},
		{"schedule with the completion anchor", func(r *Recurrence) { r.Trigger = TriggerSchedule; r.Anchor = AnchorCompletion; r.Weekdays = nil }, "recurrence.anchor"},
		{"negative count", func(r *Recurrence) { r.Count = -1 }, "recurrence.count"},
		{"huge count", func(r *Recurrence) { r.Count = 100000 }, "recurrence.count"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := good
			r.Weekdays = append([]int(nil), good.Weekdays...)
			tc.mutate(&r)
			err := r.Validate()
			var fieldErr *FieldError
			if !errors.As(err, &fieldErr) || !errors.Is(err, ErrInvalidRecurrence) {
				t.Fatalf("err = %v, want a field error", err)
			}
			for _, p := range fieldErr.Problems {
				if p.Field == tc.field {
					return
				}
			}
			t.Fatalf("no problem for %s in %v", tc.field, fieldErr.Problems)
		})
	}
}

func TestMonthDayRulesAndNormalize(t *testing.T) {
	for _, day := range []int{1, 15, 31, -1} {
		if err := rule(RecurMonthly, 1, func(r *Recurrence) { r.MonthDay = day }).Validate(); err != nil {
			t.Errorf("month day %d rejected: %v", day, err)
		}
	}
	for _, day := range []int{32, -2} {
		if err := rule(RecurMonthly, 1, func(r *Recurrence) { r.MonthDay = day }).Validate(); err == nil {
			t.Errorf("month day %d accepted", day)
		}
	}
	r := Recurrence{Frequency: RecurWeekly, Weekdays: []int{5, 1}}
	r.Normalize()
	if r.Interval != 1 || r.Anchor != AnchorDue || r.TimeZone != "UTC" || r.Occurrence != 1 || r.Weekdays[0] != 1 {
		t.Fatalf("normalized rule = %+v", r)
	}
}

func TestScheduledRuleNeedsADueDate(t *testing.T) {
	due := time.Date(2030, 1, 7, 9, 0, 0, 0, time.UTC)
	scheduled := Recurrence{Trigger: TriggerSchedule}
	if err := scheduled.RequireDueDate(&due); err != nil {
		t.Fatalf("a scheduled rule with a due date was rejected: %v", err)
	}
	var fieldErr *FieldError
	if err := scheduled.RequireDueDate(nil); !errors.As(err, &fieldErr) || fieldErr.Problems[0].Field != "recurrence.trigger" {
		t.Fatalf("err = %v, want a recurrence.trigger problem", err)
	}
	if err := (Recurrence{Trigger: TriggerCompletion}).RequireDueDate(nil); err != nil {
		t.Fatalf("a completion rule needs no due date: %v", err)
	}
	var legacy Recurrence
	legacy.Normalize()
	if legacy.Trigger != TriggerCompletion || legacy.Scheduled() {
		t.Fatalf("trigger = %q, want the completion default", legacy.Trigger)
	}
}
