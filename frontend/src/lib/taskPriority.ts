// The priority assigned when a task is created without an explicit value.
// Keep the frontend default aligned with parser.DefaultTaskPriority.
export const DEFAULT_TASK_PRIORITY = 2

// Older task lines omitted the priority token and are intentionally treated as
// the legacy low-priority bucket until the user explicitly changes them.
export const LEGACY_MISSING_TASK_PRIORITY = 3
