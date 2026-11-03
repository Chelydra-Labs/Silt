/**
 * IPC error-code coercion (#478).
 *
 * Wire shape (Wails v3, since the 2026-07 migration): the backend's
 * MarshalError (formatIPCError in ipc_errors.go) supplies the
 * `{"code","message"}` envelope for a typed error, and the v3 runtime's HTTP
 * bridge rejects a failed binding call with the 422 CallError body
 * `{"message": <prose>, "cause": <envelope>, "kind": "RuntimeError"}`. The
 * runtime then throws `new Error(json.message)` with the already-parsed cause
 * attached on `.cause` — so `.message` carries only the human prose and the
 * machine-readable code lives on `.cause` (a single envelope object, or an
 * ARRAY of envelopes when Wails joined multiple error returns).
 *
 * `coerceIPCError` reads `.cause` first. If it carries no typed code (a
 * plain-prose error, or a foreign error shape), it falls back to parsing a
 * JSON envelope off `.message` — the Wails v2 shape this library was born
 * under (v2's `new Error(t.error)` preserved a JSON string on `.message`),
 * kept for backward compatibility. If neither yields a code, the raw prose is
 * returned so callers can fall back to their unmigrated-sentinel handling.
 * This lets callers map on a machine-readable code instead of
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
 * shape. Reads the Wails v3 CallError cause first (see the file comment);
 * falls back to JSON.parse on the Error's `.message` (Wails v2 legacy), then
 * to the raw message (the pre-contract fallback).
 */
export function coerceIPCError(e: unknown): IPCErrorShape {
  if (e instanceof Error) {
    const fromCause = coerceIPCCause(e.cause, e.message)
    if (fromCause) {
      return fromCause
    }
    return coerceIPCErrorMessage(e.message)
  }
  return coerceIPCErrorMessage(String(e))
}

/**
 * Coerce the already-parsed `cause` of a Wails v3 runtime error: the
 * `{"code","message"}` envelope for a typed backend error, or an array of
 * envelopes when Wails joined multiple error returns (the first typed entry
 * wins). Returns undefined when the cause carries no string `code` (plain
 * prose, or an unrelated error cause) so the caller falls through to the
 * legacy `.message` path.
 */
function coerceIPCCause(
  cause: unknown,
  fallbackMessage: string
): IPCErrorShape | undefined {
  const candidates: unknown[] = Array.isArray(cause) ? cause : [cause]
  for (const candidate of candidates) {
    if (!isIPCEnvelope(candidate)) {
      continue
    }
    return {
      code: candidate.code,
      message:
        typeof candidate.message === 'string' && candidate.message !== ''
          ? candidate.message
          : fallbackMessage,
      plugin:
        typeof candidate.plugin === 'string' ? candidate.plugin : undefined,
      capability:
        typeof candidate.capability === 'string'
          ? candidate.capability
          : undefined,
      requested:
        typeof candidate.requested === 'string'
          ? candidate.requested
          : undefined,
      granted:
        typeof candidate.granted === 'string' ? candidate.granted : undefined,
      disabled:
        typeof candidate.disabled === 'boolean' ? candidate.disabled : undefined
    }
  }
  return undefined
}

/** Type guard for a parsed IPC envelope: an object carrying a string code. */
function isIPCEnvelope(value: unknown): value is {
  code: string
  message?: unknown
  plugin?: unknown
  capability?: unknown
  requested?: unknown
  granted?: unknown
  disabled?: unknown
} {
  return (
    !!value &&
    typeof value === 'object' &&
    typeof (value as { code?: unknown }).code === 'string'
  )
}

// coerceIPCErrorMessage is the internal string-parse half of coerceIPCError:
// it parses a Wails v2-era JSON envelope off a raw message string, falling
// back to plain prose. Kept unexported so callers cannot bypass the v3 cause
// check that coerceIPCError always runs first.
function coerceIPCErrorMessage(raw: string): IPCErrorShape {
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
