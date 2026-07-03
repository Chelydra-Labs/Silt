// Package recurrence resolves the natural-language recurrence rules carried
// by the [recur::] Dataview token on task lines (#296).
//
// The grammar is deliberately small and human-readable, matching the v1
// interval vocabulary agreed in PLAN.md (informed by Todoist / Things /
// Obsidian Tasks UX research):
//
//	every day
//	every weekday          (Mon–Fri)
//	every week
//	every N days|weeks|months|years   (N >= 1)
//
// Resolution follows the "next instance on completion" model: given a base
// date (the task's due date, or today if absent) and the rule, NextInstance
// returns the next due date strictly after the base, advancing in whole
// interval steps. If the computed date is still in the past (a task left
// overdue), the resolver skips the missed instances and lands on the next
// future occurrence rather than backfilling — the Todoist default that
// avoids "catch-up hell" (see PLAN.md §1 design decisions).
//
// The package is pure (no I/O, no app state) so it is trivially unit-testable
// and can be called from the UpdateBlockState DONE hook without introducing a
// new dependency surface.
package recurrence

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// maxSkipIterations bounds the skip-missed advancement loop. A pathological
// rule (e.g. "every 1 day" with a base date centuries in the past) must never
// spin forever. 10000 intervals is well past any realistic vault lifetime
// while keeping the loop cheap.
const maxSkipIterations = 10000

// Unit enumerates the calendar units a recurrence rule can advance by.
type Unit int

const (
	UnitDay Unit = iota
	UnitWeek
	UnitMonth
	UnitYear
	UnitWeekday // Mon–Fri only; skips weekends
)

// Rule is a parsed recurrence rule: advance by Interval Units each cycle.
// Interval is always >= 1 (a 0/negative interval is a parse error).
type Rule struct {
	Unit     Unit
	Interval int
}

// ParseRule parses a recurrence string into a typed Rule. The input is
// expected to be already lowercased + whitespace-normalized (scanTaskTokens
// does that), but ParseRule tolerates raw input too. Returns an error for
// unsupported grammar — the caller (UpdateBlockState) treats this as a
// no-op + log rather than blocking the DONE transition.
//
// Supported forms:
//
//	every day
//	every weekday
//	every week
//	every month
//	every year
//	every <N> days
//	every <N> weeks
//	every <N> months
//	every <N> years
func ParseRule(s string) (Rule, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Join(strings.Fields(s), " ") // collapse whitespace defensively
	if s == "" {
		return Rule{}, fmt.Errorf("empty recurrence rule")
	}
	if !strings.HasPrefix(s, "every ") {
		return Rule{}, fmt.Errorf("recurrence rule must start with 'every': %q", s)
	}
	rest := strings.TrimSpace(strings.TrimPrefix(s, "every "))

	// Singular interval-less units.
	switch rest {
	case "day":
		return Rule{Unit: UnitDay, Interval: 1}, nil
	case "weekday":
		return Rule{Unit: UnitWeekday, Interval: 1}, nil
	case "week":
		return Rule{Unit: UnitWeek, Interval: 1}, nil
	case "month":
		return Rule{Unit: UnitMonth, Interval: 1}, nil
	case "year":
		return Rule{Unit: UnitYear, Interval: 1}, nil
	}

	// "<N> <unit>" plural form: split on the first space.
	parts := strings.SplitN(rest, " ", 2)
	if len(parts) != 2 {
		return Rule{}, fmt.Errorf("unsupported recurrence rule: %q", s)
	}
	n, err := strconv.Atoi(parts[0])
	if err != nil || n < 1 {
		return Rule{}, fmt.Errorf("invalid recurrence interval %q in %q: must be a positive integer", parts[0], s)
	}
	switch strings.TrimSpace(parts[1]) {
	case "days":
		return Rule{Unit: UnitDay, Interval: n}, nil
	case "weeks":
		return Rule{Unit: UnitWeek, Interval: n}, nil
	case "months":
		return Rule{Unit: UnitMonth, Interval: n}, nil
	case "years":
		return Rule{Unit: UnitYear, Interval: n}, nil
	default:
		return Rule{}, fmt.Errorf("unsupported recurrence unit %q in %q", parts[1], s)
	}
}

// IsValid reports whether s is a parseable recurrence rule. Used by the UI /
// IPC validation layer to reject malformed rules on save rather than on the
// (much later) completion event.
func IsValid(s string) bool {
	_, err := ParseRule(s)
	return err == nil
}

// addMonths adds n months to from with end-of-month clamping: Jan 31 + 1
// month → Feb 28/29 (not March 3). Go's time.AddDate normalizes overflow
// days, so we clamp the target day to the last day of the target month
// explicitly — matching RFC 5545 / standard task-app behaviour for monthly
// recurrence from a high day-of-month anchor.
func addMonths(from time.Time, n int) time.Time {
	y, m, d := from.Date()
	targetMonth := int(m) - 1 + n // 0-based month for modular arithmetic
	targetYear := y + targetMonth/12
	targetMonth = targetMonth % 12
	if targetMonth < 0 {
		targetMonth += 12
		targetYear--
	}
	// Clamp the day to the last day of the target month.
	lastDay := lastDayOfMonth(targetYear, time.Month(targetMonth+1))
	if d > lastDay {
		d = lastDay
	}
	return time.Date(targetYear, time.Month(targetMonth+1), d, from.Hour(), from.Minute(), from.Second(), from.Nanosecond(), from.Location())
}

// lastDayOfMonth returns the last calendar day (28-31) of the given month.
func lastDayOfMonth(year int, month time.Month) int {
	// First day of the NEXT month, minus one day.
	firstOfNext := time.Date(year, month+1, 1, 0, 0, 0, 0, time.UTC)
	return firstOfNext.AddDate(0, 0, -1).Day()
}

// advanceFromAnchor computes the n-th interval step from anchor, always
// re-deriving from the original base date so month-end anchors (Jan 31) are
// retained across multiple hops. Chaining advanceOnce would drift: Jan 31 →
// Feb 28 → Mar 28 (wrong). Re-deriving gives Jan 31 → Feb 28 → Mar 31 (right)
// because addMonths always clamps from the original day-of-month.
func (r Rule) advanceFromAnchor(anchor time.Time, n int) time.Time {
	switch r.Unit {
	case UnitDay:
		return anchor.AddDate(0, 0, r.Interval*n)
	case UnitWeek:
		return anchor.AddDate(0, 0, 7*r.Interval*n)
	case UnitMonth:
		return addMonths(anchor, r.Interval*n)
	case UnitYear:
		return addMonths(anchor, 12*r.Interval*n)
	case UnitWeekday:
		// Sequential: advance one weekday at a time. Weekday rules have no
		// clamping issue, so chaining is safe. Interval is always 1 today
		// (the grammar accepts only singular `every weekday`); a future
		// plural `every N weekdays` would need N-step-per-cycle semantics.
		next := anchor
		for j := 0; j < n; j++ {
			next = next.AddDate(0, 0, r.Interval)
			for {
				wd := next.Weekday()
				if wd != time.Saturday && wd != time.Sunday {
					break
				}
				next = next.AddDate(0, 0, 1)
			}
		}
		return next
	default:
		return anchor
	}
}

// NextInstance returns the next due date after `from` for this rule. This is
// the raw interval step — it does NOT skip past dates. Callers that need the
// "next strictly-future occurrence" behaviour should use NextFutureInstance.
func (r Rule) NextInstance(from time.Time) time.Time {
	return r.advanceFromAnchor(from, 1)
}

// NextFutureInstance returns the next due date that is strictly after `now`,
// advancing in whole interval steps from `base`. If base + one interval is
// already in the future, that is returned directly. Otherwise the resolver
// skips missed instances (Todoist "skip-missed" model) until it lands on a
// future date, bounded by maxSkipIterations to prevent a pathological loop.
//
// Each step re-derives from the original base (not the previous result) so
// month-end anchors are retained: Jan 31 + 2 months = Mar 31, not Mar 28.
//
// Both base and now are normalised to local midnight (using now's timezone)
// so the "future" boundary matches the user's local calendar, not UTC.
// `base` is the anchor: typically the task's due date. `now` is the reference
// for "future" — pass time.Now() in production and a fixed clock in tests.
func (r Rule) NextFutureInstance(base, now time.Time) time.Time {
	// Normalise to midnight in now's local timezone so day math is not
	// perturbed by clock time and the "today" boundary matches the user's
	// calendar (not UTC midnight, which disagrees near local midnight).
	loc := now.Location()
	base = time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, loc)
	now = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	for i := 1; i <= maxSkipIterations; i++ {
		next := r.advanceFromAnchor(base, i)
		if next.After(now) {
			return next
		}
	}
	// Cap hit: return the furthest computed date as a best-effort fallback.
	// This only happens for pathological cases (base centuries in the past).
	return r.advanceFromAnchor(base, maxSkipIterations)
}

// FormatDate renders a time.Time as the YYYY-MM-DD string used by the
// [due::] token, the SQLite due_date column, and the frontend. Centralised
// here so the resolver is the only place that owns the date format.
func FormatDate(t time.Time) string {
	return t.Format("2006-01-02")
}
