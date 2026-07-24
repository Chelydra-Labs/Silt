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

  it('maps Error instances with PluginAIError codes to friendly copy', () => {
    const rateLimited = Object.assign(new Error('rl'), {
      name: 'PluginAIError',
      code: 'rate-limited'
    })
    expect(formatAIError(rateLimited)).toMatch(/rate limit/i)

    const unauthorized = Object.assign(new Error('bad key'), {
      name: 'PluginAIError',
      code: 'unauthorized'
    })
    expect(formatAIError(unauthorized)).toMatch(/API key/i)
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
