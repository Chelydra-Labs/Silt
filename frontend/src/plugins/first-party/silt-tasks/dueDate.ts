/**
 * Due-date urgency classification shared across the Tasks hub surfaces.
 *
 * #457: List/Board/Calendar previously rendered due dates as uniformly muted
 * text; section *headers* got `text-error` for overdue groups but individual
 * rows/cards/chips did not. This helper is the single source of truth for the
 * four-tier urgency signal so every surface agrees.
 *
 * Comparison is a pure lexicographic compare on `YYYY-MM-DD` strings. This is
 * safe because both operands are date-only strings in the user's LOCAL
 * timezone: `today` is derived from `new Date().toISOString().slice(0,10)` and
 * task due dates are stored date-only with no time component, so no timezone
 * offset can flip the comparison. (Matches the existing inline derivation in
 * `ListView.svelte` for the overdue group.)
 *
 * Keep consistent with the SQL `CASE WHEN t.due_date < ?` overdue count in
 * `Sidebar.svelte` and the `overdueSurfaced` derivation in `CalendarView.svelte`
 * — those are the same rule expressed in SQL / set form.
 *
 * NOTE: this helper is intentionally DATE-ONLY. The Sidebar's overdue COUNT
 * additionally excludes DONE tasks (`status != 'DONE'`) because a completed
 * task is no longer "on fire". Callers that color a DONE task's date should
 * suppress the overdue/today tones (render muted) to match that semantics —
 * see the ListView/BoardView call sites in #457.
 */
export type DueDateClass = 'overdue' | 'today' | 'upcoming' | 'none'

export function dueDateClass(
  iso: string | null | undefined,
  today: string
): DueDateClass {
  if (!iso || !iso.trim()) return 'none'
  if (iso < today) return 'overdue'
  if (iso === today) return 'today'
  return 'upcoming'
}

/**
 * Tailwind text-color class for a due date, resolved from `dueDateClass`.
 * Overdue → error tone, today → accent, upcoming/none → muted. Used inline on
 * the date span so the surrounding row/card layout is untouched.
 */
export function dueDateTextClass(cls: DueDateClass): string {
  switch (cls) {
    case 'overdue':
      return 'text-error'
    case 'today':
      return 'text-accent-primary-start'
    default:
      return 'text-text-muted'
  }
}
