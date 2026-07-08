import { describe, expect, it } from 'vitest'
import { friendlyCaughtError, friendlyTaskError } from './errors'

describe('friendlyTaskError', () => {
  it('maps the backend "being edited" sentinel to actionable copy', () => {
    const friendly = 'This task is open in the editor — save or close it first.'
    expect(friendlyTaskError('block is being edited in another view')).toBe(
      friendly
    )
  })

  it('passes through unknown errors unchanged (fail-loudly)', () => {
    expect(friendlyTaskError('network error')).toBe('network error')
  })
})

describe('friendlyCaughtError', () => {
  const friendly = 'This task is open in the editor — save or close it first.'

  it('reads .message off an Error and runs it through the mapper', () => {
    const err = new Error('block is being edited in another view')
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
