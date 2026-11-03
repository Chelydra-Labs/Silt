import { describe, expect, it } from 'vitest'
import { coerceIPCError, coerceIPCErrorMessage } from './ipcError'
import { wailsIPCError, wailsIPCErrorJoin } from './testing/wailsIpcError'

describe('coerceIPCError — Wails v3 cause shape (production)', () => {
  it('recovers the code from the cause envelope, prose on .message', () => {
    const e = wailsIPCError({
      code: 'block_being_edited',
      message: 'block is being edited in another view'
    })
    expect(coerceIPCError(e)).toEqual({
      code: 'block_being_edited',
      message: 'block is being edited in another view',
      plugin: undefined,
      capability: undefined,
      requested: undefined,
      granted: undefined,
      disabled: undefined
    })
  })

  it('recovers the code from a RuntimeError-kind rejection', () => {
    // The runtime constructs typed CallError kinds as Error subclasses; the
    // coercion must not depend on which one arrived.
    const e = wailsIPCError(
      {
        code: 'verified_write_failed',
        message: 'Note.md could not be written safely'
      },
      'RuntimeError'
    )
    expect(coerceIPCError(e)).toMatchObject({
      code: 'verified_write_failed',
      message: 'Note.md could not be written safely'
    })
  })

  it('takes the first typed entry from a multi-error join cause array', () => {
    const e = wailsIPCErrorJoin([
      { code: 'page_write_stale', message: 'the page was updated elsewhere' },
      {
        code: 'block_being_edited',
        message: 'block is being edited in another view'
      }
    ])
    expect(coerceIPCError(e)).toMatchObject({
      code: 'page_write_stale',
      message: 'the page was updated elsewhere'
    })
  })

  it('preserves structured capability-denial fields from the cause', () => {
    const e = wailsIPCError({
      code: 'capability_denied',
      message: 'denied',
      plugin: 'p',
      capability: 'network',
      requested: '*',
      granted: '',
      disabled: false
    })
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

  it('falls back to the error message when the cause envelope has none', () => {
    const e = Object.assign(new Error('prose only'), { cause: { nope: 1 } })
    expect(coerceIPCError(e)).toEqual({ message: 'prose only' })
  })

  it('falls back to prose when the cause is a non-envelope string', () => {
    const e = Object.assign(new Error('prose only'), { cause: 'raw string' })
    expect(coerceIPCError(e)).toEqual({ message: 'prose only' })
  })
})

describe('coerceIPCError — Wails v2 JSON-string shape (legacy compat)', () => {
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
