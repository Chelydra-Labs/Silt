/**
 * Friendly-error mapping for task-setter failures.
 *
 * The backend focus-lock guard (#444) returns the sentinel
 * `errBlockBeingEdited` ("block is being edited in another view") whenever a
 * task setter (Owner/Priority/Tags/Title/Order/DueDate/Meta) targets a file the
 * user is actively editing in the editor surface. That raw string is technical
 * and doesn't tell the user what to DO. This helper maps it (and any other
 * known backend sentinel we grow here) to actionable copy so the shared
 * `<ErrorBanner>` surfaces something a user can act on.
 *
 * Keep the raw message as the fallback so unknown errors stay diagnosable
 * (fail-loudly: never swallow the underlying detail).
 */
export function friendlyTaskError(raw: string): string {
  if (raw.includes('being edited')) {
    return 'This task is open in the editor — save or close it first.'
  }
  return raw
}

/** Convenience: coerce a caught value to a friendly message string. */
export function friendlyCaughtError(e: unknown): string {
  return friendlyTaskError(e instanceof Error ? e.message : String(e))
}
