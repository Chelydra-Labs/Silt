/**
 * IPC error-code coercion (#478).
 *
 * The backend ErrorFormatter (main.go) serializes IPCError-carriers
 * (errBlockBeingEdited, errVaultClosing) and CapabilityDeniedError as a JSON
 * STRING on the Wails error envelope — Wails v2's JS runtime wraps every
 * backend error in `new Error(t.error)`, which flattens objects to
 * "[object Object]" but preserves strings on `.message`. So a typed backend
 * error arrives here as `Error` whose `.message` is `'{"code":"...","message":"..."}'`.
 *
 * `coerceIPCError` parses that JSON to recover the stable code; if parsing
 * fails (an unmigrated sentinel whose error is plain prose), it falls back to
 * the raw message. This lets callers map on a machine-readable code instead of
 * substring-matching Go prose, so a backend wording change can't silently
 * regress the friendly mapping.
 */

export interface IPCErrorShape {
  /** Stable machine-readable code (e.g. 'block_being_edited'), or undefined
   *  for plain-prose errors from unmigrated sentinels. */
  code?: string
  /** The human message. Always present. */
  message: string
  /** Additional structured fields a capability denial carries (optional). */
  plugin?: string
  capability?: string
  requested?: string
  granted?: string
  disabled?: boolean
}

/**
 * Coerce a caught Wails error (or any value) into a stable { code?, message }
 * shape. Tries JSON.parse on the Error's `.message`; if it yields an object
 * with a string `code`, returns the parsed shape, else returns the raw
 * message with no code (the pre-contract fallback).
 */
export function coerceIPCError(e: unknown): IPCErrorShape {
  const raw = e instanceof Error ? e.message : String(e)
  return coerceIPCErrorMessage(raw)
}

/**
 * Variant that takes a pre-extracted message string (used by helpers that
 * already pulled `.message` off the caught value). Same parse-or-fallback
 * behavior as coerceIPCError.
 */
export function coerceIPCErrorMessage(raw: string): IPCErrorShape {
  // A backend IPCError serializes as a JSON object string. Cheap probe: only
  // attempt a parse when the string starts with '{' (the formatter never
  // emits leading whitespace). Avoids throwing on every plain-prose error.
  //
  // Assumption: the ErrorFormatter (formatIPCError in ipc_errors.go) is the
  // SOLE producer of a '{'-prefixed error message in this app. A future code
  // path that echoes a raw JSON body in an error string could be misparsed
  // here — but the guard below (require a string `code` field) plus the catch
  // fallback (non-JSON → raw prose) keep that case safe.
  if (raw.length > 0 && raw[0] === '{') {
    try {
      const parsed = JSON.parse(raw)
      if (
        parsed &&
        typeof parsed === 'object' &&
        typeof parsed.code === 'string'
      ) {
        return {
          code: parsed.code,
          message: typeof parsed.message === 'string' ? parsed.message : raw,
          plugin: typeof parsed.plugin === 'string' ? parsed.plugin : undefined,
          capability:
            typeof parsed.capability === 'string'
              ? parsed.capability
              : undefined,
          requested:
            typeof parsed.requested === 'string' ? parsed.requested : undefined,
          granted:
            typeof parsed.granted === 'string' ? parsed.granted : undefined,
          disabled:
            typeof parsed.disabled === 'boolean' ? parsed.disabled : undefined
        }
      }
    } catch {
      // Not JSON — fall through to the plain-prose return.
    }
  }
  return { message: raw }
}
