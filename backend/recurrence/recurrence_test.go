package recurrence

import (
	"testing"
	"time"
)

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("bad test date %q: %v", s, err)
	}
	return d
}

func TestParseRule(t *testing.T) {
	tests := []struct {
		input   string
		want    Rule
		wantErr bool
	}{
		{"every day", Rule{UnitDay, 1}, false},
		{"every weekday", Rule{UnitWeekday, 1}, false},
		{"every week", Rule{UnitWeek, 1}, false},
		{"every month", Rule{UnitMonth, 1}, false},
		{"every year", Rule{UnitYear, 1}, false},
		{"every 2 weeks", Rule{UnitWeek, 2}, false},
		{"every 3 months", Rule{UnitMonth, 3}, false},
		{"every 1 days", Rule{UnitDay, 1}, false},
		// Whitespace/case tolerated even though scanTaskTokens pre-normalizes.
		{"  Every  WEEK  ", Rule{UnitWeek, 1}, false},
		// Errors.
		{"", Rule{}, true},
		{"weekly", Rule{}, true},
		{"every", Rule{}, true},
		{"every fortnight", Rule{}, true},
		{"every 0 weeks", Rule{}, true},
		{"every -1 days", Rule{}, true},
		{"every abc weeks", Rule{}, true},
		{"every 2", Rule{}, true},
		{"every 2 hours", Rule{}, true},
	}
	for _, tc := range tests {
		got, err := ParseRule(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseRule(%q): expected error, got %+v", tc.input, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseRule(%q): unexpected error: %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseRule(%q): got %+v, want %+v", tc.input, got, tc.want)
		}
	}
}

func TestIsValid(t *testing.T) {
	valid := []string{"every day", "every week", "every 2 months", "every weekday"}
	invalid := []string{"", "weekly", "every", "every 0 days", "every fortnight"}
	for _, s := range valid {
		if !IsValid(s) {
			t.Errorf("IsValid(%q) = false, want true", s)
		}
	}
	for _, s := range invalid {
		if IsValid(s) {
			t.Errorf("IsValid(%q) = true, want false", s)
		}
	}
}

func TestNextInstance(t *testing.T) {
	jan1 := mustDate(t, "2026-01-01") // a Thursday
	tests := []struct {
		name string
		rule Rule
		from time.Time
		want string
	}{
		{"every day", Rule{UnitDay, 1}, jan1, "2026-01-02"},
		{"every 3 days", Rule{UnitDay, 3}, jan1, "2026-01-04"},
		{"every week", Rule{UnitWeek, 1}, jan1, "2026-01-08"},
		{"every 2 weeks", Rule{UnitWeek, 2}, jan1, "2026-01-15"},
		{"every month", Rule{UnitMonth, 1}, jan1, "2026-02-01"},
		{"every 3 months", Rule{UnitMonth, 3}, jan1, "2026-04-01"},
		{"every year", Rule{UnitYear, 1}, jan1, "2027-01-01"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatDate(tc.rule.NextInstance(tc.from))
			if got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestNextInstance_MonthEndClamping(t *testing.T) {
	// RFC 5545 "skip" behaviour: Jan 31 + 1 month → Feb 28 (non-leap).
	// time.AddDate normalizes the day down, which is the behaviour we want.
	jan31 := mustDate(t, "2026-01-31") // 2026 is not a leap year
	r := Rule{UnitMonth, 1}
	got := FormatDate(r.NextInstance(jan31))
	if got != "2026-02-28" {
		t.Errorf("Jan 31 + 1 month: got %s, want 2026-02-28 (clamped)", got)
	}
	// Leap year: Jan 31 2024 + 1 month → Feb 29.
	jan31_2024 := mustDate(t, "2024-01-31")
	got = FormatDate(r.NextInstance(jan31_2024))
	if got != "2024-02-29" {
		t.Errorf("Jan 31 2024 (leap) + 1 month: got %s, want 2024-02-29", got)
	}
	// May 31 + 1 month → Jun 30 (June has 30 days).
	may31 := mustDate(t, "2026-05-31")
	got = FormatDate(r.NextInstance(may31))
	if got != "2026-06-30" {
		t.Errorf("May 31 + 1 month: got %s, want 2026-06-30", got)
	}
}

func TestNextInstance_LeapYearFeb29(t *testing.T) {
	// Feb 29 2024 (leap) + 1 year → Feb 28 2025 (non-leap).
	feb29 := mustDate(t, "2024-02-29")
	r := Rule{UnitYear, 1}
	got := FormatDate(r.NextInstance(feb29))
	if got != "2025-02-28" {
		t.Errorf("Feb 29 2024 + 1 year: got %s, want 2025-02-28 (clamped)", got)
	}
}

func TestNextInstance_WeekdaySkipsWeekend(t *testing.T) {
	// Friday Jan 2 2026 → next instance should be Mon Jan 5 (skips Sat/Sun).
	fri := mustDate(t, "2026-01-02")
	r := Rule{UnitWeekday, 1}
	got := r.NextInstance(fri)
	gotStr := FormatDate(got)
	if gotStr != "2026-01-05" {
		t.Errorf("Fri + every weekday: got %s, want 2026-01-05 (Mon)", gotStr)
	}
	if wd := got.Weekday(); wd == time.Saturday || wd == time.Sunday {
		t.Errorf("every weekday landed on weekend: %s (%s)", gotStr, wd)
	}
}

func TestNextFutureInstance_AlreadyFuture(t *testing.T) {
	// Base + interval is already in the future → return it directly.
	base := mustDate(t, "2026-07-15")
	now := mustDate(t, "2026-07-02")
	r := Rule{UnitWeek, 1}
	got := FormatDate(r.NextFutureInstance(base, now))
	if got != "2026-07-22" {
		t.Errorf("got %s, want 2026-07-22", got)
	}
}

func TestNextFutureInstance_SkipsMissed(t *testing.T) {
	// A weekly task last due a month ago: completing today should skip the
	// 4 missed weeks and land on the next future occurrence, NOT backfill.
	base := mustDate(t, "2026-06-01") // ~5 weeks before now
	now := mustDate(t, "2026-07-02")
	r := Rule{UnitWeek, 1}
	got := r.NextFutureInstance(base, now)
	gotStr := FormatDate(got)
	if !got.After(now) {
		t.Errorf("skip-missed result %s is not after now %s", gotStr, FormatDate(now))
	}
	// Should be the first multiple-of-7-days from base that exceeds now.
	// base 2026-06-01, +1w=06-08, +2w=06-15, +3w=06-22, +4w=06-29, +5w=07-06.
	if gotStr != "2026-07-06" {
		t.Errorf("skip-missed: got %s, want 2026-07-06 (first future occurrence)", gotStr)
	}
}

func TestNextFutureInstance_DailyFarPast(t *testing.T) {
	// A daily task a year overdue must not spin forever; it lands on the
	// day after now.
	base := mustDate(t, "2025-01-01")
	now := mustDate(t, "2026-07-02")
	r := Rule{UnitDay, 1}
	got := FormatDate(r.NextFutureInstance(base, now))
	if got != "2026-07-03" {
		t.Errorf("daily far-past: got %s, want 2026-07-03", got)
	}
}

func TestNextFutureInstance_WeekdaySkipsMissedWeekends(t *testing.T) {
	// An every-weekday task overdue by one weekend: base Friday, now the
	// following Monday. The missed Sat/Sun are skipped; result is the next
	// future weekday (Tuesday, since Monday == now is not strictly after).
	base := mustDate(t, "2026-06-26") // Friday
	now := mustDate(t, "2026-06-29")  // Monday
	r := Rule{UnitWeekday, 1}
	got := r.NextFutureInstance(base, now)
	gotStr := FormatDate(got)
	// base+1 weekday skip-weekend = Mon 06-29, which == now (not after), so
	// advance once more → Tue 06-30.
	if gotStr != "2026-06-30" {
		t.Errorf("weekday skip-missed: got %s, want 2026-06-30 (Tue)", gotStr)
	}
}

func TestNextFutureInstance_MonthEndAnchorSkipMissed(t *testing.T) {
	// A monthly task on Jan 31 that's 3 months overdue: skip-missed should
	// land on the next future occurrence, retaining the 31st anchor.
	jan31 := mustDate(t, "2026-01-31")
	now := mustDate(t, "2026-04-15")
	r := Rule{UnitMonth, 1}
	got := FormatDate(r.NextFutureInstance(jan31, now))
	// Jan 31 + 4 months = Apr 30 (clamped from 31). Apr 30 > Apr 15 = future.
	if got != "2026-04-30" {
		t.Errorf("month-end skip-missed: got %s, want 2026-04-30 (anchor 31 retained, clamped to Apr 30)", got)
	}
}

func TestNextFutureInstance_CapFallback(t *testing.T) {
	// A daily task anchored centuries in the past hits the iteration cap.
	// The result must still be deterministic (the furthest computed date).
	base := mustDate(t, "1800-01-01")
	now := mustDate(t, "2026-07-02")
	r := Rule{UnitDay, 1}
	got := r.NextFutureInstance(base, now)
	// Should land roughly maxSkipIterations days after base — not necessarily
	// in the future, but deterministic and non-panicking.
	if got.Before(base) {
		t.Errorf("cap fallback returned a date before base: %s", FormatDate(got))
	}
}
