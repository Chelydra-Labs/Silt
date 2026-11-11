// The priority assigned when a task is created without an explicit value.
// Keep the frontend default aligned with parser.DefaultTaskPriority.
export const DEFAULT_TASK_PRIORITY = 2

// Older task lines omitted the priority token and are intentionally treated as
// the legacy low-priority bucket until the user explicitly changes them.
export const LEGACY_MISSING_TASK_PRIORITY = 3

// One full cycle for the card pill: Low→Normal→High wraps back to Low.
const NEXT_PRIORITY: Record<number, number> = {
  1: 3,
  2: 1,
  3: 2
}

/**
 * Next value when cycling a task's priority from a card pill. Levels are
 * 1=High, 2=Normal, 3=Low (0 = unset): an unset task bootstraps to Low so the
 * first click lands inside the cycle. Out-of-range values (legacy data) count
 * as unset for the same reason — a click must always move the task into the
 * cycle rather than dead-end.
 */
export function nextPriority(current: number): number {
  return NEXT_PRIORITY[current] ?? 3
}
