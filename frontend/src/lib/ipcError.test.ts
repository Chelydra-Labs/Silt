import { describe, expect, it } from 'vitest'
import { coerceIPCError, coerceIPCErrorMessage } from './ipcError'

describe('coerceIPCError', () => {
  it('parses a JSON-string IPCError into {code, message}', () => {
    const e = new Error('{"code":"block_being_edited","message":"busy"}')
    expect(coerceIPCError(e)).toEqual({
      code: 'block_being_edited',
      message: 'busy',
      plugin: undefined,
      capability: undefined,
      requested: undefined,
      granted: undefined,
      disabled: undefined
    })
  })

  it('preserves structured capability-denial fields', () => {
    const e = new Error(
      '{"code":"capability_denied","message":"denied","plugin":"p","capability":"network","requested":"*","granted":"","disabled":false}'
    )
    expect(coerceIPCError(e)).toMatchObject({
      code: 'capability_denied',
      message: 'denied',
      plugin: 'p',
      capability: 'network',
      requested: '*',
      granted: '',
      disabled: false
    })
  })

  it('falls back to plain prose when .message is not JSON', () => {
    const e = new Error('some unmigrated sentinel prose')
    expect(coerceIPCError(e)).toEqual({
      message: 'some unmigrated sentinel prose'
    })
  })

  it('falls back when JSON has no string code (not an IPC payload)', () => {
    const e = new Error('{"not":"an ipc error"}')
    expect(coerceIPCError(e)).toEqual({ message: '{"not":"an ipc error"}' })
  })

  it('coerces non-Error values via String()', () => {
    expect(coerceIPCError(42)).toEqual({ message: '42' })
    expect(coerceIPCError(null)).toEqual({ message: 'null' })
  })
})

describe('coerceIPCErrorMessage', () => {
  it('only attempts JSON.parse when the string opens with "{" (cheap probe)', () => {
    // A prose string that happens to contain a brace mid-message must not
    // trigger a parse attempt that throws.
    expect(coerceIPCErrorMessage('not json {but has a brace}')).toEqual({
      message: 'not json {but has a brace}'
    })
  })

  it('returns the raw message for an empty string', () => {
    expect(coerceIPCErrorMessage('')).toEqual({ message: '' })
  })
})
