import { describe, expect, it } from 'vitest'
import { friendlyCaughtError } from './errors'
import { wailsIPCError } from '../../../lib/testing/wailsIpcError'

// Raw-string inputs exercise the legacy Wails v2 shape (a JSON envelope
// stringified on `.message`): a non-Error caught value is String()-coerced,
// which parses the envelope off the string. Error inputs exercise the v3
// CallError cause envelope — both must reach the same typed-code mapping.
describe('friendlyCaughtError', () => {
  const friendly = 'This task is open in the editor — save or close it first.'

  it('maps on the stable #478 code (resilient to backend wording changes)', () => {
    const err = wailsIPCError({
      code: 'block_being_edited',
      message: 'totally different wording'
    })
    expect(friendlyCaughtError(err)).toBe(friendly)
  })

  it('recovers the code from a legacy v2 JSON-string error', () => {
    expect(
      friendlyCaughtError(
        '{"code":"block_being_edited","message":"totally different wording"}'
      )
    ).toBe(friendly)
  })

  it('still maps the exact sentinel prose (unmigrated return sites)', () => {
    expect(friendlyCaughtError('block is being edited in another view')).toBe(
      friendly
    )
  })

  // A typed envelope's prose is authoritative: it can embed a target path
  // that happens to contain the phrase, so the legacy match must never
  // rewrite a message that already carries a code.
  it('keeps typed recovery prose verbatim even when it contains the legacy phrase', () => {
    const prose =
      'notes on being edited.md could not be written safely and the previous version was restored. Your unsaved text is kept at /vault/.system/writebackups/notes on being edited.md.pending'
    const err = wailsIPCError({
      code: 'verified_write_failed',
      message: prose
    })
    expect(friendlyCaughtError(err)).toBe(prose)
  })

  it('keeps legacy v2 JSON-string typed prose verbatim under the same rule', () => {
    const prose =
      'notes on being edited.md could not be written safely and the previous version was restored. Your unsaved text is kept at /vault/.system/writebackups/notes on being edited.md.pending'
    expect(
      friendlyCaughtError(
        JSON.stringify({ code: 'verified_write_failed', message: prose })
      )
    ).toBe(prose)
  })

  it('passes through unknown codes unchanged (fail-loudly)', () => {
    expect(
      friendlyCaughtError(
        '{"code":"some_unrelated_code","message":"network error"}'
      )
    ).toBe('network error')
  })

  it('passes through non-JSON prose unchanged', () => {
    expect(friendlyCaughtError('network error')).toBe('network error')
  })

  it('String() coerces non-Error values without the sentinel to a passthrough', () => {
    expect(friendlyCaughtError('boom')).toBe('boom')
    expect(friendlyCaughtError(42)).toBe('42')
  })
})
