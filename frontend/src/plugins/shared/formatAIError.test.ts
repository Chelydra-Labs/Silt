import { describe, expect, it } from 'vitest'
import { formatAIError, isAbortError } from './formatAIError'

describe('formatAIError', () => {
  it('formats PluginAIError codes without [object Object]', () => {
    expect(formatAIError({ code: 'unreachable', message: 'dial tcp' })).toMatch(
      /Could not reach/i
    )
    expect(formatAIError({ code: 'timeout', message: 'deadline' })).toMatch(
      /timed out/i
    )
    expect(
      formatAIError({ code: 'unknown', message: 'provider said no' })
    ).toBe('provider said no')
  })

  it('never returns [object Object] for plain objects', () => {
    const out = formatAIError({ code: 'server', message: '' })
    expect(out).not.toContain('[object Object]')
    expect(out.length).toBeGreaterThan(0)
  })

  it('handles Error and AbortError', () => {
    expect(formatAIError(new Error('boom'))).toBe('boom')
    const abort = new Error('aborted')
    abort.name = 'AbortError'
    expect(formatAIError(abort)).toBe('Cancelled.')
  })
})

describe('isAbortError', () => {
  it('detects AbortError', () => {
    const e = new Error('x')
    e.name = 'AbortError'
    expect(isAbortError(e)).toBe(true)
    expect(isAbortError(new Error('x'))).toBe(false)
  })
})
