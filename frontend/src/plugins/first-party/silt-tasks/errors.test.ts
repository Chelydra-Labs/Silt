import { describe, expect, it } from 'vitest'
import { friendlyCaughtError, friendlyTaskError } from './errors'

describe('friendlyTaskError', () => {
  const friendly = 'This task is open in the editor — save or close it first.'

  it('maps on the stable #478 code (resilient to backend wording changes)', () => {
    // The backend ErrorFormatter serializes an IPCError as a JSON string on
    // .message; the friendly mapper must recover the code regardless of the
    // human wording.
    expect(
      friendlyTaskError(
        '{"code":"block_being_edited","message":"totally different wording"}'
      )
    ).toBe(friendly)
  })

  it('still maps the legacy substring (unmigrated return sites)', () => {
    expect(friendlyTaskError('block is being edited in another view')).toBe(
      friendly
    )
  })

  // A typed envelope's prose is authoritative: it can embed a target path
  // that happens to contain the phrase, so the legacy substring must never
  // rewrite a message that already carries a code.
  it('keeps typed prose verbatim even when it contains the legacy substring', () => {
    const prose =
      'notes on being edited.md could not be written safely and the previous version was restored. Your unsaved text is kept at /vault/.system/writebackups/notes on being edited.md.pending'
    expect(
      friendlyTaskError(
        JSON.stringify({ code: 'verified_write_failed', message: prose })
      )
    ).toBe(prose)
  })

  it('passes through unknown codes unchanged (fail-loudly)', () => {
    expect(
      friendlyTaskError(
        '{"code":"some_unrelated_code","message":"network error"}'
      )
    ).toBe('network error')
  })

  it('passes through non-JSON prose unchanged', () => {
    expect(friendlyTaskError('network error')).toBe('network error')
  })
})

describe('friendlyCaughtError', () => {
  const friendly = 'This task is open in the editor — save or close it first.'

  it('reads .message off an Error and runs it through the mapper (code path)', () => {
    const err = new Error('{"code":"block_being_edited","message":"anything"}')
    expect(friendlyCaughtError(err)).toBe(friendly)
  })

  it('String() coerces non-Error values before mapping (substring still maps)', () => {
    expect(friendlyCaughtError('block is being edited')).toBe(friendly)
  })

  it('String() coerces non-Error values without the substring to a passthrough', () => {
    expect(friendlyCaughtError('boom')).toBe('boom')
    expect(friendlyCaughtError(42)).toBe('42')
  })
})
