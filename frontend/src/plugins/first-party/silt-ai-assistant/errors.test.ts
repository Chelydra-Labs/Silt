import { describe, expect, it } from 'vitest'
import { formatAIError, isAbortError } from './errors'

describe('formatAIError', () => {
  it('maps rate-limited', () => {
    expect(formatAIError({ code: 'rate-limited', message: 'slow' })).toMatch(
      /rate limit/i
    )
  })
  it('maps unauthorized', () => {
    expect(formatAIError({ code: 'unauthorized', message: 'nope' })).toMatch(
      /API key/i
    )
  })
  it('handles AbortError', () => {
    const e = new Error('Cancelled')
    e.name = 'AbortError'
    expect(formatAIError(e)).toBe('Cancelled.')
    expect(isAbortError(e)).toBe(true)
  })
})
