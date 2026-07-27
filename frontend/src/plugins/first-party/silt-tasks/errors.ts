/**
 * Friendly-error mapping for task-setter failures.
 *
 * The backend focus-lock guard (#444) returns the sentinel
 * `errBlockBeingEdited` ("block is being edited in another view") whenever a
 * task setter (Owner/Priority/Tags/Title/Order/DueDate/Meta) targets a file the
 * user is actively editing in the editor surface. Since #478 that error crosses
 * the IPC boundary with a stable `code` field (`block_being_edited`) that
 * survives Wails' error serialization; this helper maps on the code first so a
 * backend wording change can't regress the friendly copy, then falls back to
 * the legacy substring match for any unmigrated path.
 *
 * Keep the raw message as the final fallback so unknown errors stay
 * diagnosable (fail-loudly: never swallow the underlying detail).
 */
import { coerceIPCErrorMessage } from '../../../lib/ipcError'
import { IPCErrorCode } from '../../../generated/enums'

export function friendlyTaskError(raw: string): string {
  const { code, message } = coerceIPCErrorMessage(raw)
  // #478: map on the stable code first — resilient to backend prose changes.
  if (code === IPCErrorCode.CodeBlockBeingEdited) {
    return 'This task is open in the editor — save or close it first.'
  }
  // Legacy fallback: until every focus-lock return site carries the code, a
  // plain-prose error still matches on the substring. Safe to remove once the
  // last unmigrated sentinel adopts the contract.
  if (message.includes('being edited')) {
    return 'This task is open in the editor — save or close it first.'
  }
  return message
}

/** Convenience: coerce a caught value to a friendly message string. */
export function friendlyCaughtError(e: unknown): string {
  return friendlyTaskError(e instanceof Error ? e.message : String(e))
}
