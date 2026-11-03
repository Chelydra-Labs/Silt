/**
 * Factory for the production Wails v3 wire shape of a typed IPC error, for
 * tests that mock a rejected binding call.
 *
 * The v3 runtime's HTTP bridge rejects a failed call with the 422 CallError
 * body and throws `new Error(json.message)` with the already-parsed cause on
 * `.cause` (@wailsio/runtime runtime.js: `err.cause = json.cause`). For a
 * typed backend error, `.message` is the human prose and the
 * `{"code","message"}` envelope lives on `.cause` — mocking the envelope as
 * a JSON string on `.message` tests a Wails v2 shape production no longer
 * produces (#1019 review).
 *
 * The runtime constructs Error subclasses (RuntimeError/TypeError/
 * ReferenceError) per the CallError kind; for the app's coercion they are all
 * `instanceof Error`, so the factory only sets `name` instead of importing
 * the runtime package into unit tests.
 */

/** A typed IPC envelope as MarshalError (formatIPCError) emits it, including
 *  the structured fields a capability denial carries. */
export interface WailsIPCEnvelope {
  code: string
  message: string
  plugin?: string
  capability?: string
  requested?: string
  granted?: string
  disabled?: boolean
}

/**
 * Construct the error a binding call rejects with in production. `kind`
 * defaults to a plain Error; pass 'RuntimeError'/'TypeError'/'ReferenceError'
 * to mirror what the runtime constructs for those CallError kinds.
 */
export function wailsIPCError(
  envelope: WailsIPCEnvelope,
  kind: 'Error' | 'RuntimeError' | 'TypeError' | 'ReferenceError' = 'Error'
): Error {
  const err = new Error(envelope.message)
  err.name = kind
  return Object.assign(err, { cause: envelope })
}

/**
 * Construct the multi-error-join shape: Wails joins several error returns
 * into one CallError whose cause is an ARRAY of envelopes.
 */
export function wailsIPCErrorJoin(envelopes: WailsIPCEnvelope[]): Error {
  const message = envelopes.map((e) => e.message).join('\n')
  return Object.assign(new Error(message), { cause: envelopes })
}
