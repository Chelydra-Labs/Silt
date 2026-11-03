/**
 * Friendly-error mapping for task-setter failures.
 *
 * The backend focus-lock guard (#444) returns the sentinel
 * `errBlockBeingEdited` ("block is being edited in another view") whenever a
 * task setter (Owner/Priority/Tags/Title/Order/DueDate/Meta) targets a file the
 * user is actively editing in the editor surface. Since #478 that error crosses
 * the IPC boundary with a stable `code` field (`block_being_edited`) on the
 * Wails v3 cause envelope; this helper maps on the code first so a backend
 * wording change can't regress the friendly copy, then falls back to the
 * exact sentinel prose for any unmigrated path.
 *
 * Keep the raw message as the final fallback so unknown errors stay
 * diagnosable (fail-loudly: never swallow the underlying detail).
 */
import { coerceIPCError, type IPCErrorShape } from '../../../lib/ipcError'
import { IPCErrorCode } from '../../../generated/enums'

/** Convenience: coerce a caught value to a friendly message string. */
export function friendlyCaughtError(e: unknown): string {
  // The full coercion (not e.message pre-extracted): the typed code lives on
  // the Wails v3 cause envelope, and stripping to .message would drop it.
  return mapTaskError(coerceIPCError(e))
}

function mapTaskError({ code, message }: IPCErrorShape): string {
  // #478: map on the stable code first — resilient to backend prose changes.
  if (code === IPCErrorCode.CodeBlockBeingEdited) {
    return 'This task is open in the editor — save or close it first.'
  }
  // Legacy fallback: a codeless plain-prose error still maps when it IS the
  // exact sentinel prose. Exact match (not substring): recovery prose of a
  // typed error can embed a path containing the phrase, and an unrelated
  // codeless OS error echoing such a path must render verbatim, not as the
  // editor-focus copy. Every focus-lock return site is typed now, so this
  // only catches unmigrated stragglers.
  if (!code && message === 'block is being edited in another view') {
    return 'This task is open in the editor — save or close it first.'
  }
  return message
}
