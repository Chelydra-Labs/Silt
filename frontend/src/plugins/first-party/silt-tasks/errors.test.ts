import { describe, expect, it } from 'vitest'
import { friendlyCaughtError, friendlyTaskError } from './errors'
import { wailsIPCError } from '../../../lib/testing/wailsIpcError'

describe('friendlyTaskError', () => {
  const friendly = 'This task is open in the editor — save or close it first.'

  it('maps on the stable #478 code (resilient to backend wording changes)', () => {
    // The legacy Wails v2 string shape; the friendly mapper must recover the
    // code regardless of the human wording.
    expect(
      friendlyTaskError(
        '{"code":"block_being_edited","message":"totally different wording"}'
      )
    ).toBe(friendly)
  })

  it('still maps the exact sentinel prose (unmigrated return sites)', () => {
    expect(friendlyTaskError('block is being edited in another view')).toBe(
      friendly
    )
  })

  // A typed envelope's prose is authoritative: it can embed a target path
  // that happens to contain the phrase, so the legacy match must never
  // rewrite a message that already carries a code.
  it('keeps typed prose verbatim even when it contains the legacy phrase', () => {
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

  it('reads the typed code off the Wails v3 cause envelope', () => {
    const err = wailsIPCError({
      code: 'block_being_edited',
      message: 'totally different wording'
    })
    expect(friendlyCaughtError(err)).toBe(friendly)
  })

  it('keeps typed recovery prose verbatim even when it contains the legacy phrase', () => {
    const prose =
      'notes on being edited.md could not be written safely and the previous version was restored. Your unsaved text is kept at /vault/.system/writebackups/notes on being edited.md.pending'
    const err = wailsIPCError({
      code: 'verified_write_failed',
      message: prose
    })
    expect(friendlyCaughtError(err)).toBe(prose)
  })

  it('String() coerces non-Error values before mapping (exact prose still maps)', () => {
    expect(friendlyCaughtError('block is being edited in another view')).toBe(
      friendly
    )
  })

  it('String() coerces non-Error values without the sentinel to a passthrough', () => {
    expect(friendlyCaughtError('boom')).toBe('boom')
    expect(friendlyCaughtError(42)).toBe('42')
  })
})
